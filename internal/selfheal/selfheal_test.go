package selfheal_test

import (
	"context"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/health"
	"github.com/polandy/skipper-cd/internal/selfheal"
)

// fakeHealer records Heal calls and returns a scripted (ran, err).
type fakeHealer struct {
	calls     []string
	lastDrift []events.DriftedService
	ran       bool
	err       error
}

func (f *fakeHealer) Heal(_ context.Context, stack string, drift []events.DriftedService) (bool, error) {
	f.calls = append(f.calls, stack)
	f.lastDrift = drift
	return f.ran, f.err
}

// clock is an injectable time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func snap(stack string, st health.Status) health.Snapshot {
	return health.Snapshot{Stacks: map[string]health.StackHealth{stack: {Status: st}}}
}

// newEngine builds an Engine with all stacks enabled and the given pacing.
func newEngine(h selfheal.Healer, clk *clock, minPolls, maxAttempts int, cooldown time.Duration, onExhausted func(string)) *selfheal.Engine {
	return selfheal.New(selfheal.Config{
		Healer:            h,
		Enabled:           func(string) bool { return true },
		MinUnhealthyPolls: minPolls,
		MaxAttempts:       maxAttempts,
		Cooldown:          cooldown,
		OnExhausted:       onExhausted,
		Now:               clk.now,
	})
}

func TestEngine_DebouncesBeforeHealing(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 3, 3, time.Minute, nil)

	// Two degraded polls stay below the debounce.
	eng.Observe(context.Background(), snap("web", health.Unhealthy))
	eng.Observe(context.Background(), snap("web", health.Unhealthy))
	if len(h.calls) != 0 {
		t.Fatalf("no heal expected before min_unhealthy_polls, got %d", len(h.calls))
	}
	// The third crosses it.
	eng.Observe(context.Background(), snap("web", health.Unhealthy))
	if len(h.calls) != 1 {
		t.Fatalf("expected one heal at the debounce threshold, got %d", len(h.calls))
	}
}

func TestEngine_PassesDegradedServicesAsDrift(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 3, time.Minute, nil)

	// A stack whose rollup is unhealthy: one service unhealthy, one stopped, one
	// healthy. The heal should carry only the two degraded services, in order.
	sh := health.StackHealth{
		Status: health.Unhealthy,
		Services: []health.ServiceHealth{
			{Name: "api", Status: health.Unhealthy},
			{Name: "sidecar", Status: health.Healthy},
			{Name: "worker", Status: health.Stopped},
		},
	}
	eng.Observe(context.Background(), health.Snapshot{Stacks: map[string]health.StackHealth{"web": sh}})

	want := []events.DriftedService{
		{Name: "api", Status: "unhealthy"},
		{Name: "worker", Status: "stopped"},
	}
	if len(h.lastDrift) != len(want) {
		t.Fatalf("expected %d drifted services, got %+v", len(want), h.lastDrift)
	}
	for i, d := range want {
		if h.lastDrift[i] != d {
			t.Fatalf("drift[%d] = %+v, want %+v", i, h.lastDrift[i], d)
		}
	}
}

func TestEngine_DoesNotHealIdleOnDemandStack(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 3, 0, nil)

	// An on-demand stack after a deploy: skipper started its container then
	// stopped it, so it reads as stopped. The rollup is Stopped, but the stopped
	// service is the stack's on_demand_containers — intended idle, not drift, so
	// self-heal must leave it alone (ADR-0029).
	sh := health.StackHealth{
		Status: health.Stopped,
		Services: []health.ServiceHealth{
			{Name: "whoami", Status: health.Stopped, OnDemand: true},
		},
	}
	for range 3 {
		eng.Observe(context.Background(), health.Snapshot{Stacks: map[string]health.StackHealth{"whoami": sh}})
	}
	if len(h.calls) != 0 {
		t.Fatalf("an idle on-demand stack must never be healed, got %v", h.calls)
	}
}

func TestEngine_HealsRealDriftAlongsideIdleOnDemand(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 3, time.Minute, nil)

	// A stack mixing a genuinely down service with an idle on-demand container:
	// the real drift must still heal, but the on-demand-idle container must not
	// appear in the drift list that describes what triggered the heal.
	sh := health.StackHealth{
		Status: health.Stopped,
		Services: []health.ServiceHealth{
			{Name: "api", Status: health.Stopped},
			{Name: "batch", Status: health.Stopped, OnDemand: true},
		},
	}
	eng.Observe(context.Background(), health.Snapshot{Stacks: map[string]health.StackHealth{"web": sh}})

	if len(h.calls) != 1 {
		t.Fatalf("a stack with a genuinely down service must heal, got %d", len(h.calls))
	}
	want := []events.DriftedService{{Name: "api", Status: "stopped"}}
	if len(h.lastDrift) != len(want) || h.lastDrift[0] != want[0] {
		t.Fatalf("drift should list only the real down service, got %+v", h.lastDrift)
	}
}

func TestEngine_HealsStoppedStackWithoutServiceDetail(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 3, 0, nil)

	// A normal stack that is fully down reports Stopped with no per-service
	// detail (no containers for the project). With no services to inspect for
	// on-demand markers, it must still heal as before.
	eng.Observe(context.Background(), snap("web", health.Stopped))
	if len(h.calls) != 1 {
		t.Fatalf("a stopped stack with no service detail must still heal, got %d", len(h.calls))
	}
}

func TestEngine_HonoursCooldownBetweenAttempts(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 5, time.Minute, nil)

	eng.Observe(context.Background(), snap("web", health.Unhealthy)) // heal #1
	eng.Observe(context.Background(), snap("web", health.Unhealthy)) // within cooldown → skip
	if len(h.calls) != 1 {
		t.Fatalf("cooldown should block the second heal, got %d", len(h.calls))
	}
	clk.advance(time.Minute)
	eng.Observe(context.Background(), snap("web", health.Unhealthy)) // cooldown elapsed → heal #2
	if len(h.calls) != 2 {
		t.Fatalf("expected a second heal after cooldown, got %d", len(h.calls))
	}
}

func TestEngine_GivesUpAfterMaxAttempts(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	var exhausted []string
	eng := newEngine(h, clk, 1, 2, time.Minute, func(s string) { exhausted = append(exhausted, s) })

	// Perform maxAttempts heals, advancing past the cooldown each time.
	for range 5 {
		eng.Observe(context.Background(), snap("web", health.Unhealthy))
		clk.advance(time.Minute)
	}
	if len(h.calls) != 2 {
		t.Fatalf("expected exactly max_attempts=2 heals, got %d", len(h.calls))
	}
	if len(exhausted) != 1 || exhausted[0] != "web" {
		t.Fatalf("expected heal_exhausted emitted once for web, got %v", exhausted)
	}
}

func TestEngine_RecoveryResetsBreaker(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 2, 0, nil)

	// Exhaust it.
	for range 4 {
		eng.Observe(context.Background(), snap("web", health.Unhealthy))
	}
	if len(h.calls) != 2 {
		t.Fatalf("expected 2 heals before recovery, got %d", len(h.calls))
	}
	// Recover, then degrade again — a fresh outage heals anew.
	eng.Observe(context.Background(), snap("web", health.Healthy))
	eng.Observe(context.Background(), snap("web", health.Unhealthy))
	if len(h.calls) != 3 {
		t.Fatalf("recovery should reset the breaker so a new outage heals, got %d", len(h.calls))
	}
}

func TestEngine_ResetClearsBreaker(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 1, 0, nil)

	eng.Observe(context.Background(), snap("web", health.Unhealthy)) // heal #1, now exhausted budget
	eng.Observe(context.Background(), snap("web", health.Unhealthy)) // attempts hit max
	if len(h.calls) != 1 {
		t.Fatalf("expected 1 heal, got %d", len(h.calls))
	}
	eng.Reset("web") // a real git deploy ran
	eng.Observe(context.Background(), snap("web", health.Unhealthy))
	if len(h.calls) != 2 {
		t.Fatalf("Reset should give a fresh attempt budget, got %d", len(h.calls))
	}
}

func TestEngine_IgnoresDisabledStacks(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := selfheal.New(selfheal.Config{
		Healer:            h,
		Enabled:           func(s string) bool { return s == "web" },
		MinUnhealthyPolls: 1,
		MaxAttempts:       3,
		Cooldown:          0,
		Now:               clk.now,
	})
	eng.Observe(context.Background(), snap("db", health.Unhealthy))
	if len(h.calls) != 0 {
		t.Fatalf("a stack without self-heal must never be healed, got %v", h.calls)
	}
}

func TestEngine_IgnoresStartingAndUnknown(t *testing.T) {
	h := &fakeHealer{ran: true}
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 3, 0, nil)

	eng.Observe(context.Background(), snap("web", health.Starting))
	eng.Observe(context.Background(), snap("web", health.Unknown))
	if len(h.calls) != 0 {
		t.Fatalf("starting/unknown must never trigger a heal, got %v", h.calls)
	}
}

func TestEngine_SkippedHealDoesNotConsumeBudget(t *testing.T) {
	h := &fakeHealer{ran: false} // deploy already in progress → skipped
	clk := &clock{t: time.Unix(1000, 0)}
	eng := newEngine(h, clk, 1, 1, time.Minute, nil)

	eng.Observe(context.Background(), snap("web", health.Unhealthy)) // attempted but skipped
	if len(h.calls) != 1 {
		t.Fatalf("expected one heal attempt, got %d", len(h.calls))
	}
	// A skipped heal set no cooldown and spent no attempt: the next poll retries.
	h.ran = true
	eng.Observe(context.Background(), snap("web", health.Unhealthy))
	if len(h.calls) != 2 {
		t.Fatalf("a skipped heal must not consume the budget or set cooldown, got %d", len(h.calls))
	}
}
