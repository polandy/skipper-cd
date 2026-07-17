// Package selfheal drives automatic recovery of stacks that have drifted from
// their deployed running state. It consumes the runtime-health snapshots the
// health poller already produces (ADR-0027) and, for stacks opted into
// self-heal, triggers a corrective redeploy through an injected Healer when a
// stack stays degraded — guarded by a debounce, a cooldown, and a max-attempts
// circuit breaker so an app-level fault a redeploy cannot fix never becomes a
// hot loop (ADR-0029).
//
// The package owns only the policy (when to heal, when to give up); it knows
// nothing about docker, git, or config. The redeploy itself lives behind the
// Healer seam and the per-stack opt-in behind an Enabled predicate, which keeps
// this package isolated and testable.
package selfheal

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/health"
)

// Healer performs one corrective redeploy of a single stack, restoring it to
// its currently deployed running state (a plain `compose up`, no version
// change, no rollback). drift lists the services that were degraded when the
// heal was triggered, so the healed event can show what it reacted to. It
// reports whether the redeploy actually ran: false means a deploy was already in
// progress, so this tick was skipped and must not count against the stack's
// attempt budget. Backed by the deployer's HealStack in production; tests supply
// a fake.
type Healer interface {
	Heal(ctx context.Context, stack string, drift []events.DriftedService) (ran bool, err error)
}

// Config wires an Engine.
type Config struct {
	// Healer performs the corrective redeploy.
	Healer Healer
	// Enabled reports whether self-heal is effective for a stack (per-stack
	// opt-in resolved against the global default).
	Enabled func(stack string) bool
	// MinUnhealthyPolls is how many consecutive degraded polls a stack must show
	// before the first heal — the debounce against transient blips.
	MinUnhealthyPolls int
	// MaxAttempts caps corrective redeploys per outage before giving up.
	MaxAttempts int
	// Cooldown is the minimum gap between corrective redeploys of one stack.
	Cooldown time.Duration
	// OnExhausted is called once when a stack's attempts are exhausted, so the
	// wiring can emit the heal_exhausted event. Optional.
	OnExhausted func(stack string)
	// Now overrides the clock in tests; nil uses time.Now.
	Now func() time.Time
}

// Engine applies the self-heal policy across successive health snapshots.
type Engine struct {
	healer      Healer
	enabled     func(stack string) bool
	minPolls    int
	maxAttempts int
	cooldown    time.Duration
	onExhausted func(stack string)
	now         func() time.Time

	mu     sync.Mutex
	states map[string]*stackState
}

// stackState is one stack's self-heal bookkeeping across polls.
type stackState struct {
	degraded  int       // consecutive degraded polls
	attempts  int       // corrective redeploys performed this outage
	lastTry   time.Time // when the last redeploy ran, for the cooldown
	exhausted bool      // gave up; heal_exhausted already emitted
}

// New builds an Engine from cfg.
func New(cfg Config) *Engine {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Engine{
		healer:      cfg.Healer,
		enabled:     cfg.Enabled,
		minPolls:    cfg.MinUnhealthyPolls,
		maxAttempts: cfg.MaxAttempts,
		cooldown:    cfg.Cooldown,
		onExhausted: cfg.OnExhausted,
		now:         now,
		states:      map[string]*stackState{},
	}
}

// Observe evaluates a health snapshot and heals any opted-in stack that has
// stayed degraded past the debounce, subject to the cooldown and attempt cap.
// It is called once per poll from the poller's goroutine.
func (e *Engine) Observe(ctx context.Context, snap health.Snapshot) {
	for name, sh := range snap.Stacks {
		if e.enabled != nil && !e.enabled(name) {
			continue
		}
		e.evaluate(ctx, name, sh)
	}
}

// driftedServices lists the services that are degraded (unhealthy or stopped) in
// a stack's health, in the order the poller reported them — the "what triggered
// the heal" detail carried on the healed event. Returns nil when the rollup is
// degraded but no individual service is (e.g. an unreadable per-service state).
func driftedServices(sh health.StackHealth) []events.DriftedService {
	var drift []events.DriftedService
	for _, svc := range sh.Services {
		if classify(svc.Status) == degradedCat {
			drift = append(drift, events.DriftedService{Name: svc.Name, Status: string(svc.Status)})
		}
	}
	return drift
}

// Reset clears a stack's self-heal state. The wiring calls it when a real git
// deploy of the stack runs: a push may have fixed the underlying fault, so the
// stack gets a fresh attempt budget rather than staying exhausted (ADR-0029).
func (e *Engine) Reset(stack string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.states, stack)
}

func (e *Engine) evaluate(ctx context.Context, stack string, sh health.StackHealth) {
	e.mu.Lock()
	s := e.states[stack]
	if s == nil {
		s = &stackState{}
		e.states[stack] = s
	}

	switch classify(sh.Status) {
	case recovered:
		// Healthy again: clear the outage so a future one starts fresh.
		delete(e.states, stack)
		e.mu.Unlock()
		return
	case ignore:
		// starting/unknown: neither degraded nor recovered — wait, keep state.
		e.mu.Unlock()
		return
	}

	// Degraded from here on.
	if s.exhausted {
		e.mu.Unlock()
		return
	}
	s.degraded++
	if s.degraded < e.minPolls {
		e.mu.Unlock()
		return
	}
	if s.attempts >= e.maxAttempts {
		s.exhausted = true
		e.mu.Unlock()
		slog.Warn("self-heal exhausted: stack still degraded after repeated redeploys", "stack", stack, "attempts", e.maxAttempts)
		if e.onExhausted != nil {
			e.onExhausted(stack)
		}
		return
	}
	if !s.lastTry.IsZero() && e.now().Sub(s.lastTry) < e.cooldown {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	// Trigger the corrective redeploy outside the lock (it takes the deploy
	// mutex and can block). A skipped run (deploy already in progress) does not
	// count against the attempt budget — the next poll retries.
	slog.Info("self-heal triggering corrective redeploy", "stack", stack)
	ran, err := e.healer.Heal(ctx, stack, driftedServices(sh))
	if !ran {
		slog.Debug("self-heal skipped: deploy already in progress", "stack", stack)
		return
	}
	if err != nil {
		slog.Warn("self-heal corrective redeploy errored", "stack", stack, "err", err)
	}

	e.mu.Lock()
	if s = e.states[stack]; s != nil {
		s.attempts++
		s.lastTry = e.now()
	}
	e.mu.Unlock()
}

// category is how self-heal classifies a stack's health status.
type category int

const (
	degradedCat category = iota // needs restoring
	recovered                   // healthy again
	ignore                      // starting/unknown — neither
)

// classify maps a rolled-up health status to a self-heal category. Unknown is
// deliberately ignored, never treated as degraded, so a failed health read can
// never trigger a spurious redeploy (mirrors ADR-0027's "never a false
// unhealthy").
func classify(s health.Status) category {
	switch s {
	case health.Healthy:
		return recovered
	case health.Unhealthy, health.Stopped:
		return degradedCat
	default: // Starting, Unknown
		return ignore
	}
}
