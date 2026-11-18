// Package orphans detects compose projects running on the host that skipper's
// discovered stack set no longer accounts for (ADR-0036) — the "prune /
// orphaned resources" gap for docker-compose. Detection is read-only and
// viz-only; the optional prune layer acts on the orphaned class.
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

// Container is one container of a compose project, shown in the UI's per-orphan
// expansion.
type Container struct {
	Name    string `json:"name"`
	Service string `json:"service,omitempty"`
	Image   string `json:"image,omitempty"`
	State   string `json:"state,omitempty"`  // running, exited, …
	Status  string `json:"status,omitempty"` // human text, e.g. "Up 5 days"
	Ports   string `json:"ports,omitempty"`  // published ports, e.g. "0.0.0.0:8080->80/tcp"
}

// Project is a running compose project observed on the host, identified by its
// working_dir label — the identity a rollback preserves (Invariant 3), unlike
// the compose file path.
type Project struct {
	Name       string
	WorkingDir string
	ConfigFile string   // com.docker.compose.project.config_files label
	Volumes    []string // named volumes compose created for this project
	Containers []Container
}

// Orphan is one non-managed project (or stale state entry) surfaced to the UI.
type Orphan struct {
	Project    string      `json:"project"`
	Class      Class       `json:"class"`
	WorkingDir string      `json:"working_dir,omitempty"`
	ConfigFile string      `json:"config_file,omitempty"`
	Volumes    []string    `json:"volumes,omitempty"`
	Containers []Container `json:"containers,omitempty"`
	Prunable   bool        `json:"prunable"`
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
	// BaseDir is stacks_base_dir; a project under it with no matching stack is a
	// formerly-managed orphan.
	BaseDir string
	// ActiveDirs holds each active stack's project dir — a match is managed.
	ActiveDirs map[string]bool
	// DisabledDirs holds each disabled stack's project dir — hands-off, a match
	// is managed and never pruned.
	DisabledDirs map[string]bool
	// StateDirs maps recorded stack name → last deployed project dir. Catches
	// orphans whose dir sits outside BaseDir, and stale state-only entries.
	StateDirs map[string]string
}

// Classify partitions the running projects into orphaned (formerly managed,
// prunable) and unmanaged (never pruned), appends stale state-only orphans for
// recorded stacks that are gone with nothing running, and drops managed
// projects. Sorted by project name.
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
			ConfigFile: p.ConfigFile,
			Volumes:    p.Volumes,
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
