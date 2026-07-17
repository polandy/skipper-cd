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
	"syscall"
	"time"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/health"
	"github.com/polandy/skipper-cd/internal/healthwatch"
	"github.com/polandy/skipper-cd/internal/icons"
	"github.com/polandy/skipper-cd/internal/logbuf"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/notify"
	"github.com/polandy/skipper-cd/internal/reconcile"
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
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}
	// With the UI enabled, tee all slog output (and, via sink below, child
	// process output) into an in-memory ring served live at /api/logs.
	var logRing *logbuf.Log
	logHandler := newLogHandler(cfg.LogFormat, os.Stderr)
	if cfg.UIEnabled {
		logRing = logbuf.New(logbuf.DefaultCapacity)
		logHandler = logbuf.NewHandler(logHandler, logRing)
	}
	slog.SetDefault(slog.New(logHandler))

	// Assign the sink only for a non-nil ring: a typed-nil *logbuf.Log in
	// the interface would defeat the runner's sink != nil check.
	var sink command.LineSink
	if logRing != nil {
		sink = logRing
	}

	timeout := time.Duration(cfg.CommandTimeoutSeconds) * time.Second

	slog.Info("skipper-cd starting",
		"stacks", len(cfg.Stacks),
		"webhook_port", cfg.Port,
		"metrics_port", cfg.MetricsPort,
		"branch", cfg.Branch,
		"command_timeout", timeout,
	)

	// Cancels on SIGINT/SIGTERM. The deployer abandons a pending
	// nixos-rebuild wait when it fires (ADR-0014).
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repoSync := git.NewRepoSync(cfg.RepoURL, cfg.RepoDir, cfg.Branch, timeout, sink)
	repoReader := git.NewRepoReader(repoSync.RepoDir(), timeout, sink)
	stateDir := filepath.Dir(repoSync.RepoDir())
	deployer := deploy.NewDeployerWithCommitReader(repoReader, repoSync, repoSync.RepoDir(), stateDir, timeout, sink)
	deployer.SetShutdownContext(signalCtx)

	var (
		broadcaster  *events.Broadcaster[events.DeployEvent]
		stateB       *events.Broadcaster[events.StateEvent]
		history      *events.History
		healthPoller *health.Poller
	)
	// The deploy event sink is composed from whatever consumers are configured;
	// each is independent, so notifications work with the UI off (ADR-0020).
	var eventSinks []func(events.DeployEvent)
	if cfg.UIEnabled {
		history = events.NewHistory(stateDir)
		broadcaster = events.NewBroadcaster()
		stateB = events.NewStateBroadcaster()
		deployer.InitEventID(history.MaxEventID())
		eventSinks = append(eventSinks, func(e events.DeployEvent) {
			if e.Status != events.StatusSkipped {
				history.Add(e)
			}
			broadcaster.Publish(e)
		})
		// Look-ahead: publish the run plan (what deploys next) over the same SSE
		// stream. Installing the sink is what enables the upfront planning pass.
		deployer.SetRunPlanSink(func(p deploy.RunPlan) {
			stateB.Publish(events.StateEvent{Name: "upcoming", Data: p})
		})
		slog.Info("web UI enabled")
	}

	// Outbound notifications. Config is already validated in Load; New only
	// re-derives formatters, so an error here is a programming bug, not user
	// input. Timeout 0 uses notify's built-in per-request default.
	notifier, err := notify.New(cfg.Notifications, nil, 0)
	if err != nil {
		slog.Error("failed to build notifier", "err", err)
		os.Exit(1)
	}
	if notifier.Enabled() {
		go notifier.Run(signalCtx)
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
			Healer:            deployHealer{deployer, cfg},
			Enabled:           cfg.SelfHealEnabled,
			MinUnhealthyPolls: cfg.SelfHealMinUnhealthyPolls,
			MaxAttempts:       cfg.SelfHealMaxAttempts,
			Cooldown:          time.Duration(cfg.SelfHealCooldownSeconds) * time.Second,
			OnExhausted:       deployer.EmitHealExhausted,
		})
		eventSinks = append(eventSinks, func(e events.DeployEvent) {
			if e.Status == events.StatusDeploying {
				selfHealEngine.Reset(e.Stack)
			}
		})
		slog.Info("self-heal enabled", "min_unhealthy_polls", cfg.SelfHealMinUnhealthyPolls, "max_attempts", cfg.SelfHealMaxAttempts, "cooldown_seconds", cfg.SelfHealCooldownSeconds)
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
			go alerter.Run(signalCtx)
			alertSink = alerter
		}
		healthWatcher = healthwatch.New(healthwatch.Config{
			Alerter:           alertSink,
			StatePath:         filepath.Join(stateDir, "healthwatch.yaml"),
			DebouncePolls:     hw.DebouncePolls,
			AttributionWindow: time.Duration(hw.AttributionWindowSeconds) * time.Second,
		})
		eventSinks = append(eventSinks, healthWatcher.ObserveDeploy)
		slog.Info("health watch enabled",
			"debounce_polls", hw.DebouncePolls,
			"targets", len(hw.Targets),
		)
	}

	// Stack-health poller. It feeds the UI health view (when enabled),
	// self-heal, and/or the health watchdog. For the UI it is subscriber-gated
	// so an idle dashboard does no docker work (ADR-0027); self-heal and the
	// watchdog set AlwaysPoll so it still runs headless on an unattended host
	// (ADR-0029, ADR-0031). Config validation guarantees a positive interval
	// whenever self-heal or the watchdog is active.
	if interval := *cfg.HealthPollIntervalSeconds; interval > 0 && (cfg.UIEnabled || selfHealActive || healthWatcher != nil) {
		healthTimeout := time.Duration(cfg.CommandTimeoutSeconds) * time.Second
		hpCfg := health.Config{
			Outputter:  command.NewShellRunner(healthTimeout),
			Stacks:     func() []health.StackRef { return healthStacks(cfg) },
			Interval:   time.Duration(interval) * time.Second,
			AlwaysPoll: selfHealActive || healthWatcher != nil,
		}
		if stateB != nil {
			hpCfg.Publish = func(s health.Snapshot) { stateB.Publish(events.StateEvent{Name: "health", Data: s}) }
			hpCfg.HasSubscribers = stateB.HasSubscribers
		}
		var snapshotSinks []func(health.Snapshot)
		if selfHealEngine != nil {
			snapshotSinks = append(snapshotSinks, func(s health.Snapshot) { selfHealEngine.Observe(context.Background(), s) })
		}
		if healthWatcher != nil {
			snapshotSinks = append(snapshotSinks, healthWatcher.Observe)
		}
		if len(snapshotSinks) > 0 {
			hpCfg.OnSnapshot = func(s health.Snapshot) {
				for _, sink := range snapshotSinks {
					sink(s)
				}
			}
		}
		healthPoller = health.New(hpCfg)
		go healthPoller.Run(signalCtx)
		slog.Info("stack health polling enabled", "interval_seconds", interval, "self_heal", selfHealActive, "health_watch", healthWatcher != nil)
	}

	if len(eventSinks) > 0 {
		deployer.SetEventSink(func(e events.DeployEvent) {
			for _, s := range eventSinks {
				s(e)
			}
		})
	}

	// Autosync is active regardless of the UI so config-as-code pauses apply.
	autosyncCtrl := autosync.NewController(cfg.Autosync, stackAutosyncConfig(cfg))
	autosyncQueue := autosync.NewQueue()
	deployer.SetAutosync(autosyncCtrl, autosyncQueue)
	order := func() []string { return deployOrder(cfg) }
	publishAutosync := func() {
		snap := autosyncCtrl.Snapshot(order())
		metrics.AutosyncGlobal.Set(boolToFloat(autosyncCtrl.GlobalEffective()))
		for _, s := range snap.Stacks {
			metrics.AutosyncEnabled.WithLabelValues(s.Name).Set(boolToFloat(s.Effective))
		}
		metrics.AutosyncPending.Set(float64(autosyncQueue.Count()))
		if stateB != nil {
			stateB.Publish(events.StateEvent{Name: "autosync", Data: snap})
			stateB.Publish(events.StateEvent{Name: "queue", Data: autosyncQueue.View(order())})
		}
	}
	deployer.SetPostRunHook(func() {
		publishAutosync()
		if healthPoller != nil {
			healthPoller.Poll() // refresh health right after a deploy run
		}
	})
	publishAutosync() // initialize the gauges

	// Sync repo and deploy on startup to catch changes that occurred while skipper-cd was not running.
	go deployer.SyncAndDeployAll(context.Background(), cfg)

	// Periodic reconcile: re-run sync + deploy on a timer so a missed or lost
	// webhook cannot leave the host drifted from the deploy repo indefinitely
	// (ADR-0028). Not UI-gated — it is a correctness feature that must run
	// headless. A tick is skipped while a deploy is already in flight.
	if interval := *cfg.ReconcileIntervalSeconds; interval > 0 {
		loop := reconcile.New(time.Duration(interval)*time.Second, deployReconciler{deployer, cfg})
		go loop.Run(signalCtx)
		slog.Info("periodic reconcile enabled", "interval_seconds", interval)
	}

	as := &autosyncDeps{
		ctrl:    autosyncCtrl,
		queue:   autosyncQueue,
		stateB:  stateB,
		order:   order,
		publish: publishAutosync,
		trigger: func() { go deployer.SyncAndDeployAll(context.Background(), cfg) },
	}

	bi, ok := debug.ReadBuildInfo()
	build := ui.BuildInfo{
		Version: version,
		Branch:  branch,
		Commit:  resolveCommit(commit, bi, ok),
	}

	startServer("metrics", cfg.MetricsPort, metricsMux())
	webhookServer := startServer("webhook", cfg.Port, webhookMux(cfg, deployer, healthPoller, broadcaster, history, logRing, as, build))

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

// newLogHandler returns the slog handler for the configured log_format:
// a JSON handler for "json", a logfmt text handler otherwise.
func newLogHandler(format string, w io.Writer) slog.Handler {
	if format == config.LogFormatJSON {
		return slog.NewJSONHandler(w, nil)
	}
	return slog.NewTextHandler(w, nil)
}

func metricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
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

// deployHealer adapts the deployer to selfheal.Healer, binding the process
// config so each heal restores the named stack via HealStack. It keeps the
// selfheal package free of any deploy or config dependency.
type deployHealer struct {
	deployer *deploy.Deployer
	cfg      *config.Config
}

func (h deployHealer) Heal(ctx context.Context, stack string) (bool, error) {
	return h.deployer.HealStack(ctx, h.cfg, stack)
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

// healthStacks maps each configured stack to the compose identity the health
// poller probes: the compose file from the repo clone plus the working_dir (if
// any) as --project-directory — the same identity the deploy path uses.
func healthStacks(cfg *config.Config) []health.StackRef {
	refs := make([]health.StackRef, 0, len(cfg.Stacks))
	for _, s := range cfg.Stacks {
		refs = append(refs, health.StackRef{
			Name:        s.Name,
			ComposePath: filepath.Join(cfg.StacksBaseDir, s.Name, "docker-compose.yml"),
			ProjectDir:  s.WorkingDir,
		})
	}
	return refs
}

// deployOrder returns stack names in the order DeployAllStacks processes them:
// _nixos first (when the rebuild is enabled), then the configured stacks.
func deployOrder(cfg *config.Config) []string {
	order := make([]string, 0, len(cfg.Stacks)+1)
	if cfg.NixOSRebuild.IsEnabled() {
		order = append(order, "_nixos")
	}
	for _, s := range cfg.Stacks {
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

func webhookMux(cfg *config.Config, deployer *deploy.Deployer, healthPoller *health.Poller, broadcaster *events.Broadcaster[events.DeployEvent], history *events.History, logRing *logbuf.Log, as *autosyncDeps, build ui.BuildInfo) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", webhook.Handler(cfg, deployer))
	mux.HandleFunc("GET /healthz", healthzHandler(deployer))

	if broadcaster != nil {
		initialState := func() []events.StateEvent {
			state := []events.StateEvent{
				{Name: "autosync", Data: as.ctrl.Snapshot(as.order())},
				{Name: "queue", Data: as.queue.View(as.order())},
				{Name: "upcoming", Data: deployer.CurrentRunPlan()},
			}
			if healthPoller != nil {
				state = append(state, events.StateEvent{Name: "health", Data: healthPoller.Current()})
				healthPoller.Poll() // a client just connected — refresh now
			}
			return state
		}
		autosyncH := ui.AutosyncHandler(as.ctrl, as.order, as.publish, as.trigger)

		mux.Handle("GET /{$}", ui.IndexHandler(cfg.UITheme, cfg.UIThemeSwitcher))
		mux.Handle("GET /manifest.webmanifest", ui.ManifestHandler(cfg.UITheme))
		mux.Handle("GET /sw.js", ui.ServiceWorkerHandler(build))
		mux.Handle("GET /icons/", ui.IconsHandler())
		mux.Handle("GET /api/version", ui.VersionHandler(build))
		mux.Handle("GET /api/events", ui.SSEHandler(broadcaster, as.stateB, history, initialState))
		mux.Handle("GET /api/events/{id}/diffs", ui.DiffHandler(history))
		mux.Handle("GET /api/logs", ui.LogsSSEHandler(logRing))

		iconTimeout := time.Duration(cfg.CommandTimeoutSeconds) * time.Second
		iconSvc := icons.New(cfg.Icons.CacheDir, icons.NewHTTPFetcher(cfg.Icons.SourceURL, &http.Client{Timeout: iconTimeout}))
		mux.Handle("GET /api/icons/{stack}", icons.Handler(iconSvc, stackLocator(cfg)))
		mux.Handle("POST /api/icons/refresh", icons.RefreshHandler(iconSvc))

		mux.Handle("GET /api/autosync", autosyncH)
		mux.Handle("POST /api/autosync", autosyncH)
		mux.Handle("GET /api/queue", ui.QueueHandler(as.queue, as.order))
	}
	return mux
}

// stackLocator maps a stack name to its icon-resolution inputs from config.
// The icon file lives in the stack's directory in the clone
// (stacks_base_dir/<name>), the same directory change detection reads from.
func stackLocator(cfg *config.Config) icons.StackLocator {
	return func(name string) (icons.Request, bool) {
		// The reserved NixOS pseudo-stack has no directory in the clone and is
		// not in cfg.Stacks; resolve its icon by auto-matching the "nixos" slug
		// so it gets a recognizable logo instead of the "_" monogram fallback.
		if name == deploy.NixosStateKey {
			return icons.Request{Name: "nixos"}, true
		}
		for _, s := range cfg.Stacks {
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
