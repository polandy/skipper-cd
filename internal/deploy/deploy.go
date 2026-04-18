// Package deploy handles pulling updated Docker images and applying
// docker-compose stacks. It skips stacks that have not changed since
// the last deployment using a SHA-256 hash of their configuration files.
package deploy

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/polandy/orpheus-cd/internal/config"
	"github.com/polandy/orpheus-cd/internal/metrics"
)

// Deployer orchestrates deployments for all configured stacks.
// Inject a custom Runner to replace real docker/git calls in tests.
type Deployer struct {
	runner Runner
}

// NewDeployer creates a Deployer that runs real shell commands.
func NewDeployer() *Deployer {
	return &Deployer{runner: ShellRunner{}}
}

// newDeployerWithRunner creates a Deployer with a custom Runner. Used in tests.
func newDeployerWithRunner(r Runner) *Deployer {
	return &Deployer{runner: r}
}

// RunAll deploys all stacks defined in the configuration.
// Stacks whose configuration files have not changed since the last run are skipped.
// Errors from individual stacks are logged but do not abort the remaining stacks.
func (d *Deployer) RunAll(cfg *config.Config) {
	slog.Info("starting deploy run", "stacks", len(cfg.Stacks))

	// deployState maps stack name -> hash of its last successful deployment.
	// It is persisted to disk so skipping works across restarts.
	deployState, err := loadState()
	if err != nil {
		slog.Error("could not load deploy state, deploying all stacks", "err", err)
		deployState = map[string]string{}
	}

	for _, stack := range cfg.Stacks {
		if err := d.deployStack(stack, cfg.StacksBaseDir, deployState); err != nil {
			slog.Error("deploy failed", "stack", stack.Name, "err", err)
			metrics.DeployErrors.WithLabelValues(stack.Name).Inc()
		}
	}

	if err := saveState(deployState); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}
}

// deployStack pulls and applies a single stack if its configuration has changed.
// The deployState map is updated in-place on success.
func (d *Deployer) deployStack(stack config.Stack, baseDir string, deployState map[string]string) error {
	workDir := stack.WorkDir(baseDir)

	// Hash the docker-compose.yml and all env files to detect changes.
	currentHash, err := hashStack(workDir, stack.EnvFiles)
	if err != nil {
		return fmt.Errorf("compute stack hash: %w", err)
	}

	// Skip deployment if nothing has changed since last run.
	if deployState[stack.Name] == currentHash {
		slog.Info("skipping stack, no changes detected", "stack", stack.Name)
		metrics.DeploysSkipped.WithLabelValues(stack.Name).Inc()
		return nil
	}

	slog.Info("deploying stack", "stack", stack.Name, "dir", workDir)
	metrics.DeploysTriggered.WithLabelValues(stack.Name).Inc()

	// Pull new images before recreating containers.
	if err := d.runCompose(workDir, stack.EnvFiles, "pull"); err != nil {
		return fmt.Errorf("docker compose pull: %w", err)
	}

	// Recreate containers with new images. --remove-orphans cleans up
	// containers for services that were removed from docker-compose.yml.
	if err := d.runCompose(workDir, stack.EnvFiles, "up", "-d", "--remove-orphans"); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}

	// Persist the new hash so this stack is skipped until something changes.
	deployState[stack.Name] = currentHash
	metrics.LastDeployTimestamp.WithLabelValues(stack.Name).Set(float64(time.Now().Unix()))
	slog.Info("deploy complete", "stack", stack.Name)
	return nil
}

// runCompose executes a "docker compose <args>" command in the given directory.
// Values from the env files are added to the process environment so that
// ${VAR} placeholders in docker-compose.yml are substituted at parse time.
func (d *Deployer) runCompose(workDir string, envFiles []string, args ...string) error {
	// Prepend "compose" so the full command becomes: docker compose <args>
	fullArgs := append([]string{"compose"}, args...)

	// Start with the current process environment so PATH etc. are available,
	// then append variables from the stack's env files.
	env := os.Environ()
	for _, envFile := range envFiles {
		entries, err := parseEnvFile(envFile)
		if err != nil {
			return fmt.Errorf("read env file %s: %w", envFile, err)
		}
		env = append(env, entries...)
	}

	return d.runner.Run(workDir, env, "docker", fullArgs...)
}
