// Entry-level configuration errors: an override without a stack directory, a
// broken compose file, an invalid rollout. Unlike a failed deploy they are a
// standing state, not something that happened this run — so they are reported
// once and stay quiet until the message changes or the error clears (ADR-0055).

package deploy

import (
	"log/slog"
	"sort"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// configErrorLog remembers the configuration error last reported per stack.
// It lives on the Deployer and is only touched from the deploy run, which
// serializes on the deploy mutex (Invariant 7). Deliberately in memory: a
// restart re-reports every standing error once, which is the reminder an
// operator wants on startup — see ADR-0055.
type configErrorLog struct {
	reported map[string]string
}

func newConfigErrorLog() *configErrorLog {
	return &configErrorLog{reported: map[string]string{}}
}

// record stores stack's current error message and reports whether it is worth
// announcing: true on a first sighting or a changed message, false while it
// reads exactly as last reported.
func (l *configErrorLog) record(stack, message string) bool {
	if previous, ok := l.reported[stack]; ok && previous == message {
		return false
	}
	l.reported[stack] = message
	return true
}

// forgetOthers drops every remembered stack absent from current and returns
// their names alphabetically: their error is gone, so the next one they hit is
// announced again.
func (l *configErrorLog) forgetOthers(current map[string]bool) []string {
	var cleared []string
	for stack := range l.reported {
		if !current[stack] {
			cleared = append(cleared, stack)
		}
	}
	sort.Strings(cleared)
	for _, stack := range cleared {
		delete(l.reported, stack)
	}
	return cleared
}

// reportConfigErrors announces this run's entry-level stack errors and clears
// the ones that are gone, so the standing set of broken stacks is reported
// exactly as it changes.
func (d *Deployer) reportConfigErrors(errs []config.StackError) {
	current := make(map[string]bool, len(errs))
	for _, se := range errs {
		current[se.Stack] = true
	}
	for _, stack := range d.configErrors.forgetOthers(current) {
		metrics.StackConfigError.DeleteLabelValues(stack)
		slog.Info("stack configuration error resolved", "stack", stack)
	}
	for _, se := range errs {
		d.reportConfigError(se.Stack, se.Err)
	}
}

// reportConfigError emits one failed event for a stack (or the reserved
// _config key) excluded by a configuration error — the first time it is seen
// and again whenever its message changes, but not on every reconcile in
// between: nothing happened, the same broken config was read again (ADR-0055).
// The gauge is what stays up for the whole time the error stands.
func (d *Deployer) reportConfigError(stack string, err error) {
	metrics.StackConfigError.WithLabelValues(stack).Set(1)
	if !d.configErrors.record(stack, err.Error()) {
		slog.Debug("configuration error unchanged since it was reported", "stack", stack, "err", err)
		return
	}
	d.emitDeployFailure(stack, 0, err, changeSet{})
}
