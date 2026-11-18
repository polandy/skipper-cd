package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/polandy/skipper-cd/internal/events"
)

// errReleaseConverge wraps a failed converge of a released hold (ADR-0068).
var errReleaseConverge = errors.New("converge to the recorded version")

// convergeToRecorded runs one plain up of the recorded version — the clone is
// back at the recorded inputs — and returns what it changed, measured from the
// running images before and after, plus what now runs (nil when unreadable).
// No hooks, pull, health gate or rollback: it restores a version that already
// deployed (ADR-0068). Built images are rebuilt, without --pull, because the
// held version's build carries the same tag.
func (d *Deployer) convergeToRecorded(ctx context.Context, prep stackPrep) ([]events.ServiceImageChange, serviceImageByName, error) {
	run, cleanup, err := prep.run.withClonedBuildContexts(prep.compose)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errReleaseConverge, err)
	}
	defer cleanup()

	before := d.runningImages(ctx, prep.run)
	upArgs := []string{"up", "-d", "--remove-orphans"}
	if len(prep.dockerfilePaths) > 0 {
		upArgs = append(upArgs, "--build")
	}
	if err := d.runDockerCompose(ctx, run, upArgs...); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errReleaseConverge, err)
	}
	d.stopOnDemandContainers(ctx, run.stack, prep.compose)
	after := d.runningImages(ctx, prep.run)
	changes, _ := runningImageDelta(after, before, prep.compose)
	return changes, after, nil
}

// settleReleasedHold finishes an unchanged run of a stack that is still held:
// its inputs are back at the recorded ones (a revert), but its containers may
// still run the held version — left by `rollback: false`, or applied by hand.
// It converges them before releasing the hold; a failed converge keeps the
// hold, so the next tick retries. It reports whether it emitted the stack's
// terminal event.
func (d *Deployer) settleReleasedHold(ctx context.Context, prep stackPrep, state *persistedState) (emitted bool, err error) {
	name := prep.run.stack.Name
	changes, running, err := d.convergeToRecorded(ctx, prep)
	if err != nil {
		d.emitDeployFailure(name, 0, err, changeSet{})
		return true, err
	}
	state.release(name)
	state.recordRunningImages(name, running)
	if len(changes) == 0 {
		return false, nil
	}
	services := make([]string, len(changes))
	for i, c := range changes {
		services[i] = c.Service
	}
	slog.Info("converged a reverted held change to the recorded version", "stack", name, "services", services)
	d.emit(events.StatusSuccess, name, 0, "", changeSet{imageChanges: changes})
	return true, nil
}
