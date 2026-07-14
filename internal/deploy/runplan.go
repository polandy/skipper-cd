package deploy

import (
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/config"
)

// RunPlan is the SSE snapshot of the deploys still ahead in the current run.
// Upcoming lists the stacks that will deploy after the one currently deploying,
// in deploy order; it shrinks as the run progresses and is empty when the run is
// idle or on its last stack. It is distinct from the autosync pending queue
// (deferred, paused stacks): this is the remaining work of the *active* run.
type RunPlan struct {
	Upcoming []string `json:"upcoming"`
}

// SetRunPlanSink installs a callback invoked whenever the run plan changes: as
// each stack begins deploying (carrying the stacks still to come) and once more
// with an empty plan when the run ends. nil disables run-plan tracking, so the
// upfront planning pass is skipped entirely when the UI is off. Must be called
// before any deployments start.
func (d *Deployer) SetRunPlanSink(fn func(RunPlan)) {
	d.runPlanSink = fn
}

// CurrentRunPlan returns the latest published run plan so a UI client connecting
// mid-run sees what is coming next without waiting for the next stack to start.
// Safe for concurrent use.
func (d *Deployer) CurrentRunPlan() RunPlan {
	if p := d.currentRunPlan.Load(); p != nil {
		return *p
	}
	return RunPlan{}
}

// computeRunPlan returns, in deploy order, the stacks that will actually deploy
// this run: those with file changes that autosync is not deferring. It hashes
// every stack once upfront (a read-only pass) so the UI can look ahead; the
// deploy loop re-evaluates each stack independently and stays the source of
// truth for what is deployed. _nixos is intentionally excluded — the rebuild has
// no per-stack "deploying" state in the UI and may restart skipper.
func (d *Deployer) computeRunPlan(cfg *config.Config, state *persistedState) []string {
	var plan []string
	for _, stack := range cfg.Stacks {
		if d.isPaused(stack.Name) {
			continue
		}
		if stackHasChanges(stack, cfg.StacksBaseDir, cfg.VarsFile, state) {
			plan = append(plan, stack.Name)
		}
	}
	return plan
}

// stackHasChanges reports whether the stack's tracked files differ from the
// persisted state — the same decision deployStackIfChanged makes, factored out
// for the planning pass. A hash error yields false: the real deploy would fail
// before emitting a deploying event, so the stack is not planned.
func stackHasChanges(stack config.Stack, baseDir, varsFile string, state *persistedState) bool {
	repoDir := filepath.Join(baseDir, stack.Name)
	composePath := filepath.Join(repoDir, "docker-compose.yml")

	var dockerfilePaths []string
	if compose, err := parseComposeFile(composePath); err == nil && compose != nil {
		dockerfilePaths = compose.dockerfilePaths(repoDir)
	}

	currentHashes, err := computePerFileHashes(repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		return false
	}
	return len(changedFiles(currentHashes, state.hashesFor(stack.Name))) > 0
}

// publishUpcomingAfter publishes the run plan as of `stack` starting to deploy:
// the stacks that come after it in the plan. Called right after the deploying
// event so the snapshot and the event stream stay consistent.
func (d *Deployer) publishUpcomingAfter(stack string) {
	if d.runPlanSink == nil {
		return
	}
	var upcoming []string
	for i, s := range d.plan {
		if s == stack {
			upcoming = append(upcoming, d.plan[i+1:]...)
			break
		}
	}
	d.publishRunPlan(upcoming)
}

// publishRunPlan stores the plan for late-connecting clients and forwards it to
// the sink (when installed).
func (d *Deployer) publishRunPlan(upcoming []string) {
	plan := RunPlan{Upcoming: upcoming}
	d.currentRunPlan.Store(&plan)
	if d.runPlanSink != nil {
		d.runPlanSink(plan)
	}
}
