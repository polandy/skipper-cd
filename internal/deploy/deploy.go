// Package deploy handles pulling updated Docker images and applying
// docker-compose stacks. Stacks are skipped when their configuration
// files have not changed since the last deployment.
package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// CommitReader retrieves git commit information from the repository.
// It is used to log file diffs between the last deployed commit and HEAD,
// and to retrieve old compose files for rollback.
type CommitReader interface {
	HeadCommitSHA(ctx context.Context) (string, error)
	DiffSinceCommit(ctx context.Context, fromSHA, filePath string) (string, error)
	CommitsSinceCommit(ctx context.Context, fromSHA string, filePaths []string) ([]events.CommitInfo, error)
	FileAtCommit(ctx context.Context, commitSHA, filePath string) ([]byte, error)
}

// RepoSyncer abstracts the git sync operation so the deployer can
// coordinate sync + deploy under a single lock.
type RepoSyncer interface {
	Sync(ctx context.Context) error
}

// Outputter runs a command and returns its captured stdout — rollout uses it to
// read `docker compose ps` (the plain Runner is fire-and-forget). command.ShellRunner
// implements it; nil disables rollout.
type Outputter interface {
	Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// Config wires a Deployer. Runner is the only required field; every other
// nil/zero field disables the corresponding feature, as documented per field.
// All wiring happens at construction — a built Deployer is immutable apart
// from its run state, so there is no setup-before-first-deploy ordering to
// get wrong.
type Config struct {
	// Runner executes docker/git/nixos commands; tests inject a fake.
	Runner Runner

	// Outputter captures command stdout for rollout's `docker compose ps` reads;
	// nil disables rollout. Wire it to a command.ShellRunner.
	Outputter Outputter

	// CommitReader enables diff/commit logging and compose-file rollback;
	// nil disables both.
	CommitReader CommitReader

	// Syncer syncs the repo clone at the start of SyncAndDeployAll; nil when
	// using DeployAllStacks directly.
	Syncer RepoSyncer

	// RepoDir is the repo clone directory, used to skip diffs for files
	// outside the repo and to shorten event paths to repo-relative.
	RepoDir string

	// StateDir is the directory for state.yaml persistence; "" uses
	// /var/lib/skipper.
	StateDir string

	// ShutdownCtx is the process shutdown context. When it fires while a
	// nixos-rebuild is in flight, the deployer stops waiting for the rebuild —
	// the rebuild itself keeps running in its transient systemd unit — so a
	// switch that restarts the skipper service cannot deadlock against the
	// graceful shutdown (ADR-0014). nil means rebuild waits are never
	// abandoned.
	ShutdownCtx context.Context

	// EventSink is invoked on every deploy status change; nil disables event
	// tracking.
	EventSink func(events.DeployEvent)

	// StartEventID seeds the event ID counter (e.g. from persisted history).
	StartEventID int64

	// Autosync and Queue install the autosync controller and the pending
	// registry. A nil Autosync means autosync is always on; a nil Queue
	// disables pending tracking. See docs/autosync.md.
	Autosync *autosync.Controller
	Queue    *autosync.Queue

	// PostRunHook runs once at the end of every deploy run (after state is
	// saved), e.g. to publish autosync/queue snapshots and refresh gauges.
	PostRunHook func()

	// StackSetSink runs as soon as a run has resolved the effective stack set
	// from the repo (stack-discovery mode only), before any stack deploys. The
	// set is unknown until then, so without this the UI's roster — and with it
	// every per-stack affordance derived from the stack config, such as the
	// hooks badge — would stay empty for the whole first run, however long its
	// deploys take. nil disables it.
	StackSetSink func()

	// RunPlanSink receives the run plan whenever it changes: as each stack
	// begins deploying (carrying the stacks still to come) and once more with
	// an empty plan when the run ends. nil disables run-plan tracking, so the
	// upfront planning pass is skipped entirely when the UI is off.
	RunPlanSink func(RunPlan)

	// HookRunSink receives the currently-executing deploy hook (ADR-0038), and
	// the zero value when a phase finishes. nil disables publishing (UI off).
	HookRunSink func(HookRun)

	// LastOutcome returns a stack's newest terminal audit outcome plus that
	// outcome's event ID (0 when the event has left the bounded history). A
	// success event consults it to mark itself as the retry of a rollback
	// (follows_rollback — UI_SPEC.md "Rollback linkage"). nil disables the
	// annotation; the field only feeds the UI payload.
	LastOutcome func(stack string) (status events.Status, eventID int64, ok bool)

	// The fields below are timing/probe seams. Zero values use the production
	// defaults; tests set fakes and small values so waits resolve
	// deterministically instead of racing wall-clock time.

	// ProbeClient issues the deploy_health_check URL probe requests (ADR-0022);
	// nil uses a default http.Client.
	ProbeClient HTTPDoer

	// ProbeInterval is the pause between two probe attempts; 0 uses the
	// 2-second default.
	ProbeInterval time.Duration

	// RolloutPollInterval is the pause between `docker compose ps` polls while
	// waiting for a rollout canary to turn healthy (ADR-0040); 0 uses the
	// 2-second default.
	RolloutPollInterval time.Duration

	// RolloutTimeoutOverride forces the canary health-wait deadline; 0 derives
	// it from the stack's rollout/deploy_health_check config.
	RolloutTimeoutOverride time.Duration

	// RolloutDrainOverride forces the wait between a healthy canary and
	// draining the old container; 0 uses the stack's rollout.drain_seconds.
	RolloutDrainOverride time.Duration
}

// Deployer orchestrates deployments for all configured stacks. Construct it
// with New; the Config doc comments describe each collaborator.
type Deployer struct {
	// Collaborators fixed at construction, read-only afterwards (see Config).
	runner       Runner
	outputter    Outputter
	commitReader CommitReader
	syncer       RepoSyncer
	repoDir      string
	stateDir     string
	shutdownCtx  context.Context
	eventSink    func(events.DeployEvent)
	autosync     *autosync.Controller
	queue        *autosync.Queue
	postRunHook  func()
	stackSetSink func()
	runPlanSink  func(RunPlan)
	hookRunSink  func(HookRun)
	lastOutcome  func(stack string) (events.Status, int64, bool)
	prober       *httpHealthProber

	// Timing overrides from Config; 0 = derive from stack config / defaults.
	rolloutPollInterval    time.Duration
	rolloutTimeoutOverride time.Duration
	rolloutDrainOverride   time.Duration

	// mu serializes deploy runs (Invariant 7); the fields below it are only
	// touched while it is held.
	mu           sync.Mutex
	plan         []string // stacks planned to deploy this run, in order
	bootstrapRun bool     // nothing was recorded yet: converge without force-refreshing images (ADR-0051)

	// Read/written from any goroutine without holding mu.
	nextEventID      atomic.Int64
	lastSyncErr      atomic.Pointer[syncOutcome]                  // nil until the first run
	currentRunPlan   atomic.Pointer[RunPlan]                      // latest published plan, for late joiners
	currentHookRun   atomic.Pointer[HookRun]                      // latest published hook-run state, for late joiners
	discoveredStacks atomic.Pointer[config.RepoStacks]            // stack-discovery result, nil when stacks are listed explicitly
	projectDirs      atomic.Pointer[map[string]string]            // recorded stack→project-dir, for orphan detection
	trackedFiles     atomic.Pointer[map[string][]string]          // recorded stack→hashed input paths, for the roster
	runningImagesNow atomic.Pointer[map[string]map[string]string] // recorded stack→service→running image, for the update check
}

// syncOutcome records the result of the most recent repository sync.
type syncOutcome struct {
	err error // nil on success
}

const defaultStateDir = "/var/lib/skipper"

// New builds a Deployer from cfg.
func New(cfg Config) *Deployer {
	stateDir := cfg.StateDir
	if stateDir == "" {
		stateDir = defaultStateDir
	}
	d := &Deployer{
		runner:                 cfg.Runner,
		outputter:              cfg.Outputter,
		commitReader:           cfg.CommitReader,
		syncer:                 cfg.Syncer,
		repoDir:                cfg.RepoDir,
		stateDir:               stateDir,
		shutdownCtx:            cfg.ShutdownCtx,
		eventSink:              cfg.EventSink,
		autosync:               cfg.Autosync,
		queue:                  cfg.Queue,
		postRunHook:            cfg.PostRunHook,
		stackSetSink:           cfg.StackSetSink,
		runPlanSink:            cfg.RunPlanSink,
		hookRunSink:            cfg.HookRunSink,
		lastOutcome:            cfg.LastOutcome,
		prober:                 newHealthProber(cfg.ProbeClient, cfg.ProbeInterval),
		rolloutPollInterval:    cfg.RolloutPollInterval,
		rolloutTimeoutOverride: cfg.RolloutTimeoutOverride,
		rolloutDrainOverride:   cfg.RolloutDrainOverride,
	}
	d.nextEventID.Store(cfg.StartEventID)

	// Seed the tracked-file view from the persisted state, so the roster can
	// answer "what is watched here" from the moment the UI is up rather than
	// only after a run completes. A failed git sync returns before the deploy
	// phase, and every stack would otherwise read as never deployed — the one
	// wrong answer that surface exists to prevent.
	if state, err := loadPersistedDeployState(stateDir); err != nil {
		slog.Warn("could not read deploy state to seed the tracked-file view", "err", err)
	} else {
		tracked := state.trackedFiles()
		d.trackedFiles.Store(&tracked)
		running := state.runningImagesView()
		d.runningImagesNow.Store(&running)
	}
	return d
}

// stackRun bundles the per-stack values resolved once at the start of a
// stack's deploy (or heal) and threaded through every docker compose
// invocation, so they travel together instead of as parallel parameters.
type stackRun struct {
	stack       config.Stack
	composePath string   // compose file, always from the repo clone
	projectDir  string   // --project-directory; "" = compose file's own dir
	baseEnv     []string // os.Environ() + vars_file (env_files are added per call)
}

// effectiveProjectDir returns the directory docker compose uses as the project's
// working_dir label: --project-directory when set, else the compose file's dir
// (Invariant 1). It is what orphan detection matches against.
func (r stackRun) effectiveProjectDir() string {
	if r.projectDir != "" {
		return r.projectDir
	}
	return filepath.Dir(r.composePath)
}

// newStackRun resolves the run values for a stack: the compose file from the
// repo clone (Invariant 1) and project_directory as the compose project directory.
func newStackRun(stack config.Stack, baseDir string, baseEnv []string) stackRun {
	return stackRun{
		stack:       stack,
		composePath: filepath.Join(baseDir, stack.Name, compose.FileName),
		projectDir:  stack.ProjectDirectory,
		baseEnv:     baseEnv,
	}
}

// composeInvocation returns the working directory and the leading `compose …`
// args (project selection) shared by every docker compose call for this stack.
// A non-empty projectDir is passed as --project-directory so compose uses it
// for project identity and .env loading; when empty, compose runs from the
// compose file's directory and discovers it there.
func (r stackRun) composeInvocation() (dir string, args []string) {
	dir = filepath.Dir(r.composePath)
	args = []string{"compose"}
	if r.projectDir != "" {
		args = append(args, "-f", r.composePath, "--project-directory", r.projectDir)
		dir = r.projectDir
	}
	return dir, args
}

// resolveEnv returns baseEnv extended with this stack's env_files (Invariant 6:
// env_files win over vars_file and os.Environ, which baseEnv already holds).
func (r stackRun) resolveEnv() ([]string, error) {
	env := make([]string, len(r.baseEnv))
	copy(env, r.baseEnv)
	for _, envFile := range r.stack.EnvFiles {
		envVars, err := parseEnvFile(envFile)
		if err != nil {
			return nil, fmt.Errorf("read env file %s: %w", envFile, err)
		}
		env = append(env, envVars...)
	}
	return env, nil
}

// isPaused reports whether autosync is currently not effective for the stack.
// With no controller installed, autosync is always on.
func (d *Deployer) isPaused(stack string) bool {
	return d.autosync != nil && !d.autosync.Effective(stack)
}

// markQueued records a deferred deploy in the pending registry (when
// installed). It is only reachable after isPaused reported true, which
// implies d.autosync is non-nil — hence the asymmetric nil-handling.
func (d *Deployer) markQueued(stack string, changed []string) string {
	reason := d.autosync.Reason(stack)
	if d.queue != nil {
		d.queue.Mark(stack, changed, reason)
	}
	return reason
}

// clearQueued removes any pending entry for the stack (when installed).
func (d *Deployer) clearQueued(stack string) {
	if d.queue != nil {
		d.queue.Clear(stack)
	}
}

// SyncAndDeployAll acquires the deploy lock, syncs the repository, and
// deploys all stacks. Concurrent callers wait for their turn.
func (d *Deployer) SyncAndDeployAll(ctx context.Context, cfg *config.Config) {
	if !d.mu.TryLock() {
		slog.Info("deploy already in progress, waiting")
		metrics.DeployLockWaits.Inc()
		d.mu.Lock()
	}
	defer d.mu.Unlock()

	d.syncAndDeployLocked(ctx, cfg)
}

// TrySyncAndDeployAll runs a sync + deploy only if no deploy is already in
// progress; otherwise it returns false immediately without waiting. It is the
// reconcile loop's entry point (ADR-0028): a periodic tick carries no unique
// information, so it is dropped rather than queued behind an in-flight deploy
// (ADR-0010). Returns true when the run happened.
func (d *Deployer) TrySyncAndDeployAll(ctx context.Context, cfg *config.Config) bool {
	if !d.mu.TryLock() {
		return false
	}
	defer d.mu.Unlock()

	d.syncAndDeployLocked(ctx, cfg)
	return true
}

// HealStack runs a corrective `docker compose up -d` for a single stack to
// restore it to its currently deployed running state after runtime drift (a
// stopped/removed container, an unhealthy service). It is self-heal's action
// (ADR-0029) and is deliberately *not* a git deploy: no change detection, no
// hash update, and no rollback, because the desired version is unchanged — it
// only restarts what should already be running, so a "rollback" to an older
// commit would be wrong.
//
// It serializes on the deploy mutex like every other deploy source (Invariant
// 7). If a deploy is already in progress it returns ran=false without waiting:
// that deploy converges the stack anyway, and the next health poll
// re-evaluates, so piling a heal behind it carries no unique information
// (mirrors the reconcile loop's skip-if-busy, ADR-0010/ADR-0028). A successful
// up emits a healed event; an up error is returned for the caller to count as a
// failed attempt without emitting a misleading failed-deploy event.
func (d *Deployer) HealStack(ctx context.Context, cfg *config.Config, stackName string, drift []events.DriftedService) (ran bool, err error) {
	if !d.mu.TryLock() {
		return false, nil
	}
	defer d.mu.Unlock()

	stack, ok := d.effectiveStack(cfg, stackName)
	if !ok {
		return true, fmt.Errorf("self-heal: unknown stack %q", stackName)
	}

	baseEnv, err := BaseEnv(cfg.VarsFile)
	if err != nil {
		return true, fmt.Errorf("self-heal: %w", err)
	}
	run := newStackRun(stack, cfg.StacksBaseDir, baseEnv)

	start := time.Now()
	slog.Info("self-heal: restoring stack to its deployed running state", "stack", stack.Name)
	// A plain up — no --wait/health gate, no rollback (see the doc comment).
	if err := d.runDockerCompose(ctx, run, "up", "-d", "--remove-orphans"); err != nil {
		return true, fmt.Errorf("self-heal up %q: %w", stack.Name, err)
	}
	metrics.LastDeployTimestamp.WithLabelValues(stack.Name).Set(float64(time.Now().Unix()))
	d.emitHealed(stack.Name, time.Since(start), drift)
	slog.Info("self-heal: stack restored", "stack", stack.Name)
	return true, nil
}

// BaseEnv returns the process environment extended with the entries of the
// optional global vars_file (Invariant 6: env_files > vars_file > environ,
// with env_files appended later per compose call). Read-only compose callers
// outside this package use it too: the health poller probes the same compose
// file and --project-directory the deploy path uses (Invariant 1), so it must
// resolve ${VAR} interpolation from the same environment — otherwise compose
// warns about every unset variable on every poll.
func BaseEnv(varsFile string) ([]string, error) {
	baseEnv := os.Environ()
	if varsFile == "" {
		return baseEnv, nil
	}
	varsEnv, err := parseEnvFile(varsFile)
	if err != nil {
		return nil, fmt.Errorf("load vars_file: %w", err)
	}
	return append(baseEnv, varsEnv...), nil
}

// syncAndDeployLocked syncs the repository and deploys all stacks. The caller
// must hold d.mu.
func (d *Deployer) syncAndDeployLocked(ctx context.Context, cfg *config.Config) {
	if d.syncer != nil {
		if err := d.syncer.Sync(ctx); err != nil {
			slog.Error("git sync failed, aborting deploy", "err", err)
			d.lastSyncErr.Store(&syncOutcome{err: err})
			return
		}
	}
	d.lastSyncErr.Store(&syncOutcome{})
	d.DeployAllStacks(ctx, cfg)
}

// Health reports the outcome of the most recent repository sync: nil while
// no run has happened yet (still starting up) or when the last sync
// succeeded, and the sync error otherwise. Individual stack failures do not
// make the service unhealthy — they are reported per stack via events.
func (d *Deployer) Health() error {
	if outcome := d.lastSyncErr.Load(); outcome != nil {
		return outcome.err
	}
	return nil
}

// WaitIdle blocks until no deploy run is in progress. It is used during
// shutdown to let an in-flight deploy finish instead of interrupting
// docker compose mid-run.
func (d *Deployer) WaitIdle() {
	d.mu.Lock()
	defer d.mu.Unlock()
}

// DeployAllStacks runs one full sync-and-deploy pass: it loads persisted
// state, runs the NixOS rebuild first if configured (aborting all stack
// deploys on failure), then deploys every changed stack in dependency order.
// Callers serialize on SyncAndDeployAll — this method does not lock itself.
func (d *Deployer) DeployAllStacks(ctx context.Context, cfg *config.Config) {
	baseEnv, err := BaseEnv(cfg.VarsFile)
	if err != nil {
		slog.Error("could not load vars_file, aborting", "err", err)
		return
	}

	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		slog.Error("could not load deploy state, deploying all stacks", "err", err)
		state = newEmptyState()
	}

	// Bootstrap run (ADR-0051): nothing is recorded, so every input reads as
	// changed and every stack is deployed. `up` converges what actually
	// differs, but a forced `pull` would also move every floating tag on the
	// host at once — so images are left as they are and only the missing ones
	// are fetched (compose does that itself when it creates a container).
	// Decided here, before the nixos phase, which records its own hashes into
	// the state and would otherwise make an empty state look non-empty by the
	// time the stacks are reached.
	d.bootstrapRun = state.isEmpty()
	if d.bootstrapRun {
		slog.Info("no deploy state recorded: converging every stack without refreshing images already on the host")
	}

	if cfg.NixOSRebuild.IsEnabled() && !d.rebuildNixOSIfChanged(ctx, cfg, state) {
		return
	}

	cfg, stackErrs, err := d.resolveStackSet(cfg)
	if err != nil {
		slog.Error("stack discovery failed, no stacks deploy this run", "err", err)
		d.emitDeployFailure(ConfigStateKey, 0, err, changeSet{})
		return
	}

	// Debug: a header per reconcile tick outweighs what it says; the run's
	// outcome is the summary line the caller logs at the end (ADR-0042
	// amendment).
	slog.Debug("starting deploy run", "stacks", len(cfg.Stacks))
	d.deployStacksGated(ctx, cfg, baseEnv, state, stackErrs)
	d.finishRun(ctx, state)
}

// resolveStackSet returns the effective config for this run. With stack
// discovery (ADR-0034) the repo declares the stack set each sync, with the
// host config's overrides merged in; entry-level errors (bad override, broken
// compose, …) come back as stackErrs so only the affected stacks and their
// dependents fail. A file-level error (unparseable skipper.yaml, unreadable
// base dir) is returned instead — nothing about the set can be trusted, so the
// caller aborts the stack phase; the nixos phase is host-config-driven and has
// already run by then.
func (d *Deployer) resolveStackSet(cfg *config.Config) (*config.Config, []config.StackError, error) {
	if !cfg.StackDiscovery {
		return cfg, nil, nil
	}
	repo, errs, err := config.LoadRepoStacks(cfg.StacksBaseDir, cfg.Stacks, cfg.ProjectDirectoryBase)
	if err != nil {
		return nil, nil, err
	}
	d.discoveredStacks.Store(&repo)
	// The set is now known — let the UI show it before the run's deploys start,
	// not only once they finish.
	if d.stackSetSink != nil {
		d.stackSetSink()
	}
	effective := *cfg
	effective.Stacks = repo.Stacks
	return &effective, errs, nil
}

// deployStacksGated deploys every stack in dependency order (ADR-0032, a
// stable topological sort that keeps config order among stacks not otherwise
// constrained) behind the dependency gate, and maintains the UI's run-plan
// look-ahead while doing so.
func (d *Deployer) deployStacksGated(ctx context.Context, cfg *config.Config, baseEnv []string, state *persistedState, stackErrs []config.StackError) {
	// Look-ahead for the UI: hash every stack upfront to learn which will deploy
	// this run, so the header can show what is coming next. Skipped when the UI
	// is off (no sink). The loop below re-evaluates each stack independently and
	// remains the source of truth for what actually deploys.
	if d.runPlanSink != nil {
		d.plan = d.computeRunPlan(cfg, state)
	} else {
		d.plan = nil
	}

	// The gate records each stack's outcome so a dependent can block (dependency
	// failed) or queue (dependency queued) before it deploys. deployStackGated
	// emits every stack's own terminal event — success, skipped, queued, blocked,
	// or (via emitDeployFailure) failed / rolled_back / rolled_back_unhealthy —
	// so each carries its change context; we only track the outcome here.
	gate := newDepGate()
	for _, se := range stackErrs {
		d.emitDeployFailure(se.Stack, 0, se.Err, changeSet{})
		gate.record(se.Stack, depBlocked)
	}
	for _, stack := range orderStacks(cfg.Stacks) {
		// Resolve the effective rollback policy here, where the global default is
		// in scope, so the deploy path (rollBackFailedDeploy) can honor an opt-out
		// without carrying the global config down (ADR-0050).
		rollback := cfg.RollbackEnabled(stack.Name)
		stack.Rollback = &rollback
		outcome := d.deployStackGated(ctx, stack, cfg.StacksBaseDir, cfg.VarsFile, baseEnv, state, gate.decide(stack.DependsOn))
		gate.record(stack.Name, outcome)
	}

	// Run finished: clear the look-ahead so the header returns to idle.
	d.publishRunPlan(nil)
}

// finishRun persists the run's results and publishes the post-run views.
func (d *Deployer) finishRun(ctx context.Context, state *persistedState) {
	// Record the current HEAD commit so future deploys can diff against it — but
	// only when nothing is still queued. A change deferred by paused autosync has
	// not deployed yet; advancing the base past it would make its eventual deploy
	// (or a self-restart reconcile) diff HEAD..HEAD and show no diff at all. The
	// base is also what rollback restores, so keeping it at the last fully-deployed
	// commit is the correct behaviour, not just a display fix.
	if d.commitReader != nil && (d.queue == nil || d.queue.Count() == 0) {
		if sha, err := d.commitReader.HeadCommitSHA(ctx); err != nil {
			slog.Warn("could not read HEAD commit SHA", "err", err)
		} else {
			state.LastDeployedCommit = sha
		}
	}

	if err := saveDeployState(d.stateDir, state); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}

	// Publish the recorded project dirs for out-of-run orphan detection.
	dirs := state.projectDirs()
	d.projectDirs.Store(&dirs)

	// Publish the hashed input paths so the roster can show what change
	// detection actually watches per stack.
	tracked := state.trackedFiles()
	d.trackedFiles.Store(&tracked)

	// Publish the recorded running images for the out-of-run update check.
	running := state.runningImagesView()
	d.runningImagesNow.Store(&running)

	// After the run, let the wiring publish autosync/queue snapshots and refresh
	// gauges (queue depth may have changed via defer/clear this run).
	if d.postRunHook != nil {
		d.postRunHook()
	}
}
