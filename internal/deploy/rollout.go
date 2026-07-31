package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// errCanaryUnhealthy marks a rollout where the canary never turned healthy: the
// old container is untouched, so it is reported as rolled_back without a git-restore.
var errCanaryUnhealthy = errors.New("canary did not become healthy")

// defaultRolloutTimeoutSeconds is the canary health-wait deadline when no timeout
// is configured.
const defaultRolloutTimeoutSeconds = 60

// defaultRolloutPollInterval is the pause between `docker compose ps` polls while
// waiting for a canary to turn healthy (Config.RolloutPollInterval overrides it).
const defaultRolloutPollInterval = 2 * time.Second

// rollout performs the zero-downtime cutover for the stack's rollout services and
// recreates the rest in place, replacing the plain `up` (ADR-0040). It owns its
// rollback. compose must have parsed.
func (d *Deployer) rollout(ctx context.Context, run stackRun, cf *composeFile, state *persistedState) error {
	rc := run.stack.Rollout
	if d.outputter == nil {
		return fmt.Errorf("rollout: no command outputter configured (cannot read container state)")
	}
	if cf == nil {
		return fmt.Errorf("rollout: compose file could not be parsed")
	}
	if err := config.ValidateRolloutServices(rc.Services, &cf.File); err != nil {
		return fmt.Errorf("rollout: %w", err) // nothing touched yet → plain failed, no rollback
	}

	rolled := make(map[string]bool, len(rc.Services))
	for _, s := range rc.Services {
		rolled[s] = true
	}

	// Non-rolled services first, so a rolled service's dependencies are up before
	// its canary starts.
	if nonRolled := cf.servicesExcept(rolled); len(nonRolled) > 0 {
		upArgs := []string{"up", "-d", "--remove-orphans"}
		if hc := run.stack.DeployHealthCheck; hc != nil {
			upArgs = append(upArgs, "--wait", "--wait-timeout", strconv.Itoa(hc.TimeoutSeconds))
		}
		upArgs = append(upArgs, nonRolled...)
		if err := d.runDockerCompose(ctx, run, upArgs...); err != nil {
			return d.rollBackFailedDeploy(ctx, run, state, "docker compose up", err)
		}
	}

	timeout := d.rolloutTimeout(run.stack)
	for _, service := range rc.Services {
		if err := d.rollService(ctx, run, service, timeout); err != nil {
			if errors.Is(err, errCanaryUnhealthy) {
				// Old version never stopped serving → rolled_back, no git-restore.
				metrics.DeployRollbacks.WithLabelValues(run.stack.Name).Inc()
				return fmt.Errorf("rollout %q: %w (%w)", service, err, ErrRolledBack)
			}
			// Unknown state mid-cutover → git-restore rollback for a defined one.
			return d.rollBackFailedDeploy(ctx, run, state, "rollout "+service, err)
		}
	}
	return nil
}

// rollService cuts one service to its new version with no serving gap: a canary
// alongside the old, wait healthy, drain the old. First deploy (nothing running)
// is a plain up; an unhealthy canary is removed and wraps errCanaryUnhealthy.
func (d *Deployer) rollService(ctx context.Context, run stackRun, service string, timeout time.Duration) error {
	old, err := d.serviceContainers(ctx, run, service)
	if err != nil {
		return fmt.Errorf("list containers for %q: %w", service, err)
	}
	oldIDs := containerIDSet(old)

	if len(oldIDs) == 0 {
		// Nothing is serving yet — a plain up has no gap to avoid.
		slog.Info("rollout: first deploy of service, plain up", "stack", run.stack.Name, "service", service)
		return d.runDockerCompose(ctx, run, "up", "-d", "--no-deps", service)
	}

	slog.Info("rollout: starting canary alongside running version", "stack", run.stack.Name, "service", service, "replicas", len(oldIDs))
	scale := fmt.Sprintf("%s=%d", service, len(oldIDs)+1)
	if err := d.runDockerCompose(ctx, run, "up", "-d", "--no-deps", "--no-recreate", "--scale", scale, service); err != nil {
		d.cleanupCanary(ctx, run, service, oldIDs)
		return fmt.Errorf("start canary for %q: %w", service, err)
	}

	newIDs, err := d.waitCanaryHealthy(ctx, run, service, oldIDs, timeout)
	if err != nil {
		slog.Warn("rollout: canary not healthy, removing it and keeping the old version", "stack", run.stack.Name, "service", service, "err", err)
		d.removeContainers(ctx, run, newIDs)
		return fmt.Errorf("%q: %w: %w", service, err, errCanaryUnhealthy)
	}

	// Let the proxy start routing to the new container before removing the old —
	// a healthy canary is not yet a proxy that serves it. Interruptible; on
	// shutdown we proceed to drain.
	if delay := d.drainDelay(run.stack); delay > 0 {
		slog.Info("rollout: canary healthy, waiting before draining old version", "stack", run.stack.Name, "service", service, "drain", delay)
		select {
		case <-ctx.Done():
		case <-time.After(delay):
		}
	}

	slog.Info("rollout: draining old version", "stack", run.stack.Name, "service", service)
	d.removeContainers(ctx, run, keysOf(oldIDs))
	return nil
}

// waitCanaryHealthy polls the service's containers until a container that is not
// in oldIDs reports Health "healthy", or timeout elapses. It returns the new
// (canary) container IDs it observed so the caller can remove them on failure.
func (d *Deployer) waitCanaryHealthy(ctx context.Context, run stackRun, service string, oldIDs map[string]bool, timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	interval := d.rolloutPollInterval
	if interval <= 0 {
		interval = defaultRolloutPollInterval
	}

	// lastErr explains *why* the wait ran out and rides into the returned error
	// via %w; it is diagnosis, not classification. Deliberately not sentinels:
	// all three causes — nothing ever started, a canary that stayed unhealthy,
	// or docker being unreachable — lead the caller to the identical decision
	// (leave the old container running, report rolled_back without a git
	// restore), and it branches on errCanaryUnhealthy for that. Sentinels
	// nothing tests against would be API surface without a reader.
	var newIDs []string
	lastErr := errors.New("no canary container appeared")
	for {
		current, err := d.serviceContainers(ctx, run, service)
		if err != nil {
			lastErr = err
		} else {
			newIDs = newContainerIDs(current, oldIDs)
			if anyHealthy(current, oldIDs) {
				return newIDs, nil
			}
			if len(newIDs) > 0 {
				lastErr = errors.New("canary not yet healthy")
			}
		}
		select {
		case <-ctx.Done():
			return newIDs, fmt.Errorf("canary for %q did not become healthy within %s: %w", service, timeout, lastErr)
		case <-time.After(interval):
		}
	}
}

// cleanupCanary removes any container of the service that is not in oldIDs,
// re-reading the current set (best-effort). Used when starting the canary
// errored and its state is unknown.
func (d *Deployer) cleanupCanary(ctx context.Context, run stackRun, service string, oldIDs map[string]bool) {
	current, err := d.serviceContainers(ctx, run, service)
	if err != nil {
		slog.Warn("rollout: could not list containers for canary cleanup", "stack", run.stack.Name, "service", service, "err", err)
		return
	}
	d.removeContainers(ctx, run, newContainerIDs(current, oldIDs))
}

// serviceContainers reads the running containers of one compose service via
// `docker compose ps --format json <service>`, using the same project identity
// the deploy path uses.
func (d *Deployer) serviceContainers(ctx context.Context, run stackRun, service string) ([]containerLine, error) {
	dir, args := run.composeInvocation()
	args = append(args, "ps", "--format", "json", service)
	out, err := d.outputter.Output(ctx, dir, "docker", args...)
	if err != nil {
		return nil, err
	}
	lines, err := parseComposeJSON[containerLine](out)
	if err != nil {
		return nil, fmt.Errorf("parse compose ps output: %w", err)
	}
	// Defensive: compose already filters by the service arg.
	out2 := make([]containerLine, 0, len(lines))
	for _, l := range lines {
		if l.Service == "" || l.Service == service {
			out2 = append(out2, l)
		}
	}
	return out2, nil
}

// removeContainers stops and removes the given containers, best-effort: a
// failure is logged, not fatal to the cutover.
func (d *Deployer) removeContainers(ctx context.Context, run stackRun, ids []string) {
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := d.runner.Run(ctx, "", nil, "docker", "stop", id); err != nil {
			slog.Warn("rollout: could not stop container", "stack", run.stack.Name, "id", id, "err", err)
		}
		if err := d.runner.Run(ctx, "", nil, "docker", "rm", id); err != nil {
			slog.Warn("rollout: could not remove container", "stack", run.stack.Name, "id", id, "err", err)
		}
	}
}

// drainDelay is the wait after the canary is healthy before draining the old
// container (rollout.drain_seconds; Config.RolloutDrainOverride wins).
func (d *Deployer) drainDelay(stack config.Stack) time.Duration {
	if d.rolloutDrainOverride > 0 {
		return d.rolloutDrainOverride
	}
	return time.Duration(stack.Rollout.DrainSeconds) * time.Second
}

// rolloutTimeout is the canary health-wait deadline: rollout.health_timeout_seconds,
// else the stack's deploy_health_check timeout, else the default
// (Config.RolloutTimeoutOverride wins).
func (d *Deployer) rolloutTimeout(stack config.Stack) time.Duration {
	if d.rolloutTimeoutOverride > 0 {
		return d.rolloutTimeoutOverride
	}
	secs := stack.Rollout.HealthTimeoutSeconds
	if secs <= 0 {
		if hc := stack.DeployHealthCheck; hc != nil && hc.TimeoutSeconds > 0 {
			secs = hc.TimeoutSeconds
		} else {
			secs = defaultRolloutTimeoutSeconds
		}
	}
	return time.Duration(secs) * time.Second
}

// containerIDSet collects the non-empty container IDs of the given lines.
func containerIDSet(cs []containerLine) map[string]bool {
	set := make(map[string]bool, len(cs))
	for _, c := range cs {
		if c.ID != "" {
			set[c.ID] = true
		}
	}
	return set
}

// newContainerIDs returns the IDs of containers not present in oldIDs — the
// canaries created by the scale-up.
func newContainerIDs(cs []containerLine, oldIDs map[string]bool) []string {
	var ids []string
	for _, c := range cs {
		if c.ID != "" && !oldIDs[c.ID] {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// anyHealthy reports whether any container not in oldIDs is Health "healthy".
func anyHealthy(cs []containerLine, oldIDs map[string]bool) bool {
	for _, c := range cs {
		if !oldIDs[c.ID] && strings.EqualFold(c.Health, "healthy") {
			return true
		}
	}
	return false
}

// keysOf returns the keys of a set in no particular order.
func keysOf(set map[string]bool) []string {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	return ids
}
