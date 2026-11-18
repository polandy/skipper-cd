package deploy

// HookRun is the SSE snapshot of the deploy hook currently executing (ADR-0038).
// Deploys serialize, so at most one runs at a time; the zero value means none.
type HookRun struct {
	Stack string `json:"stack"`
	Phase string `json:"phase"` // "pre_deploy" | "post_deploy"
	Index int    `json:"index"` // 1-based position of the running command
	Total int    `json:"total"` // number of commands in this phase
}

// CurrentHookRun returns the last published state so a client connecting
// mid-deploy sees the running hook. Safe for concurrent use.
func (d *Deployer) CurrentHookRun() HookRun {
	if h := d.currentHookRun.Load(); h != nil {
		return *h
	}
	return HookRun{}
}

// publishHookRun records the state for late joiners and forwards it to the sink.
// The zero value clears the indicator.
func (d *Deployer) publishHookRun(h HookRun) {
	if d.hookRunSink == nil {
		return
	}
	d.currentHookRun.Store(&h)
	d.hookRunSink(h)
}
