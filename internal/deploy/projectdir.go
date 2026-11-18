// The project_directory checkout: fast-forwarding the working copy a stack's
// relative bind mounts resolve against, before the stack phase deploys against
// it (ADR-0060). Compose resolves every relative path in a compose file — bind
// mounts included — against --project-directory, not against the clone the
// file was read from (Invariant 1), so on a host where project_directory_base
// is a separate checkout nothing otherwise keeps that content current.

package deploy

import (
	"context"
	"log/slog"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// ProjectDirStateKey is the reserved event key the project_directory
// fast-forward reports under. Like _nixos it names a run phase, not a Compose
// project. Aliases config.ReservedProjectDirStackName, the single source of
// truth for the reserved value.
const ProjectDirStateKey = config.ReservedProjectDirStackName

// ProjectDirSyncer fast-forwards the project_directory checkout. Implemented
// by git.Checkout; nil in the Deployer's Config disables the phase, which is
// what an unset project_directory_sync (or a base inside the deploy clone)
// resolves to.
type ProjectDirSyncer interface {
	// FastForward advances the checkout onto its upstream branch, returning the
	// commits it moved from and to — equal when it was already current. It
	// never merges, rebases or resets, and refuses rather than repairs.
	FastForward(ctx context.Context) (from, to string, err error)
	// Dir names the checkout, for log lines.
	Dir() string
}

// syncProjectDirectory fast-forwards the project_directory checkout, once per
// run and before any stack deploys — the ordering is the whole point of doing
// this inside skipper. A pull *after* the deploys would repair only the next
// one: content a container reads once at start (a provisioning directory, a
// scraper config) has already been read from the stale file by the container
// the deploy just recreated.
//
// It is skipped entirely while autosync is paused globally: that pause means
// the host stops changing, and unlike skipper's own clone this tree is mounted
// by running containers.
//
// A refusal never aborts the run: a stale mount is degraded, not wrong, and an
// operator mid-edit must not have their host's deploys blocked. It is reported
// as a standing condition instead — announced once and again only when its
// message changes (ADR-0055), with the gauge carrying it for as long as it
// lasts.
func (d *Deployer) syncProjectDirectory(ctx context.Context) {
	if d.projectDirSyncer == nil {
		return
	}
	dir := d.projectDirSyncer.Dir()
	// Global only: the checkout is shared, so no per-stack switch governs it.
	if d.autosync != nil && !d.autosync.GlobalEffective() {
		// Debug: nothing queues and every reconcile tick reaches here, so at info
		// this would narrate the pause the UI already shows.
		slog.Debug("project_directory checkout left alone: autosync is paused globally", "dir", dir)
		return
	}
	from, to, err := d.projectDirSyncer.FastForward(ctx)
	if err != nil {
		metrics.ProjectDirSyncError.Set(1)
		slog.Error("could not fast-forward the project_directory checkout; stacks deploy against the content it has now", "dir", dir, "err", err)
		if d.projectDirErrors.record(ProjectDirStateKey, err.Error()) {
			d.emit(events.StatusFailed, ProjectDirStateKey, 0, err.Error(), changeSet{})
		}
		return
	}
	metrics.ProjectDirSyncError.Set(0)
	if d.projectDirErrors.clear(ProjectDirStateKey) {
		slog.Info("the project_directory checkout fast-forwards again", "dir", dir)
	}
	if from == to {
		// Debug: every reconcile tick reaches here, so at info this would report
		// the poll cadence rather than an event.
		slog.Debug("project_directory checkout already current", "dir", dir, "commit", from)
		return
	}
	slog.Info("fast-forwarded the project_directory checkout", "dir", dir, "from", from, "to", to)
}
