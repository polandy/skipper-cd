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
	StatusDeploying Status = "deploying"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
)

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
	HasDiffs     bool              `json:"has_diffs,omitempty" yaml:"-"`
}

// SSEPayload returns a copy suitable for SSE streaming: diffs are stripped
// and HasDiffs is set so the UI knows diffs are available on demand.
func (e DeployEvent) SSEPayload() DeployEvent {
	e.HasDiffs = len(e.Diffs) > 0
	e.Diffs = nil
	return e
}

// Broadcaster fans out DeployEvents to all connected subscribers.
// Sends are non-blocking: slow subscribers have events dropped.
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[uint64]chan DeployEvent
	next uint64
}

// NewBroadcaster creates a ready-to-use Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: make(map[uint64]chan DeployEvent)}
}

// Subscribe returns a channel that receives broadcast events and an
// unsubscribe function. The channel is buffered (16) so slow readers
// miss events rather than blocking the broadcaster.
func (b *Broadcaster) Subscribe() (<-chan DeployEvent, func()) {
	ch := make(chan DeployEvent, 16)
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

// Publish sends an event to all subscribers. Non-blocking: if a
// subscriber's buffer is full the event is dropped for that subscriber.
func (b *Broadcaster) Publish(event DeployEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
