package orphans

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
)

// Outputter runs a command and returns its captured stdout.
// command.ShellRunner satisfies it.
type Outputter interface {
	Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// psArgs lists every compose-managed container with its project name and
// working_dir label, tab-separated. Matching on the working_dir label (not the
// compose file path) is what makes a rollback's temp /tmp compose file
// irrelevant to identity (Invariant 3). One line per container lets Detect
// count a project's containers.
var psArgs = []string{
	"ps", "-a",
	"--filter", "label=com.docker.compose.project",
	"--format", `{{.Label "com.docker.compose.project"}}` + "\t" + `{{.Label "com.docker.compose.project.working_dir"}}`,
}

// Config wires a Detector. Managed supplies skipper's current expected set on
// every detection (it changes as discovery re-runs); Publish (optional)
// receives every fresh snapshot for the UI.
type Config struct {
	Outputter Outputter
	Managed   func() Managed
	Publish   func(Snapshot)
}

// Detector finds compose projects the managed set does not account for. It owns
// no timer: the caller drives Detect on the health-poll cadence (UI-gated) and,
// when prune is enabled, from the reconcile loop.
type Detector struct {
	out     Outputter
	managed func() Managed
	publish func(Snapshot)
	last    atomic.Pointer[Snapshot]
}

// New builds a Detector from cfg.
func New(cfg Config) *Detector {
	return &Detector{out: cfg.Outputter, managed: cfg.Managed, publish: cfg.Publish}
}

// Detect lists running compose projects, classifies them against the current
// managed set, caches and publishes the snapshot, and returns it. A docker
// failure leaves the last snapshot in place (logged, not cleared) so a
// transient error does not blank the UI's orphan section.
func (d *Detector) Detect(ctx context.Context) Snapshot {
	out, err := d.out.Output(ctx, "", "docker", psArgs...)
	if err != nil {
		slog.Warn("orphan detection skipped: could not list compose projects", "err", err)
		return d.Current()
	}
	snap := Classify(parseProjects(out), d.managed())
	d.last.Store(&snap)
	if d.publish != nil {
		d.publish(snap)
	}
	return snap
}

// Current returns the most recent snapshot, for a client connecting between
// detections. Safe for concurrent use.
func (d *Detector) Current() Snapshot {
	if s := d.last.Load(); s != nil {
		return *s
	}
	return Snapshot{}
}

// parseProjects folds the tab-separated docker ps output (project<TAB>dir, one
// line per container) into one Project per compose project: container count is
// the line count, working_dir is the first non-empty label seen. Lines without
// a project label are skipped.
func parseProjects(out []byte) []Project {
	byName := map[string]*Project{}
	var order []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, dir, _ := strings.Cut(line, "\t")
		if name == "" {
			continue
		}
		p, ok := byName[name]
		if !ok {
			p = &Project{Name: name}
			byName[name] = p
			order = append(order, name)
		}
		p.Containers++
		if p.WorkingDir == "" {
			p.WorkingDir = dir
		}
	}
	projects := make([]Project, 0, len(order))
	for _, name := range order {
		projects = append(projects, *byName[name])
	}
	return projects
}
