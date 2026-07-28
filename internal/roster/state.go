package roster

import (
	"path/filepath"
	"strings"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
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
}

// RepoRef identifies the deploy repo for the UI: the local clone directory (to
// render tracked input paths repo-relative) and the forge browse URL derived
// from repo_url (to link commit SHAs). Both are fixed for a process lifetime.
type RepoRef struct {
	Dir    string
	WebURL string
}

// BuildState assembles the `stacks` snapshot from the effective stack
// set, the parked (disabled) names, each stack's newest audit record, and the
// input paths its change detection watches.
func BuildState(stacks []config.Stack, disabled []string, auditLog *audit.Log, tracked map[string][]string, repo RepoRef) State {
	last := func(name string) (audit.Record, bool) {
		recs := auditLog.Stack(name, 1)
		if len(recs) == 0 {
			return audit.Record{}, false
		}
		return recs[0], true
	}
	watched := func(name string) ([]string, bool) { return splitTrackedPaths(tracked[name], repo.Dir) }
	return State{
		Disabled:   disabled,
		Roster:     Build(stacks, disabled, last, watched),
		RepoWebURL: repo.WebURL,
	}
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
