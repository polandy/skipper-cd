// Per-stack apply pipeline: resolving one stack's deploy inputs, deciding
// whether it changed, and applying it — hooks, pull, build, up or rollout, the
// health gates — then recording the result into the run's state.

package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// stackPrep bundles everything deployStackIfChanged resolves up front, before
// change detection: the run values, the single compose parse every downstream
// consumer shares, and the current hashes/images the persisted state is
// compared against.
type stackPrep struct {
	run             stackRun
	compose         *composeFile // nil when the compose file failed to parse
	dockerfilePaths []string
	currentImages   serviceImageByName
	currentHashes   stackFileHashes
}

// prepareStackRun resolves a stack's deploy inputs: the run values from the
// repo clone (Invariant 1), the compose parse, the effective deploy gate, and
// the hashes of every tracked input (Invariant 2).
func (d *Deployer) prepareStackRun(stack config.Stack, baseDir, varsFile string, baseEnv []string) (stackPrep, error) {
	// Change detection always uses the repo clone so that merged PRs are detected.
	run := newStackRun(stack, baseDir, baseEnv)
	repoDir := filepath.Dir(run.composePath)

	// Parse the compose file once; images, pullable services and Dockerfile
	// paths are all derived from this single parse. When parsing fails, the
	// deploy degrades gracefully: no build tracking and pull everything.
	compose, err := parseComposeFile(run.composePath)
	if err != nil {
		slog.Warn("could not parse compose file, pulling all services and skipping build tracking", "stack", stack.Name, "err", err)
	}

	// The effective deploy gate is resolved once here so every downstream read
	// of run.stack.DeployHealthCheck (rollback.go, rollout.go, applyStack) sees
	// it: an explicit config, or the automatic compose-healthcheck gate
	// (ADR-0046), suppressed for on-demand stacks and by deploy_health_check:
	// false (ADR-0049). See resolveHealthCheck.
	run.stack.DeployHealthCheck = resolveHealthCheck(stack, compose)

	var dockerfilePaths []string
	var currentImages serviceImageByName
	if compose != nil {
		dockerfilePaths = compose.dockerfilePaths(repoDir)
		currentImages = compose.images()
	}

	currentHashes, err := computePerFileHashes(repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		return stackPrep{}, fmt.Errorf("compute per-file hashes: %w", err)
	}
	d.addStackConfigHash(currentHashes, stack, baseDir)

	return stackPrep{
		run:             run,
		compose:         compose,
		dockerfilePaths: dockerfilePaths,
		currentImages:   currentImages,
		currentHashes:   currentHashes,
	}, nil
}

func (d *Deployer) deployStackIfChanged(ctx context.Context, stack config.Stack, baseDir, varsFile string, baseEnv []string, state *persistedState) (err error) {
	prep, err := d.prepareStackRun(stack, baseDir, varsFile, baseEnv)
	if err != nil {
		d.emitDeployFailure(stack.Name, 0, err, changeSet{})
		return err
	}

	changed := changedFiles(prep.currentHashes, state.hashesFor(stack.Name))
	if len(changed) == 0 {
		// Debug: once per stack per reconcile tick, this is most of the log.
		// The skip is still reported as a `skipped` event and in the run
		// summary's count (ADR-0042 amendment).
		slog.Debug("skipping stack, no changes detected", "stack", stack.Name)
		d.clearQueued(stack.Name) // nothing pending anymore
		metrics.DeploysSkipped.WithLabelValues(stack.Name).Inc()
		d.emit(events.StatusSkipped, stack.Name, 0, "", changeSet{})
		return nil
	}

	// Autosync gate: when paused, defer instead of deploying. The hashes are
	// deliberately not recorded, so the stack stays dirty and re-deploys once
	// sync resumes (docs/autosync.md).
	if d.isPaused(stack.Name) {
		reason := d.markQueued(stack.Name, changed)
		metrics.DeploysQueued.WithLabelValues(stack.Name).Inc()
		// Carry the diff of what is waiting so the paused row can show the
		// effective change, not just the file paths.
		d.emit(events.StatusQueued, stack.Name, 0, "", d.collectChange(ctx, changed, state.LastDeployedCommit))
		slog.Info("deploy deferred: autosync paused", "stack", stack.Name, "reason", reason, "changed_files", changed)
		return nil
	}
	// Not paused: this stack is being deployed now, so it is no longer an
	// autosync deferral — drop it from the pending queue regardless of outcome.
	d.clearQueued(stack.Name)

	deployStart := time.Now()
	d.emit(events.StatusDeploying, stack.Name, 0, "", changeSet{files: changed})
	// This stack is now the active deploy: surface the ones still to come.
	d.publishUpcomingAfter(stack.Name)
	slog.Info("deploying stack", "stack", stack.Name, "dir", filepath.Dir(prep.run.composePath), "project_dir", prep.run.projectDir, "changed_files", d.repoRelativePaths(changed))
	cs := d.collectChange(ctx, changed, state.LastDeployedCommit)
	// Name the services whose image reference changed (old → new) so terminal
	// events — and the notifications built from them — report what updated, not
	// just the stack. Captured before the deploy runs so the deferred failure
	// path carries the same list a success does. Skipped when the compose file
	// failed to parse (compose == nil): currentImages would be nil and every
	// prior service would be reported as removed — a misleading notification.
	if prep.compose != nil {
		cs.imageChanges = imageChanges(prep.currentImages, state.imagesFor(stack.Name))
	}
	// An effective deploy_health_check (explicit or inferred from a compose
	// healthcheck) means this deploy is gated on the stack turning healthy — so
	// a success event carrying this reports a verified deploy, not just a
	// completed one.
	cs.healthGated = prep.run.stack.DeployHealthCheck != nil
	// From here the stack is actually deploying: any error returned below emits
	// the matching terminal event with the change context gathered above. The
	// success path emits StatusSuccess and returns nil, so this never double-fires.
	defer func() {
		if err != nil {
			d.emitDeployFailure(stack.Name, time.Since(deployStart), err, cs)
		}
	}()
	metrics.DeploysTriggered.WithLabelValues(stack.Name).Inc()

	if err := d.applyStack(ctx, prep, state); err != nil {
		return err
	}

	// What the stack now actually runs. Read only on the success path: it is the
	// version the deploy produced, and a failed deploy has none to report.
	// When both this read and the previous deploy's baseline are available it
	// replaces the compose-reference delta above, so a moved floating tag shows
	// up as the version change it is.
	running := d.runningImages(ctx, prep.run)
	if delta, ok := runningImageDelta(running, state.runningImagesFor(stack.Name), prep.compose); ok {
		cs.imageChanges = delta
	}

	d.recordStackSuccess(prep, state, deployStart, cs, running)
	return nil
}

// applyStack runs the deploy itself: pre hooks, pull, build, up or rollout,
// the health gates, post hooks, and the on-demand stop. Every error it returns
// is final for this deploy — already through the rollback path where one
// applies (ErrRolledBack / ErrRollbackUnhealthy wrapped for the caller's
// deferred failure emitter).
func (d *Deployer) applyStack(ctx context.Context, prep stackPrep, state *persistedState) error {
	run, compose := prep.run, prep.compose
	stack := run.stack

	// Every compose call of this apply reads the tree skipper hashed, not the
	// project directory a relative build context would otherwise resolve against
	// (ADR-0057). It covers `up` and not just `build` because a service with
	// `pull_policy: build` builds again inside `up` — an unpinned second build
	// re-tags the stale image over the one the build just produced.
	run, cleanup, err := run.withClonedBuildContexts(compose)
	if err != nil {
		return err
	}
	defer cleanup()

	// pre_deploy hooks run before any container is touched — the point at which
	// the old version is still up, so a backup can dump it (ADR-0038). A failure
	// here aborts before pull/up with no rollback (nothing changed): the deferred
	// emitDeployFailure sees a plain error and emits `failed`.
	if err := d.runHooks(ctx, run, hookPhasePre, stack.Hooks.PreDeploy); err != nil {
		return err
	}

	if err := d.pullIfImagesChanged(ctx, run, compose, prep.currentImages, state.imagesFor(stack.Name)); err != nil {
		return fmt.Errorf("docker compose pull: %w", err)
	}

	if len(prep.dockerfilePaths) > 0 {
		slog.Info("building images from Dockerfile", "stack", stack.Name, "dockerfiles", prep.dockerfilePaths)
		buildArgs := []string{"build"}
		if !d.bootstrapRun {
			// --pull refreshes each Dockerfile's base image. On a bootstrap run
			// that is the same unasked-for jump a forced pull would be.
			buildArgs = append(buildArgs, "--pull")
		}
		if err := d.runDockerCompose(ctx, run, buildArgs...); err != nil {
			return fmt.Errorf("docker compose build: %w", err)
		}
	}

	// Apply the new version. A rollout section cuts over its listed services with
	// zero downtime (ADR-0040) — new container alongside old, wait healthy, drain
	// old — while every other service recreates in place; rollout() handles its
	// own rollback and wraps the outcome. Without it, the plain in-place recreate.
	if stack.Rollout != nil {
		if err := d.rollout(ctx, run, compose, state); err != nil {
			return err
		}
	} else {
		// --remove-orphans removes containers for services deleted from docker-compose.yml.
		upArgs := []string{"up", "-d", "--remove-orphans"}
		if hc := stack.DeployHealthCheck; hc != nil {
			// First health gate: --wait makes the up itself fail when the services'
			// compose healthchecks do not turn healthy in time (ADR-0022).
			upArgs = append(upArgs, "--wait", "--wait-timeout", strconv.Itoa(hc.TimeoutSeconds))
		}
		if err := d.runDockerCompose(ctx, run, upArgs...); err != nil {
			return d.rollBackFailedDeploy(ctx, run, state, "docker compose up", err)
		}
	}

	// Second health gate: the optional HTTP probe verifies the stack from the
	// outside after a successful up (ADR-0022).
	if hc := stack.DeployHealthCheck; hc != nil && hc.URL != "" {
		timeout := time.Duration(hc.TimeoutSeconds) * time.Second
		if err := d.prober.waitHealthy(ctx, hc.URL, timeout); err != nil {
			return d.rollBackFailedDeploy(ctx, run, state, "health check", err)
		}
	}

	// post_deploy hooks validate the new version from outside compose (a smoke
	// test, a migration). A failure takes the same rollback path as a health
	// probe failure, even without a deploy_health_check (ADR-0038): the hook is itself a
	// user-authored health gate.
	if err := d.runHooks(ctx, run, hookPhasePost, stack.Hooks.PostDeploy); err != nil {
		return d.rollBackFailedDeploy(ctx, run, state, "post_deploy hook", err)
	}

	if len(stack.OnDemandContainers) > 0 {
		if compose != nil {
			compose.warnUnmatchedOnDemandContainers(stack.Name, stack.OnDemandContainers)
		}
		slog.Info("stopping on-demand containers after deploy", "stack", stack.Name, "containers", stack.OnDemandContainers)
		if err := d.runner.Run(ctx, "", nil, "docker", append([]string{"stop"}, stack.OnDemandContainers...)...); err != nil {
			slog.Warn("could not stop on-demand containers", "stack", stack.Name, "err", err)
		}
	}
	return nil
}

// recordStackSuccess persists the deployed stack's hashes, images and project
// dir into the run's state (Invariant 2) and emits the success event. running
// is what the stack's containers now run, the baseline the next deploy's
// version delta is measured against; nil when that read was unavailable, which
// leaves the previous baseline in place rather than clearing it.
func (d *Deployer) recordStackSuccess(prep stackPrep, state *persistedState, deployStart time.Time, cs changeSet, running serviceImageByName) {
	name := prep.run.stack.Name
	state.recordStack(name, prep.currentHashes)
	if prep.currentImages != nil {
		state.recordImages(name, prep.currentImages)
	}
	state.recordRunningImages(name, running)
	state.recordProjectDir(name, prep.run.effectiveProjectDir())
	metrics.LastDeployTimestamp.WithLabelValues(name).Set(float64(time.Now().Unix()))
	eventID := d.emit(events.StatusSuccess, name, time.Since(deployStart), "", cs)
	if eventID != 0 {
		// The event ID lets the log view fetch this deploy's diffs.
		slog.Info("deploy complete", "stack", name, "event_id", eventID)
	} else {
		slog.Info("deploy complete", "stack", name)
	}
}

// pullIfImagesChanged runs docker compose pull unless no image: reference
// changed since the last deploy. build:-only services are excluded from the
// pull; when the compose file could not be parsed (compose == nil), every
// service is pulled as a safe fallback.
func (d *Deployer) pullIfImagesChanged(ctx context.Context, run stackRun, compose *composeFile, currentImages, previousImages serviceImageByName) error {
	if d.bootstrapRun {
		// Nothing is recorded, so every image reads as changed and this would
		// pull the whole host at once — moving every floating tag (`:latest`,
		// `:2`) to whatever it resolves to today, unattended. `up` still
		// creates whatever is missing, and compose fetches an image the host
		// does not have, so a fresh install is unaffected (ADR-0051).
		slog.Info("skipping pull, bootstrap run", "stack", run.stack.Name)
		return nil
	}
	if currentImages != nil && !hasAnyImageChanged(currentImages, previousImages) {
		slog.Info("skipping pull, no image changes", "stack", run.stack.Name)
		return nil
	}

	if compose == nil {
		return d.runDockerCompose(ctx, run, "pull", "--quiet")
	}

	pullable := compose.pullableServices()
	if len(pullable) == 0 {
		slog.Info("skipping pull, all services use locally-built images", "stack", run.stack.Name)
		return nil
	}
	pullArgs := append([]string{"pull", "--quiet"}, pullable...)
	return d.runDockerCompose(ctx, run, pullArgs...)
}

// runDockerCompose executes a docker compose command for the given stack run.
//
// run.composePath is the compose file (always from the repo clone). A
// non-empty run.projectDir is passed as --project-directory so Docker Compose
// uses it for project identity (container labels) and .env loading; when
// empty, docker compose runs from the directory containing the compose file.
func (d *Deployer) runDockerCompose(ctx context.Context, run stackRun, args ...string) error {
	env, err := run.resolveEnv()
	if err != nil {
		return err
	}
	runDir, composeArgs := run.composeInvocation()
	composeArgs = append(composeArgs, args...)

	return d.runner.Run(ctx, runDir, env, "docker", composeArgs...)
}
