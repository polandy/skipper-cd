// Package health polls the runtime health of skipper-cd's own stacks and
// publishes per-stack snapshots for the web UI. It is read-only and covers only
// the compose projects skipper deploys — it does not watch other host
// containers, node lifecycle, or raise alerts (ADR-0027).
package health

import (
	"context"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"
)

// Status is a stack's rolled-up runtime health.
type Status string

// The health states a stack rolls up to. The precedence between them when
// services disagree is rollup's, in parse.go — not this declaration order.
const (
	Healthy   Status = "healthy"
	Unhealthy Status = "unhealthy"
	Starting  Status = "starting"
	Stopped   Status = "stopped"
	// Unknown means the health could not be read (a docker call or its output
	// failed) — never reported as a false unhealthy.
	Unknown Status = "unknown"
)

// ServiceHealth is one service's container state and health, for the UI's
// per-service breakdown panel. Status is the classified per-service status —
// the same classification the stack rollup aggregates — consumed by the
// healthwatch transition detector (ADR-0031).
type ServiceHealth struct {
	Name string `json:"name"`
	// Image is the reference the container is actually running, which the UI
	// renders as the service's live version — the running counterpart to the
	// image the compose file declares. Empty when docker reported none, so the
	// UI simply shows no version.
	Image  string `json:"image,omitempty"`
	State  string `json:"state"`
	Health string `json:"health"`
	Status Status `json:"status"`
	// OnDemand marks a service whose container is in the stack's
	// on_demand_containers: skipper stops it after the deploy and a scheduler
	// starts it on request, so the UI labels it rather than letting an exited
	// state look like a problem.
	OnDemand bool `json:"on_demand,omitempty"`
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
// the repo clone, and ProjectDir (a stack's project_directory, possibly empty)
// is the --project-directory that fixes the compose project identity, which
// the poller matches containers on (projectDir). OnDemand lists
// the stack's on_demand_containers (container names): skipper stops those
// after the deploy on purpose, so an exited one classifies as stopped, never
// unhealthy.
type StackRef struct {
	Name        string
	ComposePath string
	ProjectDir  string
	OnDemand    []string
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
	// AlwaysPoll makes every periodic tick poll regardless of HasSubscribers.
	// Self-heal sets it so the poller runs headless on an unattended host — the
	// subscriber gate then guards only the display Publish, never the poll
	// itself (ADR-0029).
	AlwaysPoll bool
	// OnSnapshot, when set, receives every fresh snapshot in addition to Publish.
	// It is self-heal's feed; it runs on the poller goroutine, so a consumer that
	// blocks (e.g. taking the deploy mutex) delays the next tick, which is the
	// intended serialization.
	OnSnapshot func(Snapshot)
	// Now is the clock PollIfStale measures against; nil means time.Now.
	Now func() time.Time
}

// clientPollMinInterval is how fresh the last poll must be for PollIfStale to
// skip, so a client reconnecting in a loop cannot keep docker busy.
const clientPollMinInterval = 5 * time.Second

// Poller periodically probes each stack's health and publishes snapshots.
type Poller struct {
	out            Outputter
	stacks         func() []StackRef
	publish        func(Snapshot)
	interval       time.Duration
	hasSubscribers func() bool
	alwaysPoll     bool
	onSnapshot     func(Snapshot)
	now            func() time.Time

	last       atomic.Pointer[Snapshot]
	lastPolled atomic.Int64 // UnixNano of the last finished poll
	trigger    chan struct{}
}

// New builds a Poller from cfg.
func New(cfg Config) *Poller {
	p := &Poller{
		out:            cfg.Outputter,
		stacks:         cfg.Stacks,
		publish:        cfg.Publish,
		interval:       cfg.Interval,
		hasSubscribers: cfg.HasSubscribers,
		alwaysPoll:     cfg.AlwaysPoll,
		onSnapshot:     cfg.OnSnapshot,
		now:            cfg.Now,
		trigger:        make(chan struct{}, 1),
	}
	if p.now == nil {
		p.now = time.Now
	}
	return p
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

// PollIfStale is Poll for a request an unauthenticated client can repeat (a UI
// connect, a snapshot fetch): it skips when the last poll finished within
// clientPollMinInterval, since Current is then fresh anyway.
func (p *Poller) PollIfStale() {
	if p.now().Sub(time.Unix(0, p.lastPolled.Load())) < clientPollMinInterval {
		return
	}
	p.Poll()
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
// does no health work — unless AlwaysPoll is set (self-heal), which needs a
// snapshot every interval regardless of who is watching.
func (p *Poller) tick(ctx context.Context) {
	if p.alwaysPoll || p.hasSubscribers == nil || p.hasSubscribers() {
		p.pollOnce(ctx)
	}
}

// pollOnce reads every stack's containers in one batch, stores the snapshot
// for late joiners, publishes it to the UI, and feeds it to any snapshot
// consumer (self-heal). A failed read reports every stack Unknown rather than
// a misleading Unhealthy.
func (p *Poller) pollOnce(ctx context.Context) {
	snap := Snapshot{Stacks: map[string]StackHealth{}}
	if stacks := p.stacks(); len(stacks) > 0 {
		byDir, err := p.listContainers(ctx)
		for _, s := range stacks {
			if err != nil {
				snap.Stacks[s.Name] = StackHealth{Status: Unknown}
				continue
			}
			lines := slices.Clone(byDir[s.projectDir()])
			markOnDemand(lines, s.OnDemand)
			snap.Stacks[s.Name] = StackHealth{Status: rollup(lines), Services: servicesOf(lines)}
		}
	}
	p.last.Store(&snap)
	p.lastPolled.Store(p.now().UnixNano())
	if p.publish != nil {
		p.publish(snap)
	}
	if p.onSnapshot != nil {
		p.onSnapshot(snap)
	}
}

// projectDir is the working_dir compose labels the stack's containers with:
// project_directory when set, else the compose file's own dir — the identity
// the deploy path gives the project (Invariant 1).
func (s StackRef) projectDir() string {
	if s.ProjectDir != "" {
		return filepath.Clean(s.ProjectDir)
	}
	return filepath.Dir(s.ComposePath)
}

// markOnDemand flags the lines whose container is one of the stack's
// on_demand_containers, so classification treats their exited state as the
// intended idle, not a failure.
func markOnDemand(lines []psLine, onDemand []string) {
	if len(onDemand) == 0 {
		return
	}
	set := make(map[string]bool, len(onDemand))
	for _, n := range onDemand {
		set[n] = true
	}
	for i := range lines {
		if set[lines[i].Name] {
			lines[i].onDemand = true
		}
	}
}
