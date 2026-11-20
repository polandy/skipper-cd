package deploy

// RolloutRun is the SSE snapshot of the service a zero-downtime rollout is
// cutting over (ADR-0040), the zero value when none. Deploys serialize, so at
// most one runs at a time.
type RolloutRun struct {
	Stack   string `json:"stack"`
	Service string `json:"service"`
	Index   int    `json:"index"` // 1-based position among the stack's rolled services
	Total   int    `json:"total"` // number of rolled services
}

// CurrentRolloutRun returns the last published state so a client connecting
// mid-rollout sees it. Safe for concurrent use.
func (d *Deployer) CurrentRolloutRun() RolloutRun {
	if r := d.currentRolloutRun.Load(); r != nil {
		return *r
	}
	return RolloutRun{}
}

// publishRolloutRun records the state for late joiners and forwards it to the
// sink; a no-op without one. The zero value clears the indicator.
func (d *Deployer) publishRolloutRun(r RolloutRun) {
	if d.rolloutRunSink == nil {
		return
	}
	d.currentRolloutRun.Store(&r)
	d.rolloutRunSink(r)
}
