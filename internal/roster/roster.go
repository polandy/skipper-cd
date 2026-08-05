// Package roster builds the Stacks-view inventory: the full set of stacks
// skipper owns (stack discovery, ADR-0034, or the host stacks: list)
// merged with each stack's last deploy outcome from the audit log (ADR-0033).
//
// Unlike the deploy table — an event log that only shows stacks with recent
// events — the roster is inventory: every declared stack appears, including
// ones that have never deployed and ones parked with disabled: true. It is
// pure visualization; see dev-docs/stack-roster-spec.md.
package roster

import (
	"sort"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// recentCap bounds how many terminal records ride each roster entry for the
// Stacks view's outcome strip (≤ 10 dots per row, UI_SPEC.md).
const recentCap = 10

// incidentStatuses are the bad terminal outcomes the rollback-visibility
// surfaces count: the last-incident line, and the header incident badge's
// 24 h window.
var incidentStatuses = map[events.Status]bool{
	events.StatusFailed:              true,
	events.StatusRolledBack:          true,
	events.StatusRolledBackUnhealthy: true,
	events.StatusHealExhausted:       true,
}

// OutcomeRef is a compact reference to one terminal audit record, as the
// outcome strip, the last-incident line and the incident badge carry it.
// Stack is set only on the cross-stack incident list; Commit only on the
// per-stack strip.
type OutcomeRef struct {
	Stack  string        `json:"stack,omitempty"`
	Status events.Status `json:"status"`
	At     time.Time     `json:"at"`
	Commit string        `json:"commit,omitempty"`
}

// Entry is one stack's inventory row. The frontend resolves the icon from Name
// via /api/icons/<name>, so no icon travels here.
type Entry struct {
	Name string `json:"name"`
	// Disabled marks a stack parked with disabled: true — present in the repo,
	// deliberately not deployed. Disabled entries carry no live outcome.
	Disabled bool `json:"disabled"`
	// LastStatus is the newest terminal deploy status; empty means the stack
	// has never deployed.
	LastStatus events.Status `json:"last_status,omitempty"`
	LastAt     *time.Time    `json:"last_at,omitempty"`
	LastCommit string        `json:"last_commit,omitempty"`
	// Recent lists the stack's newest terminal records (newest first, at most
	// recentCap), driving the Stacks view's outcome strip without a fetch.
	Recent []OutcomeRef `json:"recent,omitempty"`
	// LastIncident is the newest bad terminal record (incidentStatuses) when
	// later successes have taken LastStatus — the case where a rollback is no
	// longer visible on the badge. nil when no bad record exists in the
	// retained history, and nil when the newest record itself is bad (the
	// badge already says it, so a line would double it).
	LastIncident *OutcomeRef `json:"last_incident,omitempty"`
	// Hooks carries the stack's deploy-hook command lines so the UI can show the
	// badge + panel without a fetch (ADR-0038); nil when the stack has none.
	Hooks *Hooks `json:"hooks,omitempty"`
	// Watched lists the input files whose hashes decide whether this stack
	// redeploys (Invariant 2), as recorded by its last successful deploy. It
	// answers the roster's "why has nothing happened here" without reading
	// state.yaml on the host. Empty for a stack that has never deployed.
	Watched []string `json:"watched,omitempty"`
	// WatchedConfig reports that the stack's deploy-shaping config is hashed
	// too, so editing it redeploys the stack. It is deliberately not in
	// Watched: that hash is recorded under a synthetic key, not a file anyone
	// can open, and listing it as a path would send an operator looking for a
	// file that does not exist.
	WatchedConfig bool `json:"watched_config,omitempty"`
}

// Hooks is the roster view of a stack's deploy hooks: just the command lines,
// no timeout_seconds (the UI never shows it).
type Hooks struct {
	PreDeploy  []string `json:"pre_deploy,omitempty"`
	PostDeploy []string `json:"post_deploy,omitempty"`
}

// Build merges the effective stack set with per-stack outcomes into a
// stable, sorted roster. stacks is the deploy set (disabled stacks are already
// excluded from it, so disabled is merged in separately); history returns a
// stack's retained audit records, newest first (empty for a stack that has
// never deployed); watched returns the stack's tracked input files and whether
// its config hash is tracked too. Enabled stacks sort first (alphabetical),
// then disabled (alphabetical), so the list reads live-then-parked.
func Build(stacks []config.Stack, disabled []string, history func(name string) []audit.Record, watched func(name string) (files []string, config bool)) []Entry {
	entries := make([]Entry, 0, len(stacks)+len(disabled))

	for _, s := range stacks {
		e := Entry{Name: s.Name}
		if recs := history(s.Name); len(recs) > 0 {
			ts := recs[0].Timestamp
			e.LastStatus = recs[0].Status
			e.LastAt = &ts
			e.LastCommit = recs[0].CommitSHA
			e.Recent = recentOutcomes(recs)
			e.LastIncident = lastIncident(recs)
		}
		if len(s.Hooks.PreDeploy) > 0 || len(s.Hooks.PostDeploy) > 0 {
			e.Hooks = &Hooks{PreDeploy: s.Hooks.PreDeploy, PostDeploy: s.Hooks.PostDeploy}
		}
		if watched != nil {
			e.Watched, e.WatchedConfig = watched(s.Name)
		}
		entries = append(entries, e)
	}
	for _, name := range disabled {
		// Parked stacks show as disabled with no live outcome, even if a
		// historical record exists — the inventory fact is "not deployed".
		// Watched files are omitted for the same reason: skipper is not
		// watching a parked stack, whatever its last deploy recorded.
		entries = append(entries, Entry{Name: name, Disabled: true})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Disabled != entries[j].Disabled {
			return !entries[i].Disabled // enabled before disabled
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// recentOutcomes projects a stack's newest records (newest first) onto the
// outcome-strip refs, capped at recentCap.
func recentOutcomes(recs []audit.Record) []OutcomeRef {
	count := min(len(recs), recentCap)
	out := make([]OutcomeRef, 0, count)
	for _, r := range recs[:count] {
		out = append(out, OutcomeRef{Status: r.Status, At: r.Timestamp, Commit: r.CommitSHA})
	}
	return out
}

// lastIncident returns the newest bad record when later records have taken the
// badge — the rollback that a successful retry papered over. It scans the full
// retained history, not just the strip's recentCap window: an incident's
// visibility must not depend on how busy the stack has been since. nil when
// the newest record itself is bad (the badge already says it) or no bad record
// is retained.
func lastIncident(recs []audit.Record) *OutcomeRef {
	for i, r := range recs {
		if !incidentStatuses[r.Status] {
			continue
		}
		if i == 0 {
			return nil
		}
		return &OutcomeRef{Status: r.Status, At: r.Timestamp}
	}
	return nil
}
