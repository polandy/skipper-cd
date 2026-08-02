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

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/notify"
	"github.com/polandy/skipper-cd/internal/peers"
	"github.com/polandy/skipper-cd/internal/prettylog"
	"github.com/polandy/skipper-cd/internal/reconcile"
	"github.com/polandy/skipper-cd/internal/roster"
	"github.com/polandy/skipper-cd/internal/safego"
	"github.com/polandy/skipper-cd/internal/ui"
	"github.com/polandy/skipper-cd/internal/updatecheck"
)

// readHeaderTimeout bounds how long a client may take to send request
// headers, protecting the servers against slowloris-style connections.
const readHeaderTimeout = 10 * time.Second

// shutdownTimeout bounds how long in-flight HTTP requests (e.g. open SSE
// streams) may delay shutdown after a termination signal.
const shutdownTimeout = 10 * time.Second

// peerPollTimeout bounds a single peer read (its snapshot + audit fetch) in
// the multi-host fan-in (ADR-0048), so one slow or hung peer cannot stall
// the poll loop.
const peerPollTimeout = 10 * time.Second

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
	logRing, sink := setupLogging(cfg, os.Stderr)

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
	repo := roster.RepoRef{Dir: repoSync.RepoDir(), WebURL: cfg.EffectiveRepoWebURL()}

	// The deployer is constructed below via deploy.New once all its
	// collaborators are built — deploy.Config wires everything at construction.
	// Collaborators that call back into it resolve it through ref at call time
	// (see deployerRef); main completes the wiring right after deploy.New.
	ref := &deployerRef{}
	views := stackViews{cfg: cfg, deployer: ref}

	uiw := buildUILayer(cfg, stateDir)

	// Durable per-stack deploy audit trail (ADR-0033). Recorded unconditionally
	// so the history is complete even with the UI off; only the query API below
	// is UI-gated. Empty stateDir disables persistence (in-memory only).
	auditLog := audit.NewLog(stateDir)

	// Outbound notifications. Config is already validated in Load; New only
	// re-derives formatters, so an error here is a programming bug, not user
	// input. Timeout 0 uses notify's built-in per-request default.
	notifier, err := notify.New(cfg.Notifications, nil, 0)
	if err != nil {
		slog.Error("failed to build notifier", "err", err)
		os.Exit(1) //nolint:gocritic // exitAfterDefer: stop() is the only pending defer and merely unregisters the signal handler; nothing is serving yet
	}
	var notifierSink func(events.DeployEvent)
	if notifier.Enabled() {
		safego.Go("notifier", func() { notifier.Run(signalCtx) })
		notifierSink = notifier.Notify
		slog.Info("notifications enabled", "targets", len(cfg.Notifications))
	}

	selfHealEngine := buildSelfHeal(cfg, views, ref)

	healthWatcher, err := buildHealthWatch(signalCtx, cfg, stateDir, uiw.stateB)
	if err != nil {
		slog.Error("failed to build health alerter", "err", err)
		os.Exit(1) //nolint:gocritic // exitAfterDefer: as above — only stop() is pending, nothing is serving yet
	}

	hl := buildHealthLayer(cfg, views, uiw.stateB, selfHealEngine, healthWatcher)

	// Tallies every run's per-stack outcomes for the "run complete" summary
	// PostRunHook logs below; independent of the UI so it works headless too.
	tally := newRunTally()
	eventSink := buildEventFanout(
		tally.observe,
		uiw.deploySink(),
		auditLog.Record,
		notifierSink,
		selfHealResetSink(selfHealEngine),
		healthWatchSink(healthWatcher),
	)

	// Autosync is active regardless of the UI so config-as-code pauses apply.
	autosyncCtrl := autosync.NewController(cfg.Autosync, stackAutosyncConfig(cfg))
	autosyncQueue := autosync.NewQueue()
	// The update checker (ADR-0054) and the publisher reference each other —
	// the checker republishes the stacks snapshot after every run, the snapshot
	// carries the checker's result — so the snapshot accessor closes over a
	// variable assigned right below, before anything runs.
	var updChecker *updatecheck.Checker
	asPublisher := autosyncPublisher{
		ctrl:     autosyncCtrl,
		queue:    autosyncQueue,
		stateB:   uiw.stateB,
		views:    views,
		deployer: ref,
		auditLog: auditLog,
		repo:     repo,
		updates: func() *updatecheck.Snapshot {
			if updChecker == nil {
				return nil
			}
			return updChecker.Snapshot()
		},
	}
	updChecker, err = buildUpdateCheck(signalCtx, cfg, stateDir, timeout, views, ref, asPublisher.publishStacks)
	if err != nil {
		slog.Error("failed to build update checker", "err", err)
		os.Exit(1) //nolint:gocritic // exitAfterDefer: as above — only stop() is pending, nothing is serving yet
	}
	deployer := deploy.New(deploy.Config{
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
		StartEventID: uiw.startEventID,
		Autosync:     autosyncCtrl,
		Queue:        autosyncQueue,
		PostRunHook: func() {
			logRunSummary(tally.flush())
			// In stack-discovery mode the set is only known once the first run
			// resolves it; a static host list already logged this at startup, so
			// every call after the first is a no-op (sync.Once).
			logRosterOnce(views.effective(), ref.get().CurrentDisabledStacks())
			asPublisher.publish()
			if hl.poller != nil {
				hl.poller.Poll() // refresh health right after a deploy run
			}
			// Re-check updates when the run changed what runs, so an applied
			// update's marker clears now, not at the next 6h tick (ADR-0054).
			// No-op runs (the 5-minute reconcile) skip without registry traffic.
			if updChecker != nil {
				safego.Go("update-check-nudge", func() { updChecker.RunOnceIfChanged(signalCtx) })
			}
		},
		StackSetSink: asPublisher.publishStacks,
		RunPlanSink:  uiw.runPlanSink,
		HookRunSink:  uiw.hookRunSink,
	})
	ref.set(deployer)

	// Start the health poller only now that the deployer exists: its snapshot
	// feed may drive a self-heal, which redeploys through the deployer.
	if hl.poller != nil {
		safego.Go("health-poller", func() { hl.poller.Run(signalCtx) })
	}
	// Same for the update checker: it reads the deployer's running images.
	if updChecker != nil {
		safego.Go("update-check", func() { updChecker.Run(signalCtx) })
	}
	asPublisher.publish() // initialize the gauges

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
	if uiw.peerRegistry != nil {
		stateB := uiw.stateB
		loop := peers.NewLoop(uiw.peerRegistry,
			time.Duration(*cfg.RuntimeHealthPollIntervalSeconds)*time.Second,
			func(s peers.State) { stateB.Publish(events.StateEvent{Name: events.StatePeers, Data: s}) })
		safego.Go("peers-fanin", func() { loop.Run(signalCtx) })
		slog.Info("multi-host fan-in enabled", "peers", len(cfg.Peers), "poll_interval_seconds", int(loop.Interval()/time.Second))
	}

	as := &autosyncDeps{
		ctrl:    autosyncCtrl,
		queue:   autosyncQueue,
		stateB:  uiw.stateB,
		order:   views.order,
		publish: asPublisher.publish,
		trigger: func() { safego.Go("autosync-trigger", func() { deployer.SyncAndDeployAll(context.Background(), cfg) }) },
	}

	bi, ok := debug.ReadBuildInfo()
	build := ui.BuildInfo{
		Version: version,
		Branch:  branch,
		Commit:  resolveCommit(commit, bi, ok),
	}

	// Buffered for both servers: a failing server must be able to report and
	// return even if the other one reported first and main is already shutting
	// down.
	serverFail := make(chan error, 2)
	metricsServer := startServer("metrics", cfg.MetricsPort, metricsMux(), serverFail)
	webhookServer := startServer("webhook", cfg.Port, webhookMux(webhookDeps{
		cfg:             cfg,
		stacks:          views.effective,
		deployer:        deployer,
		healthPoller:    hl.poller,
		healthWatcher:   healthWatcher,
		orphanDetector:  hl.orphans,
		appLinkDetector: hl.appLinks,
		peerRegistry:    uiw.peerRegistry,
		broadcaster:     uiw.broadcaster,
		history:         uiw.history,
		auditLog:        auditLog,
		logRing:         logRing,
		autosync:        as,
		build:           build,
		repo:            repo,
		updates:         asPublisher.updates,
	}), serverFail)

	// Block until SIGINT/SIGTERM or a server giving up, then shut down
	// gracefully: stop accepting requests, then let an in-flight deploy finish
	// so docker compose is not interrupted mid-run. Both exits take the same
	// path — a server that cannot listen is a reason to stop, not a reason to
	// abandon a deploy that is already running.
	failed := false
	select {
	case <-signalCtx.Done():
		slog.Info("shutdown signal received")
	case err := <-serverFail:
		slog.Error("shutting down after a server error", "err", err)
		failed = true
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := webhookServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("webhook server shutdown", "err", err)
	}

	slog.Info("waiting for in-flight deploy to finish")
	deployer.WaitIdle()

	// Metrics go last, on their own deadline: the drain above is exactly when a
	// scrape is still worth answering, and by now shutdownCtx may well have
	// expired waiting for it.
	metricsCtx, cancelMetrics := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelMetrics()
	if err := metricsServer.Shutdown(metricsCtx); err != nil {
		slog.Warn("metrics server shutdown", "err", err)
	}

	// The ctx-bound background loops (notifier, health poller, reconciler,
	// peer fan-in) are cancelled by signalCtx but deliberately not joined:
	// deploys are the only work that must not be cut short and WaitIdle above
	// covers those, and every state write goes through fsatomic, so a loop
	// killed mid-write leaves the previous file rather than a truncated one.
	slog.Info("skipper-cd stopped")
	if failed {
		os.Exit(1)
	}
}

// newLogHandler returns the slog handler for the configured log_format: a
// JSON handler for "json", a logfmt text handler for "text", and prettylog's
// colored console handler otherwise (the default, "pretty"). All three drop
// records below level (log_level, default Info).
func newLogHandler(format, level string, w io.Writer) slog.Handler {
	threshold := slogLevel(level)
	opts := &slog.HandlerOptions{Level: threshold}
	switch format {
	case config.LogFormatJSON:
		return slog.NewJSONHandler(w, opts)
	case config.LogFormatText:
		return slog.NewTextHandler(w, opts)
	default:
		return prettylog.New(w, threshold)
	}
}

// slogLevel maps a validated log_level value to its slog threshold. An
// unrecognized value cannot reach here (config validation rejects it) and
// falls back to Info rather than silently muting or flooding the log.
func slogLevel(level string) slog.Level {
	switch level {
	case config.LogLevelDebug:
		return slog.LevelDebug
	case config.LogLevelWarn:
		return slog.LevelWarn
	case config.LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
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

// stackAutosyncConfig maps each configured stack to its config-as-code autosync
// value (nil = inherit global).
func stackAutosyncConfig(cfg *config.Config) map[string]*bool {
	m := make(map[string]*bool, len(cfg.Stacks))
	for _, s := range cfg.Stacks {
		m[s.Name] = s.Autosync
	}
	return m
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

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// startServer runs an HTTP server in a goroutine and returns it so the caller
// can shut it down. A listen failure (a port already in use, most often) is
// reported on fail rather than exiting here: an os.Exit from this goroutine
// would skip main's shutdown, and the startup sync may already be running
// `docker compose up` by the time a port turns out to be taken. fail must be
// buffered so a second server's failure cannot block on an unread channel.
func startServer(name string, port int, mux *http.ServeMux, fail chan<- error) *http.Server {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() {
		slog.Info(name+" server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fail <- fmt.Errorf("%s server: %w", name, err)
		}
	}()
	return srv
}
