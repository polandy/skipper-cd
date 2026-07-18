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

// psColumns are the tab-separated fields psArgs emits per container, in order.
// Matching on the working_dir label (not the compose file path) is what makes a
// rollback's temp /tmp compose file irrelevant to identity (Invariant 3); the
// remaining columns populate the UI's per-orphan container expansion. Status is
// last because it is free human text (it never contains a tab).
var psColumns = []string{
	`{{.Label "com.docker.compose.project"}}`,
	`{{.Label "com.docker.compose.project.working_dir"}}`,
	`{{.Label "com.docker.compose.project.config_files"}}`,
	`{{.Names}}`,
	`{{.Label "com.docker.compose.service"}}`,
	`{{.Image}}`,
	`{{.State}}`,
	`{{.Status}}`,
	`{{.Ports}}`,
}

// psArgs lists every compose-managed container, one line per container so Detect
// can group them into projects with their full container detail.
var psArgs = []string{
	"ps", "-a",
	"--filter", "label=com.docker.compose.project",
	"--format", strings.Join(psColumns, "\t"),
}

// volumeArgs lists every named volume with its owning compose project, so Detect
// can show which volumes an orphan holds — the data prune deliberately keeps
// (no --volumes). Only compose-created volumes carry the project label; external
// volumes have none and are skipped (prune never touches them either).
var volumeArgs = []string{
	"volume", "ls",
	"--format", `{{.Label "com.docker.compose.project"}}` + "\t" + `{{.Name}}`,
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
	projects := parseProjects(out)

	// Volumes are best-effort: a failure just omits the data-safety note, it
	// never blocks detection.
	if volsOut, verr := d.out.Output(ctx, "", "docker", volumeArgs...); verr != nil {
		slog.Warn("orphan detection: could not list volumes", "err", verr)
	} else {
		attachVolumes(projects, parseVolumes(volsOut))
	}

	snap := Classify(projects, d.managed())
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

// parseProjects folds the tab-separated docker ps output (one line per
// container, columns per psColumns) into one Project per compose project: its
// working_dir is the first non-empty label seen and its Containers the lines
// that belong to it. Lines without a project label are skipped.
func parseProjects(out []byte) []Project {
	byName := map[string]*Project{}
	var order []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.SplitN(line, "\t", len(psColumns))
		if f[0] == "" {
			continue
		}
		name, dir := f[0], field(f, 1)
		p, ok := byName[name]
		if !ok {
			p = &Project{Name: name}
			byName[name] = p
			order = append(order, name)
		}
		if p.WorkingDir == "" {
			p.WorkingDir = dir
		}
		if p.ConfigFile == "" {
			p.ConfigFile = field(f, 2)
		}
		p.Containers = append(p.Containers, Container{
			Name:    field(f, 3),
			Service: field(f, 4),
			Image:   field(f, 5),
			State:   field(f, 6),
			Status:  field(f, 7),
			Ports:   field(f, 8),
		})
	}
	projects := make([]Project, 0, len(order))
	for _, name := range order {
		projects = append(projects, *byName[name])
	}
	return projects
}

// field returns column i of a split line, or "" when the line had fewer columns
// (an older docker without some template field).
func field(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

// parseVolumes folds `docker volume ls` output (project<TAB>volume, one line per
// volume) into a project→volume-names map, skipping volumes with no project
// label (not compose-created). Names are kept in the order docker returns them.
func parseVolumes(out []byte) map[string][]string {
	byProject := map[string][]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		project, vol, _ := strings.Cut(line, "\t")
		if project == "" || vol == "" {
			continue
		}
		byProject[project] = append(byProject[project], vol)
	}
	return byProject
}

// attachVolumes fills each project's Volumes from the project→volumes map, keyed
// by compose project name (the same label volumes and containers share).
func attachVolumes(projects []Project, vols map[string][]string) {
	for i := range projects {
		projects[i].Volumes = vols[projects[i].Name]
	}
}
