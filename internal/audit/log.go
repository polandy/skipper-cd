// Package audit keeps a durable, per-stack log of terminal deploy outcomes —
// the "what happened to this stack, and when" trail that the bounded, global
// live-event ring (internal/events.History) cannot answer once records roll off
// its 100-event window. Records are compact metadata (no diffs); see ADR-0033.
package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/fsatomic"
)

const (
	// DefaultPerStackCap bounds how many recent records are kept per stack, so a
	// busy stack can never evict a quiet one's history (the flaw of a global cap).
	DefaultPerStackCap = 200
	auditFileName      = "deploy-audit.jsonl"
	// compactionSlack lets the on-disk log grow a little past the live record
	// count before it is rewritten, so compaction is amortised rather than run
	// on every append.
	compactionSlack = 8
)

// auditableStatuses are the terminal deploy outcomes worth an audit record.
// In-progress (deploying), no-op (skipped) and deferral (queued, blocked)
// statuses are excluded: they are live-feed concerns, not outcomes.
var auditableStatuses = map[events.Status]bool{
	events.StatusSuccess:             true,
	events.StatusFailed:              true,
	events.StatusRolledBack:          true,
	events.StatusRolledBackUnhealthy: true,
	events.StatusHealed:              true,
	events.StatusHealExhausted:       true,
	// A stack leaving the deploy set is not a deploy, but it is the durable
	// answer to "when did this stop being managed" — and the one record that
	// survives the bounded event ring if the stack ever comes back.
	events.StatusRemoved: true,
}

// Record is one terminal deploy outcome for one stack: metadata only, no diffs.
// The commit SHA lets the change be reconstructed from git if ever needed.
type Record struct {
	Stack     string        `json:"stack"`
	Timestamp time.Time     `json:"timestamp"`
	Status    events.Status `json:"status"`
	// ID is the originating deploy event's id, carried so a consumer can fetch
	// that deploy's diff via /api/events/{id}/diffs — notably the multi-host
	// fan-in, where the primary proxies a peer's diff on demand (ADR-0048). The
	// event history is persisted and bounded, so the id stays valid across
	// restarts until the event is evicted.
	ID           int64  `json:"id,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	ChangedFiles int    `json:"changed_files,omitempty"`
	Error        string `json:"error,omitempty"`
	// RepeatCount is how many identical occurrences this record stands for when
	// a standing failure repeated every reconcile and the repeats collapsed into
	// it (ADR-0056): >= 2 when collapsed, 0 for a single occurrence. Timestamp is
	// then the newest occurrence and FirstSeen the oldest.
	RepeatCount int `json:"repeat_count,omitempty"`
	// FirstSeen is when the collapsed run started; zero unless RepeatCount is set.
	FirstSeen time.Time `json:"first_seen,omitzero"`
}

// repeatsOf reports whether r merely repeats prev — the same standing outcome
// with the same error — and so should collapse into it (ADR-0056). Callers
// compare within one stack's records, so the stack matches by construction.
func (r Record) repeatsOf(prev Record) bool {
	return events.Repeatable(r.Status) && r.Status == prev.Status && r.Error == prev.Error
}

// absorb returns r carrying prev's run: one more occurrence, and the oldest
// FirstSeen of the two.
func (r Record) absorb(prev Record) Record {
	r.RepeatCount = prev.occurrences() + 1
	r.FirstSeen = earlier(prev.firstOccurrence(), r.Timestamp)
	r.Timestamp = later(prev.Timestamp, r.Timestamp)
	return r
}

// earlier and later keep a collapsed run's span honest regardless of the order
// occurrences arrived in — a reloaded log can hold them out of order.
func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// occurrences is how many identical outcomes a record stands for.
func (r Record) occurrences() int {
	if r.RepeatCount > 1 {
		return r.RepeatCount
	}
	return 1
}

// firstOccurrence is when a record's run started.
func (r Record) firstOccurrence() time.Time {
	if !r.FirstSeen.IsZero() {
		return r.FirstSeen
	}
	return r.Timestamp
}

// collapseRuns folds each run of identical repeated outcomes in a
// chronological slice into its newest record. Idempotent: a slice the collapse
// already produced holds no adjacent repeats and passes through unchanged.
func collapseRuns(recs []Record) []Record {
	out := make([]Record, 0, len(recs))
	for _, r := range recs {
		if n := len(out); n > 0 && r.repeatsOf(out[n-1]) {
			out[n-1] = r.absorb(out[n-1])
			continue
		}
		out = append(out, r)
	}
	return out
}

// Log is a thread-safe, per-stack bounded audit trail with NDJSON persistence.
// Records are held in memory indexed by stack; the file is the durable backing
// store, kept bounded by periodic compaction. Appends are the normal path — a
// record that collapses a repeat (ADR-0056) rewrites the file instead, since it
// supersedes a line already written.
type Log struct {
	mu          sync.RWMutex
	byStack     map[string][]Record // per stack, chronological (oldest first)
	filePath    string
	perStackCap int
	diskLines   int // approximate line count of the backing file
}

// NewLog loads any existing audit log from stateDir and returns a ready Log.
// An empty stateDir disables persistence (in-memory only).
func NewLog(stateDir string) *Log { return newLog(stateDir, DefaultPerStackCap) }

func newLog(stateDir string, perStackCap int) *Log {
	l := &Log{byStack: map[string][]Record{}, perStackCap: perStackCap}
	if stateDir != "" {
		l.filePath = filepath.Join(stateDir, auditFileName)
		l.load()
	}
	return l
}

// Record appends the event as an audit record when its status is a terminal
// outcome; other statuses are ignored. Best-effort persistence: a failed write
// never blocks or fails a deploy.
func (l *Log) Record(e events.DeployEvent) {
	if !auditableStatuses[e.Status] {
		return
	}
	rec := recordFromEvent(e)

	l.mu.Lock()
	defer l.mu.Unlock()

	// A record that only repeats this stack's last outcome replaces it instead
	// of taking a slot, so a standing failure cannot evict the stack's real
	// deploy history (ADR-0056).
	recs := l.byStack[rec.Stack]
	collapsed := false
	if n := len(recs); n > 0 && rec.repeatsOf(recs[n-1]) {
		rec = rec.absorb(recs[n-1])
		recs = recs[:n-1]
		collapsed = true
	}
	recs = append(recs, rec)
	if over := len(recs) - l.perStackCap; over > 0 {
		recs = recs[over:] // drop this stack's oldest, not another stack's
	}
	l.byStack[rec.Stack] = recs

	if collapsed {
		// The superseded line is still on disk; only a rewrite can drop it, and
		// without one the file would grow per repeat exactly as before.
		l.compact()
		return
	}
	l.appendLine(rec)
}

// recordFromEvent projects a DeployEvent onto a compact audit record.
func recordFromEvent(e events.DeployEvent) Record {
	r := Record{
		Stack:        e.Stack,
		Timestamp:    e.Timestamp,
		Status:       e.Status,
		ID:           e.ID,
		DurationMs:   e.DurationMs,
		ChangedFiles: len(e.ChangedFiles),
		Error:        e.Error,
	}
	if len(e.Commits) > 0 {
		r.CommitSHA = e.Commits[0].SHA // newest (HEAD) — git log is newest-first
	}
	return r
}

// Stack returns one stack's records, newest first. limit <= 0 returns all.
func (l *Log) Stack(name string, limit int) []Record {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return newestFirst(l.byStack[name], limit)
}

// Recent returns records across all stacks, newest first. limit <= 0 returns all.
func (l *Log) Recent(limit int) []Record {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var all []Record
	for _, recs := range l.byStack {
		all = append(all, recs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Timestamp.After(all[j].Timestamp) })
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return all
}

// newestFirst reverses a chronological slice and applies an optional limit.
func newestFirst(recs []Record, limit int) []Record {
	count := len(recs)
	if limit > 0 && limit < count {
		count = limit
	}
	out := make([]Record, 0, count)
	for i := len(recs) - 1; i >= 0 && len(out) < count; i-- {
		out = append(out, recs[i])
	}
	return out
}

// --- persistence (caller holds l.mu) ---

// appendLine writes one record to the backing file and compacts when the file
// has grown too far past the live record count.
func (l *Log) appendLine(rec Record) {
	if l.filePath == "" {
		return
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fsatomic.PrivateFileMode)
	if err != nil {
		return
	}
	if _, err := f.Write(append(line, '\n')); err == nil {
		l.diskLines++
	}
	f.Close()

	if l.diskLines > 2*l.totalLocked()+compactionSlack {
		l.compact()
	}
}

// compact rewrites the file from the (already capped) in-memory state, globally
// time-sorted, so the log stays bounded and chronological. Atomic via rename.
func (l *Log) compact() {
	if l.filePath == "" {
		return
	}
	all := make([]Record, 0, l.totalLocked())
	for _, recs := range l.byStack {
		all = append(all, recs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Timestamp.Before(all[j].Timestamp) })

	var buf bytes.Buffer
	for _, r := range all {
		line, err := json.Marshal(r)
		if err != nil {
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := fsatomic.WriteFile(l.filePath, buf.Bytes(), fsatomic.PrivateFileMode); err != nil {
		return
	}
	l.diskLines = len(all)
}

// load reads the backing file, skipping any torn line (e.g. a partial trailing
// record from a crash mid-append), trims each stack to cap, then compacts so the
// on-disk log reflects the capped state.
func (l *Log) load() {
	data, err := os.ReadFile(l.filePath)
	if err != nil {
		return // no file yet, or unreadable — start empty
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue // skip malformed / torn line
		}
		l.byStack[r.Stack] = append(l.byStack[r.Stack], r)
	}
	for s, recs := range l.byStack {
		// Fold runs first: a log written before the collapse existed holds a past
		// flood as separate lines, and folding it frees the cap for real deploys.
		recs = collapseRuns(recs)
		if over := len(recs) - l.perStackCap; over > 0 {
			recs = recs[over:]
		}
		l.byStack[s] = recs
	}
	l.compact()
}

func (l *Log) totalLocked() int {
	n := 0
	for _, recs := range l.byStack {
		n += len(recs)
	}
	return n
}
