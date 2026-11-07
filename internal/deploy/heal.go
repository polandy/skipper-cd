package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// ErrHealDeferred marks a self-heal that HealStack declined to run because the
// version it would apply cannot be shown to be the one last deployed: a change
// is pending (paused, held, or not yet deployed) in an input other than the
// compose file, or nothing is recorded for the stack at all. It comes back with
// ran=false, so the attempt budget is untouched (ADR-0029).
var ErrHealDeferred = errors.New("self-heal deferred: the deployed version cannot be reproduced")

// HealStack runs a corrective `docker compose up -d` for a single stack to
// restore its last deployed version after runtime drift (a stopped/removed
// container, an unhealthy service). It is self-heal's action (ADR-0029) and is
// deliberately *not* a git deploy: no hash update, no hooks, no pull, no health
// gate and no rollback — it only restarts what should already be running.
//
// "Last deployed" is the recorded version, never the clone's HEAD: a change
// that is queued behind a pause or held after a failure (ADR-0062) must not
// reach the host through a heal. When the clone's tracked inputs match the
// recorded hashes the clone is used as is; when only the compose file differs
// it is restored from the stack's base commit the way a rollback does; any
// other difference returns ErrHealDeferred with ran=false.
//
// It serializes on the deploy mutex like every other deploy source (Invariant
// 7). If a deploy is already in progress it returns ran=false, nil without
// waiting: that deploy converges the stack anyway, and the next health poll
// re-evaluates (mirrors the reconcile loop's skip-if-busy, ADR-0010/ADR-0028).
// It emits no event: the healed event waits until the stack is seen healthy
// again (EmitHealed, driven by the self-heal engine). An up error is returned
// for the caller to count as a failed attempt without emitting a misleading
// failed-deploy event.
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
	run, commit, cleanup, err := d.deployedVersionRun(ctx, newStackRun(stack, cfg.StacksBaseDir, baseEnv), cfg)
	if errors.Is(err, ErrHealDeferred) {
		return false, err
	}
	if err != nil {
		return true, fmt.Errorf("self-heal %q: %w", stack.Name, err)
	}
	defer cleanup()

	slog.Info("self-heal: restoring stack to its deployed version", "stack", stack.Name, "drift", len(drift), "restored_from", commit)
	// A plain up — no --wait/health gate, no rollback (see the doc comment).
	if err := d.runDockerCompose(ctx, run, "up", "-d", "--remove-orphans"); err != nil {
		return true, fmt.Errorf("self-heal up %q: %w", stack.Name, err)
	}
	metrics.LastDeployTimestamp.WithLabelValues(stack.Name).Set(float64(time.Now().Unix()))
	// No healed event here: an up that exits 0 does not mean the stack came
	// back. The engine reports it once a poll sees it healthy (EmitHealed).
	return true, nil
}

// deployedVersionRun returns the run that applies the stack's last deployed
// version (see HealStack). commit is the base commit the compose file was
// restored from, empty when the clone is used as is; cleanup is never nil on
// success.
func (d *Deployer) deployedVersionRun(ctx context.Context, run stackRun, cfg *config.Config) (_ stackRun, commit string, cleanup func(), err error) {
	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		return stackRun{}, "", nil, fmt.Errorf("load deploy state: %w", err)
	}
	recorded := state.hashesFor(run.stack.Name)
	if len(recorded) == 0 {
		return stackRun{}, "", nil, fmt.Errorf("%w: no deployed version is recorded", ErrHealDeferred)
	}

	compose, err := parseComposeFile(run.composePath)
	if err != nil {
		slog.Debug("self-heal: compose file does not parse, Dockerfiles are not compared", "stack", run.stack.Name, "err", err)
	}
	current, _, err := d.hashStackInputs(ctx, run.stack, cfg.StacksBaseDir, filepath.Dir(run.composePath), cfg.VarsFile, compose)
	if err != nil {
		return stackRun{}, "", nil, fmt.Errorf("compute per-file hashes: %w", err)
	}

	differing := differingInputs(current, recorded)
	switch {
	case len(differing) == 0:
		return run, "", func() {}, nil
	case !slices.Equal(differing, []string{run.composePath}):
		return stackRun{}, "", nil, fmt.Errorf("%w: pending change in %v", ErrHealDeferred, d.repoRelativePaths(differing))
	}

	base := state.baseCommitFor(run.stack.Name)
	if d.commitReader == nil || base == "" {
		return stackRun{}, "", nil, fmt.Errorf("%w: the compose file has a pending change and no base commit is recorded", ErrHealDeferred)
	}
	restored, cleanup, err := d.runAtCommit(ctx, run, base)
	if err != nil {
		return stackRun{}, "", nil, err
	}
	return restored, base, cleanup, nil
}

// differingInputs lists, sorted, every tracked input whose hash differs between
// current and recorded, including one present on only one side — a removed env
// file is as much a change as an edited one.
func differingInputs(current, recorded stackFileHashes) []string {
	differing := changedFiles(current, recorded)
	for path := range recorded {
		if _, ok := current[path]; !ok {
			differing = append(differing, path)
		}
	}
	slices.Sort(differing)
	return differing
}
