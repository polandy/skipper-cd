// Package notify delivers a message to configured outbound targets on every
// terminal deploy outcome. Delivery is fire-and-forget over HTTP and never
// blocks the deploy path. See ADR-0020.
package notify

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// notifyBufferSize bounds the number of pending events; a full buffer drops
// new events rather than blocking the deploy path (same stance as the log ring).
const notifyBufferSize = 64

// defaultRequestTimeout is the per-request HTTP timeout applied when a
// Notifier/HealthAlerter is built with timeout <= 0.
const defaultRequestTimeout = 10 * time.Second

// Doer sends an HTTP request. *http.Client satisfies it; tests inject a fake.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Formatter builds the provider-specific HTTP request for a deploy event.
// A new provider is a new Formatter — the dispatch machinery is unchanged.
type Formatter interface {
	Format(ctx context.Context, ev events.DeployEvent) (*http.Request, error)
}

type target struct {
	format    string // config format name, used as a metric label
	formatter Formatter
	on        map[events.Status]bool
}

// Notifier fans terminal deploy events out to its configured targets.
type Notifier struct {
	*transport[events.DeployEvent]
	targets []target
}

// New builds a Notifier from the config targets. A nil doer uses
// http.DefaultClient. Returns a Notifier with no targets when the list is empty
// (Enabled reports false); Notify on it is a no-op.
func New(cfgTargets []config.NotificationTarget, doer Doer, timeout time.Duration) (*Notifier, error) {
	targets := make([]target, 0, len(cfgTargets))
	for i, ct := range cfgTargets {
		f, err := formatterFor(ct)
		if err != nil {
			return nil, fmt.Errorf("notifications[%d]: %w", i, err)
		}
		targets = append(targets, target{format: ct.Format, formatter: f, on: statusSet(ct.On)})
	}
	return &Notifier{
		transport: newTransport[events.DeployEvent](doer, timeout, "notification dropped: buffer full"),
		targets:   targets,
	}, nil
}

// Enabled reports whether any target is configured.
func (n *Notifier) Enabled() bool { return n != nil && len(n.targets) > 0 }

// Notify enqueues a terminal deploy event for asynchronous delivery. It never
// blocks: non-terminal statuses are ignored, and a full buffer drops the event.
func (n *Notifier) Notify(ev events.DeployEvent) {
	if !n.Enabled() || !isTerminal(ev.Status) {
		return
	}
	n.push(ev, "stack", ev.Stack, "status", ev.Status)
}

// Run delivers queued events until ctx is cancelled, then drains what is left.
func (n *Notifier) Run(ctx context.Context) { n.run(ctx, n.handle) }

// handle delivers one event to every target subscribed to its status. A failing
// target is logged and skipped; it never aborts the others.
func (n *Notifier) handle(ctx context.Context, ev events.DeployEvent) {
	for _, t := range n.targets {
		if !t.on[ev.Status] {
			continue
		}
		n.deliver(ctx, t, ev)
	}
}

func (n *Notifier) deliver(ctx context.Context, t target, ev events.DeployEvent) {
	deliverOne(ctx, n.doer, n.timeout,
		func(ctx context.Context) (*http.Request, error) { return t.formatter.Format(ctx, ev) },
		metrics.NotificationsSent, t.format, "notification", "stack", ev.Stack)
}

func statusSet(on []string) map[events.Status]bool {
	m := make(map[events.Status]bool, len(on))
	for _, s := range on {
		m[events.Status(s)] = true
	}
	return m
}

// isTerminal reports whether a status ends a stack's deploy attempt and is
// therefore deliverable. It must stay in sync with the NotifyOn* vocabulary in
// internal/config — a status accepted in a target's `on` list but missing here
// is silently undeliverable.
func isTerminal(s events.Status) bool {
	return s == events.StatusSuccess || s == events.StatusFailed ||
		s == events.StatusRolledBack || s == events.StatusRolledBackUnhealthy ||
		s == events.StatusHealExhausted
}
