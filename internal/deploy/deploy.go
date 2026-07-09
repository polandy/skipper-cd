// Package deploy handles pulling updated Docker images and applying
// docker-compose stacks. Stacks are skipped when their configuration
// files have not changed since the last deployment.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/nixos"
)

// CommitReader retrieves git commit information from the repository.
// It is used to log file diffs between the last deployed commit and HEAD,
// and to retrieve old compose files for rollback.
type CommitReader interface {
	HeadCommitSHA(ctx context.Context) (string, error)
	DiffSinceCommit(ctx context.Context, fromSHA, filePath string) (string, error)
	FileAtCommit(ctx context.Context, commitSHA, filePath string) ([]byte, error)
}

// RepoSyncer abstracts the git sync operation so the deployer can
// coordinate sync + deploy under a single lock.
type RepoSyncer interface {
	Sync(ctx context.Context) error
}

// ErrRolledBack marks a failed deploy whose stack was successfully rolled
// back to the previous compose file version. DeployAllStacks checks it with
// errors.Is to emit a rolled_back (instead of failed) event.
var ErrRolledBack = errors.New("rolled back to previous version")

// Deployer orchestrates deployments for all configured stacks.
// Inject a custom Runner to replace real docker/git calls in tests.
// Optionally inject a CommitReader to enable diff logging on deploy.
type Deployer struct {
	runner       Runner
	commitReader CommitReader // nil disables diff logging
	syncer       RepoSyncer   // nil when using DeployAllStacks directly
	repoDir      string       // used to skip diff for files outside the repo
	stateDir     string       // directory for state.yaml persistence
	mu           sync.Mutex
	eventSink    func(events.DeployEvent) // nil = no event tracking
	nextEventID  atomic.Int64
}

const (
	defaultStateDir = "/var/lib/skipper"
	nixosStateKey   = "_nixos"
)

func NewDeployer() *Deployer {
	return &Deployer{runner: ShellRunner{}, stateDir: defaultStateDir}
}

func NewDeployerWithCommitReader(commitReader CommitReader, syncer RepoSyncer, repoDir, stateDir string, commandTimeout time.Duration) *Deployer {
	if stateDir == "" {
		stateDir = defaultStateDir
	}
	return &Deployer{runner: command.NewShellRunner(commandTimeout), commitReader: commitReader, syncer: syncer, repoDir: repoDir, stateDir: stateDir}
}

func newDeployerWithRunner(r Runner) *Deployer {
	return &Deployer{runner: r, stateDir: defaultStateDir}
}

// SetEventSink configures an optional callback invoked on every deploy
// status change. Must be called before any deployments start.
func (d *Deployer) SetEventSink(fn func(events.DeployEvent)) {
	d.eventSink = fn
}

// InitEventID sets the starting event ID counter (e.g. from persisted history).
func (d *Deployer) InitEventID(startID int64) {
	d.nextEventID.Store(startID)
}

func (d *Deployer) emit(status events.Status, stack string, duration time.Duration, errMsg string, changedFiles []string, diffs map[string]string) {
	if d.eventSink == nil {
		return
	}
	d.eventSink(events.DeployEvent{
		ID:           d.nextEventID.Add(1),
		Timestamp:    time.Now(),
		Stack:        stack,
		Status:       status,
		DurationMs:   duration.Milliseconds(),
		Error:        errMsg,
		ChangedFiles: changedFiles,
		Diffs:        diffs,
	})
}

// SyncAndDeployAll acquires the deploy lock, syncs the repository, and
// deploys all stacks. Concurrent callers wait for their turn.
func (d *Deployer) SyncAndDeployAll(ctx context.Context, cfg *config.Config) {
	if !d.mu.TryLock() {
		slog.Info("deploy already in progress, waiting")
		d.mu.Lock()
	}
	defer d.mu.Unlock()

	if d.syncer != nil {
		if err := d.syncer.Sync(ctx); err != nil {
			slog.Error("git sync failed, aborting deploy", "err", err)
			return
		}
	}
	d.DeployAllStacks(ctx, cfg)
}

// WaitIdle blocks until no deploy run is in progress. It is used during
// shutdown to let an in-flight deploy finish instead of interrupting
// docker compose mid-run.
func (d *Deployer) WaitIdle() {
	d.mu.Lock()
	defer d.mu.Unlock()
}

func (d *Deployer) DeployAllStacks(ctx context.Context, cfg *config.Config) {
	slog.Info("starting deploy run", "stacks", len(cfg.Stacks))

	baseEnv := os.Environ()
	if cfg.VarsFile != "" {
		varsEnv, err := parseEnvFile(cfg.VarsFile)
		if err != nil {
			slog.Error("could not load vars_file, aborting", "err", err)
			return
		}
		baseEnv = append(baseEnv, varsEnv...)
	}

	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		slog.Error("could not load deploy state, deploying all stacks", "err", err)
		state = newEmptyState()
	}

	if cfg.NixOSRebuild.IsEnabled() && !d.rebuildNixOSIfChanged(ctx, cfg, state) {
		return
	}

	for _, stack := range cfg.Stacks {
		startTime := time.Now()
		if err := d.deployStackIfChanged(ctx, stack, cfg.StacksBaseDir, cfg.VarsFile, baseEnv, state); err != nil {
			duration := time.Since(startTime)
			metrics.DeployErrors.WithLabelValues(stack.Name).Inc()
			if errors.Is(err, ErrRolledBack) {
				slog.Warn("deploy failed but rolled back", "stack", stack.Name, "err", err)
				d.emit(events.StatusRolledBack, stack.Name, duration, err.Error(), nil, nil)
			} else {
				slog.Error("deploy failed", "stack", stack.Name, "err", err)
				d.emit(events.StatusFailed, stack.Name, duration, err.Error(), nil, nil)
			}
		}
	}

	// Record the current HEAD commit so future deploys can diff against it.
	if d.commitReader != nil {
		if sha, err := d.commitReader.HeadCommitSHA(ctx); err != nil {
			slog.Warn("could not read HEAD commit SHA", "err", err)
		} else {
			state.LastDeployedCommit = sha
		}
	}

	if err := saveDeployState(d.stateDir, state); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}
}

// rebuildNixOSIfChanged hashes the repo's nix files and runs nixos-rebuild
// when any of them changed. The new hashes are persisted to state *before*
// the rebuild, because the rebuild may restart the skipper-cd service
// (killing this process); pre-saving avoids a redundant rebuild on restart.
// Returns false when the rebuild failed and all stack deploys must abort.
func (d *Deployer) rebuildNixOSIfChanged(ctx context.Context, cfg *config.Config, state *persistedState) bool {
	startTime := time.Now()

	currentNixHashes, _ := nixos.HashFiles(d.repoDir)
	changed := nixos.DiffHashes(currentNixHashes, state.hashesFor(nixosStateKey))

	if len(changed) == 0 {
		metrics.DeploysSkipped.WithLabelValues(nixosStateKey).Inc()
		d.emit(events.StatusSkipped, nixosStateKey, 0, "", nil, nil)
		return true
	}

	state.recordStack(nixosStateKey, currentNixHashes)
	_ = saveDeployState(d.stateDir, state)

	if err := nixos.New(d.runner).Rebuild(ctx, d.repoDir, cfg.NixOSRebuild.Flake); err != nil {
		slog.Error("nixos-rebuild failed, aborting all stack deploys", "err", err)
		metrics.DeployErrors.WithLabelValues(nixosStateKey).Inc()
		d.emit(events.StatusFailed, nixosStateKey, time.Since(startTime), err.Error(), changed, nil)
		return false
	}

	metrics.DeploysTriggered.WithLabelValues(nixosStateKey).Inc()
	metrics.LastDeployTimestamp.WithLabelValues(nixosStateKey).Set(float64(time.Now().Unix()))
	d.emit(events.StatusSuccess, nixosStateKey, time.Since(startTime), "", changed, nil)
	slog.Info("nixos-rebuild complete", "changed_files", changed)
	return true
}

func (d *Deployer) deployStackIfChanged(ctx context.Context, stack config.Stack, baseDir, varsFile string, baseEnv []string, state *persistedState) error {
	// Change detection always uses the repo clone so that merged PRs are detected.
	repoDir := filepath.Join(baseDir, stack.Name)
	composePath := filepath.Join(repoDir, "docker-compose.yml")

	// When working_dir is set, use it as --project-directory for Docker Compose
	// project identity (container labels, .env loading) while the compose file
	// is always read from the repo clone via -f.
	projectDir := stack.WorkingDir

	// Parse the compose file once; images, pullable services and Dockerfile
	// paths are all derived from this single parse. When parsing fails, the
	// deploy degrades gracefully: no build tracking and pull everything.
	compose, err := parseComposeFile(composePath)
	if err != nil {
		slog.Warn("could not parse compose file, pulling all services and skipping build tracking", "stack", stack.Name, "err", err)
	}

	var dockerfilePaths []string
	if compose != nil {
		dockerfilePaths = compose.dockerfilePaths(repoDir)
	}

	currentHashes, err := computePerFileHashes(repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		return fmt.Errorf("compute per-file hashes: %w", err)
	}

	changed := changedFiles(currentHashes, state.hashesFor(stack.Name))
	if len(changed) == 0 {
		slog.Debug("skipping stack, no changes detected", "stack", stack.Name)
		metrics.DeploysSkipped.WithLabelValues(stack.Name).Inc()
		d.emit(events.StatusSkipped, stack.Name, 0, "", nil, nil)
		return nil
	}

	deployStart := time.Now()
	d.emit(events.StatusDeploying, stack.Name, 0, "", changed, nil)
	slog.Info("deploying stack", "stack", stack.Name, "dir", repoDir, "project_dir", projectDir, "changed_files", changed)
	diffs := d.collectDiffs(ctx, changed, state.LastDeployedCommit)
	metrics.DeploysTriggered.WithLabelValues(stack.Name).Inc()

	var currentImages serviceImageByName
	if compose != nil {
		currentImages = compose.images()
	}

	if currentImages == nil || hasAnyImageChanged(currentImages, state.imagesFor(stack.Name)) {
		if compose == nil {
			// Fallback: pull everything (couldn't parse compose file).
			if err := d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, "pull", "--quiet"); err != nil {
				return fmt.Errorf("docker compose pull: %w", err)
			}
		} else if pullable := compose.pullableServices(); len(pullable) > 0 {
			pullArgs := append([]string{"pull", "--quiet"}, pullable...)
			if err := d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, pullArgs...); err != nil {
				return fmt.Errorf("docker compose pull: %w", err)
			}
		} else {
			slog.Info("skipping pull, all services use locally-built images", "stack", stack.Name)
		}
	} else {
		slog.Info("skipping pull, no image changes", "stack", stack.Name)
	}

	if len(dockerfilePaths) > 0 {
		slog.Info("building images from Dockerfile", "stack", stack.Name, "dockerfiles", dockerfilePaths)
		if err := d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, "build", "--pull"); err != nil {
			return fmt.Errorf("docker compose build: %w", err)
		}
	}

	// --remove-orphans removes containers for services deleted from docker-compose.yml.
	if err := d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, "up", "-d", "--remove-orphans"); err != nil {
		slog.Error("docker compose up failed, attempting rollback", "stack", stack.Name, "err", err)

		if rbErr := d.rollbackStack(ctx, composePath, projectDir, baseEnv, stack, state); rbErr != nil {
			slog.Error("rollback failed", "stack", stack.Name, "err", rbErr)
			return fmt.Errorf("docker compose up: %w (rollback also failed: %v)", err, rbErr)
		}
		slog.Info("rollback successful, old containers restored", "stack", stack.Name)
		metrics.DeployRollbacks.WithLabelValues(stack.Name).Inc()
		return fmt.Errorf("docker compose up: %w (%w)", err, ErrRolledBack)
	}

	if len(stack.OnDemandContainers) > 0 {
		slog.Info("stopping on-demand containers after deploy", "stack", stack.Name, "containers", stack.OnDemandContainers)
		if err := d.runner.Run(ctx, "", nil, "docker", append([]string{"stop"}, stack.OnDemandContainers...)...); err != nil {
			slog.Warn("could not stop on-demand containers", "stack", stack.Name, "err", err)
		}
	}

	state.recordStack(stack.Name, currentHashes)
	if currentImages != nil {
		state.recordImages(stack.Name, currentImages)
	}
	metrics.LastDeployTimestamp.WithLabelValues(stack.Name).Set(float64(time.Now().Unix()))
	d.emit(events.StatusSuccess, stack.Name, time.Since(deployStart), "", changed, diffs)
	slog.Info("deploy complete", "stack", stack.Name)
	return nil
}

const (
	maxDiffPerFile = 10 * 1024 // 10 KB per file
	maxDiffTotal   = 50 * 1024 // 50 KB total per event
)

// collectDiffs collects git diffs for each changed file and returns them
// as a map of file path to diff content. Large diffs are truncated.
// Returns nil when no CommitReader is configured or no previous commit is known.
func (d *Deployer) collectDiffs(ctx context.Context, changedFilePaths []string, lastDeployedCommit string) map[string]string {
	if d.commitReader == nil || lastDeployedCommit == "" {
		return nil
	}
	result := make(map[string]string)
	totalSize := 0
	for _, filePath := range changedFilePaths {
		if d.repoDir != "" && !strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(d.repoDir)) {
			continue
		}
		diff, err := d.commitReader.DiffSinceCommit(ctx, lastDeployedCommit, filePath)
		if err != nil {
			slog.Warn("could not compute diff", "file", filePath, "err", err)
			continue
		}
		if diff == "" {
			continue
		}
		slog.Info("file changed", "file", filePath)

		if len(diff) > maxDiffPerFile {
			diff = diff[:maxDiffPerFile] + "\n... (truncated)"
		}
		if totalSize+len(diff) > maxDiffTotal {
			remaining := maxDiffTotal - totalSize
			if remaining > 0 {
				diff = diff[:remaining] + "\n... (truncated)"
			} else {
				break
			}
		}
		result[filePath] = diff
		totalSize += len(diff)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// runDockerCompose executes a docker compose command.
//
// composePath is the absolute path to the docker-compose.yml file (always from the repo clone).
// projectDir, when non-empty, is passed as --project-directory so Docker Compose uses it for
// project identity (container labels) and .env loading. When empty, docker compose runs from the
// directory containing composePath.
func (d *Deployer) runDockerCompose(ctx context.Context, composePath, projectDir string, baseEnv []string, envFiles []string, args ...string) error {
	env := make([]string, len(baseEnv))
	copy(env, baseEnv)
	for _, envFile := range envFiles {
		envVars, err := parseEnvFile(envFile)
		if err != nil {
			return fmt.Errorf("read env file %s: %w", envFile, err)
		}
		env = append(env, envVars...)
	}

	composeArgs := []string{"compose"}
	runDir := filepath.Dir(composePath)
	if projectDir != "" {
		composeArgs = append(composeArgs, "-f", composePath, "--project-directory", projectDir)
		runDir = projectDir
	}
	composeArgs = append(composeArgs, args...)

	return d.runner.Run(ctx, runDir, env, "docker", composeArgs...)
}

// rollbackStack restores containers to the previous compose file version after
// a failed docker compose up. It retrieves the old compose file from git and
// runs docker compose up with it.
func (d *Deployer) rollbackStack(ctx context.Context, composePath, projectDir string, baseEnv []string, stack config.Stack, state *persistedState) error {
	if d.commitReader == nil || state.LastDeployedCommit == "" {
		return fmt.Errorf("no previous commit available for rollback")
	}

	oldContent, err := d.commitReader.FileAtCommit(ctx, state.LastDeployedCommit, composePath)
	if err != nil {
		return fmt.Errorf("retrieve old compose file: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "skipper-rollback-*.yml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(oldContent); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	// When projectDir is empty, runDockerCompose uses filepath.Dir(composePath)
	// as the working directory. Since composePath here is a temp file in /tmp/,
	// we must explicitly set projectDir to the original compose file's directory.
	rbProjectDir := projectDir
	if rbProjectDir == "" {
		rbProjectDir = filepath.Dir(composePath)
	}

	slog.Info("rolling back with previous compose file", "stack", stack.Name, "commit", state.LastDeployedCommit)
	return d.runDockerCompose(ctx, tmpFile.Name(), rbProjectDir, baseEnv, stack.EnvFiles, "up", "-d")
}
