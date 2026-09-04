package deploy

import (
	"context"
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

// rebuildNixOSIfChanged hashes the repo's nix files and runs nixos-rebuild
// when any of them changed. The new hashes are persisted to state *before*
// the rebuild, because the rebuild may restart the skipper-cd service
// (killing this process); pre-saving avoids a redundant rebuild on restart.
// Returns false when the rebuild failed and all stack deploys must abort.
func (d *Deployer) rebuildNixOSIfChanged(ctx context.Context, cfg *config.Config, state *persistedState) bool {
	startTime := time.Now()

	currentNixHashes, err := nixos.HashFiles(d.repoDir)
	if err != nil {
		// Without the hashes nothing can be said about the host config. Diffing
		// an empty set would report the phase as skipped and deploy the stacks
		// against a config that may never have been applied — the silent
		// never-applied rebuild ADR-0015 exists to prevent. Report it and abort
		// like a failed rebuild does; nothing has been persisted yet, so there is
		// no pre-saved state to revert.
		slog.Error("could not hash the repo's nix files, aborting all stack deploys", "err", err)
		metrics.DeployErrors.WithLabelValues(NixosStateKey).Inc()
		d.emit(events.StatusFailed, NixosStateKey, time.Since(startTime), err.Error(), changeSet{})
		return false
	}
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
		if err := saveDeployState(d.stateDir, state); err != nil {
			slog.Error("could not save deploy state", "err", err)
		}
		metrics.DeploysTriggered.WithLabelValues(NixosStateKey).Inc()
		metrics.LastDeployTimestamp.WithLabelValues(NixosStateKey).Set(float64(time.Now().Unix()))
		// The interrupted run never advanced LastDeployedCommit, so it still points
		// at the pre-rebuild baseline: diff the reconciled files against it so the UI
		// shows what changed, exactly like a normal rebuild success (ADR-0025).
		d.emit(events.StatusSuccess, NixosStateKey, 0, "", d.collectChange(ctx, reconciled, state.LastDeployedCommit))
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
		d.emit(events.StatusSkipped, NixosStateKey, 0, "", changeSet{})
		return true
	}

	// Diff the changed nix files against the last deployed commit so the UI can
	// show *what* changed, not just which files did (LastDeployedCommit is only
	// advanced at the end of the run, so it still points at the previous state).
	cs := d.collectChange(ctx, changed, state.LastDeployedCommit)

	// Autosync gate: when _nixos is paused, defer the rebuild. Keep the previous
	// nix hashes (do not pre-save) so the change stays pending, and return true
	// so Docker stack deploys still run this pass (docs/autosync.md).
	if d.isPaused(NixosStateKey) {
		reason := d.markQueued(NixosStateKey, changed)
		metrics.DeploysQueued.WithLabelValues(NixosStateKey).Inc()
		d.emit(events.StatusQueued, NixosStateKey, 0, "", cs)
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
	if err := saveDeployState(d.stateDir, state); err != nil {
		slog.Error("could not save deploy state", "err", err)
	}

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
		if err := saveDeployState(d.stateDir, state); err != nil {
			slog.Error("could not save deploy state", "err", err)
		}
		metrics.DeployErrors.WithLabelValues(NixosStateKey).Inc()
		d.emit(events.StatusFailed, NixosStateKey, time.Since(startTime), err.Error(), cs)
		return false
	}

	// The rebuild completed without restarting skipper: clear the in-flight
	// marker (persisted by the run's end-of-run save) so no reconciliation fires.
	state.clearNixOSRebuildInFlight()
	metrics.DeploysTriggered.WithLabelValues(NixosStateKey).Inc()
	metrics.LastDeployTimestamp.WithLabelValues(NixosStateKey).Set(float64(time.Now().Unix()))
	d.emit(events.StatusSuccess, NixosStateKey, time.Since(startTime), "", cs)
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
