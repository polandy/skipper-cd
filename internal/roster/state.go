package roster

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/updatecheck"
)

// State is the `stacks` SSE snapshot: stack-set facts that are not
// deploy events. Disabled carries the names parked via disabled: true in
// stack-discovery mode (ADR-0034), driving the Deploys view's disabled line
// (empty in host-list mode). Roster is the full inventory for the Stacks view —
// every declared stack with its last outcome (dev-docs/stack-roster-spec.md).
type State struct {
	Disabled []string `json:"disabled"`
	Roster   []Entry  `json:"roster"`
	// RepoWebURL is the deploy repo's forge browse URL, so the UI can turn every
	// commit SHA it shows into a link to that commit. Empty when none can be
	// derived from repo_url — the UI then renders SHAs as plain text. It rides
	// on this state (rather than a dedicated endpoint) so the multi-host fan-in
	// carries each peer's own forge along with that peer's roster (ADR-0048).
	RepoWebURL string `json:"repo_web_url,omitempty"`
	// Updates is the latest registry update check (ADR-0054): per stack, the
	// services with an available image update. nil until a check has run (or
	// when the check is disabled). It rides this state so the UI's version
	// chips and the multi-host fan-in get it with the roster they annotate.
	Updates *updatecheck.Snapshot `json:"updates,omitempty"`
	// Incidents24h lists the bad terminal audit records of the last 24 h
	// across all stacks (newest first, capped), driving the header incident
	// badge. The client re-filters the list against the window on its own
	// clock, so the count ages out between republishes.
	Incidents24h []OutcomeRef `json:"incidents_24h,omitempty"`
}

// RepoRef identifies the deploy repo for the UI: the local clone directory (to
// render tracked input paths repo-relative) and the forge browse URL derived
// from repo_url (to link commit SHAs). Both are fixed for a process lifetime.
type RepoRef struct {
	Dir    string
	WebURL string
}

// incidentWindow is how far back the header incident badge counts bad
// terminal outcomes; incidentsCap bounds the list riding the snapshot.
const (
	incidentWindow = 24 * time.Hour
	incidentsCap   = 50
)

// BuildState assembles the `stacks` snapshot from the effective stack
// set, the parked (disabled) names, each stack's retained audit records, the
// input paths its change detection watches, and the latest update-check
// result (nil while none exists).
func BuildState(stacks []config.Stack, disabled []string, auditLog *audit.Log, tracked map[string][]string, repo RepoRef, updates *updatecheck.Snapshot) State {
	return buildState(stacks, disabled, auditLog, tracked, repo, updates, time.Now())
}

// buildState is BuildState with an injected clock for the incident window.
func buildState(stacks []config.Stack, disabled []string, auditLog *audit.Log, tracked map[string][]string, repo RepoRef, updates *updatecheck.Snapshot, now time.Time) State {
	history := func(name string) []audit.Record { return auditLog.Stack(name, 0) }
	watched := func(name string) ([]string, bool) { return splitTrackedPaths(tracked[name], repo.Dir) }
	return State{
		Disabled:     disabled,
		Roster:       Build(stacks, disabled, history, watched),
		RepoWebURL:   repo.WebURL,
		Updates:      updates,
		Incidents24h: recentIncidents(auditLog, now),
	}
}

// recentIncidents collects the bad terminal records of the incident window
// across all stacks, newest first, capped at incidentsCap.
func recentIncidents(auditLog *audit.Log, now time.Time) []OutcomeRef {
	cutoff := now.Add(-incidentWindow)
	var out []OutcomeRef
	for _, r := range auditLog.Recent(0) { // newest first
		if r.Timestamp.Before(cutoff) {
			continue
		}
		if !incidentStatuses[r.Status] {
			continue
		}
		out = append(out, OutcomeRef{Stack: r.Stack, Status: r.Status, At: r.Timestamp})
		if len(out) == incidentsCap {
			break
		}
	}
	return out
}

// splitTrackedPaths renders a stack's tracked input paths for the UI and
// separates out the one entry that is not a file.
//
// Files: a path inside the repo clone shows repo-relative (the form the
// operator edits and commits), while a host path — an env file, a vars file —
// stays absolute, since that is where it actually lives.
//
// Config: a stack's deploy-shaping config is hashed under a synthetic
// <stacks_base_dir>/skipper.yaml key (ADR-0043 moved that config host-side, so
// no such file exists). Reporting it as a watched path would send an operator
// looking for a file that is not there, so it is returned as a flag instead.
func splitTrackedPaths(paths []string, repoDir string) (files []string, configHashed bool) {
	for _, p := range paths {
		if filepath.Base(p) == config.RepoConfigFileName {
			configHashed = true
			continue
		}
		if rel, err := filepath.Rel(repoDir, p); err == nil && !strings.HasPrefix(rel, "..") {
			files = append(files, rel)
			continue
		}
		files = append(files, p)
	}
	return files, configHashed
}
