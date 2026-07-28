package main

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/roster"
	"github.com/polandy/skipper-cd/internal/selfheal"
)

// uiConfig returns a minimal config with the UI toggled and every pointer
// field the layer builders dereference populated (config.Load applies these
// defaults in production).
func uiConfig(ui bool) *config.Config {
	return &config.Config{
		UIEnabled:                        ptr(ui),
		RuntimeHealthPollIntervalSeconds: ptr(0),
		SelfHealCooldownSeconds:          ptr(0),
	}
}

func TestBuildUILayer_DisabledLeavesEveryFieldNil(t *testing.T) {
	l := buildUILayer(uiConfig(false), t.TempDir())

	if l.enabled() {
		t.Error("expected the zero layer to report disabled")
	}
	if l.history != nil || l.broadcaster != nil || l.stateB != nil || l.peerRegistry != nil {
		t.Errorf("expected every subsystem nil with the UI off, got %+v", l)
	}
	if l.deploySink() != nil || l.runPlanSink != nil || l.hookRunSink != nil {
		t.Error("expected no sinks with the UI off")
	}
}

func TestBuildUILayer_EnabledWiresHistoryBroadcastersAndStartID(t *testing.T) {
	stateDir := t.TempDir()
	// Pre-seed the persisted history so the layer must resume event IDs after
	// the highest recorded one instead of restarting at zero.
	events.NewHistory(stateDir).Add(events.DeployEvent{ID: 41, Stack: "gitea"})

	l := buildUILayer(uiConfig(true), stateDir)

	if !l.enabled() {
		t.Fatal("expected the layer to report enabled")
	}
	if l.history == nil || l.broadcaster == nil || l.stateB == nil {
		t.Fatal("expected history and both broadcasters wired")
	}
	if l.startEventID != 41 {
		t.Errorf("startEventID = %d, want the persisted maximum 41", l.startEventID)
	}
	if l.peerRegistry != nil {
		t.Error("expected no peer registry without peers configured")
	}
}

func TestBuildUILayer_PeerRegistryOnlyWithPeersConfigured(t *testing.T) {
	cfg := uiConfig(true)
	cfg.HostName = "primary"
	cfg.Peers = []config.Peer{{Name: "nuc", URL: "http://nuc:8000"}}

	l := buildUILayer(cfg, t.TempDir())

	if l.peerRegistry == nil {
		t.Error("expected the multi-host fan-in registry with peers configured")
	}
}

func TestUILayer_DeploySinkRecordsAndBroadcastsButSkipsSkippedFromHistory(t *testing.T) {
	l := buildUILayer(uiConfig(true), t.TempDir())
	sink := l.deploySink()
	if sink == nil {
		t.Fatal("expected a deploy sink with the UI enabled")
	}
	ch, cancel := l.broadcaster.Subscribe()
	defer cancel()

	sink(events.DeployEvent{ID: 1, Stack: "gitea", Status: events.StatusSkipped})
	sink(events.DeployEvent{ID: 2, Stack: "gitea", Status: events.StatusSuccess})

	got := l.history.Events()
	if len(got) != 1 || got[0].ID != 2 {
		t.Errorf("history = %v, want only the non-skipped event", got)
	}
	// Publish delivers into the subscriber's buffer before returning, so both
	// events are receivable deterministically.
	for _, wantID := range []int64{1, 2} {
		select {
		case e := <-ch:
			if e.ID != wantID {
				t.Errorf("broadcast event ID = %d, want %d", e.ID, wantID)
			}
		default:
			t.Fatalf("expected broadcast event %d buffered", wantID)
		}
	}
}

func TestUILayer_LookAheadSinksPublishStateEvents(t *testing.T) {
	l := buildUILayer(uiConfig(true), t.TempDir())
	ch, cancel := l.stateB.Subscribe()
	defer cancel()

	l.runPlanSink(deploy.RunPlan{})
	l.hookRunSink(deploy.HookRun{})

	for _, want := range []string{events.StateUpcoming, events.StateHookRun} {
		select {
		case e := <-ch:
			if e.Name != want {
				t.Errorf("state event = %q, want %q", e.Name, want)
			}
		default:
			t.Fatalf("expected state event %q buffered", want)
		}
	}
}

func TestBuildSelfHeal_NilWhenSelfHealIsOff(t *testing.T) {
	cfg := uiConfig(false)
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}

	if engine := buildSelfHeal(cfg, views, &deployerRef{}); engine != nil {
		t.Error("expected no engine while self-heal is inactive")
	}
}

func TestBuildSelfHeal_EngineWhenActive(t *testing.T) {
	cfg := uiConfig(false)
	cfg.SelfHeal = ptr(true)
	cfg.Stacks = []config.Stack{{Name: "gitea"}}
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}

	if engine := buildSelfHeal(cfg, views, &deployerRef{}); engine == nil {
		t.Error("expected an engine while self-heal is effective for a stack")
	}
}

func TestBuildHealthWatch_NilWithoutConfig(t *testing.T) {
	w, err := buildHealthWatch(t.Context(), uiConfig(false), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != nil {
		t.Error("expected no watcher without health_watch configured")
	}
}

func TestBuildHealthWatch_WatcherWithConfig(t *testing.T) {
	cfg := uiConfig(true)
	cfg.HealthWatch = &config.HealthWatch{AlertCooldownSeconds: ptr(0)}

	w, err := buildHealthWatch(t.Context(), cfg, t.TempDir(), events.NewStateBroadcaster())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Error("expected a watcher with health_watch configured")
	}
}

func TestBuildHealthLayer_NothingWhenPollingDisabled(t *testing.T) {
	cfg := uiConfig(true) // interval 0: polling explicitly off
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}

	hl := buildHealthLayer(cfg, views, events.NewStateBroadcaster(), nil, nil)

	if hl.poller != nil || hl.orphans != nil || hl.appLinks != nil {
		t.Errorf("expected an empty health layer with interval 0, got %+v", hl)
	}
}

func TestBuildHealthLayer_NothingWithoutAnyConsumer(t *testing.T) {
	cfg := uiConfig(false)
	cfg.RuntimeHealthPollIntervalSeconds = ptr(30)
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}

	hl := buildHealthLayer(cfg, views, nil, nil, nil)

	if hl.poller != nil {
		t.Error("expected no poller when neither UI, self-heal nor health-watch consumes it")
	}
}

func TestBuildHealthLayer_UIGetsPollerAndDetectors(t *testing.T) {
	cfg := uiConfig(true)
	cfg.RuntimeHealthPollIntervalSeconds = ptr(30)
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}

	hl := buildHealthLayer(cfg, views, events.NewStateBroadcaster(), nil, nil)

	if hl.poller == nil {
		t.Fatal("expected a poller with the UI on")
	}
	if hl.orphans == nil || hl.appLinks == nil {
		t.Error("expected orphan and app-link detection riding the poll cadence with the UI on")
	}
}

func TestBuildHealthLayer_HeadlessSelfHealGetsPollerWithoutDetectors(t *testing.T) {
	cfg := uiConfig(false)
	cfg.RuntimeHealthPollIntervalSeconds = ptr(30)
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}
	engine := selfheal.New(selfheal.Config{})

	hl := buildHealthLayer(cfg, views, nil, engine, nil)

	if hl.poller == nil {
		t.Fatal("expected a poller for headless self-heal (AlwaysPoll)")
	}
	if hl.orphans != nil || hl.appLinks != nil {
		t.Error("expected no UI-only detectors while the UI is off")
	}
}

// newAutosyncPublisher wires a publisher over a fresh controller/queue and the
// given deployer ref, against the given state broadcaster (nil = UI off).
func newAutosyncPublisher(t *testing.T, cfg *config.Config, stateB *events.Broadcaster[events.StateEvent], ref *deployerRef) autosyncPublisher {
	t.Helper()
	return autosyncPublisher{
		ctrl:     autosync.NewController(nil, nil),
		queue:    autosync.NewQueue(),
		stateB:   stateB,
		views:    stackViews{cfg: cfg, deployer: ref},
		deployer: ref,
		auditLog: audit.NewLog(t.TempDir()),
		repo:     roster.RepoRef{Dir: "/var/lib/skipper/repo"},
	}
}

func TestAutosyncPublisher_PublishesAutosyncQueueAndStacksOverSSE(t *testing.T) {
	stateB := events.NewStateBroadcaster()
	ref := &deployerRef{}
	ref.set(deploy.New(deploy.Config{}))
	p := newAutosyncPublisher(t, uiConfig(true), stateB, ref)
	ch, cancel := stateB.Subscribe()
	defer cancel()

	p.publish()

	for _, want := range []string{events.StateAutosync, events.StateQueue, events.StateStacks} {
		select {
		case e := <-ch:
			if e.Name != want {
				t.Errorf("state event = %q, want %q", e.Name, want)
			}
		default:
			t.Fatalf("expected state event %q buffered", want)
		}
	}
}

func TestAutosyncPublisher_HeadlessStillRefreshesGauges(t *testing.T) {
	// The ref is deliberately left unset: with the UI off the publisher must
	// refresh the gauges without ever resolving the deployer, and get() panics
	// if a regression pulls that resolution out of the stateB != nil branch.
	p := newAutosyncPublisher(t, uiConfig(false), nil, &deployerRef{})
	p.queue.Mark("gitea", nil, "autosync paused")

	p.publish() // must not publish (no stateB) and must not touch the deployer

	var m dto.Metric
	if err := metrics.AutosyncPending.Write(&m); err != nil {
		t.Fatalf("read gauge: %v", err)
	}
	if got := m.GetGauge().GetValue(); got != 1 {
		t.Errorf("autosync pending gauge = %v, want 1", got)
	}
}
