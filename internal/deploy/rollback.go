package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/polandy/skipper-cd/internal/metrics"
)

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

// rollBackFailedDeploy handles a deploy that failed at the given stage ("docker
// compose up" or "health check"): it attempts a rollback and wraps the outcome
// so DeployAllStacks emits rolled_back on success, rolled_back_unhealthy when
// the restored version also fails the health gate, and failed otherwise
// (errors.Is on ErrRolledBack / ErrRollbackUnhealthy).
func (d *Deployer) rollBackFailedDeploy(ctx context.Context, run stackRun, state *persistedState, stage string, cause error) error {
	slog.Error(stage+" failed, attempting rollback", "stack", run.stack.Name, "err", cause)

	if rbErr := d.rollbackStack(ctx, run, state); rbErr != nil {
		if errors.Is(rbErr, ErrRollbackUnhealthy) {
			slog.Error("rollback ran but the restored version is still unhealthy", "stack", run.stack.Name, "err", rbErr)
			return fmt.Errorf("%s: %w (%w)", stage, cause, rbErr)
		}
		slog.Error("rollback failed", "stack", run.stack.Name, "err", rbErr)
		return fmt.Errorf("%s: %w (rollback also failed: %w)", stage, cause, rbErr)
	}
	slog.Info("rollback successful, old containers restored", "stack", run.stack.Name)
	metrics.DeployRollbacks.WithLabelValues(run.stack.Name).Inc()
	return fmt.Errorf("%s: %w (%w)", stage, cause, ErrRolledBack)
}

// rollbackStack restores containers to the previous compose file version after
// a failed docker compose up. It retrieves the old compose file from git and
// runs docker compose up with it. With a health_check configured the rollback
// reruns the same gate (--wait plus the optional HTTP probe) so a restored
// version that stays unhealthy is reported, not assumed good; those failures
// wrap ErrRollbackUnhealthy.
func (d *Deployer) rollbackStack(ctx context.Context, run stackRun, state *persistedState) error {
	if d.commitReader == nil || state.LastDeployedCommit == "" {
		return fmt.Errorf("no previous commit available for rollback")
	}

	oldContent, err := d.commitReader.FileAtCommit(ctx, state.LastDeployedCommit, run.composePath)
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

	// The rollback runs the old compose file from a temp file in /tmp/. With no
	// projectDir configured, runDockerCompose would use the temp file's
	// directory as the working directory, so pin it to the original compose
	// file's directory instead (Invariant 3).
	rbRun := run
	rbRun.composePath = tmpFile.Name()
	if rbRun.projectDir == "" {
		rbRun.projectDir = filepath.Dir(run.composePath)
	}

	slog.Info("rolling back with previous compose file", "stack", run.stack.Name, "commit", state.LastDeployedCommit)
	upArgs := []string{"up", "-d"}
	if hc := run.stack.HealthCheck; hc != nil {
		upArgs = append(upArgs, "--wait", "--wait-timeout", strconv.Itoa(hc.TimeoutSeconds))
	}
	if err := d.runDockerCompose(ctx, rbRun, upArgs...); err != nil {
		if run.stack.HealthCheck != nil {
			return fmt.Errorf("restored version did not come up healthy: %w (%w)", err, ErrRollbackUnhealthy)
		}
		return err
	}
	if hc := run.stack.HealthCheck; hc != nil && hc.URL != "" {
		timeout := time.Duration(hc.TimeoutSeconds) * time.Second
		if err := d.healthProber().waitHealthy(ctx, hc.URL, timeout); err != nil {
			return fmt.Errorf("restored version failed the health probe: %w (%w)", err, ErrRollbackUnhealthy)
		}
	}
	return nil
}
