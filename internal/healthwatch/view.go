package healthwatch

import (
	"time"

	"github.com/polandy/skipper-cd/internal/health"
)

// PhaseView is one status phase as shown to the UI: the persisted phase plus
// the deploy correlation, which is derived at view-build time from the stack's
// last deploy and the attribution window — never stored (ADR-0031).
type PhaseView struct {
	Status           health.Status `json:"status"`
	Since            time.Time     `json:"since"`
	Commit           string        `json:"commit,omitempty"`
	DeployCorrelated bool          `json:"deploy_correlated,omitempty"`
}

// View is the watcher's current knowledge for the UI's per-service panel:
// stack → service → phases, newest first (≤ 10 per service). Published as the
// `healthwatch` SSE state event and served as initial state on connect.
type View struct {
	Stacks map[string]map[string][]PhaseView `json:"stacks"`
}

// Current returns the watcher's state as a View. Safe for concurrent use.
func (w *Watcher) Current() View {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.view()
}

// view builds the View. Callers hold w.mu.
func (w *Watcher) view() View {
	v := View{Stacks: make(map[string]map[string][]PhaseView, len(w.state.stacks))}
	for stack, ss := range w.state.stacks {
		if len(ss.Services) == 0 {
			continue
		}
		svcs := make(map[string][]PhaseView, len(ss.Services))
		for svc, phases := range ss.Services {
			pv := make([]PhaseView, len(phases))
			for i, p := range phases {
				pv[i] = PhaseView{
					Status:           p.Status,
					Since:            p.Since,
					Commit:           p.Commit,
					DeployCorrelated: w.deployCorrelated(ss, p.Since),
				}
			}
			svcs[svc] = pv
		}
		v.Stacks[stack] = svcs
	}
	return v
}
