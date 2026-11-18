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

// DefaultCapacity is the number of log lines kept in memory. It matches the
// web UI's own client-side buffer, so a reload replays the whole window a
// viewer could have scrolled through rather than half of it.
const DefaultCapacity = 2000

// subscriberBuffer is the channel buffer per subscriber. It is larger than
// the deploy-event buffer because child-process output arrives in bursts.
const subscriberBuffer = 256

// pinnedCap bounds the separate retention set for deploy-outcome lines. It is
// deliberately small: outcome lines are rare (one per deploy attempt), so 50
// covers far more runs than the ring itself ever holds.
const pinnedCap = 50

// msgRunComplete is the run summary's message; it needs its own pin rule
// (isPinnedOutcome) because most of its occurrences are routine.
const msgRunComplete = "run complete"

// outcomeMessages are the log messages that say how a deploy ended — the one
// line a viewer must still find after the child-process burst of later runs
// has rolled the ring over (UI_SPEC.md, Log API). The message text is
// duplicated here from internal/deploy et al. by hand, following the same rule
// as internal/prettylog's anchor table: a capture/display layer must not gain
// influence over the core packages' log wording. A message that drifts simply
// stops being pinned; it is never dropped from the ring.
var outcomeMessages = map[string]bool{
	"deploy complete":               true,
	"deploy failed":                 true,
	"deploy failed but rolled back": true,
	"deploy failed, rollback ran but stack is still unhealthy":           true,
	"self-heal: stack restored":                                          true,
	"self-heal exhausted: stack still degraded after repeated redeploys": true,
	msgRunComplete: true,
}

// runOutcomeCounters are the run-summary attrs that mark a run as having a
// real outcome. A summary where all of them are zero — the periodic
// reconcile's no-op line, one per tick — is routine and must not be pinned:
// at that rate it would fill the small pinned set within hours and evict
// exactly the outcomes the exemption exists to keep.
var runOutcomeCounters = []string{"deployed", "failed", "rolled_back", "rolled_back_unhealthy"}

// isPinnedOutcome reports whether a line is retained past ring eviction: any
// terminal outcome line, except a run summary whose outcome counters are all
// zero. Attrs arrive stringified from the slog tee, so "nonzero" is any value
// other than empty or "0".
func isPinnedOutcome(msg string, attrs map[string]string) bool {
	if !outcomeMessages[msg] {
		return false
	}
	if msg != msgRunComplete {
		return true
	}
	for _, k := range runOutcomeCounters {
		if v := attrs[k]; v != "" && v != "0" {
			return true
		}
	}
	return false
}

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
//
// Deploy-outcome lines (isPinnedOutcome) are exempt from ring eviction: they
// are additionally retained in a small pinned set and merged back into the
// replay in chronological position, so the outcome of a rollback survives the
// output bursts that follow it.
type Log struct {
	mu       sync.Mutex
	capacity int
	entries  []Entry
	pinned   []Entry // outcome lines, chronological, bounded by pinnedCap
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
	if isPinnedOutcome(msg, attrs) {
		l.pinned = append(l.pinned, e)
		if len(l.pinned) > pinnedCap {
			l.pinned = l.pinned[len(l.pinned)-pinnedCap:]
		}
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
func (l *Log) ChildLine(cmd, stream, line, stack string) {
	attrs := map[string]string{"cmd": cmd, "stream": stream}
	if stack != "" { // deploy hooks attribute output to a stack (ADR-0038); docker/git don't
		attrs["stack"] = stack
	}
	l.Append(time.Now(), "INFO", line, attrs)
}

// Entries returns a copy of the buffered entries, oldest first, with evicted
// outcome lines merged back in chronological position.
func (l *Log) Entries() []Entry {
	return l.EntriesAfter(0)
}

// EntriesAfter returns a copy of the buffered entries with ID > afterID,
// oldest first (SSE reconnect via Last-Event-ID), with evicted outcome lines
// merged back in chronological position.
func (l *Log) EntriesAfter(afterID int64) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()

	// A pinned entry still inside the ring would duplicate; IDs are assigned
	// monotonically, so "evicted" is exactly "older than the ring's oldest".
	ringStart := l.nextID // one past the newest — an empty ring keeps every pinned line
	if len(l.entries) > 0 {
		ringStart = l.entries[0].ID
	}
	var out []Entry
	for _, e := range l.pinned {
		if e.ID < ringStart && e.ID > afterID {
			out = append(out, e)
		}
	}
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
