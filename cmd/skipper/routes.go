// Route surface: the muxes served on the webhook and metrics ports — the
// registrars that build the route table, the handler-adapter types they wire,
// and the initial state snapshot a newly connected UI client receives.

package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/polandy/skipper-cd/internal/applink"
	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/containerlogs"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/health"
	"github.com/polandy/skipper-cd/internal/healthwatch"
	"github.com/polandy/skipper-cd/internal/icons"
	"github.com/polandy/skipper-cd/internal/logbuf"
	"github.com/polandy/skipper-cd/internal/orphans"
	"github.com/polandy/skipper-cd/internal/peers"
	"github.com/polandy/skipper-cd/internal/roster"
	"github.com/polandy/skipper-cd/internal/ui"
	"github.com/polandy/skipper-cd/internal/updatecheck"
	"github.com/polandy/skipper-cd/internal/webhook"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func metricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
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
	repo            roster.RepoRef
	updates         func() *updatecheck.Snapshot
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
			updates:         d.updates,
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
// worker, manifest, fonts, static icons, app + render + helper scripts and version
// endpoint.
func registerAppRoutes(mux *http.ServeMux, cfg *config.Config, build ui.BuildInfo) {
	mux.Handle("GET /{$}", ui.IndexHandler(cfg.UITheme, cfg.UIThemeSwitcher))
	mux.Handle("GET /manifest.webmanifest", ui.ManifestHandler(cfg.UITheme))
	mux.Handle("GET /sw.js", ui.ServiceWorkerHandler(build))
	mux.Handle("GET /app.js", ui.AppJSHandler())
	mux.Handle("GET /app-render.js", ui.AppRenderJSHandler())
	mux.Handle("GET /app-state.js", ui.AppStateJSHandler())
	mux.Handle("GET /app-panels.js", ui.AppPanelsJSHandler())
	mux.Handle("GET /app-hosts.js", ui.AppHostsJSHandler())
	mux.Handle("GET /app-autosync.js", ui.AppAutosyncJSHandler())
	mux.Handle("GET /app-logs.js", ui.AppLogsJSHandler())
	mux.Handle("GET /app-clog.js", ui.AppClogJSHandler())
	mux.Handle("GET /app-helpers.js", ui.AppHelpersHandler())
	mux.Handle("GET /app.css", ui.AppCSSHandler())
	mux.Handle("GET /icons/", ui.IconsHandler())
	mux.Handle("GET /fonts/", ui.FontsHandler())
	mux.Handle("GET /api/version", ui.VersionHandler(build))
}

// registerEventRoutes wires the live data surface: the SSE stream (seeded with
// snap's initial state burst) plus the diff, audit and log endpoints.
func registerEventRoutes(mux *http.ServeMux, broadcaster *events.Broadcaster[events.DeployEvent], history *events.History, auditLog *audit.Log, logRing *logbuf.Log, snap stateSnapshot) {
	mux.Handle("GET /api/events", ui.SSEHandler(broadcaster, snap.autosync.stateB, history, snap.collect))
	mux.Handle("GET /api/v1/snapshot", ui.SnapshotHandler(snap.collect))
	mux.Handle("GET /api/events/{id}/diffs", ui.DiffHandler(history))
	// One handler on both routes: the legacy path serves the UI, the /v1 alias
	// is the versioned contract the multi-host fan-in polls (ADR-0039).
	auditH := ui.AuditHandler(auditLog)
	mux.Handle("GET /api/audit", auditH)
	mux.Handle("GET /api/v1/audit", auditH)
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
	mux.Handle("GET /api/icons/{stack}", icons.Handler(iconSvc, icons.NewStackLocator(cfg, stacks)))
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
	repo            roster.RepoRef // clone dir for repo-relative paths, forge URL for commit links
	// updates returns the latest registry update check (ADR-0054); nil-safe —
	// nil (or a nil return) means no check has run or the check is disabled.
	updates func() *updatecheck.Snapshot
}

// currentUpdates resolves the latest update-check snapshot, tolerating both a
// missing accessor (UI wiring without a checker) and a checker with no
// completed run yet.
func (s stateSnapshot) currentUpdates() *updatecheck.Snapshot {
	if s.updates == nil {
		return nil
	}
	return s.updates()
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
		{Name: events.StateStacks, Data: roster.BuildState(s.stacks(), s.deployer.CurrentDisabledStacks(), s.auditLog, s.deployer.CurrentTrackedFiles(), s.repo, s.currentUpdates())},
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
