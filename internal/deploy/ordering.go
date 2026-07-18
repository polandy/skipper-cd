package deploy

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// depOutcome is what happened to a stack in the current deploy run, tracked so a
// dependent stack's depends_on edges can be evaluated before it deploys
// (ADR-0032). The zero value is depReady, so an unrecorded stack reads as "at
// its desired state" — safe because orderStacks guarantees every dependency is
// recorded before its dependent.
type depOutcome int

const (
	// depReady: deployed or skipped — the stack is at its desired state, so
	// dependents may proceed.
	depReady depOutcome = iota
	// depQueued: deferred by autosync (or waiting behind a queued dependency) —
	// dependents queue too, so ordering holds across the pause.
	depQueued
	// depBlocked: failed, rolled back, or itself blocked by a dependency —
	// dependents block, so a broken dependency stops the chain.
	depBlocked
)

// gateDecision is how an upstream dependency constrains a stack about to be
// considered for deploy. The zero value (depReady, no dependency) means no
// constraint, so callers with no ordering can pass it through unchanged.
type gateDecision struct {
	outcome depOutcome // depReady = free to deploy; depQueued / depBlocked = forced by a dependency
	depName string     // the dependency that caused a queue/block, for the reason and error text
}

// orderStacks returns the stacks in a deploy order that never places a stack
// before one it depends_on, preserving config order among stacks not otherwise
// constrained (a stable topological sort seeded by config order, ADR-0032). The
// graph is validated acyclic at config load; this stays defensive anyway,
// appending any unresolvable remainder in config order rather than looping.
func orderStacks(stacks []config.Stack) []config.Stack {
	emitted := make(map[string]bool, len(stacks))
	ordered := make([]config.Stack, 0, len(stacks))

	for len(ordered) < len(stacks) {
		progressed := false
		for _, s := range stacks {
			if emitted[s.Name] || !dependenciesEmitted(s.DependsOn, emitted) {
				continue
			}
			ordered = append(ordered, s)
			emitted[s.Name] = true
			progressed = true
		}
		if !progressed {
			for _, s := range stacks {
				if !emitted[s.Name] {
					ordered = append(ordered, s)
					emitted[s.Name] = true
				}
			}
		}
	}
	return ordered
}

func dependenciesEmitted(deps []string, emitted map[string]bool) bool {
	for _, dep := range deps {
		if !emitted[dep] {
			return false
		}
	}
	return true
}

// depGate records each stack's outcome as a deploy run proceeds and, before a
// stack deploys, reports whether an upstream dependency forces it to block or
// queue (ADR-0032). It owns the outcome map so the orchestration loop never
// handles that state directly. Not safe for concurrent use — a deploy run is
// single-threaded under the deploy mutex.
type depGate struct {
	outcome map[string]depOutcome
}

func newDepGate() *depGate {
	return &depGate{outcome: make(map[string]depOutcome)}
}

// record stores a stack's outcome so its dependents can react to it. It must be
// called for every stack in deploy order, which orderStacks guarantees.
func (g *depGate) record(stack string, o depOutcome) {
	g.outcome[stack] = o
}

// decide reports how the given dependencies constrain a dependent stack. A
// failed or blocked dependency wins (block), else a queued dependency queues the
// stack, else it is free to deploy. The named dependency is the one that caused
// a non-ready decision.
func (g *depGate) decide(deps []string) gateDecision {
	queuedBy := ""
	for _, dep := range deps {
		switch g.outcome[dep] {
		case depBlocked:
			return gateDecision{outcome: depBlocked, depName: dep}
		case depQueued:
			if queuedBy == "" {
				queuedBy = dep
			}
		}
	}
	if queuedBy != "" {
		return gateDecision{outcome: depQueued, depName: queuedBy}
	}
	return gateDecision{}
}

// deployStackGated is the ordering layer over the single-stack primitive
// deployStackIfChanged (ADR-0032). When an upstream dependency constrains the
// stack and the stack has a pending change, it defers rather than deploying;
// otherwise it deploys normally. Either way it reports the outcome so the caller
// can gate this stack's own dependents.
func (d *Deployer) deployStackGated(ctx context.Context, stack config.Stack, baseDir, varsFile string, baseEnv []string, state *persistedState, gate gateDecision) depOutcome {
	if gate.outcome != depReady {
		// A dependency failed or is queued. Only a stack that actually has a
		// change needs holding back; an unchanged stack is already at its desired
		// state, so the constraint is moot and it does not block its dependents.
		if changed, pending := d.pendingChanges(stack, baseDir, varsFile, state); pending {
			return d.deferForDependency(ctx, stack, changed, state, gate)
		}
	}
	err := d.deployStackIfChanged(ctx, stack, baseDir, varsFile, baseEnv, state)
	return d.classifyDeployOutcome(stack.Name, err)
}

// deferForDependency records a changed stack that an upstream dependency held
// back this run: blocked (the dependency failed) or queued (the dependency is
// itself queued). It emits the matching event with the held-back change, marks
// the pending registry — which also pins the diff/rollback base until the chain
// clears — and leaves hashes unrecorded so the stack retries on the next sync.
func (d *Deployer) deferForDependency(ctx context.Context, stack config.Stack, changed []string, state *persistedState, gate gateDecision) depOutcome {
	cs := d.collectChange(ctx, changed, state.LastDeployedCommit)

	if gate.outcome == depBlocked {
		d.markPending(stack.Name, changed, "blocked by "+gate.depName)
		metrics.DeploysBlocked.WithLabelValues(stack.Name).Inc()
		d.emit(events.StatusBlocked, stack.Name, 0, "dependency "+gate.depName+" failed this run", cs)
		slog.Warn("deploy blocked by failed dependency", "stack", stack.Name, "dependency", gate.depName, "changed_files", changed)
		return depBlocked
	}

	d.markPending(stack.Name, changed, "waiting for dependency "+gate.depName)
	metrics.DeploysQueued.WithLabelValues(stack.Name).Inc()
	d.emit(events.StatusQueued, stack.Name, 0, "", cs)
	slog.Info("deploy deferred: waiting for queued dependency", "stack", stack.Name, "dependency", gate.depName, "changed_files", changed)
	return depQueued
}

// classifyDeployOutcome maps a completed single-stack deploy to its run outcome
// for the dependency gate: a failure blocks dependents; a stack left in the
// pending registry (its own autosync paused) queues them; a successful or
// skipped deploy leaves them free. The registry is the source of truth for
// deferral, so no queued/skipped ambiguity is inferred from the nil error.
func (d *Deployer) classifyDeployOutcome(stack string, err error) depOutcome {
	switch {
	case err != nil:
		return depBlocked
	case d.queue != nil && d.queue.Has(stack):
		return depQueued
	default:
		return depReady
	}
}

// markPending adds a deferred entry with an explicit reason (nil-safe). Unlike
// markQueued it does not consult the autosync controller — the reason here is a
// dependency edge, not an autosync pause.
func (d *Deployer) markPending(stack string, changed []string, reason string) {
	if d.queue != nil {
		d.queue.Mark(stack, changed, reason)
	}
}

// pendingChanges returns the stack's changed tracked files versus persisted
// state, and whether any exist — the same detection deployStackIfChanged makes,
// exposed so the ordering layer can describe a stack it defers without deploying
// it. A hash error yields no changes; the real deploy would surface it.
func (d *Deployer) pendingChanges(stack config.Stack, baseDir, varsFile string, state *persistedState) ([]string, bool) {
	repoDir := filepath.Join(baseDir, stack.Name)
	composePath := filepath.Join(repoDir, "docker-compose.yml")

	var dockerfilePaths []string
	if compose, err := parseComposeFile(composePath); err == nil && compose != nil {
		dockerfilePaths = compose.dockerfilePaths(repoDir)
	}

	currentHashes, err := computePerFileHashes(repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		return nil, false
	}
	d.addStackConfigHash(currentHashes, stack)
	changed := changedFiles(currentHashes, state.hashesFor(stack.Name))
	return changed, len(changed) > 0
}
