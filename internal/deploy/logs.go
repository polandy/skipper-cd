package deploy

import "github.com/polandy/skipper-cd/internal/config"

// LogComposeInvocation returns the working directory, resolved environment, and
// the leading `docker compose` args (project selection) needed to run a compose
// subcommand — such as `logs` — against a currently-known stack. It reuses the
// deploy path's stackRun, env resolution and project selection so a logs read
// targets exactly the project a deploy would (Invariants 1 and 6). ok is false
// when the stack is not currently known; the lookup is discovery-aware
// (Invariant 8). The container-logs handler owns the streaming; this only
// supplies the invocation.
func (d *Deployer) LogComposeInvocation(cfg *config.Config, name string) (dir string, env, composeArgs []string, ok bool, err error) {
	stack, ok := d.effectiveStack(cfg, name)
	if !ok {
		return "", nil, nil, false, nil
	}
	baseEnv, err := buildBaseEnv(cfg.VarsFile)
	if err != nil {
		return "", nil, nil, false, err
	}
	run := newStackRun(stack, cfg.StacksBaseDir, baseEnv)
	env, err = run.resolveEnv()
	if err != nil {
		return "", nil, nil, false, err
	}
	dir, composeArgs = run.composeInvocation()
	return dir, env, composeArgs, true, nil
}
