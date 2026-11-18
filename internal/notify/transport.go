package notify

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/polandy/skipper-cd/internal/metrics"
)

// transport is the fire-and-forget delivery machinery the alerters in this
// package share: a bounded queue one goroutine drains, the HTTP Doer, and the
// per-request timeout that also bounds the shutdown drain. ADR-0031 keeps the
// three alerters' *semantics* separate — which events they accept, which
// targets they carry — but the way an item reaches the wire is the same for
// all of them, so it lives here once.
type transport[T any] struct {
	doer    Doer
	timeout time.Duration
	items   chan T
	dropMsg string // logged when a full buffer drops an item
}

// newTransport builds the shared machinery. A nil doer uses
// http.DefaultClient and a non-positive timeout the package default.
func newTransport[T any](doer Doer, timeout time.Duration, dropMsg string) *transport[T] {
	if doer == nil {
		doer = http.DefaultClient
	}
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &transport[T]{
		doer:    doer,
		timeout: timeout,
		items:   make(chan T, notifyBufferSize),
		dropMsg: dropMsg,
	}
}

// push enqueues an item for delivery. It never blocks: a full buffer drops the
// item, because the deploy and health paths must not wait on an outbound HTTP
// call. attrs identify the dropped item in the warning.
func (t *transport[T]) push(item T, attrs ...any) {
	select {
	case t.items <- item:
	default:
		slog.Warn(t.dropMsg, attrs...)
		metrics.NotificationsDropped.Inc()
	}
}

// run consumes queued items until ctx is cancelled, then best-effort drains
// what is still buffered within one timeout and returns. Intended to run in
// its own goroutine; the caller must not block shutdown on it (ADR-0014 ethos).
func (t *transport[T]) run(ctx context.Context, handle func(context.Context, T)) {
	for {
		select {
		case <-ctx.Done():
			t.drain(handle)
			return
		case item := <-t.items:
			handle(ctx, item)
		}
	}
}

func (t *transport[T]) drain(handle func(context.Context, T)) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()
	for {
		select {
		case item := <-t.items:
			handle(ctx, item)
		default:
			return
		}
	}
}
