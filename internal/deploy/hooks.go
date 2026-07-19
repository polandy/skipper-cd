package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/polandy/skipper-cd/internal/command"
)

// Hook phase names: injected as SKIPPER_HOOK and used in error/log messages.
const (
	hookPhasePre  = "pre_deploy"
	hookPhasePost = "post_deploy"
)

// runHooks runs the phase's commands sequentially via `sh -c`, stopping at the
// first failure (ADR-0038). Each runs in the stack's project directory with its
// deploy environment (Invariant 6) plus SKIPPER_STACK and SKIPPER_HOOK. A
// per-hook timeout_seconds bounds each command, under the global ceiling.
func (d *Deployer) runHooks(ctx context.Context, run stackRun, phase string, cmds []string) error {
	if len(cmds) == 0 {
		return nil
	}
	env, err := run.resolveEnv()
	if err != nil {
		return err
	}
	env = append(env, "SKIPPER_STACK="+run.stack.Name, "SKIPPER_HOOK="+phase)
	ctx = command.WithStack(ctx, run.stack.Name) // attribute hook output to the stack in the log
	timeout := time.Duration(run.stack.Hooks.TimeoutSeconds) * time.Second
	defer d.publishHookRun(HookRun{}) // clear the indicator when the phase finishes, pass or fail
	for i, cmd := range cmds {
		d.publishHookRun(HookRun{Stack: run.stack.Name, Phase: phase, Index: i + 1, Total: len(cmds)})
		slog.Info("running deploy hook", "stack", run.stack.Name, "phase", phase, "index", i)
		if err := d.runHook(ctx, run.effectiveProjectDir(), env, timeout, cmd); err != nil {
			return fmt.Errorf("%s hook %d (%q): %w", phase, i+1, cmd, err)
		}
	}
	return nil
}

// runHook runs one hook command with the per-hook timeout. Split out so each
// command's context cancellation is released per iteration, not accumulated.
func (d *Deployer) runHook(ctx context.Context, dir string, env []string, timeout time.Duration, cmd string) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return d.runner.Run(ctx, dir, env, "sh", "-c", cmd)
}
