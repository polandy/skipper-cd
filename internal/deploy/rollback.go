package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// ErrRolledBack marks a failed deploy whose stack was successfully rolled
// back to the previous compose file version. DeployAllStacks checks it with
// errors.Is to emit a rolled_back (instead of failed) event.
var ErrRolledBack = errors.New("rolled back to previous version")

// ErrRollbackUnhealthy marks a failed deploy whose rollback ran, but whose
// restored version also failed the health gate: the stack is back on the old
// compose file yet not verified healthy. Only possible with a deploy_health_check
// configured (the rollback then reruns the same gate). DeployAllStacks checks
// it with errors.Is to emit a rolled_back_unhealthy event.
var ErrRollbackUnhealthy = errors.New("rolled back but still unhealthy")

// ErrNewVersionFailed marks a deploy whose new version was applied and then
// failed — its start, a health gate, a rollout cutover or a post_deploy hook —
// as opposed to a failure before any container was touched. It rides alongside
// the rollback outcome, so it holds with rollback disabled too. Such a change
// is held rather than retried every tick (ADR-0062): the same inputs fail the
// same way.
var ErrNewVersionFailed = errors.New("new version failed after it started")

// errNewVersionMark tags an error as ErrNewVersionFailed without adding to its
// text: the stage and rollback outcome already say what happened, and that
// text is what the event, the UI and the notification show.
var errNewVersionMark error = silentMark{ErrNewVersionFailed}

// errRolledForward marks a deploy whose rollback came up unhealthy and whose
// one roll-forward to the new version then passed every gate (ADR-0072): the
// new version runs verified, so the caller records it as a success.
var errRolledForward = errors.New("rolled forward to the new version")

// silentMark is an error with no text of its own that unwraps to target.
type silentMark struct{ target error }

func (m silentMark) Error() string { return "" }
func (m silentMark) Unwrap() error { return m.target }

// rollBackFailedDeploy handles a deploy that failed at the given stage ("docker
// compose up" or "health check"): unless the stack's rollback policy skips the
// restore (a held failed), it attempts a rollback and wraps the outcome so
// DeployAllStacks emits rolled_back on success, rolled_back_unhealthy when the
// restored version also fails the health gate, and failed otherwise (errors.Is
// on ErrRolledBack / ErrRollbackUnhealthy).
func (d *Deployer) rollBackFailedDeploy(ctx context.Context, run stackRun, compose *composeFile, state *persistedState, stage string, cause error) error {
	// A skipped restore leaves the failed version running and held (ADR-0050).
	switch rollbackPolicyOf(run.stack) {
	case config.RollbackNever:
		slog.Error(stage+" failed; automatic rollback is disabled for this stack, leaving the failed version running for inspection", "stack", run.stack.Name, "err", cause)
		return fmt.Errorf("%s: %w%w", stage, cause, errNewVersionMark)
	case config.RollbackPatchOnly:
		// Restoring an older image over data a bigger update migrated (ADR-0074).
		if reason := updateBeyondPatch(compose, state.imagesFor(run.stack.Name)); reason != "" {
			slog.Error(stage+" failed; rollback skipped (rollback: patch_only), leaving the failed version running for inspection", "stack", run.stack.Name, "reason", reason, "err", cause)
			return fmt.Errorf("%s: %w (rollback skipped: %s)%w", stage, cause, reason, errNewVersionMark)
		}
	}

	slog.Error(stage+" failed, attempting rollback", "stack", run.stack.Name, "err", cause)

	if rbErr := d.rollbackStack(ctx, run, state); rbErr != nil {
		if errors.Is(rbErr, ErrRollbackUnhealthy) {
			slog.Error("rollback ran but the restored version is still unhealthy", "stack", run.stack.Name, "err", rbErr)
			if errors.Is(rbErr, errStillStarting) {
				return fmt.Errorf("%s: %w (%w)%w", stage, cause, rbErr, errNewVersionMark)
			}
			fwdErr := d.rollForward(ctx, run)
			if fwdErr == nil {
				slog.Warn("rolled forward: the new version passed its gates on the second try", "stack", run.stack.Name, "first_failure", cause)
				return fmt.Errorf("%s: %w (%w)", stage, cause, errRolledForward)
			}
			slog.Error("roll-forward to the new version failed too", "stack", run.stack.Name, "err", fwdErr)
			return fmt.Errorf("%s: %w (%w; roll-forward also failed: %w)%w", stage, cause, rbErr, fwdErr, errNewVersionMark)
		}
		slog.Error("rollback failed", "stack", run.stack.Name, "err", rbErr)
		return fmt.Errorf("%s: %w (rollback also failed: %w)%w", stage, cause, rbErr, errNewVersionMark)
	}
	slog.Info("rollback successful, old containers restored", "stack", run.stack.Name)
	metrics.DeployRollbacks.WithLabelValues(run.stack.Name).Inc()
	return fmt.Errorf("%s: %w (%w)%w", stage, cause, ErrRolledBack, errNewVersionMark)
}

// rollbackStack restores containers to the previous compose file version after
// a failed docker compose up. It retrieves the old compose file from the
// stack's own base commit (ADR-0061) and
// runs docker compose up with it. With a deploy_health_check configured the rollback
// reruns the same gate (the readiness wait plus the optional HTTP probe) so a
// restored version that stays unhealthy is reported, not assumed good; those
// failures wrap ErrRollbackUnhealthy.
func (d *Deployer) rollbackStack(ctx context.Context, run stackRun, state *persistedState) error {
	base := state.baseCommitFor(run.stack.Name)
	if d.commitReader == nil || base == "" {
		return fmt.Errorf("no previous commit available for rollback")
	}

	rbRun, cleanup, err := d.runAtCommit(ctx, run, base)
	if err != nil {
		return err
	}
	defer cleanup()

	slog.Info("rolling back with previous compose file", "stack", run.stack.Name, "commit", base)
	if err := d.runDockerCompose(ctx, rbRun, "up", "-d"); err != nil {
		if run.stack.DeployHealthCheck != nil {
			return fmt.Errorf("restored version did not come up healthy: %w (%w)", err, ErrRollbackUnhealthy)
		}
		return err
	}
	if err := d.awaitReady(ctx, rbRun); err != nil {
		return fmt.Errorf("restored version did not come up healthy: %w (%w)", err, ErrRollbackUnhealthy)
	}
	if err := d.probeHealthURL(ctx, run.stack.DeployHealthCheck); err != nil {
		return fmt.Errorf("restored version failed the health probe: %w (%w)", err, ErrRollbackUnhealthy)
	}
	return nil
}

// rollForward reapplies the new version once after its rollback came up
// unhealthy too (ADR-0072): neither version is verified, and the new one may
// only have needed longer. A plain up — a rollout's zero-downtime cutover has
// nothing healthy left to protect — then the same gates as the first try.
func (d *Deployer) rollForward(ctx context.Context, run stackRun) error {
	slog.Warn("rolling forward to the new version once", "stack", run.stack.Name)
	if err := d.runDockerCompose(ctx, run, "up", "-d", "--remove-orphans"); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}
	if stage, err := d.verifyNewVersion(ctx, run); err != nil {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return nil
}

// runAtCommit returns a copy of run that applies the stack's compose file as of
// commit, materialised into a temp file; cleanup removes it. --project-directory
// stays pinned to the original compose file's directory (Invariant 3) and the
// build-context override is dropped: it was generated from the *current*
// compose file, so against the restored one it can name services that version
// does not have — and pinning such a build to the clone would build the very
// version being stepped away from (ADR-0057).
func (d *Deployer) runAtCommit(ctx context.Context, run stackRun, commit string) (stackRun, func(), error) {
	content, err := d.commitReader.FileAtCommit(ctx, commit, run.composePath)
	if err != nil {
		return stackRun{}, nil, fmt.Errorf("retrieve old compose file: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "skipper-restored-*.yml")
	if err != nil {
		return stackRun{}, nil, fmt.Errorf("create temp file: %w", err)
	}
	cleanup := func() {
		if err := os.Remove(tmpFile.Name()); err != nil {
			slog.Warn("could not remove restored compose file", "path", tmpFile.Name(), "err", err)
		}
	}
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		cleanup()
		return stackRun{}, nil, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return stackRun{}, nil, fmt.Errorf("write temp file: %w", err)
	}

	restored := run
	restored.composePath = tmpFile.Name()
	if restored.projectDir == "" {
		restored.projectDir = filepath.Dir(run.composePath)
	}
	restored.extraComposeFiles = nil
	return restored, cleanup, nil
}
