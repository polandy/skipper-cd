package reconcile

import (
	"context"
	"testing"
	"time"
)

// failTimeout only stops a broken test from hanging forever. No assertion
// depends on it elapsing: every wait below blocks on a signal the loop sends.
const failTimeout = 5 * time.Second

// fakeReconciler signals each pass on a channel and returns a fixed result, so
// a test can drive an exact number of passes and observe every one of them
// without a git/docker/deploy dependency — and without waiting on a clock.
type fakeReconciler struct {
	passes chan struct{}
	ran    bool // what Reconcile reports (false = "deploy already in progress")
}

func newFakeReconciler(ran bool) *fakeReconciler {
	return &fakeReconciler{passes: make(chan struct{}, 64), ran: ran}
}

func (f *fakeReconciler) Reconcile(context.Context) bool {
	f.passes <- struct{}{}
	return f.ran
}

// awaitPass blocks until the loop reports one completed pass.
func (f *fakeReconciler) awaitPass(t *testing.T) {
	t.Helper()
	select {
	case <-f.passes:
	case <-time.After(failTimeout):
		t.Fatal("the loop did not run a reconcile pass")
	}
}

// tick delivers one tick and waits for the pass it causes, so the test and the
// loop stay in lockstep instead of racing.
func tick(t *testing.T, ticks chan<- time.Time, r *fakeReconciler) {
	t.Helper()
	ticks <- time.Now()
	r.awaitPass(t)
}

func TestLoop_RunsOnePassPerTick(t *testing.T) {
	r := newFakeReconciler(true)
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		New(time.Hour, r).run(ctx, ticks)
		close(done)
	}()

	// Each tick is awaited, so three ticks are proven to cause at least three
	// passes.
	for range 3 {
		tick(t, ticks, r)
	}

	// Stopping the loop settles the count, so "and no more than three" is read
	// off a finished goroutine rather than waited for.
	cancel()
	select {
	case <-done:
	case <-time.After(failTimeout):
		t.Fatal("run did not return after the context was cancelled")
	}
	if got := len(r.passes); got != 0 {
		t.Errorf("three ticks must cause exactly three passes, got %d extra", got)
	}
}

func TestLoop_KeepsTickingWhenPassIsSkipped(t *testing.T) {
	// A Reconciler that always skips (a deploy is always in progress) must not
	// stop the loop — the next tick tries again.
	r := newFakeReconciler(false)
	ticks := make(chan time.Time)
	go New(time.Hour, r).run(t.Context(), ticks)

	for range 3 {
		tick(t, ticks, r)
	}
}

func TestLoop_StopsOnContextCancel(t *testing.T) {
	r := newFakeReconciler(true)
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		New(time.Hour, r).run(ctx, ticks)
		close(done)
	}()

	tick(t, ticks, r)
	cancel()

	select {
	case <-done:
	case <-time.After(failTimeout):
		t.Fatal("run did not return after the context was cancelled")
	}

	// run has returned, so the pass count is settled: no further pass can be
	// recorded, and the assertion needs no waiting to prove it.
	if got := len(r.passes); got != 0 {
		t.Errorf("no pass may run after cancel, got %d", got)
	}
}

func TestLoop_DisabledWhenIntervalNonPositive(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		r := newFakeReconciler(true)
		// Called synchronously: a disabled loop must return by itself. If it
		// ever started ticking, this test hangs rather than passing by luck.
		New(interval, r).Run(t.Context())

		if got := len(r.passes); got != 0 {
			t.Errorf("interval %v must disable the loop, got %d passes", interval, got)
		}
	}
}
