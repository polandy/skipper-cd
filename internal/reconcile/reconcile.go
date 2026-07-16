// Package reconcile periodically re-runs skipper's git sync + deploy so a
// missed or lost webhook cannot leave the host drifted from the deploy repo
// indefinitely (ADR-0028). It drives an injected Reconciler on a timer and
// knows nothing about git, docker, or config — the reconcile pass itself lives
// behind the Reconciler seam, which keeps this package isolated and testable.
package reconcile

import (
	"context"
	"log/slog"
	"time"
)

// Reconciler performs one reconcile pass — a git sync followed by deploying
// only the stacks whose tracked inputs changed — and reports whether it ran. A
// false return means a deploy was already in progress, so the pass was skipped
// rather than queued behind it. In production it is backed by the deployer's
// TrySyncAndDeployAll; tests supply a fake.
type Reconciler interface {
	Reconcile(ctx context.Context) bool
}

// Loop drives a Reconciler on a fixed interval until its context is cancelled.
type Loop struct {
	interval   time.Duration
	reconciler Reconciler
}

// New builds a Loop that calls r every interval. A non-positive interval
// disables the loop (see Run).
func New(interval time.Duration, r Reconciler) *Loop {
	return &Loop{interval: interval, reconciler: r}
}

// Run runs one reconcile pass every interval until ctx is cancelled. A
// non-positive interval disables the loop: Run returns immediately, so pure
// webhook + startup behaviour is restored. A skipped pass (a deploy already in
// flight) does not stop the loop — the next tick tries again.
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
			if !l.reconciler.Reconcile(ctx) {
				slog.Debug("reconcile tick skipped: deploy already in progress")
			}
		}
	}
}
