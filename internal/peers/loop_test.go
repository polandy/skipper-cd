package peers

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakePoller records poll passes and serves a canned State, so a Loop can be
// tested without any HTTP peer.
type fakePoller struct {
	mu    sync.Mutex
	polls int
	state State
}

func (f *fakePoller) Poll(context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
}

func (f *fakePoller) State() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakePoller) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

// recordingPublisher collects every published State and signals each publish,
// so tests can wait for a pass deterministically instead of sleeping.
type recordingPublisher struct {
	mu        sync.Mutex
	published []State
	signal    chan struct{}
}

func newRecordingPublisher() *recordingPublisher {
	return &recordingPublisher{signal: make(chan struct{}, 64)}
}

func (p *recordingPublisher) publish(s State) {
	p.mu.Lock()
	p.published = append(p.published, s)
	p.mu.Unlock()
	p.signal <- struct{}{}
}

func (p *recordingPublisher) states() []State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]State(nil), p.published...)
}

// waitPublishes waits until n publish passes have been signalled.
func (p *recordingPublisher) waitPublishes(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-p.signal:
		case <-time.After(time.Second):
			t.Fatalf("publish %d of %d did not happen within 1s", i+1, n)
		}
	}
}

func TestNewLoop_FallsBackToDefaultIntervalWhenNonPositive(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		l := NewLoop(&fakePoller{}, interval, func(State) {})
		if got := l.Interval(); got != DefaultPollInterval {
			t.Errorf("NewLoop(%v).Interval() = %v, want %v", interval, got, DefaultPollInterval)
		}
	}
}

func TestNewLoop_KeepsPositiveInterval(t *testing.T) {
	l := NewLoop(&fakePoller{}, 7*time.Second, func(State) {})
	if got := l.Interval(); got != 7*time.Second {
		t.Errorf("Interval() = %v, want 7s", got)
	}
}

func TestLoop_PollsAndPublishesImmediately(t *testing.T) {
	// The first pass must not wait for the first tick — the UI gets a `peers`
	// state right at startup. A huge interval proves the pass ran tick-free.
	p := &fakePoller{state: State{Self: "primary"}}
	pub := newRecordingPublisher()
	l := NewLoop(p, time.Hour, pub.publish)

	go l.Run(t.Context())

	pub.waitPublishes(t, 1)
	if got := p.pollCount(); got < 1 {
		t.Fatalf("expected at least one poll before the first tick, got %d", got)
	}
	if states := pub.states(); states[0].Self != "primary" {
		t.Errorf("published state = %+v, want the poller's state", states[0])
	}
}

func TestLoop_PublishesAfterEveryPoll(t *testing.T) {
	p := &fakePoller{}
	pub := newRecordingPublisher()
	l := NewLoop(p, 2*time.Millisecond, pub.publish)

	go l.Run(t.Context())

	pub.waitPublishes(t, 3)
	if got := p.pollCount(); got < 3 {
		t.Errorf("expected at least 3 polls after 3 publishes, got %d", got)
	}
}

func TestLoop_StopsOnContextCancel(t *testing.T) {
	p := &fakePoller{}
	pub := newRecordingPublisher()
	l := NewLoop(p, 2*time.Millisecond, pub.publish)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()

	pub.waitPublishes(t, 1)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
