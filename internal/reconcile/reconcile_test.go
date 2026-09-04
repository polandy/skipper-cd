package reconcile

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// failTimeout only stops a broken test from hanging forever. No assertion
// depends on it elapsing: every wait below blocks on a signal the loop sends.
const failTimeout = 5 * time.Second

// fakeReconciler counts passes and signals each one, so a test can drive an
// exact number of passes and observe every one of them without a
// git/docker/deploy dependency — and without waiting on a clock. The count is
// exact; the signal is best-effort, so a pass is never blocked by an unread
// one.
type fakeReconciler struct {
	passes atomic.Int32
	signal chan struct{}
	ran    bool // what Reconcile reports (false = "deploy already in progress")
}

func newFakeReconciler(ran bool) *fakeReconciler {
	return &fakeReconciler{signal: make(chan struct{}, 64), ran: ran}
}

func (f *fakeReconciler) Reconcile(context.Context) bool {
	f.passes.Add(1)
	select {
	case f.signal <- struct{}{}:
	default:
	}
	return f.ran
}

// awaitPass blocks until the loop reports one completed pass.
func (f *fakeReconciler) awaitPass(t *testing.T) {
	t.Helper()
	select {
	case <-f.signal:
	case <-time.After(failTimeout):
		t.Fatal("the loop did not run a reconcile pass")
	}
}

// runLoop starts l in its own goroutine and returns a stop function that
// cancels it and waits for it to return — the settled state an "and no more
// passes" assertion is read off.
func runLoop(t *testing.T, start func(context.Context)) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		start(ctx)
		close(done)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(failTimeout):
			t.Fatal("the loop did not return after its context was cancelled")
		}
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
	l := New(time.Hour, r)
	stop := runLoop(t, func(ctx context.Context) { l.run(ctx, ticks) })

	for range 3 {
		tick(t, ticks, r)
	}
	// Stopping settles the count, so "and no more than three" is read off a
	// finished goroutine rather than waited for.
	stop()

	if got := r.passes.Load(); got != 3 {
		t.Errorf("three ticks must cause exactly three passes, got %d", got)
	}
}

func TestLoop_KeepsTickingWhenPassIsSkipped(t *testing.T) {
	// A Reconciler that always skips (a deploy is always in progress) must not
	// stop the loop — the next tick tries again.
	r := newFakeReconciler(false)
	ticks := make(chan time.Time)
	l := New(time.Hour, r)
	stop := runLoop(t, func(ctx context.Context) { l.run(ctx, ticks) })

	for range 3 {
		tick(t, ticks, r)
	}
	stop()

	if got := r.passes.Load(); got != 3 {
		t.Errorf("a skipped pass must not stop the loop; want 3 passes, got %d", got)
	}
}

func TestLoop_StopsOnContextCancel(t *testing.T) {
	r := newFakeReconciler(true)
	ticks := make(chan time.Time)
	l := New(time.Hour, r)
	stop := runLoop(t, func(ctx context.Context) { l.run(ctx, ticks) })

	tick(t, ticks, r)
	stop()

	if got := r.passes.Load(); got != 1 {
		t.Errorf("no pass may run after cancel; want 1 pass, got %d", got)
	}
}

func TestLoop_RunDrivesPassesOnItsOwnTicker(t *testing.T) {
	// Run's own ticker, rather than injected ticks. The interval is short to
	// keep the test quick, but nothing is timed: it blocks until the pass the
	// ticker causes actually arrives.
	r := newFakeReconciler(true)
	l := New(time.Millisecond, r)
	stop := runLoop(t, l.Run)

	r.awaitPass(t)
	stop()

	if got := r.passes.Load(); got < 1 {
		t.Errorf("Run must drive passes on its own ticker, got %d", got)
	}
}

func TestLoop_DisabledWhenIntervalNonPositive(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		r := newFakeReconciler(true)
		// Called synchronously: a disabled loop must return by itself. If it
		// ever started ticking, this test hangs rather than passing by luck.
		New(interval, r).Run(t.Context())

		if got := r.passes.Load(); got != 0 {
			t.Errorf("interval %v must disable the loop, got %d passes", interval, got)
		}
	}
}
