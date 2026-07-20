// Package deploy handles pulling updated Docker images and applying
// docker-compose stacks. Stacks are skipped when their configuration
// files have not changed since the last deployment.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

	// RunPlanSink receives the run plan whenever it changes: as each stack
	// begins deploying (carrying the stacks still to come) and once more with
	// an empty plan when the run ends. nil disables run-plan tracking, so the
	// upfront planning pass is skipped entirely when the UI is off.
	RunPlanSink func(RunPlan)

	// HookRunSink receives the currently-executing deploy hook (ADR-0038), and
	// the zero value when a phase finishes. nil disables publishing (UI off).
	HookRunSink func(HookRun)
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
	runPlanSink  func(RunPlan)
	hookRunSink  func(HookRun)

	// mu serializes deploy runs (Invariant 7); the fields below it are only
	// touched while it is held.
	mu                     sync.Mutex
	plan                   []string          // stacks planned to deploy this run, in order
	prober                 *httpHealthProber // nil = lazily built real prober; tests pre-set a fake
	rolloutPollInterval    time.Duration     // 0 = default; tests set a small value for the canary wait
	rolloutTimeoutOverride time.Duration     // 0 = derive from config; tests set a short canary-wait deadline
	rolloutDrainOverride   time.Duration     // 0 = derive from config; tests set a short pre-drain wait

	// Read/written from any goroutine without holding mu.
	nextEventID      atomic.Int64
	lastSyncErr      atomic.Pointer[syncOutcome]       // nil until the first run
	currentRunPlan   atomic.Pointer[RunPlan]           // latest published plan, for late joiners
	currentHookRun   atomic.Pointer[HookRun]           // latest published hook-run state, for late joiners
	discoveredStacks atomic.Pointer[config.RepoStacks] // stack-discovery result, nil when stacks are listed explicitly
	projectDirs      atomic.Pointer[map[string]string] // recorded stack→project-dir, for orphan detection
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
		runner:       cfg.Runner,
		outputter:    cfg.Outputter,
		commitReader: cfg.CommitReader,
		syncer:       cfg.Syncer,
		repoDir:      cfg.RepoDir,
		stateDir:     stateDir,
		shutdownCtx:  cfg.ShutdownCtx,
		eventSink:    cfg.EventSink,
		autosync:     cfg.Autosync,
		queue:        cfg.Queue,
		postRunHook:  cfg.PostRunHook,
		runPlanSink:  cfg.RunPlanSink,
		hookRunSink:  cfg.HookRunSink,
	}
	d.nextEventID.Store(cfg.StartEventID)
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

// changeSet carries what a deploy is applying: the changed tracked files and
// their git context (diffs and commits since the last deployed commit). The
// zero value means "no change context" (e.g. a skipped stack).
type changeSet struct {
	files   []string
	diffs   map[string]string
	commits []events.CommitInfo
}

// collectChange gathers the full change context for the given changed files:
// their diffs and the commits that produced them, both against
// lastDeployedCommit. Diffs and commits are nil when no CommitReader is
// configured or no previous commit is known.
func (d *Deployer) collectChange(ctx context.Context, changedFiles []string, lastDeployedCommit string) changeSet {
	return changeSet{
		files:   changedFiles,
		diffs:   d.collectDiffs(ctx, changedFiles, lastDeployedCommit),
		commits: d.collectCommits(ctx, changedFiles, lastDeployedCommit),
	}
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

// emit sends a deploy event to the sink and returns its ID (0 when no
// sink is configured). The ID lets log lines reference the event, e.g.
// for diff lookups via /api/events/{id}/diffs.
func (d *Deployer) emit(status events.Status, stack string, duration time.Duration, errMsg string, cs changeSet) int64 {
	if d.eventSink == nil {
		return 0
	}
	id := d.nextEventID.Add(1)
	d.eventSink(events.DeployEvent{
		ID:           id,
		Timestamp:    time.Now(),
		Stack:        stack,
		Status:       status,
		DurationMs:   duration.Milliseconds(),
		Error:        errMsg,
		ChangedFiles: d.repoRelativePaths(cs.files),
		Diffs:        d.repoRelativeDiffs(cs.diffs),
		Commits:      cs.commits,
	})
	return id
}

// emitDeployFailure counts the error and emits the terminal event that matches
// how the deploy ended: rolled_back_unhealthy when the restored version also
// failed its health gate, rolled_back when the rollback recovered, else a plain
// failed. It carries the same change set a success does, so the UI can show
// what the failed deploy was applying and render its diff — not just the file
// paths left over from the deploying row.
func (d *Deployer) emitDeployFailure(stack string, duration time.Duration, err error, cs changeSet) {
	metrics.DeployErrors.WithLabelValues(stack).Inc()
	switch {
	case errors.Is(err, ErrRollbackUnhealthy):
		slog.Error("deploy failed, rollback ran but stack is still unhealthy", "stack", stack, "err", err)
		d.emit(events.StatusRolledBackUnhealthy, stack, duration, err.Error(), cs)
	case errors.Is(err, ErrRolledBack):
		slog.Warn("deploy failed but rolled back", "stack", stack, "err", err)
		d.emit(events.StatusRolledBack, stack, duration, err.Error(), cs)
	default:
		slog.Error("deploy failed", "stack", stack, "err", err)
		d.emit(events.StatusFailed, stack, duration, err.Error(), cs)
	}
}

// relToRepo returns path relative to repoDir and whether it lies inside it.
// An empty repoDir never matches.
func relToRepo(repoDir, path string) (string, bool) {
	if repoDir == "" {
		return "", false
	}
	rel, err := filepath.Rel(repoDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// insideRepo reports whether path lies inside the repo clone, for excluding
// out-of-repo files (e.g. env files under /etc) from git diff/log lookups.
// With no repo dir configured every path counts as inside — there is nothing
// to exclude against.
func (d *Deployer) insideRepo(path string) bool {
	if d.repoDir == "" {
		return true
	}
	_, inside := relToRepo(d.repoDir, path)
	return inside
}

// repoRelative shortens an absolute path under the repo clone to a repo-relative
// path for display: the hashing and diff layers work in absolute filesystem
// paths, but the UI has no notion of the repo dir. Paths outside the repo (or
// when the repo dir is unknown) are returned unchanged.
func (d *Deployer) repoRelative(path string) string {
	if rel, inside := relToRepo(d.repoDir, path); inside {
		return rel
	}
	return path
}

// repoRelativePaths returns a copy of files with each path shortened to
// repo-relative for display; nil in, nil out (leaves the caller's slice intact).
func (d *Deployer) repoRelativePaths(files []string) []string {
	if files == nil {
		return nil
	}
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = d.repoRelative(f)
	}
	return out
}

// repoRelativeDiffs returns a copy of the diff map re-keyed to repo-relative
// paths for display; nil in, nil out.
func (d *Deployer) repoRelativeDiffs(diffs map[string]string) map[string]string {
	if diffs == nil {
		return nil
	}
	out := make(map[string]string, len(diffs))
	for path, diff := range diffs {
		out[d.repoRelative(path)] = diff
	}
	return out
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

	baseEnv, err := buildBaseEnv(cfg.VarsFile)
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

// emitHealed emits the self-heal corrective-redeploy event. A heal has no
// changed files, diffs, or commits (the desired version is unchanged), so it
// carries only the drift that triggered it — the services the UI shows the heal
// reacted to. Its own path rather than emit's, whose diff/commit params never
// apply here.
func (d *Deployer) emitHealed(stack string, duration time.Duration, drift []events.DriftedService) {
	if d.eventSink == nil {
		return
	}
	d.eventSink(events.DeployEvent{
		ID:         d.nextEventID.Add(1),
		Timestamp:  time.Now(),
		Stack:      stack,
		Status:     events.StatusHealed,
		DurationMs: duration.Milliseconds(),
		HealDrift:  drift,
	})
}

// EmitHealExhausted records that self-heal gave up on a stack it could not
// restore after repeated corrective redeploys (ADR-0029). It is routed through
// the deploy event pipeline so it lands in history, the SSE stream, and
// notifications like any other terminal outcome, and counts a deploy error for
// metrics/alerting.
func (d *Deployer) EmitHealExhausted(stack string) {
	metrics.DeployErrors.WithLabelValues(stack).Inc()
	d.emit(events.StatusHealExhausted, stack, 0, "self-heal exhausted: still unhealthy after repeated corrective redeploys", changeSet{})
}

// buildBaseEnv returns the process environment extended with the entries of
// the optional global vars_file (Invariant 6: env_files > vars_file > environ,
// with env_files appended later per compose call).
func buildBaseEnv(varsFile string) ([]string, error) {
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
	baseEnv, err := buildBaseEnv(cfg.VarsFile)
	if err != nil {
		slog.Error("could not load vars_file, aborting", "err", err)
		return
	}

	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		slog.Error("could not load deploy state, deploying all stacks", "err", err)
		state = newEmptyState()
	}

	if cfg.NixOSRebuild.IsEnabled() && !d.rebuildNixOSIfChanged(ctx, cfg, state) {
		return
	}

	// Stack discovery (ADR-0034): the repo declares the stack set. A file-level
	// error (unparseable skipper.yaml, unreadable base dir) aborts the stack
	// phase — nothing about the set can be trusted, so nothing deploys or is
	// touched; the nixos phase above is host-config-driven and already ran.
	// Entry-level errors fail only the affected stacks, seeded into the
	// dependency gate below so their dependents block.
	var stackErrs []config.StackError
	if cfg.StackDiscovery {
		repo, errs, err := config.LoadRepoStacks(cfg.StacksBaseDir, cfg.Stacks, cfg.ProjectDirectoryBase)
		if err != nil {
			slog.Error("stack discovery failed, no stacks deploy this run", "err", err)
			d.emitDeployFailure(ConfigStateKey, 0, err, changeSet{})
			return
		}
		d.discoveredStacks.Store(&repo)
		stackErrs = errs
		effective := *cfg
		effective.Stacks = repo.Stacks
		cfg = &effective
	}

	slog.Info("starting deploy run", "stacks", len(cfg.Stacks))

	// Deploy in dependency order (ADR-0032): a stable topological sort that keeps
	// config order among stacks not otherwise constrained.
	ordered := orderStacks(cfg.Stacks)

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
	for _, stack := range ordered {
		outcome := d.deployStackGated(ctx, stack, cfg.StacksBaseDir, cfg.VarsFile, baseEnv, state, gate.decide(stack.DependsOn))
		gate.record(stack.Name, outcome)
	}

	// Run finished: clear the look-ahead so the header returns to idle.
	d.publishRunPlan(nil)

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

	// After the run, let the wiring publish autosync/queue snapshots and refresh
	// gauges (queue depth may have changed via defer/clear this run).
	if d.postRunHook != nil {
		d.postRunHook()
	}
}

func (d *Deployer) deployStackIfChanged(ctx context.Context, stack config.Stack, baseDir, varsFile string, baseEnv []string, state *persistedState) (err error) {
	// Change detection always uses the repo clone so that merged PRs are detected.
	run := newStackRun(stack, baseDir, baseEnv)
	repoDir := filepath.Dir(run.composePath)

	// Parse the compose file once; images, pullable services and Dockerfile
	// paths are all derived from this single parse. When parsing fails, the
	// deploy degrades gracefully: no build tracking and pull everything.
	compose, err := parseComposeFile(run.composePath)
	if err != nil {
		slog.Warn("could not parse compose file, pulling all services and skipping build tracking", "stack", stack.Name, "err", err)
	}

	var dockerfilePaths []string
	if compose != nil {
		dockerfilePaths = compose.dockerfilePaths(repoDir)
	}

	currentHashes, err := computePerFileHashes(repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		err = fmt.Errorf("compute per-file hashes: %w", err)
		d.emitDeployFailure(stack.Name, 0, err, changeSet{})
		return err
	}
	d.addStackConfigHash(currentHashes, stack, baseDir)

	changed := changedFiles(currentHashes, state.hashesFor(stack.Name))
	if len(changed) == 0 {
		slog.Info("skipping stack, no changes detected", "stack", stack.Name)
		d.clearQueued(stack.Name) // nothing pending anymore
		metrics.DeploysSkipped.WithLabelValues(stack.Name).Inc()
		d.emit(events.StatusSkipped, stack.Name, 0, "", changeSet{})
		return nil
	}

	// Autosync gate: when paused, defer instead of deploying. The hashes are
	// deliberately not recorded, so the stack stays dirty and re-deploys once
	// sync resumes (docs/autosync.md).
	if d.isPaused(stack.Name) {
		reason := d.markQueued(stack.Name, changed)
		metrics.DeploysQueued.WithLabelValues(stack.Name).Inc()
		// Carry the diff of what is waiting so the paused row can show the
		// effective change, not just the file paths.
		d.emit(events.StatusQueued, stack.Name, 0, "", d.collectChange(ctx, changed, state.LastDeployedCommit))
		slog.Info("deploy deferred: autosync paused", "stack", stack.Name, "reason", reason, "changed_files", changed)
		return nil
	}
	// Not paused: this stack is being deployed now, so it is no longer an
	// autosync deferral — drop it from the pending queue regardless of outcome.
	d.clearQueued(stack.Name)

	deployStart := time.Now()
	d.emit(events.StatusDeploying, stack.Name, 0, "", changeSet{files: changed})
	// This stack is now the active deploy: surface the ones still to come.
	d.publishUpcomingAfter(stack.Name)
	slog.Info("deploying stack", "stack", stack.Name, "dir", repoDir, "project_dir", run.projectDir, "changed_files", d.repoRelativePaths(changed))
	cs := d.collectChange(ctx, changed, state.LastDeployedCommit)
	// From here the stack is actually deploying: any error returned below emits
	// the matching terminal event with the change context gathered above. The
	// success path emits StatusSuccess and returns nil, so this never double-fires.
	defer func() {
		if err != nil {
			d.emitDeployFailure(stack.Name, time.Since(deployStart), err, cs)
		}
	}()
	metrics.DeploysTriggered.WithLabelValues(stack.Name).Inc()

	// pre_deploy hooks run before any container is touched — the point at which
	// the old version is still up, so a backup can dump it (ADR-0038). A failure
	// here aborts before pull/up with no rollback (nothing changed): the deferred
	// emitDeployFailure sees a plain error and emits `failed`.
	if err := d.runHooks(ctx, run, hookPhasePre, stack.Hooks.PreDeploy); err != nil {
		return err
	}

	var currentImages serviceImageByName
	if compose != nil {
		currentImages = compose.images()
	}

	if err := d.pullIfImagesChanged(ctx, run, compose, currentImages, state.imagesFor(stack.Name)); err != nil {
		return fmt.Errorf("docker compose pull: %w", err)
	}

	if len(dockerfilePaths) > 0 {
		slog.Info("building images from Dockerfile", "stack", stack.Name, "dockerfiles", dockerfilePaths)
		if err := d.runDockerCompose(ctx, run, "build", "--pull"); err != nil {
			return fmt.Errorf("docker compose build: %w", err)
		}
	}

	// Apply the new version. A rollout section cuts over its listed services with
	// zero downtime (ADR-0040) — new container alongside old, wait healthy, drain
	// old — while every other service recreates in place; rollout() handles its
	// own rollback and wraps the outcome. Without it, the plain in-place recreate.
	if stack.Rollout != nil {
		if err := d.rollout(ctx, run, compose, state); err != nil {
			return err
		}
	} else {
		// --remove-orphans removes containers for services deleted from docker-compose.yml.
		upArgs := []string{"up", "-d", "--remove-orphans"}
		if hc := stack.HealthCheck; hc != nil {
			// First health gate: --wait makes the up itself fail when the services'
			// compose healthchecks do not turn healthy in time (ADR-0022).
			upArgs = append(upArgs, "--wait", "--wait-timeout", strconv.Itoa(hc.TimeoutSeconds))
		}
		if err := d.runDockerCompose(ctx, run, upArgs...); err != nil {
			return d.rollBackFailedDeploy(ctx, run, state, "docker compose up", err)
		}
	}

	// Second health gate: the optional HTTP probe verifies the stack from the
	// outside after a successful up (ADR-0022).
	if hc := stack.HealthCheck; hc != nil && hc.URL != "" {
		timeout := time.Duration(hc.TimeoutSeconds) * time.Second
		if err := d.healthProber().waitHealthy(ctx, hc.URL, timeout); err != nil {
			return d.rollBackFailedDeploy(ctx, run, state, "health check", err)
		}
	}

	// post_deploy hooks validate the new version from outside compose (a smoke
	// test, a migration). A failure takes the same rollback path as a health
	// probe failure, even without a health_check (ADR-0038): the hook is itself a
	// user-authored health gate.
	if err := d.runHooks(ctx, run, hookPhasePost, stack.Hooks.PostDeploy); err != nil {
		return d.rollBackFailedDeploy(ctx, run, state, "post_deploy hook", err)
	}

	if len(stack.OnDemandContainers) > 0 {
		if compose != nil {
			compose.warnUnmatchedOnDemandContainers(stack.Name, stack.OnDemandContainers)
		}
		slog.Info("stopping on-demand containers after deploy", "stack", stack.Name, "containers", stack.OnDemandContainers)
		if err := d.runner.Run(ctx, "", nil, "docker", append([]string{"stop"}, stack.OnDemandContainers...)...); err != nil {
			slog.Warn("could not stop on-demand containers", "stack", stack.Name, "err", err)
		}
	}

	state.recordStack(stack.Name, currentHashes)
	if currentImages != nil {
		state.recordImages(stack.Name, currentImages)
	}
	state.recordProjectDir(stack.Name, run.effectiveProjectDir())
	metrics.LastDeployTimestamp.WithLabelValues(stack.Name).Set(float64(time.Now().Unix()))
	eventID := d.emit(events.StatusSuccess, stack.Name, time.Since(deployStart), "", cs)
	if eventID != 0 {
		// The event ID lets the log view fetch this deploy's diffs.
		slog.Info("deploy complete", "stack", stack.Name, "event_id", eventID)
	} else {
		slog.Info("deploy complete", "stack", stack.Name)
	}
	return nil
}

// pullIfImagesChanged runs docker compose pull unless no image: reference
// changed since the last deploy. build:-only services are excluded from the
// pull; when the compose file could not be parsed (compose == nil), every
// service is pulled as a safe fallback.
func (d *Deployer) pullIfImagesChanged(ctx context.Context, run stackRun, compose *composeFile, currentImages, previousImages serviceImageByName) error {
	if currentImages != nil && !hasAnyImageChanged(currentImages, previousImages) {
		slog.Info("skipping pull, no image changes", "stack", run.stack.Name)
		return nil
	}

	if compose == nil {
		return d.runDockerCompose(ctx, run, "pull", "--quiet")
	}

	pullable := compose.pullableServices()
	if len(pullable) == 0 {
		slog.Info("skipping pull, all services use locally-built images", "stack", run.stack.Name)
		return nil
	}
	pullArgs := append([]string{"pull", "--quiet"}, pullable...)
	return d.runDockerCompose(ctx, run, pullArgs...)
}

const (
	maxDiffPerFile = 10 * 1024 // 10 KB per file
	maxDiffTotal   = 50 * 1024 // 50 KB total per event
)

// collectDiffs collects git diffs for each changed file inside the repo and
// returns them as a map of file path to diff content. Large diffs are
// truncated. Returns nil when no CommitReader is configured or no previous
// commit is known.
func (d *Deployer) collectDiffs(ctx context.Context, changedFilePaths []string, lastDeployedCommit string) map[string]string {
	if d.commitReader == nil || lastDeployedCommit == "" {
		return nil
	}
	result := make(map[string]string)
	totalSize := 0
	for _, filePath := range changedFilePaths {
		if !d.insideRepo(filePath) {
			continue
		}
		diff, err := d.commitReader.DiffSinceCommit(ctx, lastDeployedCommit, filePath)
		if err != nil {
			slog.Warn("could not compute diff", "file", filePath, "err", err)
			continue
		}
		if diff == "" {
			continue
		}
		slog.Info("file changed", "file", d.repoRelative(filePath))

		if len(diff) > maxDiffPerFile {
			diff = diff[:maxDiffPerFile] + "\n... (truncated)"
		}
		if totalSize+len(diff) > maxDiffTotal {
			remaining := maxDiffTotal - totalSize
			if remaining > 0 {
				diff = diff[:remaining] + "\n... (truncated)"
			} else {
				break
			}
		}
		result[filePath] = diff
		totalSize += len(diff)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// collectCommits returns the commits in the range lastDeployedCommit..HEAD that
// touched the event's changed files (repo-relative story of what shipped), for
// the diff panel's commit header. Returns nil when no CommitReader is configured,
// no previous commit is known, or no changed file lives inside the repo clone —
// mirroring collectDiffs so the header and the diffs stay in lockstep.
func (d *Deployer) collectCommits(ctx context.Context, changedFilePaths []string, lastDeployedCommit string) []events.CommitInfo {
	if d.commitReader == nil || lastDeployedCommit == "" {
		return nil
	}
	repoFiles := make([]string, 0, len(changedFilePaths))
	for _, filePath := range changedFilePaths {
		if !d.insideRepo(filePath) {
			continue
		}
		repoFiles = append(repoFiles, filePath)
	}
	if len(repoFiles) == 0 {
		return nil
	}
	commits, err := d.commitReader.CommitsSinceCommit(ctx, lastDeployedCommit, repoFiles)
	if err != nil {
		slog.Warn("could not read commit metadata", "err", err)
		return nil
	}
	return commits
}

// runDockerCompose executes a docker compose command for the given stack run.
//
// run.composePath is the compose file (always from the repo clone). A
// non-empty run.projectDir is passed as --project-directory so Docker Compose
// uses it for project identity (container labels) and .env loading; when
// empty, docker compose runs from the directory containing the compose file.
func (d *Deployer) runDockerCompose(ctx context.Context, run stackRun, args ...string) error {
	env, err := run.resolveEnv()
	if err != nil {
		return err
	}
	runDir, composeArgs := run.composeInvocation()
	composeArgs = append(composeArgs, args...)

	return d.runner.Run(ctx, runDir, env, "docker", composeArgs...)
}
