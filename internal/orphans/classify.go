// Package orphans detects compose projects running on the host that skipper's
// discovered stack set no longer accounts for (ADR-0036). It is the "prune /
// orphaned resources" gap for docker-compose: a stack whose directory is
// removed from the repo keeps running forever otherwise. Detection is
// read-only and viz-only; the optional prune layer acts on the orphaned class.
package orphans

import (
	"path/filepath"
	"sort"
	"strings"
)

// Class is how a running compose project relates to skipper's managed set.
type Class string

const (
	// Orphaned marks a project skipper once deployed (its dir is under
	// stacks_base_dir or was recorded in state) but that the current stack set
	// no longer contains — safe to prune.
	Orphaned Class = "orphaned"
	// Unmanaged marks a project skipper never deployed (a manually started
	// compose project, skipper's own container) — never pruned.
	Unmanaged Class = "unmanaged"
)

// Project is a running docker compose project observed on the host, identified
// by its com.docker.compose.project.working_dir label — the stable identity a
// rollback (temp compose file in /tmp, --project-directory unchanged) preserves
// (Invariant 3), unlike the compose file path.
type Project struct {
	Name       string
	WorkingDir string
	Containers int
}

// Orphan is one non-managed project (or stale state entry) surfaced to the UI.
type Orphan struct {
	Project    string `json:"project"`
	Class      Class  `json:"class"`
	WorkingDir string `json:"working_dir,omitempty"`
	Containers int    `json:"containers"`
	Prunable   bool   `json:"prunable"`
	// StateOnly marks an orphan that has no running containers, surfaced purely
	// from a stale state.yaml entry (a removed stack skipper last deployed).
	StateOnly bool `json:"state_only,omitempty"`
}

// Snapshot is the full orphan-detection result published to the UI.
type Snapshot struct {
	Orphans []Orphan `json:"orphans"`
}

// Managed describes skipper's expected set, against which running projects are
// classified. Matching is by project working_dir throughout; compose project
// names are display-only.
type Managed struct {
	// BaseDir is stacks_base_dir. A project whose working_dir lies under it,
	// but matches no active or disabled stack, is a formerly-managed orphan.
	BaseDir string
	// ActiveDirs holds the project dir of every active (deployed) discovered
	// stack; a running project matching one is managed and not surfaced.
	ActiveDirs map[string]bool
	// DisabledDirs holds the project dir of every disabled stack. A disabled
	// stack is hands-off — skipper neither deploys nor prunes it — so a matching
	// project is managed and not surfaced as an orphan.
	DisabledDirs map[string]bool
	// StateDirs maps every stack name recorded in state to its last deployed
	// project dir. It recognizes a removed stack whose working_dir pointed
	// outside BaseDir as formerly managed, and surfaces stale state entries with
	// nothing running as state-only orphans.
	StateDirs map[string]string
}

// Classify partitions the running projects into orphans (formerly managed,
// prunable) and unmanaged (never pruned), and appends stale state-only orphans
// for recorded stacks that are gone with nothing left running. Managed projects
// (active or disabled) are dropped — they render as normal rows. The result is
// sorted by project name for a stable UI and stable tests.
func Classify(projects []Project, m Managed) Snapshot {
	running := make(map[string]bool, len(projects))
	var orphans []Orphan
	for _, p := range projects {
		running[p.WorkingDir] = true
		if m.ActiveDirs[p.WorkingDir] || m.DisabledDirs[p.WorkingDir] {
			continue // managed — active row or disabled line
		}
		formerly := underDir(p.WorkingDir, m.BaseDir) || matchesStateDir(p.WorkingDir, m.StateDirs)
		o := Orphan{
			Project:    p.Name,
			Class:      Unmanaged,
			WorkingDir: p.WorkingDir,
			Containers: p.Containers,
		}
		if formerly {
			o.Class = Orphaned
			o.Prunable = true
		}
		orphans = append(orphans, o)
	}

	// Stale state: a recorded stack that is no longer active/disabled and has
	// nothing running is a state-only orphan, so a prune can clean the entry.
	for name, dir := range m.StateDirs {
		if m.ActiveDirs[dir] || m.DisabledDirs[dir] || running[dir] {
			continue
		}
		orphans = append(orphans, Orphan{
			Project:    name,
			Class:      Orphaned,
			WorkingDir: dir,
			Prunable:   true,
			StateOnly:  true,
		})
	}

	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Project < orphans[j].Project })
	return Snapshot{Orphans: orphans}
}

// underDir reports whether path lies at or under base. An empty base never
// matches (nothing is known to be managed).
func underDir(path, base string) bool {
	if base == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// matchesStateDir reports whether path equals any recorded state project dir.
func matchesStateDir(path string, stateDirs map[string]string) bool {
	for _, dir := range stateDirs {
		if dir == path {
			return true
		}
	}
	return false
}
