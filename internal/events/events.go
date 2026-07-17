// Package events defines deploy event types and a broadcaster for real-time
// fan-out to multiple subscribers (e.g. SSE clients).
package events

import (
	"sync"
	"time"
)

// Status represents the current state of a deployment.
type Status string

const (
	StatusDeploying  Status = "deploying"
	StatusSuccess    Status = "success"
	StatusFailed     Status = "failed"
	StatusSkipped    Status = "skipped"
	StatusRolledBack Status = "rolled_back"
	// StatusRolledBackUnhealthy marks a failed deploy whose rollback ran, but
	// whose restored version also failed the health gate (ADR-0022): the stack
	// is back on the old compose file yet not verified healthy.
	StatusRolledBackUnhealthy Status = "rolled_back_unhealthy"
	// StatusQueued marks a deploy deferred because autosync is paused; the
	// change waits and deploys when sync resumes. See docs/autosync.md.
	StatusQueued Status = "queued"
	// StatusHealed marks a corrective redeploy that self-heal ran to restore a
	// stack that had drifted from its deployed running state (a stopped/removed
	// container, an unhealthy service). It is not a git deploy — the desired
	// version is unchanged — so it carries no changed files or diffs (ADR-0029).
	StatusHealed Status = "healed"
	// StatusHealExhausted marks a stack that self-heal gave up on: repeated
	// corrective redeploys did not restore it, so skipper stopped trying and
	// left it reported unhealthy. Emitted once per outage, not per interval, and
	// is the high-signal "a stack is down and I could not fix it" notification
	// (ADR-0029).
	StatusHealExhausted Status = "heal_exhausted"
	// StatusBlocked marks a changed stack that did not deploy because a
	// depends_on dependency failed (or was itself blocked) in the same run
	// (ADR-0032). Like a queued stack it stays dirty and retries on the next
	// sync; it is deliberately not a notification status (the dependency's own
	// failure already pages, and this recurs on every reconcile tick).
	StatusBlocked Status = "blocked"
)

// CommitInfo describes one git commit deployed by an event: the metadata
// shown above the file diffs (message, author, time, SHA). Populated for the
// range state.LastDeployedCommit..HEAD, restricted to the event's changed
// files, and — like Diffs — carried on the event but fetched on demand rather
// than streamed over SSE.
type CommitInfo struct {
	SHA     string    `json:"sha" yaml:"sha"`
	Subject string    `json:"subject" yaml:"subject"`
	Author  string    `json:"author" yaml:"author"`
	Date    time.Time `json:"date" yaml:"date"`
}

// DeployEvent represents a single deployment status change.
type DeployEvent struct {
	ID           int64             `json:"id" yaml:"id"`
	Timestamp    time.Time         `json:"timestamp" yaml:"timestamp"`
	Stack        string            `json:"stack" yaml:"stack"`
	Status       Status            `json:"status" yaml:"status"`
	DurationMs   int64             `json:"duration_ms,omitempty" yaml:"duration_ms,omitempty"`
	Error        string            `json:"error,omitempty" yaml:"error,omitempty"`
	ChangedFiles []string          `json:"changed_files,omitempty" yaml:"changed_files,omitempty"`
	Diffs        map[string]string `json:"diffs,omitempty" yaml:"diffs,omitempty"`
	Commits      []CommitInfo      `json:"commits,omitempty" yaml:"commits,omitempty"`
	HasDiffs     bool              `json:"has_diffs,omitempty" yaml:"-"`
}

// SSEPayload returns a copy suitable for SSE streaming: diffs and commits are
// stripped (fetched on demand via /api/events/{id}/diffs) and HasDiffs is set
// so the UI knows the detail panel is available.
func (e DeployEvent) SSEPayload() DeployEvent {
	e.HasDiffs = len(e.Diffs) > 0
	e.Diffs = nil
	e.Commits = nil
	return e
}

// StateEvent is a non-deploy SSE message — an autosync or queue snapshot pushed
// to UI clients over the same /api/events stream under its own event name.
type StateEvent struct {
	Name string // SSE event name, e.g. "autosync" or "queue"
	Data any    // JSON-serializable payload
}

// Broadcaster fans out values of type T to all connected subscribers.
// Sends are non-blocking: slow subscribers have values dropped.
type Broadcaster[T any] struct {
	mu   sync.RWMutex
	subs map[uint64]chan T
	next uint64
}

// NewBroadcaster creates a ready-to-use deploy-event Broadcaster.
func NewBroadcaster() *Broadcaster[DeployEvent] { return newBroadcaster[DeployEvent]() }

// NewStateBroadcaster creates a ready-to-use Broadcaster for StateEvents.
func NewStateBroadcaster() *Broadcaster[StateEvent] { return newBroadcaster[StateEvent]() }

func newBroadcaster[T any]() *Broadcaster[T] {
	return &Broadcaster[T]{subs: make(map[uint64]chan T)}
}

// Subscribe returns a channel that receives broadcast values and an
// unsubscribe function. The channel is buffered (16) so slow readers
// miss values rather than blocking the broadcaster.
func (b *Broadcaster[T]) Subscribe() (<-chan T, func()) {
	ch := make(chan T, 16)
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
	}
}

// HasSubscribers reports whether at least one subscriber is currently
// connected. It lets a producer skip work that no one is watching — e.g. the
// stack-health poller idles while no UI client is subscribed (ADR-0027).
func (b *Broadcaster[T]) HasSubscribers() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs) > 0
}

// Publish sends a value to all subscribers. Non-blocking: if a
// subscriber's buffer is full the value is dropped for that subscriber.
func (b *Broadcaster[T]) Publish(event T) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
