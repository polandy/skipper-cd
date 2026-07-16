package reconcile

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

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

func TestLoop_CallsReconcileOnInterval(t *testing.T) {
	var calls atomic.Int32
	l := New(2*time.Millisecond, func(context.Context) bool {
		calls.Add(1)
		return true
	})

	go l.Run(t.Context())

	waitFor(t, func() bool { return calls.Load() >= 3 }, time.Second)
}

func TestLoop_DisabledWhenIntervalNonPositive(t *testing.T) {
	var calls atomic.Int32
	l := New(0, func(context.Context) bool {
		calls.Add(1)
		return true
	})

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
	if got := calls.Load(); got != 0 {
		t.Errorf("expected no reconcile calls when disabled, got %d", got)
	}
}

func TestLoop_StopsOnContextCancel(t *testing.T) {
	var calls atomic.Int32
	l := New(2*time.Millisecond, func(context.Context) bool {
		calls.Add(1)
		return true
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()

	waitFor(t, func() bool { return calls.Load() >= 1 }, time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}

	// No further ticks fire once Run has returned.
	settled := calls.Load()
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != settled {
		t.Errorf("expected no reconcile calls after cancel, got %d more", got-settled)
	}
}
