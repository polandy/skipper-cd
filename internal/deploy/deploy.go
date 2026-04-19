// Package deploy handles pulling updated Docker images and applying
// docker-compose stacks. Stacks are skipped when their configuration
// files have not changed since the last deployment.
package deploy

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// Deployer orchestrates deployments for all configured stacks.
// Inject a custom Runner to replace real docker/git calls in tests.
// Optionally inject a CommitReader to enable diff logging on deploy.
type Deployer struct {
	runner       Runner
	commitReader CommitReader // nil disables diff logging
}

func NewDeployer() *Deployer {
	return &Deployer{runner: ShellRunner{}}
}

func NewDeployerWithCommitReader(commitReader CommitReader) *Deployer {
	return &Deployer{runner: ShellRunner{}, commitReader: commitReader}
}

func newDeployerWithRunner(r Runner) *Deployer {
	return &Deployer{runner: r}
}

func (d *Deployer) DeployAllStacks(cfg *config.Config) {
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
		if err := d.deployStackIfChanged(stack, cfg.StacksBaseDir, varsEnv, state); err != nil {
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

func (d *Deployer) deployStackIfChanged(stack config.Stack, baseDir string, varsEnv []string, state persistedState) error {
	workDir := stack.WorkDir(baseDir)

	currentHashes, err := computePerFileHashes(workDir, stack.EnvFiles, stack.WatchDirs)
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
		if err := d.runDockerCompose(workDir, varsEnv, stack.EnvFiles, "pull", "--quiet"); err != nil {
			return fmt.Errorf("docker compose pull: %w", err)
		}
	} else {
		slog.Info("skipping pull, no image changes", "stack", stack.Name)
	}

	// --remove-orphans removes containers for services deleted from docker-compose.yml.
	if err := d.runDockerCompose(workDir, varsEnv, stack.EnvFiles, "up", "-d", "--remove-orphans"); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
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

func (d *Deployer) runDockerCompose(workDir string, varsEnv []string, envFiles []string, args ...string) error {
	env := os.Environ()
	env = append(env, varsEnv...)
	for _, envFile := range envFiles {
		envVars, err := parseEnvFile(envFile)
		if err != nil {
			return fmt.Errorf("read env file %s: %w", envFile, err)
		}
		env = append(env, envVars...)
	}

	return d.runner.Run(workDir, env, "docker", append([]string{"compose"}, args...)...)
}
