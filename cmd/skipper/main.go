// skipper-cd is a lightweight Docker Compose CD tool.
// It listens for push webhooks, pulls the repository, and deploys
// changed Docker Compose stacks. Unchanged stacks are skipped via hash tracking.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/polandy/skipper-cd/internal/applink"
	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/containerlogs"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/health"
	"github.com/polandy/skipper-cd/internal/healthwatch"
	"github.com/polandy/skipper-cd/internal/icons"
	"github.com/polandy/skipper-cd/internal/logbuf"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/notify"
	"github.com/polandy/skipper-cd/internal/orphans"
	"github.com/polandy/skipper-cd/internal/peers"
	"github.com/polandy/skipper-cd/internal/prettylog"
	"github.com/polandy/skipper-cd/internal/reconcile"
	"github.com/polandy/skipper-cd/internal/roster"
	"github.com/polandy/skipper-cd/internal/safego"
	"github.com/polandy/skipper-cd/internal/selfheal"
	"github.com/polandy/skipper-cd/internal/ui"
	"github.com/polandy/skipper-cd/internal/webhook"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// readHeaderTimeout bounds how long a client may take to send request
// headers, protecting the servers against slowloris-style connections.
const readHeaderTimeout = 10 * time.Second

// shutdownTimeout bounds how long in-flight HTTP requests (e.g. open SSE
// streams) may delay shutdown after a termination signal.
const shutdownTimeout = 10 * time.Second

const (
	// peerPollTimeout bounds a single peer read (its snapshot + audit fetch) in
	// the multi-host fan-in (ADR-0048), so one slow or hung peer cannot stall
	// the poll loop.
	peerPollTimeout = 10 * time.Second
	// peerPollFallbackSeconds is the fan-in poll cadence used when the runtime
	// health poll — whose cadence the fan-in normally rides — is disabled.
	peerPollFallbackSeconds = 30
)

// Build identity surfaced in the UI header (GET /api/version), injected via
// -ldflags at build time:
//   - version: semver from .release-please-manifest.json ("dev" for local builds).
//   - commit:  short git SHA. The Nix flake and Docker inject it; a local
//     `go build` leaves it empty and it is recovered from the Go build info.
//   - branch:  git branch name. Only CI/Docker builds know it — the Nix flake
//     and plain local builds leave it empty.
var (
	version = "dev"
	commit  = ""
	branch  = ""
)

// commitLen bounds the short commit rendered in the header.
const commitLen = 12

// resolveCommit returns the short commit identity for the running build. A
// commit injected via -ldflags (Nix/Docker) always wins; otherwise it falls
// back to the VCS revision Go stamps into the build info for a local `go build`
// in a git tree, suffixed "-dirty" for an uncommitted tree. Returns "" when
// neither source is available.
func resolveCommit(injected string, info *debug.BuildInfo, ok bool) string {
	if injected != "" {
		return short(injected)
	}
	if !ok || info == nil {
		return ""
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return ""
	}
	rev = short(rev)
	if dirty {
		rev += "-dirty"
	}
	return rev
}

// short truncates the hex part of a commit SHA to commitLen characters while
// preserving a trailing marker such as "-dirty" (as produced by the Nix flake's
// dirtyShortRev). A full 40-char SHA from Docker/CI is shortened; an already
// short revision is left intact.
func short(sha string) string {
	hexPart, rest, hasRest := strings.Cut(sha, "-")
	if len(hexPart) > commitLen {
		hexPart = hexPart[:commitLen]
	}
	if hasRest {
		return hexPart + "-" + rest
	}
	return hexPart
}

func main() {
	configPath := flag.String("config", "/etc/skipper/skipper.yml", "path to the skipper.yml config file")
	validate := flag.Bool("validate", false, "validate the config file (and the discovered stack set, when the repo clone exists) and exit: 0 = valid, 1 = invalid")
	flag.Parse()

	if *validate {
		os.Exit(validateConfigFile(*configPath, os.Stdout))
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}
	// With the UI enabled, tee all slog output (and, via sink below, child
	// process output) into an in-memory ring served live at /api/logs.
	var logRing *logbuf.Log
	logHandler := newLogHandler(cfg.LogFormat, os.Stderr)
	if *cfg.UIEnabled {
		logRing = logbuf.New(logbuf.DefaultCapacity)
		logHandler = logbuf.NewHandler(logHandler, logRing)
	}
	slog.SetDefault(slog.New(logHandler))

	// Config load already rejected clear errors; these are valid-but-suspicious
	// setups worth flagging without refusing to start. Logged first, before any
	// other startup line, so they are the first thing an operator sees.
	for _, w := range cfg.Warnings {
		slog.Warn("config warning", "msg", w)
	}

	// Assign the sink only for a non-nil ring: a typed-nil *logbuf.Log in
	// the interface would defeat the runner's sink != nil check.
	var sink command.LineSink
	if logRing != nil {
		sink = logRing
	}

	timeout := time.Duration(cfg.CommandTimeoutSeconds) * time.Second

	slog.Info("skipper-cd starting",
		"stacks", len(cfg.Stacks),
		"stack_discovery", cfg.StackDiscovery,
		"webhook_port", cfg.Port,
		"metrics_port", cfg.MetricsPort,
		"branch", cfg.Branch,
		"command_timeout", timeout,
	)

	// Log the effective stack set (name, hooks, watch dirs) once so an
	// operator can see what skipper is watching without waiting for a deploy
	// event. A static host `stacks:` list is known now; stack-discovery mode
	// (ADR-0034) only learns it once the first sync resolves it, so the
	// PostRunHook wiring below calls this again on every run — sync.Once makes
	// every call after the first a no-op.
	var rosterOnce sync.Once
	logRosterOnce := func(stacks []config.Stack, disabled []string) {
		rosterOnce.Do(func() { logStackRoster(stacks, disabled) })
	}
	if !cfg.StackDiscovery {
		logRosterOnce(cfg.Stacks, nil)
	}

	// Cancels on SIGINT/SIGTERM. The deployer abandons a pending
	// nixos-rebuild wait when it fires (ADR-0014).
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repoSync := git.NewRepoSync(cfg.RepoURL, cfg.RepoDir, cfg.Branch, timeout, sink)
	repoReader := git.NewRepoReader(repoSync.RepoDir(), timeout, sink)
	stateDir := filepath.Dir(repoSync.RepoDir())
	// Deriving from repo_url strips any credentials, so a repo_url holding a
	// token stays out of the UI (the browse URL only ever reaches the browser as
	// a link target).
	repo := repoRef{dir: repoSync.RepoDir(), webURL: cfg.EffectiveRepoWebURL()}

	// The deployer is constructed below via deploy.New once all its
	// collaborators are built — deploy.Config wires everything at construction.
	// Closures built earlier (e.g. the self-heal healer) capture this variable;
	// they only run once the background loops start, after the construction.
	var deployer *deploy.Deployer

	// stacksNow returns the effective stack set: the host config's static list,
	// or — in stack-discovery mode (ADR-0034) — the set most recently discovered
	// from the repo (empty until the startup sync completes). Every consumer
	// that enumerates stacks (health poller, self-heal, autosync order, icons)
	// reads through this so discovery updates reach them on the next call.
	stacksNow := func() []config.Stack {
		if cfg.StackDiscovery {
			return deployer.CurrentStacks()
		}
		return cfg.Stacks
	}

	// managedNow builds the expected set for orphan detection (ADR-0036) from the
	// effective stacks, the disabled set, and the recorded project dirs.
	managedNow := func() orphans.Managed {
		m := orphans.Managed{
			BaseDir:      cfg.StacksBaseDir,
			ActiveDirs:   map[string]bool{},
			DisabledDirs: map[string]bool{},
			StateDirs:    deployer.CurrentProjectDirs(),
		}
		for _, s := range stacksNow() {
			m.ActiveDirs[stackProjectDir(cfg, s)] = true
		}
		for _, name := range deployer.CurrentDisabledStacks() {
			m.DisabledDirs[filepath.Join(cfg.StacksBaseDir, name)] = true
		}
		return m
	}

	// appLinkManaged builds the stack-name -> working_dir map app-link
	// detection matches discovered Traefik hosts against (dev-docs/traefik-app-links-spec.md).
	// Only active stacks: a parked (disabled) or removed stack has no icon.
	appLinkManaged := func() map[string]string {
		stacks := stacksNow()
		m := make(map[string]string, len(stacks))
		for _, s := range stacks {
			m[s.Name] = stackProjectDir(cfg, s)
		}
		return m
	}

	var (
		broadcaster     *events.Broadcaster[events.DeployEvent]
		stateB          *events.Broadcaster[events.StateEvent]
		history         *events.History
		healthPoller    *health.Poller
		orphanDetector  *orphans.Detector
		appLinkDetector *applink.Detector
		peerRegistry    *peers.Registry
		startEventID    int64
		runPlanSink     func(deploy.RunPlan)
		hookRunSink     func(deploy.HookRun)
	)
	// The deploy event sink is composed from whatever consumers are configured;
	// each is independent, so notifications work with the UI off (ADR-0020).
	var eventSinks []func(events.DeployEvent)
	// Tallies every run's per-stack outcomes for the "run complete" summary
	// PostRunHook logs below; independent of the UI so it works headless too.
	tally := newRunTally()
	eventSinks = append(eventSinks, tally.observe)
	if *cfg.UIEnabled {
		history = events.NewHistory(stateDir)
		broadcaster = events.NewBroadcaster()
		stateB = events.NewStateBroadcaster()
		startEventID = history.MaxEventID()
		eventSinks = append(eventSinks, func(e events.DeployEvent) {
			if e.Status != events.StatusSkipped {
				history.Add(e)
			}
			broadcaster.Publish(e)
		})
		// Look-ahead: publish the run plan (what deploys next) over the same SSE
		// stream. Installing the sink is what enables the upfront planning pass.
		runPlanSink = func(p deploy.RunPlan) {
			stateB.Publish(events.StateEvent{Name: events.StateUpcoming, Data: p})
		}
		hookRunSink = func(h deploy.HookRun) {
			stateB.Publish(events.StateEvent{Name: events.StateHookRun, Data: h})
		}
		// Multi-host fan-in (ADR-0048): when peers are configured, the primary
		// reads each peer's `/api/v1` surface and merges it into one UI. The poll
		// loop that refreshes it starts alongside the other loops below.
		if len(cfg.Peers) > 0 {
			peerRegistry = peers.New(cfg.HostName, cfg.Peers, peers.NewHTTPClient(&http.Client{Timeout: peerPollTimeout}), peerPollTimeout)
		}
		slog.Info("web UI enabled")
	}

	// Durable per-stack deploy audit trail (ADR-0033). Recorded unconditionally
	// so the history is complete even with the UI off; only the query API below
	// is UI-gated. Empty stateDir disables persistence (in-memory only).
	auditLog := audit.NewLog(stateDir)
	eventSinks = append(eventSinks, auditLog.Record)

	// Outbound notifications. Config is already validated in Load; New only
	// re-derives formatters, so an error here is a programming bug, not user
	// input. Timeout 0 uses notify's built-in per-request default.
	notifier, err := notify.New(cfg.Notifications, nil, 0)
	if err != nil {
		slog.Error("failed to build notifier", "err", err)
		os.Exit(1)
	}
	if notifier.Enabled() {
		safego.Go("notifier", func() { notifier.Run(signalCtx) })
		eventSinks = append(eventSinks, notifier.Notify)
		slog.Info("notifications enabled", "targets", len(cfg.Notifications))
	}

	// Self-heal: automatically restore a stack the health poller finds degraded
	// (ADR-0029). The engine owns the policy; the deployer performs the
	// corrective redeploy. A real git deploy of a stack resets its breaker so a
	// push that fixes the fault grants a fresh attempt budget.
	selfHealActive := cfg.SelfHealActive()
	var selfHealEngine *selfheal.Engine
	if selfHealActive {
		selfHealEngine = selfheal.New(selfheal.Config{
			// Both closures capture the deployer variable; they only run from
			// the health poller, which starts after the deployer is constructed.
			Healer: healerFunc(func(ctx context.Context, stack string, drift []events.DriftedService) (bool, error) {
				return deployer.HealStack(ctx, cfg, stack, drift)
			}),
			Enabled:           func(name string) bool { return cfg.EffectiveSelfHeal(stacksNow(), name) },
			MinUnhealthyPolls: cfg.SelfHealMinUnhealthyPolls,
			MaxAttempts:       cfg.SelfHealMaxAttempts,
			Cooldown:          time.Duration(*cfg.SelfHealCooldownSeconds) * time.Second,
			OnExhausted:       func(stack string) { deployer.EmitHealExhausted(stack) },
		})
		eventSinks = append(eventSinks, func(e events.DeployEvent) {
			if e.Status == events.StatusDeploying {
				selfHealEngine.Reset(e.Stack)
			}
		})
		slog.Info("self-heal enabled", "min_unhealthy_polls", cfg.SelfHealMinUnhealthyPolls, "max_attempts", cfg.SelfHealMaxAttempts, "cooldown_seconds", *cfg.SelfHealCooldownSeconds)
	}

	// Own-stack health watchdog (ADR-0031): detects per-service health
	// transitions and alerts on newly-failed and recovered services. It owns no
	// poll loop — it consumes the shared health poller's snapshot feed below
	// (the ADR-0029 seam) and observes deploy events only for commit context;
	// the deploy path is untouched.
	var healthWatcher *healthwatch.Watcher
	if hw := cfg.HealthWatch; hw != nil {
		alerter, err := notify.NewHealthAlerter(hw.Targets, nil, 0)
		if err != nil {
			slog.Error("failed to build health alerter", "err", err)
			os.Exit(1)
		}
		var alertSink healthwatch.Alerter
		if alerter.Enabled() {
			safego.Go("health-alerter", func() { alerter.Run(signalCtx) })
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
		healthWatcher = healthwatch.New(hwCfg)
		eventSinks = append(eventSinks, healthWatcher.ObserveDeploy)
		slog.Info("health watch enabled",
			"debounce_polls", hw.DebouncePolls,
			"alert_cooldown_seconds", *hw.AlertCooldownSeconds,
			"targets", len(hw.Targets),
		)
	}

	// Stack-health poller. It feeds the UI health view (when enabled),
	// self-heal, and/or the health watchdog. For the UI it is subscriber-gated
	// so an idle dashboard does no docker work (ADR-0027); self-heal and the
	// watchdog set AlwaysPoll so it still runs headless on an unattended host
	// (ADR-0029, ADR-0031). Config validation guarantees a positive interval
	// whenever self-heal or the watchdog is active.
	if interval := *cfg.RuntimeHealthPollIntervalSeconds; interval > 0 && (*cfg.UIEnabled || selfHealActive || healthWatcher != nil) {
		healthTimeout := time.Duration(cfg.CommandTimeoutSeconds) * time.Second
		hpCfg := health.Config{
			Outputter:  command.NewShellRunner(healthTimeout),
			Stacks:     func() []health.StackRef { return healthStacks(cfg, stacksNow()) },
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
			orphanDetector = orphans.New(orphans.Config{
				Outputter: command.NewShellRunner(healthTimeout),
				Managed:   managedNow,
				Publish:   func(s orphans.Snapshot) { stateB.Publish(events.StateEvent{Name: events.StateOrphans, Data: s}) },
			})
			snapshotSinks = append(snapshotSinks, func(health.Snapshot) {
				if stateB.HasSubscribers() {
					orphanDetector.Detect(context.Background())
				}
			})
			// App-link detection (dev-docs/traefik-app-links-spec.md) rides the
			// same health-poll cadence for the same reason orphan detection does:
			// no second timer, UI-gated via HasSubscribers.
			appLinkDetector = applink.New(applink.Config{
				Outputter: command.NewShellRunner(healthTimeout),
				Managed:   appLinkManaged,
				Publish:   func(s applink.Snapshot) { stateB.Publish(events.StateEvent{Name: events.StateAppLinks, Data: s}) },
			})
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
		healthPoller = health.New(hpCfg)
		slog.Info("stack health polling enabled", "interval_seconds", interval, "self_heal", selfHealActive, "health_watch", healthWatcher != nil)
	}

	// The deploy event sink fans out to all configured consumers.
	var eventSink func(events.DeployEvent)
	if len(eventSinks) > 0 {
		eventSink = func(e events.DeployEvent) {
			for _, s := range eventSinks {
				s(e)
			}
		}
	}

	// Autosync is active regardless of the UI so config-as-code pauses apply.
	autosyncCtrl := autosync.NewController(cfg.Autosync, stackAutosyncConfig(cfg))
	autosyncQueue := autosync.NewQueue()
	order := func() []string { return deployOrder(cfg, stacksNow()) }
	publishAutosync := func() {
		snap := autosyncCtrl.Snapshot(order())
		metrics.AutosyncGlobal.Set(boolToFloat(autosyncCtrl.GlobalEffective()))
		for _, s := range snap.Stacks {
			metrics.AutosyncEnabled.WithLabelValues(s.Name).Set(boolToFloat(s.Effective))
		}
		metrics.AutosyncPending.Set(float64(autosyncQueue.Count()))
		if stateB != nil {
			stateB.Publish(events.StateEvent{Name: events.StateAutosync, Data: snap})
			stateB.Publish(events.StateEvent{Name: events.StateQueue, Data: autosyncQueue.View(order())})
			stateB.Publish(events.StateEvent{Name: events.StateStacks, Data: buildStacksState(stacksNow(), deployer.CurrentDisabledStacks(), auditLog, deployer.CurrentTrackedFiles(), repo)})
		}
	}
	deployer = deploy.New(deploy.Config{
		Runner: command.NewShellRunnerWithSink(timeout, sink),
		// Rollout reads container state via `docker compose ps`; a plain
		// (non-sink) runner captures its stdout (ADR-0040).
		Outputter:    command.NewShellRunner(timeout),
		CommitReader: repoReader,
		Syncer:       repoSync,
		RepoDir:      repoSync.RepoDir(),
		StateDir:     stateDir,
		ShutdownCtx:  signalCtx,
		EventSink:    eventSink,
		StartEventID: startEventID,
		Autosync:     autosyncCtrl,
		Queue:        autosyncQueue,
		PostRunHook: func() {
			logRunSummary(tally.flush())
			// In stack-discovery mode the set is only known once the first run
			// resolves it; a static host list already logged this at startup, so
			// every call after the first is a no-op (sync.Once).
			logRosterOnce(stacksNow(), deployer.CurrentDisabledStacks())
			publishAutosync()
			if healthPoller != nil {
				healthPoller.Poll() // refresh health right after a deploy run
			}
		},
		RunPlanSink: runPlanSink,
		HookRunSink: hookRunSink,
	})

	// Start the health poller only now that the deployer exists: its snapshot
	// feed may drive a self-heal, which redeploys through the deployer.
	if healthPoller != nil {
		safego.Go("health-poller", func() { healthPoller.Run(signalCtx) })
	}
	publishAutosync() // initialize the gauges

	// Sync repo and deploy on startup to catch changes that occurred while skipper-cd was not running.
	safego.Go("startup-sync", func() { deployer.SyncAndDeployAll(context.Background(), cfg) })

	// Periodic reconcile: re-run sync + deploy on a timer so a missed or lost
	// webhook cannot leave the host drifted from the deploy repo indefinitely
	// (ADR-0028). Not UI-gated — it is a correctness feature that must run
	// headless. A tick is skipped while a deploy is already in flight.
	if interval := *cfg.ReconcileIntervalSeconds; interval > 0 {
		loop := reconcile.New(time.Duration(interval)*time.Second, deployReconciler{deployer, cfg})
		safego.Go("reconcile", func() { loop.Run(signalCtx) })
		slog.Info("periodic reconcile enabled", "interval_seconds", interval)
	}

	// Multi-host fan-in poll loop (ADR-0048): refresh every peer's read data on
	// the health-poll cadence and republish the merged `peers` state over SSE.
	// UI-only — it exists only when the UI is on and peers are configured.
	if peerRegistry != nil {
		interval := *cfg.RuntimeHealthPollIntervalSeconds
		if interval <= 0 {
			interval = peerPollFallbackSeconds
		}
		d := time.Duration(interval) * time.Second
		safego.Go("peers-fanin", func() {
			t := time.NewTicker(d)
			defer t.Stop()
			for {
				peerRegistry.Poll(signalCtx)
				stateB.Publish(events.StateEvent{Name: events.StatePeers, Data: peerRegistry.State()})
				select {
				case <-signalCtx.Done():
					return
				case <-t.C:
				}
			}
		})
		slog.Info("multi-host fan-in enabled", "peers", len(cfg.Peers), "poll_interval_seconds", interval)
	}

	as := &autosyncDeps{
		ctrl:    autosyncCtrl,
		queue:   autosyncQueue,
		stateB:  stateB,
		order:   order,
		publish: publishAutosync,
		trigger: func() { safego.Go("autosync-trigger", func() { deployer.SyncAndDeployAll(context.Background(), cfg) }) },
	}

	bi, ok := debug.ReadBuildInfo()
	build := ui.BuildInfo{
		Version: version,
		Branch:  branch,
		Commit:  resolveCommit(commit, bi, ok),
	}

	startServer("metrics", cfg.MetricsPort, metricsMux())
	webhookServer := startServer("webhook", cfg.Port, webhookMux(webhookDeps{
		cfg:             cfg,
		stacks:          stacksNow,
		deployer:        deployer,
		healthPoller:    healthPoller,
		healthWatcher:   healthWatcher,
		orphanDetector:  orphanDetector,
		appLinkDetector: appLinkDetector,
		peerRegistry:    peerRegistry,
		broadcaster:     broadcaster,
		history:         history,
		auditLog:        auditLog,
		logRing:         logRing,
		autosync:        as,
		build:           build,
		repo:            repo,
	}))

	// Block until SIGINT/SIGTERM, then shut down gracefully: stop accepting
	// requests, then let an in-flight deploy finish so docker compose is not
	// interrupted mid-run.
	<-signalCtx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := webhookServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("webhook server shutdown", "err", err)
	}

	slog.Info("waiting for in-flight deploy to finish")
	deployer.WaitIdle()
	slog.Info("skipper-cd stopped")
}

// newLogHandler returns the slog handler for the configured log_format: a
// JSON handler for "json", a logfmt text handler for "text", and prettylog's
// colored console handler otherwise (the default, "pretty").
func newLogHandler(format string, w io.Writer) slog.Handler {
	switch format {
	case config.LogFormatJSON:
		return slog.NewJSONHandler(w, nil)
	case config.LogFormatText:
		return slog.NewTextHandler(w, nil)
	default:
		return prettylog.New(w)
	}
}

func metricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

// stacksState is the `stacks` SSE snapshot: stack-set facts that are not
// deploy events. Disabled carries the names parked via disabled: true in
// stack-discovery mode (ADR-0034), driving the Deploys view's disabled line
// (empty in host-list mode). Roster is the full inventory for the Stacks view —
// every declared stack with its last outcome (dev-docs/stack-roster-spec.md).
type stacksState struct {
	Disabled []string       `json:"disabled"`
	Roster   []roster.Entry `json:"roster"`
	// RepoWebURL is the deploy repo's forge browse URL, so the UI can turn every
	// commit SHA it shows into a link to that commit. Empty when none can be
	// derived from repo_url — the UI then renders SHAs as plain text. It rides
	// on this state (rather than a dedicated endpoint) so the multi-host fan-in
	// carries each peer's own forge along with that peer's roster (ADR-0048).
	RepoWebURL string `json:"repo_web_url,omitempty"`
}

// repoRef identifies the deploy repo for the UI: the local clone directory (to
// render tracked input paths repo-relative) and the forge browse URL derived
// from repo_url (to link commit SHAs). Both are fixed for a process lifetime.
type repoRef struct {
	dir    string
	webURL string
}

// buildStacksState assembles the `stacks` snapshot from the effective stack
// set, the parked (disabled) names, each stack's newest audit record, and the
// input paths its change detection watches.
func buildStacksState(stacks []config.Stack, disabled []string, auditLog *audit.Log, tracked map[string][]string, repo repoRef) stacksState {
	last := func(name string) (audit.Record, bool) {
		recs := auditLog.Stack(name, 1)
		if len(recs) == 0 {
			return audit.Record{}, false
		}
		return recs[0], true
	}
	watched := func(name string) ([]string, bool) { return splitTrackedPaths(tracked[name], repo.dir) }
	return stacksState{
		Disabled:   disabled,
		Roster:     roster.Build(stacks, disabled, last, watched),
		RepoWebURL: repo.webURL,
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

// autosyncDeps bundles the autosync wiring the UI handlers need.
type autosyncDeps struct {
	ctrl    *autosync.Controller
	queue   *autosync.Queue
	stateB  *events.Broadcaster[events.StateEvent]
	order   func() []string
	publish func() // publish snapshots + refresh gauges
	trigger func() // start a deploy run (drains the queue)
}

// webhookDeps bundles everything the webhook + UI mux wires together, so the
// route table is built from one cohesive value instead of a long positional
// argument list.
type webhookDeps struct {
	cfg             *config.Config
	stacks          func() []config.Stack
	deployer        *deploy.Deployer
	healthPoller    *health.Poller
	healthWatcher   *healthwatch.Watcher
	orphanDetector  *orphans.Detector
	appLinkDetector *applink.Detector
	peerRegistry    *peers.Registry
	broadcaster     *events.Broadcaster[events.DeployEvent]
	history         *events.History
	auditLog        *audit.Log
	logRing         *logbuf.Log
	autosync        *autosyncDeps
	build           ui.BuildInfo
	repo            repoRef
}

// deployReconciler adapts the deployer to reconcile.Reconciler, binding the
// process config so each reconcile pass runs the same skip-if-busy sync +
// deploy a webhook does. It keeps the reconcile package free of any deploy or
// config dependency.
type deployReconciler struct {
	deployer *deploy.Deployer
	cfg      *config.Config
}

func (r deployReconciler) Reconcile(ctx context.Context) bool {
	return r.deployer.TrySyncAndDeployAll(ctx, r.cfg)
}

// containerLogResolver adapts the deployer + health poller to
// containerlogs.Resolver: the deployer supplies the compose invocation (reusing
// the deploy path so a logs read targets the same project), the poller supplies
// the current service names for validation.
type containerLogResolver struct {
	cfg      *config.Config
	deployer *deploy.Deployer
	health   *health.Poller
}

func (r containerLogResolver) Resolve(stack string) (containerlogs.Invocation, []string, bool, error) {
	dir, env, args, ok, err := r.deployer.LogComposeInvocation(r.cfg, stack)
	if err != nil || !ok {
		return containerlogs.Invocation{}, nil, ok, err
	}
	return containerlogs.Invocation{Dir: dir, Env: env, Args: args}, servicesOf(r.health.Current(), stack), true, nil
}

// servicesOf returns the service names the health snapshot currently reports
// for a stack, or nil if the stack is not (yet) in the snapshot.
func servicesOf(snap health.Snapshot, stack string) []string {
	sh, ok := snap.Stacks[stack]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(sh.Services))
	for _, s := range sh.Services {
		names = append(names, s.Name)
	}
	return names
}

// healerFunc adapts a plain function to selfheal.Healer, so main can wire the
// deployer's HealStack (with the process config bound) without the selfheal
// package knowing about deploy or config.
type healerFunc func(ctx context.Context, stack string, drift []events.DriftedService) (bool, error)

func (f healerFunc) Heal(ctx context.Context, stack string, drift []events.DriftedService) (bool, error) {
	return f(ctx, stack, drift)
}

// stackAutosyncConfig maps each configured stack to its config-as-code autosync
// value (nil = inherit global).
func stackAutosyncConfig(cfg *config.Config) map[string]*bool {
	m := make(map[string]*bool, len(cfg.Stacks))
	for _, s := range cfg.Stacks {
		m[s.Name] = s.Autosync
	}
	return m
}

// healthStacks maps each effective stack to the compose identity the health
// poller probes: the compose file from the repo clone plus project_directory
// (if any) as --project-directory — the same identity the deploy path uses.
func healthStacks(cfg *config.Config, stacks []config.Stack) []health.StackRef {
	refs := make([]health.StackRef, 0, len(stacks))
	for _, s := range stacks {
		refs = append(refs, health.StackRef{
			Name:        s.Name,
			ComposePath: filepath.Join(cfg.StacksBaseDir, s.Name, compose.FileName),
			ProjectDir:  s.ProjectDirectory,
			OnDemand:    s.OnDemandContainers,
		})
	}
	return refs
}

// stackProjectDir returns the compose project directory of a stack — its
// project_directory when set, else stacks_base_dir/<name> — matching the
// working_dir label a running project carries, for orphan detection (ADR-0036).
func stackProjectDir(cfg *config.Config, s config.Stack) string {
	if s.ProjectDirectory != "" {
		return s.ProjectDirectory
	}
	return filepath.Join(cfg.StacksBaseDir, s.Name)
}

// deployOrder returns stack names in the order DeployAllStacks processes them:
// _nixos first (when the rebuild is enabled), then the effective stacks.
func deployOrder(cfg *config.Config, stacks []config.Stack) []string {
	order := make([]string, 0, len(stacks)+1)
	if cfg.NixOSRebuild.IsEnabled() {
		order = append(order, deploy.NixosStateKey)
	}
	for _, s := range stacks {
		order = append(order, s.Name)
	}
	return order
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// webhookMux builds the routes served on the main port: the headless core
// endpoints always, and — when a broadcaster is present (UI enabled) — the app
// shell, live data surface, icons and autosync controls. Each concern is wired
// by its own registrar, so this function only distributes dependencies.
func webhookMux(d webhookDeps) *http.ServeMux {
	mux := http.NewServeMux()
	registerCoreRoutes(mux, d.cfg, d.deployer)

	if d.broadcaster != nil {
		snap := stateSnapshot{
			stacks:          d.stacks,
			deployer:        d.deployer,
			healthPoller:    d.healthPoller,
			healthWatcher:   d.healthWatcher,
			orphanDetector:  d.orphanDetector,
			appLinkDetector: d.appLinkDetector,
			peers:           d.peerRegistry,
			auditLog:        d.auditLog,
			autosync:        d.autosync,
			repo:            d.repo,
		}
		registerAppRoutes(mux, d.cfg, d.build)
		registerEventRoutes(mux, d.broadcaster, d.history, d.auditLog, d.logRing, snap)
		registerIconRoutes(mux, d.cfg, d.stacks)
		registerAutosyncRoutes(mux, d.autosync)
		registerContainerLogRoutes(mux, d.cfg, d.deployer, d.healthPoller)
	}
	return mux
}

// registerCoreRoutes wires the headless endpoints that exist regardless of the
// UI: the push webhook and the health probe.
func registerCoreRoutes(mux *http.ServeMux, cfg *config.Config, deployer *deploy.Deployer) {
	mux.HandleFunc("POST /webhook", webhook.Handler(cfg, deployer))
	mux.HandleFunc("GET /healthz", healthzHandler(deployer))
}

// registerAppRoutes wires the static app shell: the PWA document, service
// worker, manifest, fonts, static icons, helper script and version endpoint.
func registerAppRoutes(mux *http.ServeMux, cfg *config.Config, build ui.BuildInfo) {
	mux.Handle("GET /{$}", ui.IndexHandler(cfg.UITheme, cfg.UIThemeSwitcher))
	mux.Handle("GET /manifest.webmanifest", ui.ManifestHandler(cfg.UITheme))
	mux.Handle("GET /sw.js", ui.ServiceWorkerHandler(build))
	mux.Handle("GET /app-helpers.js", ui.AppHelpersHandler())
	mux.Handle("GET /app.css", ui.AppCSSHandler())
	mux.Handle("GET /icons/", ui.IconsHandler())
	mux.Handle("GET /fonts/", ui.FontsHandler())
	mux.Handle("GET /api/version", ui.VersionHandler(build))
}

// registerEventRoutes wires the live data surface: the SSE stream (seeded with
// snap's initial state burst) plus the diff, audit and log endpoints.
func registerEventRoutes(mux *http.ServeMux, broadcaster *events.Broadcaster[events.DeployEvent], history *events.History, auditLog *audit.Log, logRing *logbuf.Log, snap stateSnapshot) {
	mux.Handle("GET /api/events", ui.SSEHandler(broadcaster, snap.autosync.stateB, history))
	mux.Handle("GET /api/v1/snapshot", ui.SnapshotHandler(snap.collect))
	mux.Handle("GET /api/events/{id}/diffs", ui.DiffHandler(history))
	mux.Handle("GET /api/audit", ui.AuditHandler(auditLog))
	mux.Handle("GET /api/logs", ui.LogsSSEHandler(logRing))
	registerPeerRoutes(mux, snap.peers)
}

// registerPeerRoutes wires the multi-host fan-in proxies (ADR-0048): the merged
// peer list, the per-peer diff fetch and the per-peer container-logs SSE follow.
// A no-op when no peers are configured (reg is nil).
func registerPeerRoutes(mux *http.ServeMux, reg *peers.Registry) {
	if reg == nil {
		return
	}
	mux.Handle("GET /api/peers", ui.PeersHandler(func() any { return reg.Hosts() }))
	mux.Handle("GET /api/peers/{name}/events/{id}/diffs",
		ui.PeerDiffsHandler(reg.PeerDiffsURL, &http.Client{Timeout: peerPollTimeout}))
	// The container-logs proxy streams an open-ended SSE follow, so its client
	// carries no timeout (unlike the bounded diff fetch); the client's
	// disconnect cancels the request context and tears the stream down.
	logsProxyClient := &http.Client{}
	mux.Handle("GET /api/peers/{name}/container-logs/{stack}",
		ui.PeerContainerLogsHandler(reg.PeerContainerLogsURL, logsProxyClient))
}

// registerIconRoutes wires per-stack icon resolution and the cache-refresh hook.
func registerIconRoutes(mux *http.ServeMux, cfg *config.Config, stacks func() []config.Stack) {
	iconTimeout := time.Duration(cfg.CommandTimeoutSeconds) * time.Second
	iconSvc := icons.New(cfg.Icons.CacheDir, icons.NewHTTPFetcher(cfg.Icons.SourceURL, &http.Client{Timeout: iconTimeout}))
	mux.Handle("GET /api/icons/{stack}", icons.Handler(iconSvc, stackLocator(cfg, stacks)))
	mux.Handle("POST /api/icons/refresh", ui.RequireSameOrigin(icons.RefreshHandler(iconSvc)))
}

// registerAutosyncRoutes wires the autosync toggle and the deploy-queue view.
func registerAutosyncRoutes(mux *http.ServeMux, as *autosyncDeps) {
	// The guard passes GET through untouched; only the POST override is gated.
	autosyncH := ui.RequireSameOrigin(ui.AutosyncHandler(as.ctrl, as.order, as.publish, as.trigger))
	mux.Handle("GET /api/autosync", autosyncH)
	mux.Handle("POST /api/autosync", autosyncH)
	mux.Handle("GET /api/queue", ui.QueueHandler(as.queue, as.order))
}

// registerContainerLogRoutes wires the live container-log stream for a stack,
// narrowable to a subset of its services via a comma-separated ?services= list
// (ADR-0037). UI-only; skipped without a health poller, whose snapshot the
// resolver needs to validate the selected services.
func registerContainerLogRoutes(mux *http.ServeMux, cfg *config.Config, deployer *deploy.Deployer, hp *health.Poller) {
	if hp == nil {
		return
	}
	h := containerlogs.Handler(containerlogs.ExecStreamer{}, containerLogResolver{cfg: cfg, deployer: deployer, health: hp})
	mux.Handle("GET /api/container-logs/{stack}", h)
}

// stateSnapshot gathers the current state of every subsystem the UI mirrors,
// producing the initial SSE state burst a newly connected client receives.
type stateSnapshot struct {
	stacks          func() []config.Stack
	deployer        *deploy.Deployer
	healthPoller    *health.Poller
	healthWatcher   *healthwatch.Watcher
	orphanDetector  *orphans.Detector
	appLinkDetector *applink.Detector
	peers           *peers.Registry
	auditLog        *audit.Log
	autosync        *autosyncDeps
	repo            repoRef // clone dir for repo-relative paths, forge URL for commit links
}

// collect returns the current state events. As a side effect it polls the
// health poller so a just-connected client sees fresh data (which also
// refreshes orphans and app links). Optional subsystems are skipped when
// their component is absent.
func (s stateSnapshot) collect() []events.StateEvent {
	as := s.autosync
	state := []events.StateEvent{
		{Name: events.StateAutosync, Data: as.ctrl.Snapshot(as.order())},
		{Name: events.StateQueue, Data: as.queue.View(as.order())},
		{Name: events.StateUpcoming, Data: s.deployer.CurrentRunPlan()},
		{Name: events.StateHookRun, Data: s.deployer.CurrentHookRun()},
		{Name: events.StateStacks, Data: buildStacksState(s.stacks(), s.deployer.CurrentDisabledStacks(), s.auditLog, s.deployer.CurrentTrackedFiles(), s.repo)},
	}
	if s.healthPoller != nil {
		state = append(state, events.StateEvent{Name: events.StateHealth, Data: s.healthPoller.Current()})
		s.healthPoller.Poll() // a client just connected — refresh now (also refreshes orphans and app links)
	}
	if s.healthWatcher != nil {
		state = append(state, events.StateEvent{Name: events.StateHealthWatch, Data: s.healthWatcher.Current()})
	}
	if s.orphanDetector != nil {
		state = append(state, events.StateEvent{Name: events.StateOrphans, Data: s.orphanDetector.Current()})
	}
	if s.appLinkDetector != nil {
		state = append(state, events.StateEvent{Name: events.StateAppLinks, Data: s.appLinkDetector.Current()})
	}
	if s.peers != nil {
		state = append(state, events.StateEvent{Name: events.StatePeers, Data: s.peers.State()})
	}
	return state
}

// stackLocator maps a stack name to its icon-resolution inputs from the
// effective stack set. The icon file lives in the stack's directory in the
// clone (stacks_base_dir/<name>), the same directory change detection reads
// from.
func stackLocator(cfg *config.Config, stacks func() []config.Stack) icons.StackLocator {
	return func(name string) (icons.Request, bool) {
		// The reserved NixOS pseudo-stack has no directory in the clone and is
		// not in the stack set; resolve its icon by auto-matching the "nixos"
		// slug so it gets a recognizable logo instead of the "_" monogram
		// fallback.
		if name == deploy.NixosStateKey {
			return icons.Request{Name: "nixos"}, true
		}
		// The reserved stack-config pseudo-stack (ADR-0034) likewise has no
		// directory; its failures are about the repo skipper.yaml, so the git
		// logo is the recognizable stand-in.
		if name == deploy.ConfigStateKey {
			return icons.Request{Name: "git"}, true
		}
		for _, s := range stacks() {
			if s.Name == name {
				return icons.Request{
					Name: s.Name,
					Slug: s.Icon,
					Dir:  filepath.Join(cfg.StacksBaseDir, s.Name),
				}, true
			}
		}
		return icons.Request{}, false
	}
}

// startServer runs an HTTP server in a goroutine and returns it so the
// caller can shut it down. An immediate listen failure exits the process.
func startServer(name string, port int, mux *http.ServeMux) *http.Server {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() {
		slog.Info(name+" server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error(name+" server stopped", "err", err)
			os.Exit(1)
		}
	}()
	return srv
}

// healthzHandler reports 200 while the last repository sync succeeded (or
// none ran yet) and 503 with the sync error otherwise.
func healthzHandler(deployer *deploy.Deployer) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := deployer.Health(); err != nil {
			http.Error(w, "last repository sync failed: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	}
}
