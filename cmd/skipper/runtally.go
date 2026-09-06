package main

import (
	"log/slog"
	"sync"

	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/prettylog"
)

// runTally counts one sync-and-deploy run's terminal per-stack outcomes so
// PostRunHook can log a one-line summary (prettylog.MsgRunComplete) mirroring
// what the Deploys view just showed.
//
// Self-heal's Healed/HealExhausted events come from an unrelated async path
// (the health poller, never DeployAllStacks) and already have their own log
// lines, so they are not counted here.
//
// The reserved pseudo-stacks need special handling. A NixOS rebuild failure and
// any stack-discovery config failure abort DeployAllStacks *before* it reaches
// PostRunHook (Invariants 4 and 8), so a failed count for either would
// otherwise linger in the tally and get misattributed to whichever later run
// happens to flush next. NixosStateKey's non-failure outcomes
// (deployed/skipped/queued) complete normally within a run and are counted like
// any stack's. A refused project_directory fast-forward (ADR-0060) is skipped
// for the opposite reason: it does *not* abort the run, so the stacks' own
// counts are complete — adding it would make the summary line say a stack
// failed when none did.
type runTally struct {
	mu     sync.Mutex
	counts map[events.Status]int
}

func newRunTally() *runTally {
	return &runTally{counts: map[events.Status]int{}}
}

// observe is an events.DeployEvent sink: wire it into eventSinks alongside
// history/notify/audit so every run's outcomes are tallied regardless of
// whether the UI is enabled.
func (t *runTally) observe(e events.DeployEvent) {
	if e.Stack == deploy.ConfigStateKey || e.Stack == deploy.ProjectDirStateKey {
		return
	}
	if e.Stack == deploy.NixosStateKey && e.Status == events.StatusFailed {
		return
	}
	switch e.Status {
	case events.StatusSuccess, events.StatusFailed, events.StatusSkipped,
		events.StatusRolledBack, events.StatusRolledBackUnhealthy,
		events.StatusQueued, events.StatusBlocked, events.StatusRemoved:
		t.mu.Lock()
		t.counts[e.Status]++
		t.mu.Unlock()
	}
}

// flush returns the accumulated counts and resets the tally for the next run.
func (t *runTally) flush() map[events.Status]int {
	t.mu.Lock()
	defer t.mu.Unlock()
	counts := t.counts
	t.counts = map[events.Status]int{}
	return counts
}

// logRunSummary logs one run's tallied outcomes as a single line: a narrated
// summary under log_format=pretty, plain structured counts under text/json.
func logRunSummary(counts map[events.Status]int) {
	slog.Info(prettylog.MsgRunComplete,
		"deployed", counts[events.StatusSuccess],
		"rolled_back", counts[events.StatusRolledBack],
		"rolled_back_unhealthy", counts[events.StatusRolledBackUnhealthy],
		"queued", counts[events.StatusQueued],
		"blocked", counts[events.StatusBlocked],
		"skipped", counts[events.StatusSkipped],
		"removed", counts[events.StatusRemoved],
		"failed", counts[events.StatusFailed],
	)
}
