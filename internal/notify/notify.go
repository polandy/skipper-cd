// Package notify delivers a message to configured outbound targets on every
// terminal deploy outcome. Delivery is fire-and-forget over HTTP and never
// blocks the deploy path. A run's successes go out as one digest at the end of
// the run (docs/dev/notification-digest-spec.md). See ADR-0020.
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

// Formatter builds the provider-specific HTTP request for a deploy event, and
// for a run's digest of successes. A new provider is a new Formatter — the
// dispatch machinery is unchanged.
type Formatter interface {
	Format(ctx context.Context, ev events.DeployEvent) (*http.Request, error)
	FormatDigest(ctx context.Context, d runDigest) (*http.Request, error)
}

type target struct {
	format    string // config format name, used as a metric label
	formatter Formatter
	on        map[events.Status]bool
	digest    bool // successes wait for the run's digest
}

// digestsSuccesses reports whether this target receives successes as the
// run's digest.
func (t target) digestsSuccesses() bool { return t.digest && t.on[events.StatusSuccess] }

// queueItem is one entry of the notifier's queue: a deploy event, or the
// end-of-run marker that flushes the digest. The marker rides the same queue so
// it can never overtake a success still queued ahead of it.
type queueItem struct {
	ev     events.DeployEvent
	runEnd bool
	at     time.Time // when the run ended; set on the marker only
}

// Notifier fans terminal deploy events out to its configured targets.
type Notifier struct {
	*transport[queueItem]
	targets []target
	now     func() time.Time
	pending digestBuffer // the current run's successes; owned by the worker
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
		targets = append(targets, target{
			format: ct.Format, formatter: f, on: statusSet(ct.On),
			digest: ct.Digest != config.NotifyDigestOff,
		})
	}
	return &Notifier{
		transport: newTransport[queueItem](doer, timeout, "notification dropped: buffer full"),
		targets:   targets,
		now:       time.Now,
	}, nil
}

// Enabled reports whether any target is configured.
func (n *Notifier) Enabled() bool { return n != nil && len(n.targets) > 0 }

// Notify enqueues a terminal deploy event for asynchronous delivery. It never
// blocks: non-terminal statuses are ignored, and a full buffer drops the event.
func (n *Notifier) Notify(ev events.DeployEvent) {
	if !n.Enabled() || !notifiable(ev.Status) {
		return
	}
	n.push(queueItem{ev: ev}, "stack", ev.Stack, "status", ev.Status)
}

// EndRun marks the end of a deploy run: the successes queued before it are
// sent as that run's digest. Wire it to the deployer's RunEndHook.
func (n *Notifier) EndRun() {
	if !n.Enabled() {
		return
	}
	n.push(queueItem{runEnd: true, at: n.now()}, "item", "run end")
}

// Run delivers queued events until ctx is cancelled, then drains what is left
// and sends the partial digest of the run in flight.
func (n *Notifier) Run(ctx context.Context) {
	n.runThen(ctx, n.handleItem, func(ctx context.Context) { n.flushDigest(ctx, n.now()) })
}

func (n *Notifier) handleItem(ctx context.Context, it queueItem) {
	if it.runEnd {
		n.flushDigest(ctx, it.at)
		return
	}
	n.handle(ctx, it.ev)
}

// handle delivers one event to every target subscribed to its status, or holds
// a success back for the run's digest. A failing target is logged and skipped;
// it never aborts the others.
func (n *Notifier) handle(ctx context.Context, ev events.DeployEvent) {
	buffered := false
	for _, t := range n.targets {
		switch {
		case !t.on[ev.Status]:
		case ev.Status == events.StatusSuccess && t.digestsSuccesses():
			if !buffered {
				n.pending.add(ev)
				buffered = true
			}
		default:
			n.deliver(ctx, t, ev)
		}
	}
}

// flushDigest sends the buffered successes to every digesting target and
// empties the buffer, so the next run starts clean. A single success goes out
// as its ordinary message.
func (n *Notifier) flushDigest(ctx context.Context, end time.Time) {
	d, ok := n.pending.take(end)
	if !ok {
		return
	}
	for _, t := range n.targets {
		if !t.digestsSuccesses() {
			continue
		}
		if d.single() {
			n.deliver(ctx, t, d.events[0])
			continue
		}
		deliverOne(ctx, n.doer, n.timeout,
			func(ctx context.Context) (*http.Request, error) { return t.formatter.FormatDigest(ctx, d) },
			metrics.NotificationsSent, t.format, "notification", "digest_stacks", d.count())
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

// notifiable reports whether a status ends a stack's deploy attempt and is
// therefore deliverable. It must stay in sync with the NotifyOn* vocabulary in
// internal/config — a status accepted in a target's `on` list but missing here
// is silently undeliverable.
func notifiable(s events.Status) bool {
	return s == events.StatusSuccess || s == events.StatusFailed ||
		s == events.StatusRolledBack || s == events.StatusRolledBackUnhealthy ||
		s == events.StatusHealExhausted
}
