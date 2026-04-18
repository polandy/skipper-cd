// Package deploy handles pulling updated Docker images and applying
// docker-compose stacks. Stacks are skipped when their configuration
// files have not changed since the last deployment.
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

func NewDeployer() *Deployer {
	return &Deployer{runner: ShellRunner{}}
}

func newDeployerWithRunner(r Runner) *Deployer {
	return &Deployer{runner: r}
}

func (d *Deployer) DeployAllStacks(cfg *config.Config) {
	slog.Info("starting deploy run", "stacks", len(cfg.Stacks))

	lastDeployHashByStack, err := loadPersistedDeployState()
	if err != nil {
		slog.Error("could not load deploy state, deploying all stacks", "err", err)
		lastDeployHashByStack = map[string]string{}
	}

	for _, stack := range cfg.Stacks {
		if err := d.deployStackIfChanged(stack, cfg.StacksBaseDir, lastDeployHashByStack); err != nil {
			slog.Error("deploy failed", "stack", stack.Name, "err", err)
			metrics.DeployErrors.WithLabelValues(stack.Name).Inc()
		}
	}

	if err := persistDeployState(lastDeployHashByStack); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}
}

func (d *Deployer) deployStackIfChanged(stack config.Stack, baseDir string, lastDeployHashByStack map[string]string) error {
	workDir := stack.WorkDir(baseDir)

	currentHash, err := computeStackConfigHash(workDir, stack.EnvFiles)
	if err != nil {
		return fmt.Errorf("compute stack hash: %w", err)
	}

	if lastDeployHashByStack[stack.Name] == currentHash {
		slog.Info("skipping stack, no changes detected", "stack", stack.Name)
		metrics.DeploysSkipped.WithLabelValues(stack.Name).Inc()
		return nil
	}

	slog.Info("deploying stack", "stack", stack.Name, "dir", workDir)
	metrics.DeploysTriggered.WithLabelValues(stack.Name).Inc()

	if err := d.runDockerCompose(workDir, stack.EnvFiles, "pull"); err != nil {
		return fmt.Errorf("docker compose pull: %w", err)
	}

	// --remove-orphans removes containers for services deleted from docker-compose.yml.
	if err := d.runDockerCompose(workDir, stack.EnvFiles, "up", "-d", "--remove-orphans"); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}

	lastDeployHashByStack[stack.Name] = currentHash
	metrics.LastDeployTimestamp.WithLabelValues(stack.Name).Set(float64(time.Now().Unix()))
	slog.Info("deploy complete", "stack", stack.Name)
	return nil
}

func (d *Deployer) runDockerCompose(workDir string, envFiles []string, args ...string) error {
	env := os.Environ()
	for _, envFile := range envFiles {
		envVars, err := parseEnvFile(envFile)
		if err != nil {
			return fmt.Errorf("read env file %s: %w", envFile, err)
		}
		env = append(env, envVars...)
	}

	return d.runner.Run(workDir, env, "docker", append([]string{"compose"}, args...)...)
}
