package peers

import (
	"context"
	"time"
)

// DefaultPollInterval is the fan-in poll cadence used when the runtime health
// poll — whose cadence the fan-in normally rides — is disabled.
const DefaultPollInterval = 30 * time.Second

// Poller is the consumer-side seam the fan-in loop drives: one refresh pass
// over every peer plus the merged state to publish afterwards. In production
// it is the Registry; tests inject a fake.
type Poller interface {
	Poll(ctx context.Context)
	State() State
}

// Loop refreshes a Poller on a fixed interval and hands the merged state to a
// publish callback after every pass, until its context is cancelled. It knows
// nothing about SSE — the callback is the only outbound edge.
type Loop struct {
	poller   Poller
	interval time.Duration
	publish  func(State)
}

// NewLoop builds a Loop that polls p every interval. A non-positive interval
// falls back to DefaultPollInterval — unlike the reconcile loop, the fan-in is
// never disabled by cadence: with peers configured it must keep refreshing.
func NewLoop(p Poller, interval time.Duration, publish func(State)) *Loop {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	return &Loop{poller: p, interval: interval, publish: publish}
}

// Interval reports the effective poll cadence after the fallback is applied,
// so the caller can narrate it at startup.
func (l *Loop) Interval() time.Duration { return l.interval }

// Run polls immediately — the UI gets a `peers` state at startup, not one
// interval later — then once per tick, publishing after every pass, until ctx
// is cancelled.
func (l *Loop) Run(ctx context.Context) {
	t := time.NewTicker(l.interval)
	defer t.Stop()
	for {
		l.poller.Poll(ctx)
		l.publish(l.poller.State())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
