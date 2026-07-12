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
	"syscall"
	"time"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/icons"
	"github.com/polandy/skipper-cd/internal/logbuf"
	"github.com/polandy/skipper-cd/internal/metrics"
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

// version is the build-time skipper-cd version, injected via
// -ldflags "-X main.version=…" from .release-please-manifest.json. It defaults
// to "dev" for local builds and is surfaced in the UI header (GET /api/version).
var version = "dev"

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
		broadcaster *events.Broadcaster[events.DeployEvent]
		stateB      *events.Broadcaster[events.StateEvent]
		history     *events.History
	)
	if cfg.UIEnabled {
		history = events.NewHistory(stateDir)
		broadcaster = events.NewBroadcaster()
		stateB = events.NewStateBroadcaster()
		deployer.InitEventID(history.MaxEventID())
		deployer.SetEventSink(func(e events.DeployEvent) {
			if e.Status != events.StatusSkipped {
				history.Add(e)
			}
			broadcaster.Publish(e)
		})
		slog.Info("web UI enabled")
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
	deployer.SetPostRunHook(publishAutosync)
	publishAutosync() // initialize the gauges

	// Sync repo and deploy on startup to catch changes that occurred while skipper-cd was not running.
	go deployer.SyncAndDeployAll(context.Background(), cfg)

	as := &autosyncDeps{
		ctrl:    autosyncCtrl,
		queue:   autosyncQueue,
		stateB:  stateB,
		order:   order,
		publish: publishAutosync,
		trigger: func() { go deployer.SyncAndDeployAll(context.Background(), cfg) },
	}

	startServer("metrics", cfg.MetricsPort, metricsMux())
	webhookServer := startServer("webhook", cfg.Port, webhookMux(cfg, deployer, broadcaster, history, logRing, as, version))

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

// stackAutosyncConfig maps each configured stack to its config-as-code autosync
// value (nil = inherit global).
func stackAutosyncConfig(cfg *config.Config) map[string]*bool {
	m := make(map[string]*bool, len(cfg.Stacks))
	for _, s := range cfg.Stacks {
		m[s.Name] = s.Autosync
	}
	return m
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

func webhookMux(cfg *config.Config, deployer *deploy.Deployer, broadcaster *events.Broadcaster[events.DeployEvent], history *events.History, logRing *logbuf.Log, as *autosyncDeps, version string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", webhook.Handler(cfg, deployer))
	mux.HandleFunc("GET /healthz", healthzHandler(deployer))

	if broadcaster != nil {
		initialState := func() []events.StateEvent {
			return []events.StateEvent{
				{Name: "autosync", Data: as.ctrl.Snapshot(as.order())},
				{Name: "queue", Data: as.queue.View(as.order())},
			}
		}
		autosyncH := ui.AutosyncHandler(as.ctrl, as.order, as.publish, as.trigger)

		mux.Handle("GET /{$}", ui.IndexHandler())
		mux.Handle("GET /manifest.webmanifest", ui.ManifestHandler())
		mux.Handle("GET /sw.js", ui.ServiceWorkerHandler(version))
		mux.Handle("GET /icons/", ui.IconsHandler())
		mux.Handle("GET /api/version", ui.VersionHandler(version))
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
