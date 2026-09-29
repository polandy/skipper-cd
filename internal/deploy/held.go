// Held changes (ADR-0062): a change whose new version failed after it started
// is not retried on every reconcile tick. It waits, recorded in state.yaml,
// until the stack's inputs change (a new push) or an operator asks for one
// more attempt.

package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"sort"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
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
}

// HeldStack is the out-of-run view of a held stack, for the roster and the
// retry endpoint.
type HeldStack struct {
	Since  time.Time     `json:"since"`
	Status events.Status `json:"status"`
	Commit string        `json:"commit,omitempty"`
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
		out[stack] = HeldStack{Since: h.Since, Status: h.Status, Commit: h.Commit}
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
// skipper_stack_held gauge in step: set for each held stack, deleted for a
// stack whose hold was released.
func (d *Deployer) publishHeld(state *persistedState) {
	held := state.heldView()
	if prev := d.heldNow.Load(); prev != nil {
		for stack := range *prev {
			if _, still := held[stack]; !still {
				metrics.StackHeld.DeleteLabelValues(stack)
			}
		}
	}
	for stack := range held {
		metrics.StackHeld.WithLabelValues(stack).Set(1)
	}
	d.heldNow.Store(&held)
}

// isHeld reports whether a stack's pending change is held: it has a hold for
// exactly these inputs and no retry was requested. A hold for other inputs (a
// new push) and a requested retry are released here, so the deploy that
// follows starts from no hold — and re-records one only if it fails again.
func (d *Deployer) isHeld(stack, fingerprint string, state *persistedState) bool {
	retry := d.takeRetryRequest(stack)
	h, ok := state.heldFor(stack)
	if !ok {
		return false
	}
	if h.Fingerprint == fingerprint && !retry {
		return true
	}
	state.release(stack)
	return false
}
