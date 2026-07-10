// Package logbuf provides a bounded in-memory ring of log entries with
// live fan-out to subscribers (e.g. SSE clients).
//
// Nothing in this package may call slog: the tee Handler routes every slog
// record here, so a slog call from inside logbuf would recurse through the
// handler forever.
package logbuf

import (
	"sync"
	"time"
)

// DefaultCapacity is the number of log lines kept in memory.
const DefaultCapacity = 1000

// subscriberBuffer is the channel buffer per subscriber. It is larger than
// the deploy-event buffer because child-process output arrives in bursts.
const subscriberBuffer = 256

// Entry is one captured log line.
type Entry struct {
	ID    int64             `json:"id"`
	Time  time.Time         `json:"time"`
	Level string            `json:"level"` // slog canonical: DEBUG/INFO/WARN/ERROR
	Msg   string            `json:"msg"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Log is a bounded ring of log entries combined with a broadcaster. One
// mutex covers both so a subscriber can atomically subscribe and snapshot
// the backlog without missing or duplicating entries in between.
type Log struct {
	mu       sync.Mutex
	capacity int
	entries  []Entry
	subs     map[uint64]chan Entry
	nextSub  uint64
	nextID   int64
}

// New returns a Log holding at most capacity entries. IDs are seeded from
// time.Now().UnixMilli() so IDs from a restarted process are always larger
// than any ID a reconnecting SSE client may present as Last-Event-ID
// (there is no persistence to restore the counter from).
func New(capacity int) *Log {
	return &Log{
		capacity: capacity,
		subs:     make(map[uint64]chan Entry),
		nextID:   time.Now().UnixMilli(),
	}
}

// Append stores an entry (assigning the next ID), evicts the oldest entry
// beyond capacity, and publishes to all subscribers. Publishing is
// non-blocking: slow subscribers have entries dropped.
func (l *Log) Append(t time.Time, level, msg string, attrs map[string]string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e := Entry{ID: l.nextID, Time: t, Level: level, Msg: msg, Attrs: attrs}
	l.nextID++

	l.entries = append(l.entries, e)
	if len(l.entries) > l.capacity {
		l.entries = l.entries[len(l.entries)-l.capacity:]
	}

	for _, ch := range l.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// ChildLine records one line of child-process output at level INFO with
// cmd and stream attrs. It implements command.LineSink.
func (l *Log) ChildLine(cmd, stream, line string) {
	l.Append(time.Now(), "INFO", line, map[string]string{"cmd": cmd, "stream": stream})
}

// Entries returns a copy of the buffered entries, oldest first.
func (l *Log) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// EntriesAfter returns a copy of the buffered entries with ID > afterID,
// oldest first (SSE reconnect via Last-Event-ID).
func (l *Log) EntriesAfter(afterID int64) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Entry
	for _, e := range l.entries {
		if e.ID > afterID {
			out = append(out, e)
		}
	}
	return out
}

// Subscribe returns a buffered channel of live entries and an unsubscribe
// function. Subscribing and then snapshotting via Entries sees every entry
// at least once; consumers dedupe overlap by ID.
func (l *Log) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, subscriberBuffer)
	l.mu.Lock()
	id := l.nextSub
	l.nextSub++
	l.subs[id] = ch
	l.mu.Unlock()

	return ch, func() {
		l.mu.Lock()
		delete(l.subs, id)
		l.mu.Unlock()
	}
}
