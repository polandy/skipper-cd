package reconcile

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeReconciler records how many passes ran and returns a fixed result, so a
// Loop can be tested without any git/docker/deploy dependency.
type fakeReconciler struct {
	calls atomic.Int32
	ran   bool // what Reconcile reports (false = "deploy already in progress")
}

func (f *fakeReconciler) Reconcile(context.Context) bool {
	f.calls.Add(1)
	return f.ran
}

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestLoop_ReconcilesOnInterval(t *testing.T) {
	r := &fakeReconciler{ran: true}
	l := New(2*time.Millisecond, r)

	go l.Run(t.Context())

	waitFor(t, func() bool { return r.calls.Load() >= 3 }, time.Second)
}

func TestLoop_DisabledWhenIntervalNonPositive(t *testing.T) {
	r := &fakeReconciler{ran: true}
	l := New(0, r)

	done := make(chan struct{})
	go func() {
		l.Run(t.Context())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return immediately for a non-positive interval")
	}
	if got := r.calls.Load(); got != 0 {
		t.Errorf("expected no reconcile passes when disabled, got %d", got)
	}
}

func TestLoop_KeepsTickingWhenPassIsSkipped(t *testing.T) {
	// A Reconciler that always skips (a deploy is always "in progress") must not
	// stop the loop — the next tick tries again.
	r := &fakeReconciler{ran: false}
	l := New(2*time.Millisecond, r)

	go l.Run(t.Context())

	waitFor(t, func() bool { return r.calls.Load() >= 3 }, time.Second)
}

func TestLoop_StopsOnContextCancel(t *testing.T) {
	r := &fakeReconciler{ran: true}
	l := New(2*time.Millisecond, r)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()

	waitFor(t, func() bool { return r.calls.Load() >= 1 }, time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}

	// No further passes run once Run has returned.
	settled := r.calls.Load()
	time.Sleep(20 * time.Millisecond)
	if got := r.calls.Load(); got != settled {
		t.Errorf("expected no reconcile passes after cancel, got %d more", got-settled)
	}
}
