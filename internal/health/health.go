// Package health polls the runtime health of skipper-cd's own stacks and
// publishes per-stack snapshots for the web UI. It is read-only and covers only
// the compose projects skipper deploys — it does not watch other host
// containers, node lifecycle, or raise alerts (ADR-0027).
package health

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Status is a stack's rolled-up runtime health.
type Status string

const (
	Healthy   Status = "healthy"
	Unhealthy Status = "unhealthy"
	Starting  Status = "starting"
	Stopped   Status = "stopped"
	// Unknown means the health could not be read (the ps command or its output
	// failed) — never reported as a false unhealthy.
	Unknown Status = "unknown"
)

// ServiceHealth is one service's container state and health, for the UI's
// per-service breakdown panel.
type ServiceHealth struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Health string `json:"health"`
}

// StackHealth is a stack's rolled-up status plus its per-service detail.
type StackHealth struct {
	Status   Status          `json:"status"`
	Services []ServiceHealth `json:"services,omitempty"`
}

// Snapshot is the health of every polled stack, published to the UI.
type Snapshot struct {
	Stacks map[string]StackHealth `json:"stacks"`
}

// StackRef identifies a stack to probe. ComposePath and ProjectDir mirror how
// the deploy path invokes compose (Invariant 1): the compose file comes from
// the repo clone, and ProjectDir (a stack's working_dir, possibly empty) is the
// --project-directory that fixes the compose project identity.
type StackRef struct {
	Name        string
	ComposePath string
	ProjectDir  string
}

// Outputter runs a command and returns its captured stdout.
// command.ShellRunner satisfies it.
type Outputter interface {
	Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// Config wires a Poller. Publish receives every fresh snapshot; HasSubscribers
// (optional) gates the periodic poll so it idles while no UI client is watching.
type Config struct {
	Outputter      Outputter
	Stacks         func() []StackRef
	Publish        func(Snapshot)
	Interval       time.Duration
	HasSubscribers func() bool
}

// Poller periodically probes each stack's health and publishes snapshots.
type Poller struct {
	out            Outputter
	stacks         func() []StackRef
	publish        func(Snapshot)
	interval       time.Duration
	hasSubscribers func() bool

	last    atomic.Pointer[Snapshot]
	trigger chan struct{}
}

// New builds a Poller from cfg.
func New(cfg Config) *Poller {
	return &Poller{
		out:            cfg.Outputter,
		stacks:         cfg.Stacks,
		publish:        cfg.Publish,
		interval:       cfg.Interval,
		hasSubscribers: cfg.HasSubscribers,
		trigger:        make(chan struct{}, 1),
	}
}

// Run polls on the configured interval until ctx is cancelled. Each tick polls
// only while a UI client is subscribed; a Poll request (client connect, deploy
// finished) always refreshes immediately.
func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		case <-p.trigger:
			p.pollOnce(ctx)
		}
	}
}

// Poll requests an immediate refresh. Non-blocking and coalescing: several
// requests before the poll runs collapse into one.
func (p *Poller) Poll() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}

// Current returns the latest published snapshot, for a client connecting
// between polls. Safe for concurrent use.
func (p *Poller) Current() Snapshot {
	if s := p.last.Load(); s != nil {
		return *s
	}
	return Snapshot{Stacks: map[string]StackHealth{}}
}

// tick polls only when someone is watching, so a UI-enabled but unattended host
// does no health work.
func (p *Poller) tick(ctx context.Context) {
	if p.hasSubscribers == nil || p.hasSubscribers() {
		p.pollOnce(ctx)
	}
}

// pollOnce probes every stack, stores the snapshot for late joiners, and
// publishes it.
func (p *Poller) pollOnce(ctx context.Context) {
	snap := Snapshot{Stacks: map[string]StackHealth{}}
	for _, s := range p.stacks() {
		snap.Stacks[s.Name] = p.probe(ctx, s)
	}
	p.last.Store(&snap)
	if p.publish != nil {
		p.publish(snap)
	}
}

// probe reads one stack's health via `docker compose ps`, using the same
// compose file + --project-directory identity the deploy path uses so the
// project resolves to exactly the containers skipper deployed. Any failure
// degrades to Unknown rather than a misleading Unhealthy.
func (p *Poller) probe(ctx context.Context, s StackRef) StackHealth {
	args := []string{"compose"}
	dir := filepath.Dir(s.ComposePath)
	if s.ProjectDir != "" {
		args = append(args, "-f", s.ComposePath, "--project-directory", s.ProjectDir)
		dir = s.ProjectDir
	}
	args = append(args, "ps", "--format", "json", "--all")

	out, err := p.out.Output(ctx, dir, "docker", args...)
	if err != nil {
		return StackHealth{Status: Unknown}
	}
	lines, err := parsePS(out)
	if err != nil {
		return StackHealth{Status: Unknown}
	}
	return StackHealth{Status: rollup(lines), Services: servicesOf(lines)}
}
