package deploy

// HookRun is the SSE snapshot of the deploy hook currently executing (ADR-0038):
// a single {stack, phase, index, total}, since deploys serialize so at most one
// hook runs at a time. The zero value (empty Stack) means no hook is running —
// published to clear the UI's running-hook indicator when a phase's hooks
// finish. It drives the phase sub-label on the deploying row and the pulsing
// hooks badge; it is distinct from RunPlan (which stacks come next).
type HookRun struct {
	Stack string `json:"stack"`
	Phase string `json:"phase"` // "pre_deploy" | "post_deploy"
	Index int    `json:"index"` // 1-based position of the running command
	Total int    `json:"total"` // number of commands in this phase
}

// CurrentHookRun returns the latest published hook-run state so a client
// connecting mid-deploy sees a running hook without waiting for the next one.
// Safe for concurrent use.
func (d *Deployer) CurrentHookRun() HookRun {
	if h := d.currentHookRun.Load(); h != nil {
		return *h
	}
	return HookRun{}
}

// publishHookRun stores the state for late-connecting clients and forwards it to
// the sink (when installed). Publishing the zero value clears the indicator.
func (d *Deployer) publishHookRun(h HookRun) {
	if d.hookRunSink == nil {
		return
	}
	d.currentHookRun.Store(&h)
	d.hookRunSink(h)
}
