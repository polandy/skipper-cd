package deploy

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/polandy/orpheus-cd/internal/config"
	"github.com/polandy/orpheus-cd/internal/metrics"
)

func RunAll(cfg *config.Config) {
	slog.Info("starting deploy run", "stacks", len(cfg.Stacks))

	state, err := loadState()
	if err != nil {
		slog.Error("failed to load state", "err", err)
		state = map[string]string{}
	}

	for _, stack := range cfg.Stacks {
		if err := deployStack(stack, state); err != nil {
			slog.Error("deploy failed", "stack", stack.Name, "err", err)
			metrics.DeployErrors.WithLabelValues(stack.Name).Inc()
		}
	}

	if err := saveState(state); err != nil {
		slog.Error("failed to save state", "err", err)
	}
}

func deployStack(stack config.Stack, state map[string]string) error {
	hash, err := hashStack(stack.WorkingDir, stack.EnvFiles)
	if err != nil {
		return fmt.Errorf("hash stack: %w", err)
	}

	if state[stack.Name] == hash {
		slog.Info("skipping stack (no changes)", "stack", stack.Name)
		metrics.DeploysSkipped.WithLabelValues(stack.Name).Inc()
		return nil
	}

	slog.Info("deploying stack", "stack", stack.Name)
	metrics.DeploysTriggered.WithLabelValues(stack.Name).Inc()

	if err := run(stack, "docker", "compose", "pull"); err != nil {
		return fmt.Errorf("compose pull: %w", err)
	}

	if err := run(stack, "docker", "compose", "up", "-d", "--remove-orphans"); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}

	state[stack.Name] = hash
	metrics.LastDeployTimestamp.WithLabelValues(stack.Name).Set(float64(time.Now().Unix()))
	slog.Info("deploy complete", "stack", stack.Name)
	return nil
}

func run(stack config.Stack, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = stack.WorkingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	env := os.Environ()
	for _, ef := range stack.EnvFiles {
		entries, err := parseEnvFile(ef)
		if err != nil {
			return fmt.Errorf("parse env file %s: %w", ef, err)
		}
		env = append(env, entries...)
	}
	cmd.Env = env

	return cmd.Run()
}
