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
	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/nixos"
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

// ErrRolledBack marks a failed deploy whose stack was successfully rolled
// back to the previous compose file version. DeployAllStacks checks it with
// errors.Is to emit a rolled_back (instead of failed) event.
var ErrRolledBack = errors.New("rolled back to previous version")

// ErrRollbackUnhealthy marks a failed deploy whose rollback ran, but whose
// restored version also failed the health gate: the stack is back on the old
// compose file yet not verified healthy. Only possible with a health_check
// configured (the rollback then reruns the same gate). DeployAllStacks checks
// it with errors.Is to emit a rolled_back_unhealthy event.
var ErrRollbackUnhealthy = errors.New("rolled back but still unhealthy")

// Deployer orchestrates deployments for all configured stacks.
// Inject a custom Runner to replace real docker/git calls in tests.
// Optionally inject a CommitReader to enable diff logging on deploy.
type Deployer struct {
	runner       Runner
	commitReader CommitReader    // nil disables diff logging
	syncer       RepoSyncer      // nil when using DeployAllStacks directly
	repoDir      string          // used to skip diff for files outside the repo
	stateDir     string          // directory for state.yaml persistence
	shutdownCtx  context.Context // nil = rebuild waits are never abandoned
	mu           sync.Mutex
	eventSink    func(events.DeployEvent) // nil = no event tracking
	nextEventID  atomic.Int64
	lastSyncErr  atomic.Pointer[syncOutcome] // nil until the first run
	autosync     *autosync.Controller        // nil = autosync always on
	queue        *autosync.Queue             // nil = no pending tracking
	postRunHook  func()                      // nil = none; runs after each deploy run
	prober       *httpHealthProber           // nil = lazily built real prober

	runPlanSink    func(RunPlan)           // nil = no run-plan tracking (UI off)
	currentRunPlan atomic.Pointer[RunPlan] // latest published plan, for late joiners
	plan           []string                // stacks planned to deploy this run, in order
}

// syncOutcome records the result of the most recent repository sync.
type syncOutcome struct {
	err error // nil on success
}

const defaultStateDir = "/var/lib/skipper"

// NixosStateKey is the reserved stack key used for the NixOS rebuild in the
// persisted state, deploy events, and metrics. It is exported so the UI wiring
// can recognize the pseudo-stack (e.g. to resolve its icon).
const NixosStateKey = "_nixos"

func NewDeployer() *Deployer {
	return &Deployer{runner: ShellRunner{}, stateDir: defaultStateDir}
}

// NewDeployerWithCommitReader returns a Deployer running docker (and
// nixos-rebuild) with the given per-command timeout. A non-nil sink
// additionally receives the child processes' output line by line.
func NewDeployerWithCommitReader(commitReader CommitReader, syncer RepoSyncer, repoDir, stateDir string, commandTimeout time.Duration, sink command.LineSink) *Deployer {
	if stateDir == "" {
		stateDir = defaultStateDir
	}
	return &Deployer{runner: command.NewShellRunnerWithSink(commandTimeout, sink), commitReader: commitReader, syncer: syncer, repoDir: repoDir, stateDir: stateDir}
}

func newDeployerWithRunner(r Runner) *Deployer {
	return &Deployer{runner: r, stateDir: defaultStateDir}
}

// SetShutdownContext installs the process shutdown context. When it fires
// while a nixos-rebuild is in flight, the deployer stops waiting for the
// rebuild — the rebuild itself keeps running in its transient systemd unit —
// so a switch that restarts the skipper service cannot deadlock against the
// graceful shutdown (ADR-0014). Must be called before any deployments start.
func (d *Deployer) SetShutdownContext(ctx context.Context) {
	d.shutdownCtx = ctx
}

// SetEventSink configures an optional callback invoked on every deploy
// status change. Must be called before any deployments start.
func (d *Deployer) SetEventSink(fn func(events.DeployEvent)) {
	d.eventSink = fn
}

// SetAutosync installs the autosync controller and pending queue. When unset,
// autosync is always on and every changed stack deploys. Must be called before
// any deployments start.
func (d *Deployer) SetAutosync(c *autosync.Controller, q *autosync.Queue) {
	d.autosync = c
	d.queue = q
}

// SetPostRunHook installs a callback invoked once at the end of every deploy
// run (after state is saved). It is used to publish autosync/queue snapshots
// and refresh gauges. Must be called before any deployments start.
func (d *Deployer) SetPostRunHook(fn func()) {
	d.postRunHook = fn
}

// isPaused reports whether autosync is currently not effective for the stack.
// With no controller installed, autosync is always on.
func (d *Deployer) isPaused(stack string) bool {
	return d.autosync != nil && !d.autosync.Effective(stack)
}

// markQueued records a deferred deploy in the pending registry (when installed).
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

// InitEventID sets the starting event ID counter (e.g. from persisted history).
func (d *Deployer) InitEventID(startID int64) {
	d.nextEventID.Store(startID)
}

// emit sends a deploy event to the sink and returns its ID (0 when no
// sink is configured). The ID lets log lines reference the event, e.g.
// for diff lookups via /api/events/{id}/diffs.
func (d *Deployer) emit(status events.Status, stack string, duration time.Duration, errMsg string, changedFiles []string, diffs map[string]string, commits []events.CommitInfo) int64 {
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
		ChangedFiles: d.repoRelativePaths(changedFiles),
		Diffs:        d.repoRelativeDiffs(diffs),
		Commits:      commits,
	})
	return id
}

// repoRelative shortens an absolute path under the repo clone to a repo-relative
// path for display: the hashing and diff layers work in absolute filesystem
// paths, but the UI has no notion of the repo dir. Paths outside the repo (or
// when the repo dir is unknown) are returned unchanged.
func (d *Deployer) repoRelative(path string) string {
	if d.repoDir == "" {
		return path
	}
	rel, err := filepath.Rel(d.repoDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
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

func (d *Deployer) DeployAllStacks(ctx context.Context, cfg *config.Config) {
	slog.Info("starting deploy run", "stacks", len(cfg.Stacks))

	baseEnv := os.Environ()
	if cfg.VarsFile != "" {
		varsEnv, err := parseEnvFile(cfg.VarsFile)
		if err != nil {
			slog.Error("could not load vars_file, aborting", "err", err)
			return
		}
		baseEnv = append(baseEnv, varsEnv...)
	}

	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		slog.Error("could not load deploy state, deploying all stacks", "err", err)
		state = newEmptyState()
	}

	if cfg.NixOSRebuild.IsEnabled() && !d.rebuildNixOSIfChanged(ctx, cfg, state) {
		return
	}

	// Look-ahead for the UI: hash every stack upfront to learn which will deploy
	// this run, so the header can show what is coming next. Skipped when the UI
	// is off (no sink). The loop below re-evaluates each stack independently and
	// remains the source of truth for what actually deploys.
	if d.runPlanSink != nil {
		d.plan = d.computeRunPlan(cfg, state)
	} else {
		d.plan = nil
	}

	for _, stack := range cfg.Stacks {
		startTime := time.Now()
		if err := d.deployStackIfChanged(ctx, stack, cfg.StacksBaseDir, cfg.VarsFile, baseEnv, state); err != nil {
			duration := time.Since(startTime)
			metrics.DeployErrors.WithLabelValues(stack.Name).Inc()
			switch {
			case errors.Is(err, ErrRollbackUnhealthy):
				slog.Error("deploy failed, rollback ran but stack is still unhealthy", "stack", stack.Name, "err", err)
				d.emit(events.StatusRolledBackUnhealthy, stack.Name, duration, err.Error(), nil, nil, nil)
			case errors.Is(err, ErrRolledBack):
				slog.Warn("deploy failed but rolled back", "stack", stack.Name, "err", err)
				d.emit(events.StatusRolledBack, stack.Name, duration, err.Error(), nil, nil, nil)
			default:
				slog.Error("deploy failed", "stack", stack.Name, "err", err)
				d.emit(events.StatusFailed, stack.Name, duration, err.Error(), nil, nil, nil)
			}
		}
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

	// After the run, let the wiring publish autosync/queue snapshots and refresh
	// gauges (queue depth may have changed via defer/clear this run).
	if d.postRunHook != nil {
		d.postRunHook()
	}
}

// rebuildNixOSIfChanged hashes the repo's nix files and runs nixos-rebuild
// when any of them changed. The new hashes are persisted to state *before*
// the rebuild, because the rebuild may restart the skipper-cd service
// (killing this process); pre-saving avoids a redundant rebuild on restart.
// Returns false when the rebuild failed and all stack deploys must abort.
func (d *Deployer) rebuildNixOSIfChanged(ctx context.Context, cfg *config.Config, state *persistedState) bool {
	startTime := time.Now()

	currentNixHashes, _ := nixos.HashFiles(d.repoDir)
	changed := nixos.DiffHashes(currentNixHashes, state.hashesFor(NixosStateKey))

	// Reconcile a rebuild that a self-restart (the switch restarting skipper-cd)
	// interrupted before its outcome was recorded: the in-flight marker survived
	// the restart, the rebuild kept running in its transient unit and applied (we
	// are back up from the new system), so emit the _nixos success the interrupted
	// run could not — the persisted success supersedes the missing outcome so the
	// UI stops showing a stale failure — then clear the marker (ADR-0025).
	if len(state.NixOSRebuildInFlight) > 0 {
		reconciled := state.NixOSRebuildInFlight
		state.clearNixOSRebuildInFlight()
		_ = saveDeployState(d.stateDir, state)
		metrics.DeploysTriggered.WithLabelValues(NixosStateKey).Inc()
		metrics.LastDeployTimestamp.WithLabelValues(NixosStateKey).Set(float64(time.Now().Unix()))
		// The interrupted run never advanced LastDeployedCommit, so it still points
		// at the pre-rebuild baseline: diff the reconciled files against it so the UI
		// shows what changed, exactly like a normal rebuild success (ADR-0025).
		reconciledDiffs := d.collectDiffs(ctx, reconciled, state.LastDeployedCommit)
		reconciledCommits := d.collectCommits(ctx, reconciled, state.LastDeployedCommit)
		d.emit(events.StatusSuccess, NixosStateKey, 0, "", reconciled, reconciledDiffs, reconciledCommits)
		slog.Info("reconciled nixos-rebuild interrupted by a self-restart", "changed_files", reconciled)
		// Nothing changed since the interrupted rebuild → done. A nix change that
		// arrived afterwards still falls through to a fresh rebuild below.
		if len(changed) == 0 {
			d.clearQueued(NixosStateKey)
			return true
		}
	}

	if len(changed) == 0 {
		d.clearQueued(NixosStateKey) // nothing pending anymore
		metrics.DeploysSkipped.WithLabelValues(NixosStateKey).Inc()
		d.emit(events.StatusSkipped, NixosStateKey, 0, "", nil, nil, nil)
		return true
	}

	// Diff the changed nix files against the last deployed commit so the UI can
	// show *what* changed, not just which files did (LastDeployedCommit is only
	// advanced at the end of the run, so it still points at the previous state).
	diffs := d.collectDiffs(ctx, changed, state.LastDeployedCommit)
	commits := d.collectCommits(ctx, changed, state.LastDeployedCommit)

	// Autosync gate: when _nixos is paused, defer the rebuild. Keep the previous
	// nix hashes (do not pre-save) so the change stays pending, and return true
	// so Docker stack deploys still run this pass (docs/autosync.md).
	if d.isPaused(NixosStateKey) {
		reason := d.markQueued(NixosStateKey, changed)
		metrics.DeploysQueued.WithLabelValues(NixosStateKey).Inc()
		d.emit(events.StatusQueued, NixosStateKey, 0, "", changed, diffs, commits)
		slog.Info("nixos-rebuild deferred: autosync paused", "reason", reason, "changed_files", changed)
		return true
	}
	// Not paused: the rebuild runs now, so drop it from the pending queue.
	d.clearQueued(NixosStateKey)

	// Persist the new hashes before the rebuild: the switch may restart this
	// very service, and pre-saving avoids a redundant rebuild on restart
	// (ADR-0005). Keep the previous snapshot so a surviving failure can undo it.
	previousNixHashes := state.hashesFor(NixosStateKey)
	state.recordStack(NixosStateKey, currentNixHashes)
	// Mark the rebuild in flight (persisted with the hashes): if the switch
	// restarts skipper before the outcome is recorded, the next startup reconciles
	// this into a success rather than leaving a stale failure (ADR-0025).
	state.markNixOSRebuildInFlight(changed)
	_ = saveDeployState(d.stateDir, state)

	if err := d.runNixOSRebuild(ctx, cfg.NixOSRebuild.Flake); err != nil {
		if d.shutdownRequested() {
			// The switch is restarting skipper; the rebuild keeps running in its
			// transient unit and will apply. Keep the pre-saved hashes AND the
			// in-flight marker so the startup sync does not rebuild again and can
			// reconcile the interrupted run into a success (ADR-0005, ADR-0014,
			// ADR-0025). This is a normal outcome — do not emit a failure or count
			// an error; the canceled wait is not a rebuild failure.
			slog.Warn("shutdown during nixos-rebuild: the rebuild keeps running in its transient unit; stack deploys abort and reconcile on the next sync", "err", err)
			return false
		}
		// A genuine rebuild failure while skipper is still alive: revert the
		// pre-saved hashes so the next sync retries, instead of silently recording
		// a rebuild that never applied as done, and clear the in-flight marker so
		// no spurious reconciliation fires (ADR-0015, ADR-0025).
		slog.Error("nixos-rebuild failed, aborting all stack deploys", "err", err)
		state.revertStack(NixosStateKey, previousNixHashes)
		state.clearNixOSRebuildInFlight()
		_ = saveDeployState(d.stateDir, state)
		metrics.DeployErrors.WithLabelValues(NixosStateKey).Inc()
		d.emit(events.StatusFailed, NixosStateKey, time.Since(startTime), err.Error(), changed, diffs, commits)
		return false
	}

	// The rebuild completed without restarting skipper: clear the in-flight
	// marker (persisted by the run's end-of-run save) so no reconciliation fires.
	state.clearNixOSRebuildInFlight()
	metrics.DeploysTriggered.WithLabelValues(NixosStateKey).Inc()
	metrics.LastDeployTimestamp.WithLabelValues(NixosStateKey).Set(float64(time.Now().Unix()))
	d.emit(events.StatusSuccess, NixosStateKey, time.Since(startTime), "", changed, diffs, commits)
	slog.Info("nixos-rebuild complete", "changed_files", changed)
	return true
}

// runNixOSRebuild waits for the rebuild with a context that additionally
// cancels on shutdown: the switch may be restarting this very service, and
// blocking on the rebuild would deadlock the stop (ADR-0014).
func (d *Deployer) runNixOSRebuild(ctx context.Context, flake string) error {
	if d.shutdownCtx != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		defer context.AfterFunc(d.shutdownCtx, cancel)()
	}
	return nixos.New(d.runner).Rebuild(ctx, d.repoDir, flake)
}

func (d *Deployer) shutdownRequested() bool {
	return d.shutdownCtx != nil && d.shutdownCtx.Err() != nil
}

func (d *Deployer) deployStackIfChanged(ctx context.Context, stack config.Stack, baseDir, varsFile string, baseEnv []string, state *persistedState) error {
	// Change detection always uses the repo clone so that merged PRs are detected.
	repoDir := filepath.Join(baseDir, stack.Name)
	composePath := filepath.Join(repoDir, "docker-compose.yml")

	// When working_dir is set, use it as --project-directory for Docker Compose
	// project identity (container labels, .env loading) while the compose file
	// is always read from the repo clone via -f.
	projectDir := stack.WorkingDir

	// Parse the compose file once; images, pullable services and Dockerfile
	// paths are all derived from this single parse. When parsing fails, the
	// deploy degrades gracefully: no build tracking and pull everything.
	compose, err := parseComposeFile(composePath)
	if err != nil {
		slog.Warn("could not parse compose file, pulling all services and skipping build tracking", "stack", stack.Name, "err", err)
	}

	var dockerfilePaths []string
	if compose != nil {
		dockerfilePaths = compose.dockerfilePaths(repoDir)
	}

	currentHashes, err := computePerFileHashes(repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		return fmt.Errorf("compute per-file hashes: %w", err)
	}

	changed := changedFiles(currentHashes, state.hashesFor(stack.Name))
	if len(changed) == 0 {
		slog.Debug("skipping stack, no changes detected", "stack", stack.Name)
		d.clearQueued(stack.Name) // nothing pending anymore
		metrics.DeploysSkipped.WithLabelValues(stack.Name).Inc()
		d.emit(events.StatusSkipped, stack.Name, 0, "", nil, nil, nil)
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
		diffs := d.collectDiffs(ctx, changed, state.LastDeployedCommit)
		commits := d.collectCommits(ctx, changed, state.LastDeployedCommit)
		d.emit(events.StatusQueued, stack.Name, 0, "", changed, diffs, commits)
		slog.Info("deploy deferred: autosync paused", "stack", stack.Name, "reason", reason, "changed_files", changed)
		return nil
	}
	// Not paused: this stack is being deployed now, so it is no longer an
	// autosync deferral — drop it from the pending queue regardless of outcome.
	d.clearQueued(stack.Name)

	deployStart := time.Now()
	d.emit(events.StatusDeploying, stack.Name, 0, "", changed, nil, nil)
	// This stack is now the active deploy: surface the ones still to come.
	d.publishUpcomingAfter(stack.Name)
	slog.Info("deploying stack", "stack", stack.Name, "dir", repoDir, "project_dir", projectDir, "changed_files", changed)
	diffs := d.collectDiffs(ctx, changed, state.LastDeployedCommit)
	commits := d.collectCommits(ctx, changed, state.LastDeployedCommit)
	metrics.DeploysTriggered.WithLabelValues(stack.Name).Inc()

	var currentImages serviceImageByName
	if compose != nil {
		currentImages = compose.images()
	}

	if err := d.pullIfImagesChanged(ctx, stack, composePath, projectDir, baseEnv, compose, currentImages, state.imagesFor(stack.Name)); err != nil {
		return fmt.Errorf("docker compose pull: %w", err)
	}

	if len(dockerfilePaths) > 0 {
		slog.Info("building images from Dockerfile", "stack", stack.Name, "dockerfiles", dockerfilePaths)
		if err := d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, "build", "--pull"); err != nil {
			return fmt.Errorf("docker compose build: %w", err)
		}
	}

	// --remove-orphans removes containers for services deleted from docker-compose.yml.
	upArgs := []string{"up", "-d", "--remove-orphans"}
	if hc := stack.HealthCheck; hc != nil {
		// First health gate: --wait makes the up itself fail when the services'
		// compose healthchecks do not turn healthy in time (ADR-0022).
		upArgs = append(upArgs, "--wait", "--wait-timeout", strconv.Itoa(hc.TimeoutSeconds))
	}
	if err := d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, upArgs...); err != nil {
		return d.rollBackFailedDeploy(ctx, composePath, projectDir, baseEnv, stack, state, "docker compose up", err)
	}

	// Second health gate: the optional HTTP probe verifies the stack from the
	// outside after a successful up (ADR-0022).
	if hc := stack.HealthCheck; hc != nil && hc.URL != "" {
		timeout := time.Duration(hc.TimeoutSeconds) * time.Second
		if err := d.healthProber().waitHealthy(ctx, hc.URL, timeout); err != nil {
			return d.rollBackFailedDeploy(ctx, composePath, projectDir, baseEnv, stack, state, "health check", err)
		}
	}

	if len(stack.OnDemandContainers) > 0 {
		slog.Info("stopping on-demand containers after deploy", "stack", stack.Name, "containers", stack.OnDemandContainers)
		if err := d.runner.Run(ctx, "", nil, "docker", append([]string{"stop"}, stack.OnDemandContainers...)...); err != nil {
			slog.Warn("could not stop on-demand containers", "stack", stack.Name, "err", err)
		}
	}

	state.recordStack(stack.Name, currentHashes)
	if currentImages != nil {
		state.recordImages(stack.Name, currentImages)
	}
	metrics.LastDeployTimestamp.WithLabelValues(stack.Name).Set(float64(time.Now().Unix()))
	eventID := d.emit(events.StatusSuccess, stack.Name, time.Since(deployStart), "", changed, diffs, commits)
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
func (d *Deployer) pullIfImagesChanged(ctx context.Context, stack config.Stack, composePath, projectDir string, baseEnv []string, compose *composeFile, currentImages, previousImages serviceImageByName) error {
	if currentImages != nil && !hasAnyImageChanged(currentImages, previousImages) {
		slog.Info("skipping pull, no image changes", "stack", stack.Name)
		return nil
	}

	if compose == nil {
		return d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, "pull", "--quiet")
	}

	pullable := compose.pullableServices()
	if len(pullable) == 0 {
		slog.Info("skipping pull, all services use locally-built images", "stack", stack.Name)
		return nil
	}
	pullArgs := append([]string{"pull", "--quiet"}, pullable...)
	return d.runDockerCompose(ctx, composePath, projectDir, baseEnv, stack.EnvFiles, pullArgs...)
}

const (
	maxDiffPerFile = 10 * 1024 // 10 KB per file
	maxDiffTotal   = 50 * 1024 // 50 KB total per event
)

// collectDiffs collects git diffs for each changed file and returns them
// as a map of file path to diff content. Large diffs are truncated.
// Returns nil when no CommitReader is configured or no previous commit is known.
func (d *Deployer) collectDiffs(ctx context.Context, changedFilePaths []string, lastDeployedCommit string) map[string]string {
	if d.commitReader == nil || lastDeployedCommit == "" {
		return nil
	}
	result := make(map[string]string)
	totalSize := 0
	for _, filePath := range changedFilePaths {
		if d.repoDir != "" && !strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(d.repoDir)) {
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
		slog.Info("file changed", "file", filePath)

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
		if d.repoDir != "" && !strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(d.repoDir)) {
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

// runDockerCompose executes a docker compose command.
//
// composePath is the absolute path to the docker-compose.yml file (always from the repo clone).
// projectDir, when non-empty, is passed as --project-directory so Docker Compose uses it for
// project identity (container labels) and .env loading. When empty, docker compose runs from the
// directory containing composePath.
func (d *Deployer) runDockerCompose(ctx context.Context, composePath, projectDir string, baseEnv []string, envFiles []string, args ...string) error {
	env := make([]string, len(baseEnv))
	copy(env, baseEnv)
	for _, envFile := range envFiles {
		envVars, err := parseEnvFile(envFile)
		if err != nil {
			return fmt.Errorf("read env file %s: %w", envFile, err)
		}
		env = append(env, envVars...)
	}

	composeArgs := []string{"compose"}
	runDir := filepath.Dir(composePath)
	if projectDir != "" {
		composeArgs = append(composeArgs, "-f", composePath, "--project-directory", projectDir)
		runDir = projectDir
	}
	composeArgs = append(composeArgs, args...)

	return d.runner.Run(ctx, runDir, env, "docker", composeArgs...)
}

// rollBackFailedDeploy handles a deploy that failed at the given stage ("docker
// compose up" or "health check"): it attempts a rollback and wraps the outcome
// so DeployAllStacks emits rolled_back on success, rolled_back_unhealthy when
// the restored version also fails the health gate, and failed otherwise
// (errors.Is on ErrRolledBack / ErrRollbackUnhealthy).
func (d *Deployer) rollBackFailedDeploy(ctx context.Context, composePath, projectDir string, baseEnv []string, stack config.Stack, state *persistedState, stage string, cause error) error {
	slog.Error(stage+" failed, attempting rollback", "stack", stack.Name, "err", cause)

	if rbErr := d.rollbackStack(ctx, composePath, projectDir, baseEnv, stack, state); rbErr != nil {
		if errors.Is(rbErr, ErrRollbackUnhealthy) {
			slog.Error("rollback ran but the restored version is still unhealthy", "stack", stack.Name, "err", rbErr)
			return fmt.Errorf("%s: %w (%w)", stage, cause, rbErr)
		}
		slog.Error("rollback failed", "stack", stack.Name, "err", rbErr)
		return fmt.Errorf("%s: %w (rollback also failed: %v)", stage, cause, rbErr)
	}
	slog.Info("rollback successful, old containers restored", "stack", stack.Name)
	metrics.DeployRollbacks.WithLabelValues(stack.Name).Inc()
	return fmt.Errorf("%s: %w (%w)", stage, cause, ErrRolledBack)
}

// rollbackStack restores containers to the previous compose file version after
// a failed docker compose up. It retrieves the old compose file from git and
// runs docker compose up with it. With a health_check configured the rollback
// reruns the same gate (--wait plus the optional HTTP probe) so a restored
// version that stays unhealthy is reported, not assumed good; those failures
// wrap ErrRollbackUnhealthy.
func (d *Deployer) rollbackStack(ctx context.Context, composePath, projectDir string, baseEnv []string, stack config.Stack, state *persistedState) error {
	if d.commitReader == nil || state.LastDeployedCommit == "" {
		return fmt.Errorf("no previous commit available for rollback")
	}

	oldContent, err := d.commitReader.FileAtCommit(ctx, state.LastDeployedCommit, composePath)
	if err != nil {
		return fmt.Errorf("retrieve old compose file: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "skipper-rollback-*.yml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(oldContent); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	// When projectDir is empty, runDockerCompose uses filepath.Dir(composePath)
	// as the working directory. Since composePath here is a temp file in /tmp/,
	// we must explicitly set projectDir to the original compose file's directory.
	rbProjectDir := projectDir
	if rbProjectDir == "" {
		rbProjectDir = filepath.Dir(composePath)
	}

	slog.Info("rolling back with previous compose file", "stack", stack.Name, "commit", state.LastDeployedCommit)
	upArgs := []string{"up", "-d"}
	if hc := stack.HealthCheck; hc != nil {
		upArgs = append(upArgs, "--wait", "--wait-timeout", strconv.Itoa(hc.TimeoutSeconds))
	}
	if err := d.runDockerCompose(ctx, tmpFile.Name(), rbProjectDir, baseEnv, stack.EnvFiles, upArgs...); err != nil {
		if stack.HealthCheck != nil {
			return fmt.Errorf("restored version did not come up healthy: %v (%w)", err, ErrRollbackUnhealthy)
		}
		return err
	}
	if hc := stack.HealthCheck; hc != nil && hc.URL != "" {
		timeout := time.Duration(hc.TimeoutSeconds) * time.Second
		if err := d.healthProber().waitHealthy(ctx, hc.URL, timeout); err != nil {
			return fmt.Errorf("restored version failed the health probe: %v (%w)", err, ErrRollbackUnhealthy)
		}
	}
	return nil
}
