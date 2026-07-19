package deploy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// errCanaryUnhealthy marks a rollout that failed because a canary container did
// not become healthy in time. It is distinct from a docker/compose command
// error: on this failure the old container was never touched (it is still
// serving), so the rollout cleans up the canary and reports rolled_back without
// a git-restore. rollout() checks it with errors.Is.
var errCanaryUnhealthy = errors.New("canary did not become healthy")

// defaultRolloutTimeoutSeconds bounds the canary health wait when neither
// rollout.health_timeout_seconds nor the stack's health_check timeout is set.
const defaultRolloutTimeoutSeconds = 60

// defaultRolloutPollInterval is the pause between two `docker compose ps` polls
// while waiting for a canary to turn healthy. Tests override d.rolloutPollInterval.
const defaultRolloutPollInterval = 2 * time.Second

// rollout deploys a stack's services with a zero-downtime cutover for the
// services named in its rollout section (ADR-0040), while every other service
// recreates in place. It replaces the plain `up` step in deployStackIfChanged
// and handles its own rollback: a canary that never turns healthy is cleaned up
// (old version keeps serving) and reported as rolled_back; a docker/compose
// error mid-cutover falls through to the git-restore rollback for a defined
// state. The compose file must have parsed (rollout needs to inspect services).
func (d *Deployer) rollout(ctx context.Context, run stackRun, compose *composeFile, state *persistedState) error {
	rc := run.stack.Rollout
	if d.outputter == nil {
		return fmt.Errorf("rollout: no command outputter configured (cannot read container state)")
	}
	if compose == nil {
		return fmt.Errorf("rollout: compose file could not be parsed")
	}
	if err := validateRolloutServices(compose, rc.Services); err != nil {
		// Nothing has been touched yet: a plain error emits `failed`, no rollback.
		return err
	}

	rolled := make(map[string]bool, len(rc.Services))
	for _, s := range rc.Services {
		rolled[s] = true
	}

	// Bring up the non-rolled services in place first (as the plain path does),
	// so a rolled service's dependencies are running before its canary starts.
	if nonRolled := compose.servicesExcept(rolled); len(nonRolled) > 0 {
		upArgs := []string{"up", "-d", "--remove-orphans"}
		if hc := run.stack.HealthCheck; hc != nil {
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
				// The canary was cleaned up and the old version never stopped
				// serving: same outcome as a rollback (stack on the old version),
				// with zero downtime. Report it as such — no git restore needed.
				metrics.DeployRollbacks.WithLabelValues(run.stack.Name).Inc()
				return fmt.Errorf("rollout %q: %w (%w)", service, err, ErrRolledBack)
			}
			// A docker/compose error mid-cutover may leave the service in an
			// unknown state; fall back to the git-restore rollback for a defined one.
			return d.rollBackFailedDeploy(ctx, run, state, "rollout "+service, err)
		}
	}
	return nil
}

// rollService cuts one service over to its new version with no serving gap:
// start a canary container alongside the running one, wait for it to become
// healthy, then drain the old one. If no container is running yet (first
// deploy), a plain up suffices. A canary that never turns healthy is removed and
// the error wraps errCanaryUnhealthy so the old version keeps serving.
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

	// Give the reverse proxy time to discover the new (healthy) container and
	// route to it before the old one is removed — "canary healthy" (docker
	// healthcheck) is not yet "proxy is serving the new container". Without this
	// window the old container can be removed while the proxy still points at it,
	// causing a brief blip (docker-rollout's --wait-after-healthy). The wait is
	// interruptible; on shutdown we proceed to drain (the new version is healthy).
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
	lines, err := parseContainerLines(out)
	if err != nil {
		return nil, fmt.Errorf("parse compose ps output: %w", err)
	}
	// Defensive: compose already filters by the service arg, but keep only this
	// service's containers in case a version emits more.
	out2 := make([]containerLine, 0, len(lines))
	for _, l := range lines {
		if l.Service == "" || l.Service == service {
			out2 = append(out2, l)
		}
	}
	return out2, nil
}

// removeContainers stops and removes the given container IDs, best-effort:
// failures are logged, not returned, since a stray container is a cleanup
// nuisance, not a reason to fail an otherwise-complete cutover.
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

// validateRolloutServices checks each rolled service against the parsed compose
// file: it must exist, publish no host ports (two replicas would collide), and
// define a healthcheck (the readiness signal). These run before any container is
// touched, so a violation fails the deploy cleanly (ADR-0040).
func validateRolloutServices(compose *composeFile, services []string) error {
	for _, name := range services {
		svc, ok := compose.Services[name]
		if !ok {
			return fmt.Errorf("rollout: unknown service %q", name)
		}
		if svc.publishesPorts() {
			return fmt.Errorf("rollout: service %q publishes host ports; cannot run two replicas — route it via the proxy instead", name)
		}
		if svc.hasContainerName() {
			return fmt.Errorf("rollout: service %q sets container_name; compose cannot scale a named container — remove container_name to roll it", name)
		}
		if !svc.hasHealthcheck() {
			return fmt.Errorf("rollout: service %q has no healthcheck; rollout needs a readiness signal", name)
		}
	}
	return nil
}

// drainDelay is how long to wait after the canary is healthy before draining
// the old container. A non-zero d.rolloutDrainOverride wins (tests use a small
// value so the wait does not run for real seconds).
func (d *Deployer) drainDelay(stack config.Stack) time.Duration {
	if d.rolloutDrainOverride > 0 {
		return d.rolloutDrainOverride
	}
	return time.Duration(stack.Rollout.DrainSeconds) * time.Second
}

// rolloutTimeout is how long to wait for a canary to turn healthy:
// rollout.health_timeout_seconds if set, else the stack's health_check timeout,
// else the default. A non-zero d.rolloutTimeoutOverride wins (tests set a short
// value so the canary wait does not run for real seconds).
func (d *Deployer) rolloutTimeout(stack config.Stack) time.Duration {
	if d.rolloutTimeoutOverride > 0 {
		return d.rolloutTimeoutOverride
	}
	secs := stack.Rollout.HealthTimeoutSeconds
	if secs <= 0 {
		if hc := stack.HealthCheck; hc != nil && hc.TimeoutSeconds > 0 {
			secs = hc.TimeoutSeconds
		} else {
			secs = defaultRolloutTimeoutSeconds
		}
	}
	return time.Duration(secs) * time.Second
}

// containerLine is the subset of `docker compose ps --format json` fields
// rollout needs: the ID/Name to stop, the Service to filter, and Health to gate
// the cutover.
type containerLine struct {
	ID      string `json:"ID"`
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

// parseContainerLines parses `docker compose ps --format json` output. Compose
// emits either a JSON array or newline-delimited objects depending on its
// version, so both are accepted. Empty output yields no lines and no error.
func parseContainerLines(out []byte) ([]containerLine, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var lines []containerLine
		if err := json.Unmarshal(trimmed, &lines); err != nil {
			return nil, err
		}
		return lines, nil
	}
	var lines []containerLine
	sc := bufio.NewScanner(bytes.NewReader(trimmed))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var l containerLine
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, sc.Err()
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
