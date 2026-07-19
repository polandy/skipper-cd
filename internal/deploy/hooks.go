package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Hook phase names: injected as SKIPPER_HOOK and used in error/log messages.
const (
	hookPhasePre  = "pre_deploy"
	hookPhasePost = "post_deploy"
)

// stackEnv builds the environment for a stack command: the base env
// (os.Environ + vars_file) plus the stack's env_files (Invariant 6 precedence),
// with any extra KEY=VALUE entries appended last so they win. It is shared by
// docker compose invocations and deploy hooks.
func (d *Deployer) stackEnv(run stackRun, extra ...string) ([]string, error) {
	env := make([]string, len(run.baseEnv))
	copy(env, run.baseEnv)
	for _, envFile := range run.stack.EnvFiles {
		envVars, err := parseEnvFile(envFile)
		if err != nil {
			return nil, fmt.Errorf("read env file %s: %w", envFile, err)
		}
		env = append(env, envVars...)
	}
	return append(env, extra...), nil
}

// runHooks runs the given hook commands sequentially via `sh -c`, stopping at
// the first failure (ADR-0037). Each command runs in the stack's project
// directory with the stack's deploy environment plus SKIPPER_STACK and
// SKIPPER_HOOK. A per-hook timeout_seconds > 0 bounds each command via the
// context; the runner's global command_timeout_seconds remains the hard ceiling
// either way, so 0 means "bounded only by that global".
func (d *Deployer) runHooks(ctx context.Context, run stackRun, phase string, cmds []string) error {
	if len(cmds) == 0 {
		return nil
	}
	env, err := d.stackEnv(run, "SKIPPER_STACK="+run.stack.Name, "SKIPPER_HOOK="+phase)
	if err != nil {
		return err
	}
	timeout := time.Duration(run.stack.Hooks.TimeoutSeconds) * time.Second
	for i, cmd := range cmds {
		slog.Info("running deploy hook", "stack", run.stack.Name, "phase", phase, "index", i)
		if err := d.runHook(ctx, run.effectiveProjectDir(), env, timeout, cmd); err != nil {
			return fmt.Errorf("%s hook %d (%q): %w", phase, i+1, cmd, err)
		}
	}
	return nil
}

// runHook runs a single hook command, applying the per-hook timeout to the
// context when one is set. Split out so the timeout's context cancellation is
// released per command rather than accumulating across the loop.
func (d *Deployer) runHook(ctx context.Context, dir string, env []string, timeout time.Duration, cmd string) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return d.runner.Run(ctx, dir, env, "sh", "-c", cmd)
}
