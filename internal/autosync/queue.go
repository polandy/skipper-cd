package autosync

import (
	"sort"
	"sync"
	"time"
)

// PendingItem is one deferred deploy waiting for autosync to resume.
type PendingItem struct {
	Stack        string    `json:"stack"`
	ChangedFiles []string  `json:"changed_files"`
	Reason       string    `json:"reason"`
	Since        time.Time `json:"since"`
}

// Queue is the in-memory registry of deploys deferred while sync is paused. It
// mirrors the deploy hash state (the source of truth for what redeploys) so the
// UI can show a count and an ordered list; it is derived state and is never
// persisted. See docs/autosync.md.
type Queue struct {
	mu      sync.Mutex
	pending map[string]PendingItem
	now     func() time.Time
}

// NewQueue returns an empty Queue.
func NewQueue() *Queue {
	return &Queue{pending: make(map[string]PendingItem), now: time.Now}
}

// Mark records (or refreshes) a deferred deploy for the stack. The changed files
// and reason are updated; the original queued time (Since) is preserved across
// repeated marks so "waiting for" reflects the first deferral.
func (q *Queue) Mark(stack string, changed []string, reason string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	since := q.now()
	if prev, ok := q.pending[stack]; ok {
		since = prev.Since
	}
	q.pending[stack] = PendingItem{
		Stack:        stack,
		ChangedFiles: append([]string(nil), changed...),
		Reason:       reason,
		Since:        since,
	}
}

// Clear removes any pending entry for the stack. Safe when none exists.
func (q *Queue) Clear(stack string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, stack)
}

// Count returns the number of pending stacks.
func (q *Queue) Count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Snapshot returns the pending items ordered by the given deploy order. Stacks
// listed in order come first (in that order); any pending stack not in order
// follows, sorted by name, so nothing is silently dropped.
func (q *Queue) Snapshot(order []string) []PendingItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]PendingItem, 0, len(q.pending))
	seen := make(map[string]bool, len(q.pending))
	for _, name := range order {
		if it, ok := q.pending[name]; ok {
			out = append(out, it)
			seen[name] = true
		}
	}
	rest := make([]string, 0)
	for name := range q.pending {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		out = append(out, q.pending[name])
	}
	return out
}
