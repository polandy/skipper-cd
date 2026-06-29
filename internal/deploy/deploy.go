// Package deploy handles pulling updated Docker images and applying
// docker-compose stacks. Stacks are skipped when their configuration
// files have not changed since the last deployment.
package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// CommitReader retrieves git commit information from the repository.
// It is used to log file diffs between the last deployed commit and HEAD.
type CommitReader interface {
	HeadCommitSHA(ctx context.Context) (string, error)
	DiffSinceCommit(ctx context.Context, fromSHA, filePath string) (string, error)
}

// RepoSyncer abstracts the git sync operation so the deployer can
// coordinate sync + deploy under a single lock.
type RepoSyncer interface {
	Sync(ctx context.Context) error
}

// Deployer orchestrates deployments for all configured stacks.
// Inject a custom Runner to replace real docker/git calls in tests.
// Optionally inject a CommitReader to enable diff logging on deploy.
type Deployer struct {
	runner       Runner
	commitReader CommitReader // nil disables diff logging
	syncer       RepoSyncer  // nil when using DeployAllStacks directly
	repoDir      string      // used to skip diff for files outside the repo
	stateDir     string      // directory for state.yaml persistence
	timeout      time.Duration
	mu           sync.Mutex
	eventSink    func(events.DeployEvent) // nil = no event tracking
	nextEventID  atomic.Int64
}

const defaultStateDir = "/var/lib/skipper"

func NewDeployer() *Deployer {
	return &Deployer{runner: ShellRunner{}, stateDir: defaultStateDir}
}

func NewDeployerWithCommitReader(commitReader CommitReader, syncer RepoSyncer, repoDir, stateDir string, timeout time.Duration) *Deployer {
	if stateDir == "" {
		stateDir = defaultStateDir
	}
	return &Deployer{runner: ShellRunner{}, commitReader: commitReader, syncer: syncer, repoDir: repoDir, stateDir: stateDir, timeout: timeout}
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

	for _, stack := range cfg.Stacks {
		startTime := time.Now()
		if err := d.deployStackIfChanged(ctx, stack, cfg.StacksBaseDir, cfg.VarsFile, baseEnv, state); err != nil {
			slog.Error("deploy failed", "stack", stack.Name, "err", err)
			metrics.DeployErrors.WithLabelValues(stack.Name).Inc()
			d.emit(events.StatusFailed, stack.Name, time.Since(startTime), err.Error(), nil, nil)
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

func (d *Deployer) deployStackIfChanged(ctx context.Context, stack config.Stack, baseDir, varsFile string, baseEnv []string, state persistedState) error {
	// Change detection always uses the repo clone so that merged PRs are detected.
	repoDir := filepath.Join(baseDir, stack.Name)
	composePath := filepath.Join(repoDir, "docker-compose.yml")

	// When working_dir is set, use it as --project-directory for Docker Compose
	// project identity (container labels, .env loading) while the compose file
	// is always read from the repo clone via -f.
	projectDir := stack.WorkingDir

	dockerfilePaths, err := extractDockerfilePaths(composePath, repoDir)
	if err != nil {
		slog.Warn("could not extract dockerfile paths, continuing without build tracking", "stack", stack.Name, "err", err)
		dockerfilePaths = nil
	}

	currentHashes, err := computePerFileHashes(repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		return fmt.Errorf("compute per-file hashes: %w", err)
	}

	changed := changedFiles(currentHashes, state.Stacks[stack.Name])
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

	currentImages, err := extractComposeImages(composePath)
	if err != nil {
		slog.Warn("could not extract images, pulling to be safe", "stack", stack.Name, "err", err)
		currentImages = nil
	}

	if currentImages == nil || hasAnyImageChanged(currentImages, state.Images[stack.Name]) {
		if err := d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, "pull", "--quiet"); err != nil {
			return fmt.Errorf("docker compose pull: %w", err)
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
		return fmt.Errorf("docker compose up: %w", err)
	}

	if len(stack.OnDemandContainers) > 0 {
		slog.Info("stopping on-demand containers after deploy", "stack", stack.Name, "containers", stack.OnDemandContainers)
		if err := d.runner.Run(ctx, "", nil, "docker", append([]string{"stop"}, stack.OnDemandContainers...)...); err != nil {
			slog.Warn("could not stop on-demand containers", "stack", stack.Name, "err", err)
		}
	}

	state.Stacks[stack.Name] = currentHashes
	if currentImages != nil {
		state.Images[stack.Name] = currentImages
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
// as a map of file path to diff content. Diffs are also logged to stdout
// (preserving existing behavior). Large diffs are truncated.
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
		fmt.Print(diff)

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
