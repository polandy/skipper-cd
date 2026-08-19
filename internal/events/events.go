// Package events defines deploy event types and a broadcaster for real-time
// fan-out to multiple subscribers (e.g. SSE clients).
package events

import (
	"sync"
	"time"
)

// Status represents the current state of a deployment.
type Status string

// The statuses a deploy event can carry. The UI renders every one of them;
// only the terminal subset is notifiable (see the NotifyOn* values in
// internal/config).
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
	// StatusRemoved marks a stack that left the deploy set — its directory is
	// gone from the repo (or its entry from the host config). Nothing is torn
	// down: it records in the history *when* the stack stopped being managed,
	// against the commit that removed it, while its containers keep running and
	// surface in the Orphans section (ADR-0036 amendment). Emitted once, on the
	// first run that sees it gone.
	StatusRemoved Status = "removed"
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

// DriftedService names one service that had drifted from its running state when
// self-heal ran, with the degraded status (unhealthy/stopped) that triggered the
// corrective redeploy. Carried on healed events so the UI can show what the heal
// was reacting to (ADR-0029). Small enough to ride the SSE payload directly,
// unlike diffs — a heal has no diff to fetch on demand.
type DriftedService struct {
	Name   string `json:"name" yaml:"name"`
	Status string `json:"status" yaml:"status"`
}

// ServiceImageChange records that one service's image reference changed in the
// deploy an event describes, as old → new (name:tag[@digest]). An empty Old
// means the service had no previously deployed image (first deploy of the
// service, or state was reset); an empty New means it was removed from the
// stack. Services whose image is unchanged — and build: services with no image:
// ref — are not listed. Carried so a notification can name what actually
// updated, not just the stack; small enough to ride the SSE payload like
// HealDrift.
type ServiceImageChange struct {
	Service string `json:"service" yaml:"service"`
	Old     string `json:"old,omitempty" yaml:"old,omitempty"`
	New     string `json:"new,omitempty" yaml:"new,omitempty"`
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
	// HealDrift lists the services that had drifted when a self-heal ran; set only
	// on healed events. Unlike Diffs it is kept on the SSE payload (it is small and
	// there is no on-demand endpoint for it).
	HealDrift []DriftedService `json:"heal_drift,omitempty" yaml:"heal_drift,omitempty"`
	// ImageChanges lists the services whose image reference changed in this deploy
	// (old → new), set on deploy attempts so notifications and the UI can name what
	// updated. Kept on the SSE payload like HealDrift.
	ImageChanges []ServiceImageChange `json:"image_changes,omitempty" yaml:"image_changes,omitempty"`
	// HealthGated marks a deploy that ran under an effective deploy_health_check
	// — explicit config or one inferred from a compose healthcheck (ADR-0046).
	// On a success event it means the stack was verified healthy before the
	// deploy was called done, which is what a notification reports; on a failed
	// one it says only that the gate was in effect (the error names what failed).
	HealthGated bool `json:"health_gated,omitempty" yaml:"health_gated,omitempty"`
	// FollowsRollback marks a success that redeploys a stack whose previous
	// terminal outcome was a rollback — the retry of a rolled-back change. The
	// UI pairs the two (retry note, UI_SPEC.md "Rollback linkage") so the
	// success does not paper over the rollback it supersedes. Kept on the SSE
	// payload like HealDrift (small, no on-demand endpoint).
	FollowsRollback bool `json:"follows_rollback,omitempty" yaml:"follows_rollback,omitempty"`
	// RollbackEventID names the superseded rollback's event when it is still in
	// the bounded history; 0 when it has been evicted (the audit record remains).
	RollbackEventID int64 `json:"rollback_event_id,omitempty" yaml:"rollback_event_id,omitempty"`
	// RepeatCount is how many identical occurrences this event stands for when a
	// standing failure was re-emitted every reconcile and the repeats collapsed
	// into it (ADR-0056): >= 2 when collapsed, 0 for a single occurrence.
	// Timestamp is then the newest occurrence and FirstSeen the oldest.
	RepeatCount int `json:"repeat_count,omitempty" yaml:"repeat_count,omitempty"`
	// FirstSeen is when the collapsed run of identical outcomes started; zero
	// unless RepeatCount is set.
	FirstSeen time.Time `json:"first_seen,omitempty" yaml:"first_seen,omitempty"`
	// SupersedesID names the event this one replaced in the history when it
	// absorbed it as a repeat, so a UI holding the earlier event can drop it
	// instead of showing both. Zero unless this event collapsed a predecessor.
	SupersedesID int64 `json:"supersedes_id,omitempty" yaml:"supersedes_id,omitempty"`
}

// repeatableStatuses are the outcomes an unchanged, still-broken stack
// re-produces on every reconcile tick — the ones worth collapsing when they
// repeat verbatim (ADR-0056). Progress statuses are excluded on purpose: two
// successes in a row are two deploys, not one deploy seen twice.
var repeatableStatuses = map[Status]bool{
	StatusFailed:              true,
	StatusRolledBack:          true,
	StatusRolledBackUnhealthy: true,
	StatusHealExhausted:       true,
}

// Repeatable reports whether a status is one an unchanged, still-broken stack
// re-produces on every reconcile tick, and so may collapse when it repeats
// verbatim (ADR-0056).
func Repeatable(s Status) bool { return repeatableStatuses[s] }

// RepeatsOf reports whether e is the same standing outcome as prev — same
// stack, same repeatable status, same error text — and so should collapse into
// it rather than take a slot of its own.
func (e DeployEvent) RepeatsOf(prev DeployEvent) bool {
	return repeatableStatuses[e.Status] &&
		e.Stack == prev.Stack &&
		e.Status == prev.Status &&
		e.Error == prev.Error
}

// absorb returns e carrying prev's run: the occurrence count grows by one (or
// starts at two), FirstSeen keeps the oldest occurrence, and SupersedesID names
// the event prev was, so a UI can replace rather than append.
func (e DeployEvent) absorb(prev DeployEvent) DeployEvent {
	e.RepeatCount = prev.occurrences() + 1
	e.FirstSeen = earlier(prev.firstOccurrence(), e.Timestamp)
	e.Timestamp = later(prev.Timestamp, e.Timestamp)
	e.SupersedesID = prev.ID
	return e
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

// occurrences is how many identical outcomes an event stands for: 1 unless it
// already collapsed a run.
func (e DeployEvent) occurrences() int {
	if e.RepeatCount > 1 {
		return e.RepeatCount
	}
	return 1
}

// firstOccurrence is when an event's run started — its own timestamp unless it
// already collapsed a run.
func (e DeployEvent) firstOccurrence() time.Time {
	if !e.FirstSeen.IsZero() {
		return e.FirstSeen
	}
	return e.Timestamp
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
	Name string // SSE event name, one of the State* constants below
	Data any    // JSON-serializable payload
}

// StateEvent.Name values. Each names a UI-facing SSE stream published both on
// change and in the initial-state snapshot sent to new subscribers.
const (
	StateAutosync    = "autosync"
	StateQueue       = "queue"
	StateStacks      = "stacks"
	StateUpcoming    = "upcoming"
	StateHealth      = "health"
	StateHealthWatch = "healthwatch"
	StateOrphans     = "orphans"
	StateAppLinks    = "app_links"
	// StateHookRun is the hook a deploy is currently executing, zero when none
	// (ADR-0038).
	StateHookRun = "hookrun"
	// StatePeers is the merged multi-host read model — the primary's own label
	// plus each configured peer's reachability and last-known read data
	// (ADR-0048). Present only when peers are configured.
	StatePeers = "peers"
)

// AllStateNames is every state-event name a snapshot or the SSE stream may
// carry (the optional subsystems — health, orphans, app_links, healthwatch,
// peers — are only present when their component is enabled). Used to size the
// snapshot map and to assert coverage in tests.
var AllStateNames = []string{
	StateAutosync, StateQueue, StateStacks, StateUpcoming, StateHookRun,
	StateHealth, StateHealthWatch, StateOrphans, StateAppLinks, StatePeers,
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
