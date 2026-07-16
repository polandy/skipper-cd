// Package reconcile periodically re-runs skipper's git sync + deploy so a
// missed or lost webhook cannot leave the host drifted from the deploy repo
// indefinitely (ADR-0028). It reconciles against git desired state only, using
// the same sync + deploy path a webhook does; it does not inspect container
// runtime state.
package reconcile

import (
	"context"
	"log/slog"
	"time"
)

// Loop calls a reconcile function on a fixed interval until its context is
// cancelled. The function reports whether it ran: a false return means a deploy
// was already in progress and the tick was skipped, so the loop never queues
// reconciles behind a long deploy.
type Loop struct {
	interval  time.Duration
	reconcile func(context.Context) bool
}

// New builds a Loop that calls reconcile every interval. A non-positive
// interval disables the loop (see Run).
func New(interval time.Duration, reconcile func(context.Context) bool) *Loop {
	return &Loop{interval: interval, reconcile: reconcile}
}

// Run ticks every interval, calling reconcile once per tick, until ctx is
// cancelled. A non-positive interval disables the loop: Run returns
// immediately, so pure webhook + startup behaviour is restored.
func (l *Loop) Run(ctx context.Context) {
	if l.interval <= 0 {
		return
	}
	t := time.NewTicker(l.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !l.reconcile(ctx) {
				slog.Debug("reconcile tick skipped: deploy already in progress")
			}
		}
	}
}
