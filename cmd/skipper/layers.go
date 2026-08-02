package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/polandy/skipper-cd/internal/applink"
	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/health"
	"github.com/polandy/skipper-cd/internal/healthwatch"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/notify"
	"github.com/polandy/skipper-cd/internal/orphans"
	"github.com/polandy/skipper-cd/internal/peers"
	"github.com/polandy/skipper-cd/internal/registry"
	"github.com/polandy/skipper-cd/internal/roster"
	"github.com/polandy/skipper-cd/internal/safego"
	"github.com/polandy/skipper-cd/internal/selfheal"
	"github.com/polandy/skipper-cd/internal/updatecheck"
)

// uiLayer bundles the subsystems that exist only while the web UI is enabled:
// the persisted event history, the two SSE broadcasters, the look-ahead sinks
// and — with peers configured — the multi-host fan-in registry (ADR-0048).
// The zero value (UI disabled) leaves every field nil; consumers gate on that,
// exactly as they would on the individual nil subsystems.
type uiLayer struct {
	history      *events.History
	broadcaster  *events.Broadcaster[events.DeployEvent]
	stateB       *events.Broadcaster[events.StateEvent]
	startEventID int64
	runPlanSink  func(deploy.RunPlan)
	hookRunSink  func(deploy.HookRun)
	peerRegistry *peers.Registry
}

// buildUILayer constructs the UI-only subsystems, resuming event IDs after the
// highest one the persisted history carries. Returns the zero layer when the
// UI is disabled.
func buildUILayer(cfg *config.Config, stateDir string) uiLayer {
	if !*cfg.UIEnabled {
		return uiLayer{}
	}
	l := uiLayer{
		history:     events.NewHistory(stateDir),
		broadcaster: events.NewBroadcaster(),
		stateB:      events.NewStateBroadcaster(),
	}
	l.startEventID = l.history.MaxEventID()
	// Look-ahead: publish the run plan (what deploys next) over the same SSE
	// stream. Installing the sink is what enables the upfront planning pass.
	stateB := l.stateB
	l.runPlanSink = func(p deploy.RunPlan) {
		stateB.Publish(events.StateEvent{Name: events.StateUpcoming, Data: p})
	}
	l.hookRunSink = func(h deploy.HookRun) {
		stateB.Publish(events.StateEvent{Name: events.StateHookRun, Data: h})
	}
	// Multi-host fan-in (ADR-0048): when peers are configured, the primary
	// reads each peer's `/api/v1` surface and merges it into one UI. The poll
	// loop that refreshes it starts alongside the other loops in main.
	if len(cfg.Peers) > 0 {
		l.peerRegistry = peers.New(cfg.HostName, cfg.Peers, peers.NewHTTPClient(&http.Client{Timeout: peerPollTimeout}), peerPollTimeout)
	}
	slog.Info("web UI enabled")
	return l
}

// enabled reports whether the UI layer is live.
func (l uiLayer) enabled() bool { return l.broadcaster != nil }

// deploySink returns the UI's deploy-event consumer: record into the history
// (skipped events excluded — they carry no outcome worth persisting) and
// broadcast to connected SSE clients. Nil when the UI is disabled.
func (l uiLayer) deploySink() func(events.DeployEvent) {
	if !l.enabled() {
		return nil
	}
	return func(e events.DeployEvent) {
		if e.Status != events.StatusSkipped {
			l.history.Add(e)
		}
		l.broadcaster.Publish(e)
	}
}

// buildSelfHeal constructs the engine that automatically restores a stack the
// health poller finds degraded (ADR-0029). The engine owns the policy; the
// deployer performs the corrective redeploy — both closures resolve it through
// ref and only run from the health poller, which starts after the deployer is
// constructed. Returns nil when self-heal is effective for no stack.
func buildSelfHeal(cfg *config.Config, views stackViews, ref *deployerRef) *selfheal.Engine {
	if !cfg.SelfHealActive() {
		return nil
	}
	engine := selfheal.New(selfheal.Config{
		Healer: healerFunc(func(ctx context.Context, stack string, drift []events.DriftedService) (bool, error) {
			return ref.get().HealStack(ctx, cfg, stack, drift)
		}),
		Enabled:           func(name string) bool { return cfg.EffectiveSelfHeal(views.effective(), name) },
		MinUnhealthyPolls: cfg.SelfHealMinUnhealthyPolls,
		MaxAttempts:       cfg.SelfHealMaxAttempts,
		Cooldown:          time.Duration(*cfg.SelfHealCooldownSeconds) * time.Second,
		OnExhausted:       func(stack string) { ref.get().EmitHealExhausted(stack) },
	})
	slog.Info("self-heal enabled", "min_unhealthy_polls", cfg.SelfHealMinUnhealthyPolls, "max_attempts", cfg.SelfHealMaxAttempts, "cooldown_seconds", *cfg.SelfHealCooldownSeconds)
	return engine
}

// buildHealthWatch constructs the own-stack health watchdog (ADR-0031): it
// detects per-service health transitions and alerts on newly-failed and
// recovered services. It owns no poll loop — it consumes the shared health
// poller's snapshot feed (the ADR-0029 seam) and observes deploy events only
// for commit context; the deploy path is untouched. Its alert delivery loop
// runs until ctx is done. Returns nil when health_watch is not configured.
func buildHealthWatch(ctx context.Context, cfg *config.Config, stateDir string, stateB *events.Broadcaster[events.StateEvent]) (*healthwatch.Watcher, error) {
	hw := cfg.HealthWatch
	if hw == nil {
		return nil, nil
	}
	alerter, err := notify.NewHealthAlerter(hw.Targets, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("build health alerter: %w", err)
	}
	var alertSink healthwatch.Alerter
	if alerter.Enabled() {
		safego.Go("health-alerter", func() { alerter.Run(ctx) })
		alertSink = alerter
	}
	hwCfg := healthwatch.Config{
		Alerter:           alertSink,
		StatePath:         filepath.Join(stateDir, "healthwatch.yaml"),
		DebouncePolls:     hw.DebouncePolls,
		AttributionWindow: time.Duration(hw.AttributionWindowSeconds) * time.Second,
		AlertCooldown:     time.Duration(*hw.AlertCooldownSeconds) * time.Second,
	}
	if stateB != nil {
		// The per-service panel's since/history/commit feed (ADR-0031 UI
		// surface): every accepted change pushes the fresh view to UI clients.
		hwCfg.Publish = func(v healthwatch.View) { stateB.Publish(events.StateEvent{Name: events.StateHealthWatch, Data: v}) }
	}
	watcher := healthwatch.New(hwCfg)
	slog.Info("health watch enabled",
		"debounce_polls", hw.DebouncePolls,
		"alert_cooldown_seconds", *hw.AlertCooldownSeconds,
		"targets", len(hw.Targets),
	)
	return watcher, nil
}

// buildUpdateCheck constructs the read-only registry update checker
// (ADR-0054): every interval it compares what each stack's containers run
// against what their registries offer, publishes the result on the stacks
// snapshot (via onChange) and — only when update_check.notify opts in and a
// sink is configured — sends one message per newly appearing update. It acts
// on nothing. Returns nil when update_check.interval_seconds is 0. Run is
// started by the caller once the deployer exists; the alerter's delivery loop
// runs until ctx is done.
func buildUpdateCheck(ctx context.Context, cfg *config.Config, stateDir string, timeout time.Duration, views stackViews, ref *deployerRef, onChange func()) (*updatecheck.Checker, error) {
	interval := cfg.UpdateCheckInterval()
	if interval <= 0 {
		return nil, nil
	}

	var notifyFn func(updatecheck.Alert)
	if cfg.UpdateCheckNotify() && len(cfg.Notifications) > 0 {
		alerter, err := notify.NewUpdateAlerter(cfg.Notifications, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("build update alerter: %w", err)
		}
		safego.Go("update-alerter", func() { alerter.Run(ctx) })
		notifyFn = alerter.Fire
	}

	var checker *updatecheck.Checker
	checker = updatecheck.New(updatecheck.Config{
		Interval:  interval,
		Registry:  registry.New(nil, registry.DockerConfigCredentials(registry.DockerConfigPath())),
		Outputter: command.NewShellRunner(timeout),
		Running: func() map[string]map[string]string {
			if d := ref.get(); d != nil {
				return d.CurrentRunningImages()
			}
			return nil
		},
		Include: func(name string) bool {
			stacks := views.effective()
			for _, s := range stacks {
				if s.Name == name {
					return cfg.EffectiveUpdateCheck(stacks, name)
				}
			}
			// Not in the effective set: removed or parked — nothing to report on.
			return false
		},
		Notify: notifyFn,
		OnChange: func() {
			if snap := checker.Snapshot(); snap != nil {
				n := 0
				for _, services := range snap.Stacks {
					n += len(services)
				}
				metrics.UpdatesAvailable.Set(float64(n))
			}
			onChange()
		},
		StatePath: filepath.Join(stateDir, "update-check.yaml"),
	})
	slog.Info("registry update check enabled",
		"interval_seconds", int(interval/time.Second),
		"notify", notifyFn != nil,
	)
	return checker, nil
}

// healthLayer bundles the stack-health poller and the detectors that ride its
// cadence: orphan detection (ADR-0036) and app-link detection
// (dev-docs/traefik-app-links-spec.md). Every field is nil while health
// polling is off; the detectors are additionally nil while the UI is off.
type healthLayer struct {
	poller   *health.Poller
	orphans  *orphans.Detector
	appLinks *applink.Detector
}

// healthOutputEnv returns the environment the poller's `docker compose ps`
// runs in. The probe interpolates the same ${VAR} references the deploy path
// does, so it needs vars_file; without it compose warns per unset variable per
// stack on every tick — a continuous log stream at the poll interval. A
// vars_file that fails to load is not worth refusing to poll over: fall back
// to the process environment (nil), which the deploy path already surfaces the
// failure for loudly.
func healthOutputEnv(cfg *config.Config) []string {
	env, err := deploy.BaseEnv(cfg.VarsFile)
	if err != nil {
		slog.Warn("health poller falling back to the process environment", "err", err)
		return nil
	}
	return env
}

// buildHealthLayer constructs the stack-health poller. It feeds the UI health
// view (when enabled), self-heal, and/or the health watchdog. For the UI it is
// subscriber-gated so an idle dashboard does no docker work (ADR-0027);
// self-heal and the watchdog set AlwaysPoll so it still runs headless on an
// unattended host (ADR-0029, ADR-0031). Config validation guarantees a
// positive interval whenever self-heal or the watchdog is active. Returns the
// empty layer when polling is off or nothing consumes it.
func buildHealthLayer(cfg *config.Config, views stackViews, stateB *events.Broadcaster[events.StateEvent], selfHealEngine *selfheal.Engine, healthWatcher *healthwatch.Watcher) healthLayer {
	selfHealActive := selfHealEngine != nil
	hasConsumer := *cfg.UIEnabled || selfHealActive || healthWatcher != nil
	interval := *cfg.RuntimeHealthPollIntervalSeconds
	if interval <= 0 || !hasConsumer {
		return healthLayer{}
	}
	var hl healthLayer
	healthTimeout := time.Duration(cfg.CommandTimeoutSeconds) * time.Second
	hpCfg := health.Config{
		Outputter:  command.NewShellRunnerWithOutputEnv(healthTimeout, healthOutputEnv(cfg)),
		Stacks:     func() []health.StackRef { return health.StackRefs(cfg, views.effective()) },
		Interval:   time.Duration(interval) * time.Second,
		AlwaysPoll: selfHealActive || healthWatcher != nil,
	}
	if stateB != nil {
		hpCfg.Publish = func(s health.Snapshot) { stateB.Publish(events.StateEvent{Name: events.StateHealth, Data: s}) }
		hpCfg.HasSubscribers = stateB.HasSubscribers
	}
	var snapshotSinks []func(health.Snapshot)
	if selfHealEngine != nil {
		snapshotSinks = append(snapshotSinks, func(s health.Snapshot) { selfHealEngine.Observe(context.Background(), s) })
	}
	if healthWatcher != nil {
		snapshotSinks = append(snapshotSinks, healthWatcher.Observe)
	}
	// Orphan detection (ADR-0036) rides the health-poll cadence (no second
	// timer) and is UI-gated: HasSubscribers skips the headless AlwaysPoll ticks
	// self-heal/healthwatch drive.
	if stateB != nil {
		hl.orphans = orphans.New(orphans.Config{
			Outputter: command.NewShellRunner(healthTimeout),
			Managed:   views.managed,
			Publish:   func(s orphans.Snapshot) { stateB.Publish(events.StateEvent{Name: events.StateOrphans, Data: s}) },
		})
		orphanDetector := hl.orphans
		snapshotSinks = append(snapshotSinks, func(health.Snapshot) {
			if stateB.HasSubscribers() {
				orphanDetector.Detect(context.Background())
			}
		})
		// App-link detection (dev-docs/traefik-app-links-spec.md) rides the
		// same health-poll cadence for the same reason orphan detection does:
		// no second timer, UI-gated via HasSubscribers.
		hl.appLinks = applink.New(applink.Config{
			Outputter: command.NewShellRunner(healthTimeout),
			Managed:   views.appLinkDirs,
			Publish:   func(s applink.Snapshot) { stateB.Publish(events.StateEvent{Name: events.StateAppLinks, Data: s}) },
		})
		appLinkDetector := hl.appLinks
		snapshotSinks = append(snapshotSinks, func(health.Snapshot) {
			if stateB.HasSubscribers() {
				appLinkDetector.Detect(context.Background())
			}
		})
	}
	if len(snapshotSinks) > 0 {
		hpCfg.OnSnapshot = func(s health.Snapshot) {
			for _, sink := range snapshotSinks {
				sink(s)
			}
		}
	}
	hl.poller = health.New(hpCfg)
	slog.Info("stack health polling enabled", "interval_seconds", interval, "self_heal", selfHealActive, "health_watch", healthWatcher != nil)
	return hl
}

// autosyncPublisher refreshes the autosync metric gauges and republishes the
// autosync, queue and stacks snapshots over SSE. It runs once at wiring time
// (to initialize the gauges), after every deploy run (PostRunHook) and after
// every UI autosync toggle. With the UI off (stateB nil) only the gauges are
// refreshed and the deployer is never consulted.
type autosyncPublisher struct {
	ctrl     *autosync.Controller
	queue    *autosync.Queue
	stateB   *events.Broadcaster[events.StateEvent]
	views    stackViews
	deployer *deployerRef
	auditLog *audit.Log
	repo     roster.RepoRef
	updates  func() *updatecheck.Snapshot // latest update check; nil-safe (disabled)
}

func (p autosyncPublisher) publish() {
	snap := p.ctrl.Snapshot(p.views.order())
	metrics.AutosyncGlobal.Set(boolToFloat(p.ctrl.GlobalEffective()))
	for _, s := range snap.Stacks {
		metrics.AutosyncEnabled.WithLabelValues(s.Name).Set(boolToFloat(s.Effective))
	}
	metrics.AutosyncPending.Set(float64(p.queue.Count()))
	if p.stateB != nil {
		p.stateB.Publish(events.StateEvent{Name: events.StateAutosync, Data: snap})
		p.stateB.Publish(events.StateEvent{Name: events.StateQueue, Data: p.queue.View(p.views.order())})
		p.publishStacks()
	}
}

// publishStacks pushes the roster snapshot on its own. Wired to the deployer's
// StackSetSink as well as ridden by publish, so the one place that builds the
// snapshot serves both the end of a run and the moment stack discovery first
// learns the set — the UI would otherwise show an empty roster for the whole
// first run. A nil stateB (UI off) makes it a no-op.
func (p autosyncPublisher) publishStacks() {
	if p.stateB == nil {
		return
	}
	d := p.deployer.get()
	var updates *updatecheck.Snapshot
	if p.updates != nil {
		updates = p.updates()
	}
	p.stateB.Publish(events.StateEvent{Name: events.StateStacks, Data: roster.BuildState(p.views.effective(), d.CurrentDisabledStacks(), p.auditLog, d.CurrentTrackedFiles(), p.repo, updates)})
}
