package notify

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// counterValue reads a counter's current value. The metrics are global and
// accumulate across tests, so assertions measure a before/after delta.
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

// fakeDoer records every request it is handed and returns a configurable status.
type fakeDoer struct {
	mu     sync.Mutex
	reqs   []*http.Request
	bodies []string
	status int
	err    error
}

func (d *fakeDoer) Do(r *http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var body string
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}
	d.reqs = append(d.reqs, r)
	d.bodies = append(d.bodies, body)
	st := d.status
	if st == 0 {
		st = http.StatusOK
	}
	return &http.Response{StatusCode: st, Body: io.NopCloser(strings.NewReader("ok"))}, d.err
}

func (d *fakeDoer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.reqs)
}

func mustNew(t *testing.T, targets []config.NotificationTarget, doer Doer) *Notifier {
	t.Helper()
	n, err := New(targets, doer, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return n
}

func genericTarget(on ...string) config.NotificationTarget {
	return config.NotificationTarget{Format: config.NotifyFormatGeneric, URL: "https://target.example/h", On: on}
}

func TestNotifier_HandleFiltersByStatus(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{genericTarget(config.NotifyOnFailed)}, doer)

	n.handle(context.Background(), events.DeployEvent{Stack: "web", Status: events.StatusSuccess})
	if doer.count() != 0 {
		t.Fatalf("success should not deliver to a failed-only target, got %d", doer.count())
	}
	n.handle(context.Background(), events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	if doer.count() != 1 {
		t.Fatalf("failed should deliver once, got %d", doer.count())
	}
}

func TestNotifier_HandleFansOutToMatchingTargets(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{
		genericTarget(config.NotifyOnFailed, config.NotifyOnSuccess),
		{Format: config.NotifyFormatGeneric, URL: "https://d.example/h", On: []string{config.NotifyOnFailed}},
	}, doer)

	n.handle(context.Background(), events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	if doer.count() != 2 {
		t.Fatalf("both targets subscribe to failed, want 2 requests, got %d", doer.count())
	}
}

func TestNotifier_HandleContinuesAfterRejection(t *testing.T) {
	doer := &fakeDoer{status: http.StatusInternalServerError}
	n := mustNew(t, []config.NotificationTarget{
		genericTarget(config.NotifyOnFailed),
		genericTarget(config.NotifyOnFailed),
	}, doer)

	// A non-2xx from the first target must not abort the second.
	n.handle(context.Background(), events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	if doer.count() != 2 {
		t.Fatalf("both targets should be attempted despite rejection, got %d", doer.count())
	}
}

func TestNotifier_NotifyEnqueuesTerminalOnly(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{genericTarget(config.NotifyOnFailed)}, doer)

	n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusDeploying})
	n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusSkipped})
	n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusQueued})
	// blocked is deliberately not a notification: the failed dependency already
	// pages, and a blocked event recurs on every reconcile tick (ADR-0032).
	n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusBlocked})
	// A removed stack is a UI/history record, not an alert: nothing broke and
	// nothing is running that was not running before (ADR-0036 amendment).
	n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusRemoved})
	if len(n.items) != 0 {
		t.Fatalf("non-terminal statuses must not enqueue, got %d", len(n.items))
	}

	n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	if len(n.items) != 1 {
		t.Fatalf("terminal status should enqueue, got %d", len(n.items))
	}
}

func TestNotifier_NotifyDropsWhenBufferFull(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{genericTarget(config.NotifyOnFailed)}, doer)

	// Never drained: enqueue well past capacity. Must not block or panic.
	for range notifyBufferSize * 2 {
		n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	}
	if len(n.items) != notifyBufferSize {
		t.Fatalf("queue should cap at %d, got %d", notifyBufferSize, len(n.items))
	}
}

func TestNotifier_DrainDeliversQueued(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{genericTarget(config.NotifyOnFailed)}, doer)

	n.Notify(events.DeployEvent{Stack: "a", Status: events.StatusFailed})
	n.Notify(events.DeployEvent{Stack: "b", Status: events.StatusFailed})
	n.drain(n.handle)

	if doer.count() != 2 {
		t.Fatalf("drain should deliver all queued events, got %d", doer.count())
	}
}

func TestNotifier_MetricOnSuccess(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{genericTarget(config.NotifyOnFailed)}, doer)

	before := counterValue(t, metrics.NotificationsSent.WithLabelValues(config.NotifyFormatGeneric, "ok"))
	n.handle(context.Background(), events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	after := counterValue(t, metrics.NotificationsSent.WithLabelValues(config.NotifyFormatGeneric, "ok"))

	if after-before != 1 {
		t.Fatalf("ok counter delta = %v, want 1", after-before)
	}
}

func TestNotifier_MetricOnRejection(t *testing.T) {
	doer := &fakeDoer{status: http.StatusInternalServerError}
	n := mustNew(t, []config.NotificationTarget{genericTarget(config.NotifyOnFailed)}, doer)

	before := counterValue(t, metrics.NotificationsSent.WithLabelValues(config.NotifyFormatGeneric, "error"))
	n.handle(context.Background(), events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	after := counterValue(t, metrics.NotificationsSent.WithLabelValues(config.NotifyFormatGeneric, "error"))

	if after-before != 1 {
		t.Fatalf("error counter delta = %v, want 1", after-before)
	}
}

func TestNotifier_MetricOnDrop(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{genericTarget(config.NotifyOnFailed)}, doer)

	before := counterValue(t, metrics.NotificationsDropped)
	for range notifyBufferSize * 2 { // never drained: half overflow the buffer
		n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	}
	after := counterValue(t, metrics.NotificationsDropped)

	if after-before != float64(notifyBufferSize) {
		t.Fatalf("dropped counter delta = %v, want %d", after-before, notifyBufferSize)
	}
}

func TestNotifier_EmptyIsDisabled(t *testing.T) {
	n := mustNew(t, nil, &fakeDoer{})
	if n.Enabled() {
		t.Fatal("no targets should report disabled")
	}
	// Notify on a disabled notifier is a no-op, not a panic.
	n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusFailed})
}

// ctxProbeKey tags a context so a fakeDoer can assert a delivered request's
// context derives from a specific ctx value, not an unrelated context.Background().
type ctxProbeKey struct{}

func TestNotifier_RunThreadsRunCtxIntoLiveDelivery(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{genericTarget(config.NotifyOnFailed)}, doer)

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxProbeKey{}, "run-ctx"))
	done := make(chan struct{})
	go func() { n.Run(ctx); close(done) }()

	n.Notify(events.DeployEvent{Stack: "web", Status: events.StatusFailed})
	deadline := time.After(2 * time.Second)
	for doer.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("notification was not delivered")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	<-done

	got := doer.reqs[0].Context().Value(ctxProbeKey{})
	if got != "run-ctx" {
		t.Errorf("live delivery request context = %v, want it to derive from Run's ctx (\"run-ctx\")", got)
	}
}
