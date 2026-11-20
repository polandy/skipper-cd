package deploy

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/nixos"
)

// NixosStateKey is the reserved stack key used for the NixOS rebuild in the
// persisted state, deploy events, and metrics. It is exported so the UI wiring
// can recognize the pseudo-stack (e.g. to resolve its icon). Aliases
// config.ReservedStackName, the single source of truth for the reserved value.
const NixosStateKey = config.ReservedStackName

// rebuildNixOSIfChanged runs nixos-rebuild when the host's configuration
// changed: any nix file in files mode, the system derivation in derivation
// mode (ADR-0065). The new entry is persisted to state *before* the rebuild,
// because the rebuild may restart the skipper-cd service (killing this
// process); pre-saving avoids a redundant rebuild on restart.
// Returns false when the phase failed and all stack deploys must abort; a
// switch that applied but left units failed is reported and does not abort.
func (d *Deployer) rebuildNixOSIfChanged(ctx context.Context, cfg *config.Config, state *persistedState) bool {
	startTime := time.Now()

	sys, ok := d.measureNixSystem(ctx, cfg, state, startTime)
	if !ok {
		return false
	}
	reconciled := d.reconcileInterruptedRebuild(ctx, state)
	if !sys.changed {
		d.skipUnchangedSystem(cfg, state, sys, !reconciled)
		return true
	}

	// Diff the changed nix files against the last deployed commit so the UI can
	// show *what* changed (LastDeployedCommit only advances at the end of the run).
	cs := d.collectChange(ctx, sys.changedFiles, state.LastDeployedCommit, nil)
	if !d.gateRebuild(sys, cs) {
		return true
	}
	return d.runRebuildRecorded(ctx, cfg, state, sys, cs, startTime)
}

// nixSystem is the host configuration as measured against the recorded state.
type nixSystem struct {
	recorded     stackFileHashes // the _nixos entry before this run
	current      stackFileHashes // what to record once the rebuild is due or done
	changedFiles []string
	changed      bool // the files (files mode) or the derivation (derivation mode) moved
}

// measureNixSystem hashes the nix files and, in derivation mode, evaluates the
// system derivation. Either failure aborts the phase: "could not tell" must
// never read as "unchanged" (ADR-0015, ADR-0065). Nothing is persisted yet,
// so there is nothing to revert.
func (d *Deployer) measureNixSystem(ctx context.Context, cfg *config.Config, state *persistedState, startTime time.Time) (nixSystem, bool) {
	currentNixHashes, err := nixos.HashFiles(d.repoDir)
	if err != nil {
		return nixSystem{}, d.abortNixOSPhase(startTime, "could not hash the repo's nix files, aborting all stack deploys", err)
	}
	recorded := state.hashesFor(NixosStateKey)
	changed := nixos.DiffHashes(currentNixHashes, recorded)
	sys := nixSystem{recorded: recorded, current: stackFileHashes(currentNixHashes), changedFiles: changed, changed: len(changed) > 0}
	if cfg.NixOSRebuild.ComparesDerivation() {
		drv, err := d.systemDerivation(ctx, cfg.NixOSRebuild.Flake)
		if err != nil {
			return nixSystem{}, d.abortNixOSPhase(startTime, "could not evaluate the system derivation, aborting all stack deploys", err)
		}
		sys.current[nixos.SystemDrvKey] = drv
		sys.changed = recorded[nixos.SystemDrvKey] != drv // a missing key counts as changed
	}
	return sys, true
}

// reconcileInterruptedRebuild turns a rebuild that a self-restart interrupted
// before its outcome was recorded into the _nixos success that run could not
// emit, then clears the in-flight marker (ADR-0025). The rebuild kept running
// in its transient unit and applied — we are back up from the new system.
// Reports whether there was one to reconcile.
func (d *Deployer) reconcileInterruptedRebuild(ctx context.Context, state *persistedState) bool {
	if len(state.NixOSRebuildInFlight) == 0 {
		return false
	}
	reconciled := markedFiles(state.NixOSRebuildInFlight)
	state.clearNixOSRebuildInFlight()
	if err := saveDeployState(d.stateDir, state); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}
	metrics.DeploysTriggered.WithLabelValues(NixosStateKey).Inc()
	metrics.LastDeployTimestamp.WithLabelValues(NixosStateKey).Set(float64(time.Now().Unix()))
	// The interrupted run never advanced LastDeployedCommit, so it is still the
	// pre-rebuild baseline to diff against (ADR-0025).
	d.emit(events.StatusSuccess, NixosStateKey, 0, "", d.collectChange(ctx, reconciled, state.LastDeployedCommit, nil))
	slog.Info("reconciled nixos-rebuild interrupted by a self-restart", "changed_files", reconciled)
	return true
}

// skipUnchangedSystem settles a phase with nothing to rebuild. The skipped
// event is left out when a reconciled success already reported this run.
func (d *Deployer) skipUnchangedSystem(cfg *config.Config, state *persistedState, sys nixSystem, reportSkipped bool) {
	d.clearQueued(NixosStateKey)
	if reportSkipped {
		metrics.DeploysSkipped.WithLabelValues(NixosStateKey).Inc()
		d.emit(events.StatusSkipped, NixosStateKey, 0, "", changeSet{})
	}
	d.recordUnchangedSystem(cfg, state, sys.current)
}

// gateRebuild applies the autosync gate and reports whether the rebuild runs
// now. A paused _nixos queues the change without pre-saving its hashes, so it
// stays pending, and the stack deploys still run (docs/user/autosync.md).
func (d *Deployer) gateRebuild(sys nixSystem, cs changeSet) bool {
	if d.isPaused(NixosStateKey) {
		reason := d.markQueued(NixosStateKey, sys.changedFiles)
		metrics.DeploysQueued.WithLabelValues(NixosStateKey).Inc()
		d.emit(events.StatusQueued, NixosStateKey, 0, "", cs)
		slog.Info("nixos-rebuild deferred: autosync paused", "reason", reason, "changed_files", sys.changedFiles)
		return false
	}
	d.clearQueued(NixosStateKey)
	return true
}

// runRebuildRecorded pre-saves the new hashes with an in-flight marker, runs
// the rebuild and records its outcome. Pre-saving avoids a redundant rebuild
// when the switch restarts this service (ADR-0005); the marker lets the next
// startup reconcile that restart into a success (ADR-0025). Returns false when
// the stack deploys must abort.
func (d *Deployer) runRebuildRecorded(ctx context.Context, cfg *config.Config, state *persistedState, sys nixSystem, cs changeSet, startTime time.Time) bool {
	state.recordStack(NixosStateKey, sys.current)
	state.markNixOSRebuildInFlight(inFlightMarker(sys.changedFiles))
	if err := saveDeployState(d.stateDir, state); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}

	if err := d.runNixOSRebuild(ctx, cfg.NixOSRebuild); err != nil {
		return d.recordFailedRebuild(state, sys, cs, startTime, err)
	}

	// The marker clear is persisted by the run's end-of-run save.
	state.clearNixOSRebuildInFlight()
	metrics.DeploysTriggered.WithLabelValues(NixosStateKey).Inc()
	metrics.LastDeployTimestamp.WithLabelValues(NixosStateKey).Set(float64(time.Now().Unix()))
	d.emit(events.StatusSuccess, NixosStateKey, time.Since(startTime), "", cs)
	if len(sys.changedFiles) == 0 {
		slog.Info("nixos-rebuild complete: the system derivation changed without a nix-file change", "drv", sys.current[nixos.SystemDrvKey])
	} else {
		slog.Info("nixos-rebuild complete", "changed_files", sys.changedFiles)
	}
	return true
}

// recordFailedRebuild settles a rebuild that returned an error and reports
// whether the stack deploys may still run.
func (d *Deployer) recordFailedRebuild(state *persistedState, sys nixSystem, cs changeSet, startTime time.Time, err error) bool {
	if d.shutdownRequested() {
		// A self-restart, not a failure: keep the pre-saved hashes and the
		// in-flight marker for the next startup to reconcile (ADR-0014, ADR-0025).
		slog.Warn("shutdown during nixos-rebuild: the rebuild keeps running in its transient unit; stack deploys abort and reconcile on the next sync", "err", err)
		return false
	}
	appliedWithFailedUnits := errors.Is(err, nixos.ErrActivatedUnitsFailed)
	if appliedWithFailedUnits {
		// The configuration applied: keep the hashes, since rebuilding cannot fix
		// the units and a unit waiting on a stack needs the stack phase (ADR-0071).
		slog.Error("nixos-rebuild activated the new configuration but units failed; continuing with the stack deploys", "err", err)
	} else {
		// Never applied: revert so the next sync retries (ADR-0015).
		slog.Error("nixos-rebuild failed, aborting all stack deploys", "err", err)
		state.revertStack(NixosStateKey, sys.recorded)
	}
	state.clearNixOSRebuildInFlight()
	if err := saveDeployState(d.stateDir, state); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}
	metrics.DeployErrors.WithLabelValues(NixosStateKey).Inc()
	d.emit(events.StatusFailed, NixosStateKey, time.Since(startTime), err.Error(), cs)
	return appliedWithFailedUnits
}

// abortNixOSPhase reports a phase failure that happened before anything was
// persisted (hashing, evaluation): the stack deploys abort, nothing to revert.
func (d *Deployer) abortNixOSPhase(startTime time.Time, msg string, err error) bool {
	slog.Error(msg, "err", err)
	metrics.DeployErrors.WithLabelValues(NixosStateKey).Inc()
	d.emit(events.StatusFailed, NixosStateKey, time.Since(startTime), err.Error(), changeSet{})
	return false
}

// recordUnchangedSystem records the current nix file hashes when the
// derivation did not change: those file changes did not affect this host, so a
// later rebuild must not list them (ADR-0065). Files mode records nothing — an
// unchanged system means unchanged hashes there.
func (d *Deployer) recordUnchangedSystem(cfg *config.Config, state *persistedState, current stackFileHashes) {
	if cfg.NixOSRebuild.ComparesDerivation() {
		state.recordStack(NixosStateKey, current)
	}
}

// runNixOSRebuild waits for the rebuild with a context that additionally
// cancels on shutdown: the switch may be restarting this very service, and
// blocking on the rebuild would deadlock the stop (ADR-0014).
func (d *Deployer) runNixOSRebuild(ctx context.Context, rebuild *config.NixOSRebuild) error {
	if d.shutdownCtx != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		defer context.AfterFunc(d.shutdownCtx, cancel)()
	}
	return nixos.New(d.runner, d.outputter).Rebuild(ctx, d.repoDir, rebuild.Flake, rebuild.Timeout())
}

func (d *Deployer) shutdownRequested() bool {
	return d.shutdownCtx != nil && d.shutdownCtx.Err() != nil
}
