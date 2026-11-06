package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/polandy/skipper-cd/internal/events"
)

// errOrphanCleanup wraps a failed removal of a released hold's orphaned
// services (ADR-0066).
var errOrphanCleanup = errors.New("remove orphaned services")

// orphanedContainer is a container of the stack's compose project whose
// service the compose file no longer declares.
type orphanedContainer struct {
	id, service, image string
}

// orphanedServices lists the project's containers whose service is missing
// from the compose file. It answers nothing when it cannot tell — no
// Outputter, no compose parse, or a failed read — so the release is never
// worse than a plain skip. Like runningImages the read carries no env_files.
func (d *Deployer) orphanedServices(ctx context.Context, prep stackPrep) []orphanedContainer {
	if d.outputter == nil {
		return nil
	}
	if prep.compose == nil {
		slog.Warn("cannot check for orphaned services: compose file did not parse", "stack", prep.run.stack.Name)
		return nil
	}
	lines, err := composeJSONRead[containerLine](ctx, d, prep.run, "ps", "--format", "json", "--all")
	if err != nil {
		slog.Warn("cannot check for orphaned services: container read failed", "stack", prep.run.stack.Name, "err", err)
		return nil
	}
	var orphans []orphanedContainer
	for _, l := range lines {
		if _, declared := prep.compose.Services[l.Service]; l.Service == "" || l.ID == "" || declared {
			continue
		}
		orphans = append(orphans, orphanedContainer{id: l.ID, service: l.Service, image: l.Image})
	}
	return orphans
}

// removeOrphanedServices removes the containers of services the compose file
// no longer declares and returns them as removed-service image changes. Only
// those containers are touched: no up, so nothing else is recreated or started
// (ADR-0066).
func (d *Deployer) removeOrphanedServices(ctx context.Context, prep stackPrep) ([]events.ServiceImageChange, error) {
	orphans := d.orphanedServices(ctx, prep)
	if len(orphans) == 0 {
		return nil, nil
	}
	args := []string{"rm", "-f"}
	var removed []events.ServiceImageChange
	for _, o := range orphans {
		args = append(args, o.id)
		if !slices.ContainsFunc(removed, func(c events.ServiceImageChange) bool { return c.Service == o.service }) {
			removed = append(removed, events.ServiceImageChange{Service: o.service, Old: o.image})
		}
	}
	dir, _ := prep.run.composeInvocation()
	if err := d.runner.Run(ctx, dir, nil, "docker", args...); err != nil {
		return nil, fmt.Errorf("%w: %w", errOrphanCleanup, withFailureCause(err))
	}
	return removed, nil
}

// settleReleasedHold finishes an unchanged run of a stack that is still held:
// its inputs are back at the recorded ones (a revert), but a hand-applied held
// version may have left services running that the compose file no longer
// declares. It removes them before releasing the hold; a failed removal keeps
// the hold, so the next tick retries. It reports whether it emitted the
// stack's terminal event.
func (d *Deployer) settleReleasedHold(ctx context.Context, prep stackPrep, state *persistedState) (emitted bool, err error) {
	name := prep.run.stack.Name
	removed, err := d.removeOrphanedServices(ctx, prep)
	if err != nil {
		d.emitDeployFailure(name, 0, err, changeSet{})
		return true, err
	}
	state.release(name)
	if len(removed) == 0 {
		return false, nil
	}
	services := make([]string, len(removed))
	for i, c := range removed {
		services[i] = c.Service
	}
	slog.Info("removed orphaned services after a held change was reverted", "stack", name, "services", services)
	d.emit(events.StatusSuccess, name, 0, "", changeSet{imageChanges: removed})
	return true, nil
}
