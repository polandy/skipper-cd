// Held changes (ADR-0062): a change whose new version failed after it started
// is not retried on every reconcile tick. It waits, recorded in state.yaml,
// until the stack's inputs change (a new push) or an operator asks for one
// more attempt. A change that failed before it started is first retried with
// a growing wait, recorded the same way, and held once its attempts are used
// up (ADR-0064).

package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"maps"
	"sort"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

const (
	// preStartHoldAfter is how many consecutive failures before start, with
	// the same inputs, hold a change (ADR-0064).
	preStartHoldAfter = 3
	// preStartBackoffBase is the wait after the first failure before start;
	// each further failure doubles it.
	preStartBackoffBase = 5 * time.Minute
	// backoffTolerance lets a run that starts slightly before the next attempt
	// is due make it anyway. Reconcile ticks are as far apart as the base wait,
	// and the stack's turn in a run moves by seconds from run to run; without
	// it, whether the first retry comes after one tick or two would be chance.
	backoffTolerance = time.Minute
)

// ErrHeld is what deployStackIfChanged returns for a stack whose pending change
// is held. It is not a deploy failure — no failure event is emitted — but it
// blocks changed dependents, since the stack's change did not deploy.
var ErrHeld = errors.New("change held: its new version failed after it started")

// heldChange is one stack's hold, as persisted in state.yaml.
type heldChange struct {
	// Fingerprint identifies the exact inputs that failed (fingerprint of the
	// stack's tracked-file hashes). Any change to them releases the hold.
	Fingerprint string `yaml:"fingerprint"`
	// Since is when the change failed and the hold began.
	Since time.Time `yaml:"since"`
	// Status is the failure's event status (rolled_back, rolled_back_unhealthy,
	// failed with rollback disabled).
	Status events.Status `yaml:"status"`
	// Commit is the newest commit the failed change carried, when known.
	Commit string `yaml:"commit,omitempty"`
	// Attempts counts the consecutive failures before start with these
	// inputs; 0 when the new version failed after it started.
	Attempts int `yaml:"attempts,omitempty"`
	// RetryAt, when set, is when a change that failed before it started is
	// attempted again: a backoff rather than a hold. Zero holds until a push,
	// a retry, a revert or the stack's removal.
	RetryAt time.Time `yaml:"retry_at,omitempty"`
}

// backingOff reports whether the hold lifts on its own at RetryAt.
func (h heldChange) backingOff() bool {
	return !h.RetryAt.IsZero()
}

// due reports whether a backing-off change may be attempted again at now.
func (h heldChange) due(now time.Time) bool {
	return h.backingOff() && !now.Add(backoffTolerance).Before(h.RetryAt)
}

// HeldStack is the out-of-run view of a held stack, for the roster and the
// retry endpoint. A non-zero RetryAt marks a backoff (ADR-0064).
type HeldStack struct {
	Since    time.Time     `json:"since"`
	Status   events.Status `json:"status"`
	Commit   string        `json:"commit,omitempty"`
	Attempts int           `json:"attempts,omitempty"`
	RetryAt  time.Time     `json:"retry_at,omitzero"`
}

// fingerprint condenses a stack's tracked-file hashes into one comparable
// value, independent of map order.
func (h stackFileHashes) fingerprint() string {
	paths := make([]string, 0, len(h))
	for p := range h {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	sum := sha256.New()
	for _, p := range paths {
		sum.Write([]byte(p))
		sum.Write([]byte{0})
		sum.Write([]byte(h[p]))
		sum.Write([]byte{'\n'})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// hold records that a stack's change failed after it started.
func (s *persistedState) hold(stack string, h heldChange) {
	if s.Held == nil {
		s.Held = map[string]heldChange{}
	}
	s.Held[stack] = h
}

// preStartBackoff is the wait after the given number of consecutive failures
// before start: the base, doubled for each failure after the first.
func preStartBackoff(attempts int) time.Duration {
	return preStartBackoffBase << (attempts - 1)
}

// recordPreStartFailure counts a failure of the change before it started:
// one more attempt when the inputs are the ones that already failed, else the
// first. Below preStartHoldAfter the change is retried after a backoff
// measured from the attempt's start; at it, the change is held.
func (s *persistedState) recordPreStartFailure(stack, fingerprint string, started, failed time.Time, commit string) heldChange {
	attempts := 1
	if prev, ok := s.heldFor(stack); ok && prev.Fingerprint == fingerprint {
		attempts = prev.Attempts + 1
	}
	h := heldChange{Fingerprint: fingerprint, Since: failed, Status: events.StatusFailed, Commit: commit, Attempts: attempts}
	if attempts < preStartHoldAfter {
		h.RetryAt = started.Add(preStartBackoff(attempts))
	}
	s.hold(stack, h)
	return h
}

// release drops a stack's hold, if any.
func (s *persistedState) release(stack string) {
	delete(s.Held, stack)
}

// heldFor returns a stack's hold, if any.
func (s *persistedState) heldFor(stack string) (heldChange, bool) {
	h, ok := s.Held[stack]
	return h, ok
}

// heldView returns the holds in their out-of-run form. Never nil.
func (s *persistedState) heldView() map[string]HeldStack {
	out := make(map[string]HeldStack, len(s.Held))
	for stack, h := range s.Held {
		out[stack] = HeldStack{Since: h.Since, Status: h.Status, Commit: h.Commit, Attempts: h.Attempts, RetryAt: h.RetryAt}
	}
	return out
}

// newestCommit is the SHA of the newest commit in a change set, or "" when the
// change carries no commit context. Commits are listed newest first.
func newestCommit(cs changeSet) string {
	if len(cs.commits) == 0 {
		return ""
	}
	return cs.commits[0].SHA
}

// HeldStacks returns the stacks whose change is currently held, as of the last
// run (or the persisted state at startup). Concurrent-safe; a per-call copy.
func (d *Deployer) HeldStacks() map[string]HeldStack {
	p := d.heldNow.Load()
	if p == nil {
		return map[string]HeldStack{}
	}
	return maps.Clone(*p)
}

// RequestRetry asks the next run to deploy a held stack's change once more,
// despite the hold. It reports false, recording nothing, when the stack is not
// held. The caller triggers the run.
func (d *Deployer) RequestRetry(stack string) bool {
	if _, ok := d.HeldStacks()[stack]; !ok {
		return false
	}
	d.retryMu.Lock()
	defer d.retryMu.Unlock()
	if d.retryRequests == nil {
		d.retryRequests = map[string]bool{}
	}
	d.retryRequests[stack] = true
	return true
}

// takeRetryRequest reports whether a retry was requested for a stack and
// consumes the request: one request is one attempt.
func (d *Deployer) takeRetryRequest(stack string) bool {
	d.retryMu.Lock()
	defer d.retryMu.Unlock()
	requested := d.retryRequests[stack]
	delete(d.retryRequests, stack)
	return requested
}

// publishHeld publishes the run's holds for out-of-run readers and keeps the
// skipper_stack_held gauge in step: set for each stack held until someone
// acts, deleted for a stack whose hold was released or that is only backing
// off — a backoff ends on its own, so there is nothing to alert on.
func (d *Deployer) publishHeld(state *persistedState) {
	held := state.heldView()
	if prev := d.heldNow.Load(); prev != nil {
		for stack := range *prev {
			if h, still := held[stack]; !still || !h.RetryAt.IsZero() {
				metrics.StackHeld.DeleteLabelValues(stack)
			}
		}
	}
	for stack, h := range held {
		if h.RetryAt.IsZero() {
			metrics.StackHeld.WithLabelValues(stack).Set(1)
		}
	}
	d.heldNow.Store(&held)
}

// isHeld reports whether a stack's pending change is held at now: it has a
// hold for exactly these inputs, no retry was requested, and no backoff is
// due. A hold for other inputs (a new push) is released here. A requested
// retry or a due backoff keeps the record, so a further failure before start
// counts on from its attempts; the deploy's outcome replaces or releases it.
func (d *Deployer) isHeld(stack, fingerprint string, state *persistedState, now time.Time) bool {
	retry := d.takeRetryRequest(stack)
	h, ok := state.heldFor(stack)
	if !ok {
		return false
	}
	if h.Fingerprint != fingerprint {
		state.release(stack)
		return false
	}
	return !retry && !h.due(now)
}

// logHeld says why a run passes over a held stack. Debug: it recurs every
// reconcile tick.
func logHeld(stack string, state *persistedState) {
	h, _ := state.heldFor(stack)
	if h.backingOff() {
		slog.Debug("deploy backing off: this change failed before it started, retrying later", "stack", stack, "attempts", h.Attempts, "retry_at", h.RetryAt)
		return
	}
	slog.Debug("deploy held: this change failed, waiting for a new commit or a retry", "stack", stack)
}

// logPreStartFailure says what follows a failure before start: the next
// attempt, or the hold once the attempts are used up.
func logPreStartFailure(stack string, h heldChange) {
	if h.backingOff() {
		slog.Info("change failed before it started, retrying it later", "stack", stack, "attempts", h.Attempts, "retry_at", h.RetryAt)
		return
	}
	slog.Warn("change failed before it started too often, holding it until a new commit or a retry", "stack", stack, "attempts", h.Attempts)
}
