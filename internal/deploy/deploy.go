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
	"sync"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// CommitReader retrieves git commit information from the repository.
// It is used to log file diffs between the last deployed commit and HEAD.
type CommitReader interface {
	HeadCommitSHA() (string, error)
	DiffSinceCommit(fromSHA, filePath string) (string, error)
}

// GitSyncer abstracts the git sync operation so the deployer can
// coordinate sync + deploy under a single lock.
type GitSyncer interface {
	Sync(ctx context.Context) error
}

// Deployer orchestrates deployments for all configured stacks.
// Inject a custom Runner to replace real docker/git calls in tests.
// Optionally inject a CommitReader to enable diff logging on deploy.
type Deployer struct {
	runner       Runner
	commitReader CommitReader // nil disables diff logging
	syncer       GitSyncer   // nil when using DeployAllStacks directly
	timeout      time.Duration
	mu           sync.Mutex
}

func NewDeployer() *Deployer {
	return &Deployer{runner: ShellRunner{}}
}

func NewDeployerWithCommitReader(commitReader CommitReader, syncer GitSyncer, timeout time.Duration) *Deployer {
	return &Deployer{runner: ShellRunner{}, commitReader: commitReader, syncer: syncer, timeout: timeout}
}

func newDeployerWithRunner(r Runner) *Deployer {
	return &Deployer{runner: r}
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

	varsEnv, err := loadVarsFile(cfg.VarsFile)
	if err != nil {
		slog.Error("could not load vars_file, aborting", "err", err)
		return
	}

	state, err := loadPersistedDeployState()
	if err != nil {
		slog.Error("could not load deploy state, deploying all stacks", "err", err)
		state = newEmptyState()
	}

	for _, stack := range cfg.Stacks {
		if err := d.deployStackIfChanged(ctx, stack, cfg.StacksBaseDir, cfg.VarsFile, varsEnv, state); err != nil {
			slog.Error("deploy failed", "stack", stack.Name, "err", err)
			metrics.DeployErrors.WithLabelValues(stack.Name).Inc()
		}
	}

	// Record the current HEAD commit so future deploys can diff against it.
	if d.commitReader != nil {
		if sha, err := d.commitReader.HeadCommitSHA(); err != nil {
			slog.Warn("could not read HEAD commit SHA", "err", err)
		} else {
			state.LastDeployedCommit = sha
		}
	}

	if err := saveDeployState(state); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}
}

func (d *Deployer) deployStackIfChanged(ctx context.Context, stack config.Stack, baseDir, varsFile string, varsEnv []string, state persistedState) error {
	workDir := stack.WorkDir(baseDir)

	currentHashes, err := computePerFileHashes(workDir, stack.EnvFiles, stack.WatchDirs, varsFile)
	if err != nil {
		return fmt.Errorf("compute per-file hashes: %w", err)
	}

	changed := changedFiles(currentHashes, state.Stacks[stack.Name])
	if len(changed) == 0 {
		slog.Info("skipping stack, no changes detected", "stack", stack.Name)
		metrics.DeploysSkipped.WithLabelValues(stack.Name).Inc()
		return nil
	}

	slog.Info("deploying stack", "stack", stack.Name, "dir", workDir, "changed_files", changed)
	d.logDiffsForChangedFiles(changed, state.LastDeployedCommit)
	metrics.DeploysTriggered.WithLabelValues(stack.Name).Inc()

	composePath := filepath.Join(workDir, "docker-compose.yml")
	currentImages, err := extractComposeImages(composePath)
	if err != nil {
		slog.Warn("could not extract images, pulling to be safe", "stack", stack.Name, "err", err)
		currentImages = nil
	}

	if currentImages == nil || hasAnyImageChanged(currentImages, state.Images[stack.Name]) {
		if err := d.runDockerCompose(ctx, workDir, varsEnv, stack.EnvFiles, "pull", "--quiet"); err != nil {
			return fmt.Errorf("docker compose pull: %w", err)
		}
	} else {
		slog.Info("skipping pull, no image changes", "stack", stack.Name)
	}

	// --remove-orphans removes containers for services deleted from docker-compose.yml.
	if err := d.runDockerCompose(ctx, workDir, varsEnv, stack.EnvFiles, "up", "-d", "--remove-orphans"); err != nil {
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
	slog.Info("deploy complete", "stack", stack.Name)
	return nil
}

// logDiffsForChangedFiles logs the git diff for each changed file.
// Skipped when no CommitReader is configured or no previous commit is known.
func (d *Deployer) logDiffsForChangedFiles(changedFilePaths []string, lastDeployedCommit string) {
	if d.commitReader == nil || lastDeployedCommit == "" {
		return
	}
	for _, filePath := range changedFilePaths {
		diff, err := d.commitReader.DiffSinceCommit(lastDeployedCommit, filePath)
		if err != nil {
			slog.Warn("could not compute diff", "file", filePath, "err", err)
			continue
		}
		if diff != "" {
			slog.Info("file changed", "file", filePath)
			fmt.Print(diff)
		}
	}
}

// changedFiles returns the paths of files whose hash differs between current and last.
func changedFiles(current, last stackFileHashes) []string {
	var changed []string
	for path, hash := range current {
		if last[path] != hash {
			changed = append(changed, path)
		}
	}
	return changed
}

func (d *Deployer) runDockerCompose(ctx context.Context, workDir string, varsEnv []string, envFiles []string, args ...string) error {
	env := os.Environ()
	env = append(env, varsEnv...)
	for _, envFile := range envFiles {
		envVars, err := parseEnvFile(envFile)
		if err != nil {
			return fmt.Errorf("read env file %s: %w", envFile, err)
		}
		env = append(env, envVars...)
	}

	return d.runner.Run(ctx, workDir, env, "docker", append([]string{"compose"}, args...)...)
}
