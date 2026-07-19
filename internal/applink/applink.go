// Package applink discovers each stack's Traefik-routed hostname(s) from live
// Docker container labels, feeding the app-link icon in the Stacks view
// (dev-docs/traefik-app-links-spec.md). Detection is read-only and
// best-effort: a docker failure or a stack with no Traefik labels simply
// yields no hosts for it, never an error surfaced to the user.
package applink

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// Outputter runs a command and returns its captured stdout.
// command.ShellRunner satisfies it.
type Outputter interface {
	Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// Snapshot maps each stack with at least one discovered host to its deduped,
// sorted Traefik hostnames. A stack with none is simply absent.
type Snapshot struct {
	Stacks map[string][]string `json:"stacks"`
}

// Config wires a Detector. Managed supplies, fresh on every detection, each
// active stack's compose project working_dir — the same identity orphan
// detection matches on (stable across a rollback's /tmp compose file,
// Invariant 3), rather than the compose project name, which is not
// guaranteed to equal the stack name. Publish (optional) receives each new
// snapshot.
type Config struct {
	Outputter Outputter
	Managed   func() map[string]string // stack name -> working_dir
	Publish   func(Snapshot)
}

// Detector finds Traefik-routed hostnames for skipper's managed stacks. It
// owns no timer: like orphan detection, the caller drives Detect on the
// health-poll cadence (UI-gated).
type Detector struct {
	out     Outputter
	managed func() map[string]string
	publish func(Snapshot)
	last    atomic.Pointer[Snapshot]
}

// New builds a Detector from cfg.
func New(cfg Config) *Detector {
	return &Detector{out: cfg.Outputter, managed: cfg.Managed, publish: cfg.Publish}
}

// Detect lists every running compose container's labels, extracts Traefik
// Host() rules keyed by project working_dir, matches that against the
// managed stack set, caches and publishes the snapshot, and returns it. A
// docker failure leaves the last snapshot in place (logged, not cleared) so a
// transient error does not blank the UI's app-link icons.
func (d *Detector) Detect(ctx context.Context) Snapshot {
	refs, err := d.listContainers(ctx)
	if err != nil {
		slog.Warn("app-link detection skipped: could not list compose containers", "err", err)
		return d.Current()
	}
	if len(refs) == 0 {
		return d.publishSnapshot(Snapshot{Stacks: map[string][]string{}})
	}

	labelMaps, err := d.inspectLabels(ctx, refs)
	if err != nil {
		slog.Warn("app-link detection skipped: could not inspect containers", "err", err)
		return d.Current()
	}

	hostsByDir := hostsByWorkingDir(refs, labelMaps)
	stacks := make(map[string][]string)
	for name, dir := range d.managed() {
		if hosts := hostsByDir[dir]; len(hosts) > 0 {
			stacks[name] = hosts
		}
	}
	return d.publishSnapshot(Snapshot{Stacks: stacks})
}

func (d *Detector) publishSnapshot(snap Snapshot) Snapshot {
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
	return Snapshot{Stacks: map[string][]string{}}
}
