package main

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/git"

	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/healthwatch"
	"github.com/polandy/skipper-cd/internal/logbuf"
	"github.com/polandy/skipper-cd/internal/orphans"
	"github.com/polandy/skipper-cd/internal/selfheal"
)

// deployerRef breaks the construction cycle around the deployer: several
// collaborators wired into deploy.New (the self-heal healer, the autosync
// publisher, the stack views) call back into the deployer, which does not
// exist yet while they are built. They capture this ref and resolve the
// deployer at call time; main completes the wiring with set immediately after
// deploy.New. get panics when called before set, so a misordered call fails
// loudly at its call site instead of dereferencing nil somewhere downstream.
type deployerRef struct{ d *deploy.Deployer }

func (r *deployerRef) set(d *deploy.Deployer) { r.d = d }

func (r *deployerRef) get() *deploy.Deployer {
	if r.d == nil {
		panic("deployer used before wiring completed")
	}
	return r.d
}

// buildProjectDirSyncer wires the project_directory fast-forward phase
// (ADR-0060), or nil when it has nothing to do: the key is off, or the base is
// inside the deploy clone — which skipper already resets on every sync, so a
// second pull there would only cost a network round trip per reconcile tick.
func buildProjectDirSyncer(cfg *config.Config, repoDir string, timeout time.Duration, sink command.LineSink) deploy.ProjectDirSyncer {
	if !cfg.ProjectDirectorySync {
		return nil
	}
	// Load rejects the key without a base, so the base is set and absolute here.
	if _, inside := relToDir(repoDir, cfg.ProjectDirectoryBase); inside {
		slog.Info("project_directory_sync has nothing to do: project_directory_base is inside the repo clone, which every sync already resets",
			"project_directory_base", cfg.ProjectDirectoryBase, "repo_dir", repoDir)
		return nil
	}
	slog.Info("project_directory checkout is fast-forwarded before each run", "dir", cfg.ProjectDirectoryBase)
	return git.NewCheckout(cfg.ProjectDirectoryBase, timeout, sink)
}

// relToDir reports whether path lies inside base, and its relative form.
func relToDir(base, path string) (string, bool) {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// stackViews derives the per-consumer views of the effective stack set: the
// host config's static list, or — in stack-discovery mode (ADR-0034) — the set
// most recently discovered from the repo (empty until the startup sync
// completes). Every consumer that enumerates stacks (health poller, self-heal,
// autosync order, icons, orphan/app-link detection) reads through these
// methods so discovery updates reach it on the next call.
type stackViews struct {
	cfg      *config.Config
	deployer *deployerRef
}

// effective returns the effective stack set.
func (v stackViews) effective() []config.Stack {
	if v.cfg.StackDiscovery {
		return v.deployer.get().CurrentStacks()
	}
	return v.cfg.Stacks
}

// managed builds the expected set for orphan detection (ADR-0036) from the
// effective stacks, the disabled set, and the recorded project dirs.
func (v stackViews) managed() orphans.Managed {
	m := orphans.Managed{
		BaseDir:      v.cfg.StacksBaseDir,
		ActiveDirs:   map[string]bool{},
		DisabledDirs: map[string]bool{},
		StateDirs:    v.deployer.get().CurrentProjectDirs(),
	}
	for _, s := range v.effective() {
		m.ActiveDirs[stackProjectDir(v.cfg, s)] = true
	}
	for _, name := range v.deployer.get().CurrentDisabledStacks() {
		m.DisabledDirs[filepath.Join(v.cfg.StacksBaseDir, name)] = true
	}
	return m
}

// appLinkDirs builds the stack-name -> project-dir map app-link detection
// matches discovered Traefik hosts against (dev-docs/traefik-app-links-spec.md).
// Only active stacks: a parked (disabled) or removed stack has no icon.
func (v stackViews) appLinkDirs() map[string]string {
	stacks := v.effective()
	m := make(map[string]string, len(stacks))
	for _, s := range stacks {
		m[s.Name] = stackProjectDir(v.cfg, s)
	}
	return m
}

// order returns the effective stack names in the order DeployAllStacks
// processes them (_nixos first when the rebuild is enabled).
func (v stackViews) order() []string { return deploy.RunOrder(v.cfg, v.effective()) }

// setupLogging installs the process-wide slog handler for the configured
// log_format, writing to out. With the UI enabled, all slog output (and, via
// the returned sink, child process output) is teed into an in-memory ring
// served live at /api/logs. Config load already rejected clear errors;
// cfg.Warnings are valid-but-suspicious setups worth flagging — logged first,
// before any other startup line, so they are the first thing an operator sees.
func setupLogging(cfg *config.Config, out io.Writer) (*logbuf.Log, command.LineSink) {
	var logRing *logbuf.Log
	logHandler := newLogHandler(cfg.LogFormat, out)
	if *cfg.UIEnabled {
		logRing = logbuf.New(logbuf.DefaultCapacity)
		logHandler = logbuf.NewHandler(logHandler, logRing)
	}
	slog.SetDefault(slog.New(logHandler))

	for _, w := range cfg.Warnings {
		slog.Warn("config warning", "msg", w)
	}

	// Assign the sink only for a non-nil ring: a typed-nil *logbuf.Log in
	// the interface would defeat the runner's sink != nil check.
	var sink command.LineSink
	if logRing != nil {
		sink = logRing
	}
	return logRing, sink
}

// buildEventFanout composes the deploy-event sink from every configured
// consumer; each is independent, so notifications work with the UI off
// (ADR-0020). Nil entries (absent consumers) are skipped, letting main pass
// every optional consumer unconditionally; the result is nil when no consumer
// remains.
func buildEventFanout(sinks ...func(events.DeployEvent)) func(events.DeployEvent) {
	active := make([]func(events.DeployEvent), 0, len(sinks))
	for _, s := range sinks {
		if s != nil {
			active = append(active, s)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return func(e events.DeployEvent) {
		for _, s := range active {
			s(e)
		}
	}
}

// selfHealResetSink resets a stack's self-heal breaker when a real git deploy
// of it starts, so a push that fixes the fault grants a fresh attempt budget
// (ADR-0029). Nil when self-heal is off.
func selfHealResetSink(engine *selfheal.Engine) func(events.DeployEvent) {
	if engine == nil {
		return nil
	}
	return func(e events.DeployEvent) {
		if e.Status == events.StatusDeploying {
			engine.Reset(e.Stack)
		}
	}
}

// healthWatchSink feeds deploy events to the health watchdog, which observes
// them only for commit attribution (ADR-0031). Nil when the watchdog is off.
func healthWatchSink(w *healthwatch.Watcher) func(events.DeployEvent) {
	if w == nil {
		return nil
	}
	return w.ObserveDeploy
}
