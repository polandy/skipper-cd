package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// errStillStarting marks a readiness wait that ran out while a container was
// still starting: an undecided outcome, which never rolls back (ADR-0072).
var errStillStarting = errors.New("still starting")

// Compose `ps` states and health values the readiness wait decides on.
const (
	containerStateRunning = "running"
	containerStateExited  = "exited"
	containerStateDead    = "dead"
	containerRestarting   = "restarting"
	containerHealthy      = "healthy"
	containerUnhealthy    = "unhealthy"
)

// awaitReady waits until the gated stack's containers (only the given
// services, when any are named) are ready, polling `docker compose ps` on the
// injected clock. It returns nil when ready or the stack is ungated, an error
// wrapping errStillStarting when the gate timeout ends undecided, and any other
// error for a decided failure: a container unhealthy or exited non-zero.
func (d *Deployer) awaitReady(ctx context.Context, run stackRun, services ...string) error {
	hc := run.stack.DeployHealthCheck
	if hc == nil {
		return nil
	}
	if d.outputter == nil {
		return fmt.Errorf("no command outputter configured (cannot read container state)")
	}
	timeout := time.Duration(hc.TimeoutSeconds) * time.Second
	deadline := d.now().Add(timeout)
	interval := d.rolloutPollInterval
	if interval <= 0 {
		interval = defaultRolloutPollInterval
	}
	args := append([]string{"ps", "--all", "--format", "json", "--"}, services...)

	var pending string
	for {
		lines, err := composeJSONRead[containerLine](ctx, d, run, args...)
		if err != nil {
			pending = err.Error()
		} else {
			waiting, failure := readinessOf(lines)
			if failure != nil {
				return failure
			}
			if waiting == "" {
				return nil
			}
			pending = waiting
		}
		if !d.now().Before(deadline) {
			return fmt.Errorf("%w after %s (%s)", errStillStarting, timeout, pending)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// readinessOf decides one `compose ps` snapshot: a decided failure, or the
// first container still waiting ("" when every container is ready). A
// container that exited 0 is a finished one-shot, not a failure; one that
// Docker restarts after a non-zero exit is crash-looping.
func readinessOf(lines []containerLine) (waiting string, failure error) {
	for _, c := range lines {
		name := c.Name
		if name == "" {
			name = c.Service
		}
		state := strings.ToLower(c.State)
		switch {
		case strings.EqualFold(c.Health, containerUnhealthy):
			return "", fmt.Errorf("container %s is unhealthy", name)
		case state == containerStateExited || state == containerStateDead:
			if c.ExitCode != 0 {
				return "", fmt.Errorf("container %s exited with code %d", name, c.ExitCode)
			}
		case state == containerRestarting && c.ExitCode != 0:
			return "", fmt.Errorf("container %s is restarting after exit code %d", name, c.ExitCode)
		case state != containerStateRunning || (c.Health != "" && !strings.EqualFold(c.Health, containerHealthy)):
			if waiting == "" {
				waiting = fmt.Sprintf("container %s is %s", name, strings.ToLower(firstNonEmpty(c.Health, c.State)))
			}
		}
	}
	return waiting, nil
}

// stillStartingError turns an undecided readiness wait into the deploy's
// failure: held, never rolled back, with the way to release it once healthy.
func stillStartingError(run stackRun, err error) error {
	slog.Error("deploy health check ran out while the new version is still starting; not rolling back", "stack", run.stack.Name, "err", err)
	return fmt.Errorf("health check: %w; not rolled back — retry once it is healthy: POST /api/stacks/%s/retry%w", err, run.stack.Name, errNewVersionMark)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
