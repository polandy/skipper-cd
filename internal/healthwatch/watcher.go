// Package healthwatch is skipper-cd's own-stack health watchdog (ADR-0031):
// it consumes the shared health poller's snapshot feed — the same seam
// self-heal uses (ADR-0029) — detects per-service health transitions, records
// a bounded per-service phase history, and alerts on newly-failed and
// recovered services. It watches only the compose stacks skipper deploys;
// host-wide watching and incident state machines are out of scope.
package healthwatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/health"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// Alert is one alert-worthy health transition: a service turning unhealthy or
// recovering from it.
type Alert struct {
	Stack   string
	Service string
	From    health.Status
	To      health.Status
	// Since is when the new status was first observed (not when the debounce
	// confirmed it).
	Since time.Time
	// PrevDuration is how long the previous status held.
	PrevDuration time.Duration
	// Commit is the newest commit that had touched the stack when the new
	// status began; empty when unknown. Context, not a causal claim.
	Commit string
	// DeployCorrelated reports whether the transition began within the
	// attribution window after the stack's last successful deploy.
	DeployCorrelated bool
}

// Alerter delivers an alert. Implementations must not block: the watcher calls
// Fire inline from the poller's snapshot goroutine. notify.HealthAlerter
// satisfies it.
type Alerter interface {
	Fire(Alert)
}

// Config wires a Watcher.
type Config struct {
	// Alerter receives alert-worthy transitions; nil means log + persist only.
	Alerter   Alerter
	StatePath string
	// DebouncePolls is how many consecutive snapshots a new status must persist
	// before it is accepted; values below 1 default to 2.
	DebouncePolls int
	// AttributionWindow is how long after a deploy a transition still counts
	// as deploy-correlated.
	AttributionWindow time.Duration
	// AlertCooldown is the minimum gap between delivered alerts of the same
	// service and direction (unhealthy / recovered); 0 disables it. Within the
	// cooldown an alert-worthy transition is recorded but not delivered; once
	// it expires, a still-diverged service gets the owed alert late (catch-up),
	// so a flap never settles silently down (ADR-0031 amendment).
	AlertCooldown time.Duration
	// Now overrides the clock in tests; nil uses time.Now.
	Now func() time.Time
	// Publish, when set, receives the fresh View after every accepted change
	// (baseline, transition, or a service appearing/vanishing) — the UI's
	// healthwatch SSE feed. Called on the poller goroutine, outside the lock.
	Publish func(View)
}

// Watcher turns the health poller's snapshots into accepted per-service status
// phases, journal lines, and alerts. It owns no poll loop: main wires Observe
// into the poller's OnSnapshot feed (AlwaysPoll keeps that feed headless).
type Watcher struct {
	alerter   Alerter
	statePath string
	debounce  int
	window    time.Duration
	cooldown  time.Duration
	now       func() time.Time
	publish   func(View)

	mu      sync.Mutex
	state   *state
	pending map[string]map[string]*pendingStatus
	dirty   bool
}

// pendingStatus tracks a not-yet-accepted status change of one service through
// the debounce window. It is in-memory only — never persisted.
type pendingStatus struct {
	status health.Status
	count  int
	since  time.Time
}

// New builds a Watcher from cfg and loads its persisted state; a missing or
// corrupt state file starts clean (silent baseline).
func New(cfg Config) *Watcher {
	debounce := cfg.DebouncePolls
	if debounce < 1 {
		debounce = 2
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Watcher{
		alerter:   cfg.Alerter,
		statePath: cfg.StatePath,
		debounce:  debounce,
		window:    cfg.AttributionWindow,
		cooldown:  cfg.AlertCooldown,
		now:       now,
		publish:   cfg.Publish,
		state:     loadState(cfg.StatePath),
		pending:   map[string]map[string]*pendingStatus{},
	}
}

// ObserveDeploy records a stack's last successful deploy from the deploy event
// feed — the isolated seam for commit context; the watcher never reads deploy
// state. Registered as one more deploy-event sink in main.
func (w *Watcher) ObserveDeploy(e events.DeployEvent) {
	if e.Status != events.StatusSuccess {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	rec := &deployRecord{At: normalizeTime(e.Timestamp)}
	if len(e.Commits) > 0 {
		rec.Commit = e.Commits[0].SHA // newest first: the last commit touching the stack
	}
	w.state.ensure(e.Stack).LastDeploy = rec
	w.dirty = true
}

// Observe feeds one poller snapshot through the transition detector,
// persisting the state once when anything was accepted and publishing the
// fresh View to the UI feed. It runs on the poller's goroutine (the OnSnapshot
// contract).
func (w *Watcher) Observe(snap health.Snapshot) {
	w.mu.Lock()
	now := normalizeTime(w.now())
	for name, sh := range snap.Stacks {
		if sh.Status == health.Unknown {
			// A failed probe holds the last known state: no transition, no
			// alert, never a false unhealthy (ADR-0027 stance).
			slog.Debug("healthwatch probe unknown, holding state", "stack", name)
			continue
		}
		seen := make(map[string]bool, len(sh.Services))
		for _, svc := range sh.Services {
			seen[svc.Name] = true
			w.observe(name, svc.Name, svc.Status, now)
		}
		// A known service missing from ps output has no container anymore:
		// observe it as stopped.
		if ss := w.state.stacks[name]; ss != nil {
			for svcName := range ss.Services {
				if !seen[svcName] {
					w.observe(name, svcName, health.Stopped, now)
				}
			}
		}
		w.catchUpStack(name, now)
	}
	changed := w.dirty
	var view View
	if changed {
		if err := w.state.save(w.statePath); err != nil {
			slog.Error("healthwatch state save failed", "path", w.statePath, "err", err)
		}
		w.dirty = false
		if w.publish != nil {
			view = w.view()
		}
	}
	w.mu.Unlock()
	if changed && w.publish != nil {
		w.publish(view)
	}
}

// observe feeds one service's currently probed status through debounce and, on
// an accepted change, records the phase, logs it, and fires an alert when the
// transition is alert-worthy. Callers hold w.mu.
func (w *Watcher) observe(stack, svc string, status health.Status, now time.Time) {
	ss := w.state.ensure(stack)
	hist := ss.Services[svc]

	// First sight of a service establishes its baseline silently — a fresh
	// state file or a new compose service is never a transition.
	if len(hist) == 0 {
		ss.Services[svc] = []Phase{{Status: status, Since: now, Commit: w.commitFor(ss)}}
		w.dirty = true
		slog.Debug("healthwatch baseline", "stack", stack, "service", svc, "status", status)
		return
	}

	cur := hist[0].Status
	if status == cur {
		delete(w.pending[stack], svc) // a pending change healed before acceptance
		return
	}

	pd := w.pending[stack][svc]
	if pd == nil || pd.status != status {
		pd = &pendingStatus{status: status, since: now}
		if w.pending[stack] == nil {
			w.pending[stack] = map[string]*pendingStatus{}
		}
		w.pending[stack][svc] = pd
	}
	pd.count++
	if pd.count < w.debounce {
		return
	}
	delete(w.pending[stack], svc)

	// Accepted: since is the first snapshot of the phase, not the confirming one.
	phase := Phase{Status: status, Since: pd.since, Commit: w.commitFor(ss)}
	hist = append([]Phase{phase}, hist...)
	if len(hist) > maxPhases {
		hist = hist[:maxPhases]
	}
	ss.Services[svc] = hist
	w.dirty = true

	correlated := w.deployCorrelated(ss, pd.since)
	metrics.HealthTransitions.WithLabelValues(string(status)).Inc()
	level := slog.LevelInfo
	if status == health.Unhealthy {
		level = slog.LevelWarn
	}
	slog.Log(context.Background(), level, "stack health transition",
		"stack", stack, "service", svc, "from", cur, "to", status,
		"since", pd.since.Format(time.RFC3339), "commit", phase.Commit,
		"deploy_correlated", correlated)

	// Only newly-failed and recovered page; starting/stopped transitions are
	// recorded and logged but stay silent (an intentional down must not page).
	alertWorthy := status == health.Unhealthy || (cur == health.Unhealthy && status == health.Healthy)
	if w.alerter == nil || !alertWorthy {
		return
	}
	if w.inCooldown(ss, svc, status, now) {
		ss.ensureAlert(svc).Suppressed = true
		metrics.HealthAlertsSuppressed.WithLabelValues(string(status)).Inc()
		slog.Info("health alert suppressed by cooldown",
			"stack", stack, "service", svc, "to", status)
		return
	}
	w.deliver(svc, ss, Alert{
		Stack:            stack,
		Service:          svc,
		From:             cur,
		To:               status,
		Since:            pd.since,
		PrevDuration:     pd.since.Sub(hist[1].Since),
		Commit:           phase.Commit,
		DeployCorrelated: correlated,
	}, now)
}

// inCooldown reports whether delivering an alert with the given target status
// would violate the service's per-direction cooldown. A disabled cooldown or a
// never-alerted direction is never in cooldown. Callers hold w.mu.
func (w *Watcher) inCooldown(ss *stackState, svc string, to health.Status, now time.Time) bool {
	if w.cooldown <= 0 {
		return false
	}
	rec := ss.Alerts[svc]
	if rec == nil {
		return false
	}
	last := rec.UnhealthyAt
	if to == health.Healthy {
		last = rec.RecoveredAt
	}
	return !last.IsZero() && now.Sub(last) < w.cooldown
}

// deliver fires an alert and, when the cooldown is enabled, records the
// delivery so later transitions of the same direction are rate-limited and a
// pending catch-up is settled. Callers hold w.mu.
func (w *Watcher) deliver(svc string, ss *stackState, a Alert, now time.Time) {
	if w.cooldown > 0 {
		rec := ss.ensureAlert(svc)
		if a.To == health.Unhealthy {
			rec.UnhealthyAt = now
		} else {
			rec.RecoveredAt = now
		}
		rec.Suppressed = false
		w.dirty = true
	}
	w.alerter.Fire(a)
}

// catchUpStack delivers alerts owed from cooldown suppression: once the
// direction's cooldown has expired, a service whose current accepted status
// still diverges alert-worthily from the last delivered alert gets that alert
// late — so a flap that settles down-state never stays silently down. A
// converged or silent (starting/stopped) service resolves without paging.
// Callers hold w.mu.
func (w *Watcher) catchUpStack(stack string, now time.Time) {
	if w.cooldown <= 0 || w.alerter == nil {
		return
	}
	ss := w.state.stacks[stack]
	if ss == nil {
		return
	}
	for svc, rec := range ss.Alerts {
		if rec == nil || !rec.Suppressed {
			continue
		}
		hist := ss.Services[svc]
		if len(hist) < 2 {
			continue
		}
		cur := hist[0].Status
		// The operator's picture is the status of the most recent delivered
		// alert; only a still-diverged service owes a catch-up.
		lastAlertedUnhealthy := rec.UnhealthyAt.After(rec.RecoveredAt)
		owesUnhealthy := cur == health.Unhealthy && !lastAlertedUnhealthy
		owesRecovered := cur == health.Healthy && lastAlertedUnhealthy
		if !owesUnhealthy && !owesRecovered {
			rec.Suppressed = false
			w.dirty = true
			continue
		}
		if w.inCooldown(ss, svc, cur, now) {
			continue // still cooling; retried on the next snapshot
		}
		a := Alert{
			Stack:            stack,
			Service:          svc,
			From:             hist[1].Status,
			To:               cur,
			Since:            hist[0].Since,
			PrevDuration:     hist[0].Since.Sub(hist[1].Since),
			Commit:           hist[0].Commit,
			DeployCorrelated: w.deployCorrelated(ss, hist[0].Since),
		}
		slog.Info("health alert delivered after cooldown",
			"stack", stack, "service", svc, "to", cur,
			"since", a.Since.Format(time.RFC3339))
		w.deliver(svc, ss, a, now)
	}
}

// phases returns a copy of a service's recorded history, newest first.
func (w *Watcher) phases(stack, svc string) []Phase {
	w.mu.Lock()
	defer w.mu.Unlock()
	ss := w.state.stacks[stack]
	if ss == nil {
		return nil
	}
	return append([]Phase(nil), ss.Services[svc]...)
}

// commitFor is the commit context stamped on a new phase: the stack's last
// successful deploy's newest commit, empty when no deploy was observed yet.
func (w *Watcher) commitFor(ss *stackState) string {
	if ss.LastDeploy == nil {
		return ""
	}
	return ss.LastDeploy.Commit
}

// deployCorrelated reports whether a phase beginning at since falls within the
// attribution window after the stack's last successful deploy — derived at use,
// never stored (ADR-0031).
func (w *Watcher) deployCorrelated(ss *stackState, since time.Time) bool {
	if ss.LastDeploy == nil {
		return false
	}
	d := since.Sub(ss.LastDeploy.At)
	return d >= 0 && d <= w.window
}
