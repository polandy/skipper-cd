# ADR-0032: Stack deploy ordering via depends_on

Status: accepted
Date: 2026-07-17

## Context

`DeployAllStacks` deploys stacks strictly in the order they appear in the
config's `stacks:` list. That order is deterministic but carries no meaning: it
expresses nothing about *why* one stack should go first, and nothing reacts to
it. Two gaps follow:

1. **No intent.** "The app needs the database" lives only in the operator's
   head. Whoever edits the config must know that `postgres` has to stay above
   `app` in the list — and nothing complains when a reorder breaks that.
2. **No failure edge.** When the database deploy fails (or rolls back), the app
   deploys anyway — potentially against a schema its new version no longer
   understands. Every stack's outcome is independent today; a run is just a
   fold over the list.

This is the docker-compose analogue of ArgoCD's sync waves / `depends-on`
annotations: an ordering guarantee *between* units of deployment, where compose's
own `depends_on` only orders services *within* one stack.

The feature stays inside skipper's scope (visualization, not trigger): it adds
no trigger surface and no manual action — it only constrains the order and
gating of deploys that git already demanded.

## Decision

Add an optional per-stack **`depends_on`** list naming other stacks:

```yaml
stacks:
  - name: postgres
  - name: app
    depends_on: [postgres]
  - name: monitoring   # independent
```

### Ordering

- Stacks deploy in **topological order** of the dependency graph. Among stacks
  not ordered relative to each other, **config-list order is preserved** (a
  stable topological sort seeded by list order).
- A config with no `depends_on` anywhere therefore behaves **exactly as today**
  — zero behaviour change for existing setups.
- The NixOS rebuild (`_nixos`) keeps running before all stack deploys
  (Invariant 4); it is not part of the graph and cannot be depended on
  explicitly — it is implicitly first for everyone.
- Deploys remain **sequential** (one at a time under the deploy mutex,
  Invariant 7). This feature fixes the *sequence*; it does not introduce
  parallelism.

### Validation (config load)

`depends_on` entries must name stacks that exist; self-references and cycles
are rejected at load time with an error naming the offending stacks. A broken
graph is a config bug, not a runtime condition.

### The failure edge: block and stay dirty

Within one deploy run, each stack's outcome is tracked. A stack with pending
changes does **not** deploy when any of its dependencies (transitively) ended
this run as `failed`, `rolled_back`, or `rolled_back_unhealthy`. Instead it:

- emits a new event status **`blocked`**, carrying the same changed files /
  diffs / commits context a failed deploy would, so the UI shows *what* is
  being held back and *why* (the failed dependency's name in the error text);
- does **not** record its hashes — the stack stays dirty and is retried
  automatically on the next sync: the next webhook push or reconcile tick
  ([ADR-0028](0028-periodic-reconcile-loop.md)) re-detects the change, and once
  the dependency deploys cleanly the dependent follows in the same run. This is
  the same leave-dirty pattern autosync uses
  ([ADR-0016](0016-autosync-and-queue-via-leave-dirty.md)); no new retry
  machinery exists;
- **marks the pending registry** (reason `blocked by <name>`), exactly like a
  queued stack. This is not just for the queue drawer: end-of-run
  `LastDeployedCommit` advancement is gated on the pending registry being empty
  (the diff-base fix that previously landed for queued stacks) — without the
  mark, the diff base would advance past the blocked change and its eventual
  deploy would show an empty diff and roll back to the wrong commit. Blocked
  and queued must pin the base through the same mechanism;
- is **not** a notification status: `blocked` stays out of the notifier's
  terminal-status whitelist (like `queued`). The operator is already paged by
  the dependency's own `failed`/`rolled_back` notification, and a blocked
  event recurs on every reconcile tick while the dependency stays broken —
  paging per dependent per tick would be pure noise. The UI event history and
  the pending registry carry the visibility instead.

A dependency that was **skipped** (no changes) satisfies the edge: it is
already at its desired state. Ordering gates on *this run's deploy outcomes*
only — it does not consult runtime health of an unchanged dependency; watching
and correcting runtime state is self-heal's job
([ADR-0029](0029-runtime-drift-self-heal.md)).

### The queue edge: order holds across the queue

When a dependency's change is **queued** (autosync paused,
[ADR-0016](0016-autosync-and-queue-via-leave-dirty.md)), a changed dependent
does not overtake it: it is queued as well (`queued` event, pending-registry
entry with reason `waiting for dependency <name>`), even though its own
autosync is on. Otherwise the app could deploy against a database version whose
update is being deliberately held back — exactly the situation `depends_on`
exists to prevent. When the dependency's change eventually deploys (autosync
resumed), the dependent deploys in the same run, in order, with no further
action.

Note the asymmetry with skipped dependencies: a *skipped* dependency has no
pending change (git and runtime agree), so the dependent proceeds; a *queued*
dependency has a known, undeployed change, so the dependent waits.

### "Ready" means the deploy completed — health gating composes

`depends_on` guarantees the dependency's deploy **completed successfully**
before the dependent starts. By default `docker compose up -d` returns once
containers are started, not once they are ready. Operators who need
*readiness* (DB accepting connections before the app migrates) configure a
`health_check` on the **dependency**: its `up` then runs with `--wait` and the
optional HTTP probe ([ADR-0022](0022-health-check-gated-rollback.md)), and
because deploys are sequential, the dependent only starts after the dependency
proved healthy — or is blocked when it rolled back. `depends_on` +
`health_check` together give the health-gated wave; neither feature needs to
know about the other.

### Surfaces

- **Run plan / upcoming** ([ADR-0024](0024-upcoming-deploys-look-ahead.md)):
  `computeRunPlan` lists stacks in the topologically sorted order, so the
  header's "next up" trail matches the real deploy sequence.
- **Events/UI**: new `blocked` status (row styling / pill analogous to
  `queued`; details in UI_SPEC at implementation time). Metrics gain a
  `skipper_deploys_blocked_total{stack}` counter.
- **Docs**: `docs/configuration.md` gains the `depends_on` stack field and a
  short "Deploy ordering" section covering the failure/queue semantics and the
  health_check composition.

## Consequences

- The DB-before-app ordering is expressed declaratively and survives config
  reorders; a failed dependency now *stops* dependents instead of letting them
  race ahead, and the retry is automatic via the existing dirty-state paths.
- A blocked stack is a **delayed** deploy, never a lost one: hashes stay
  unrecorded, so every future sync retries until the chain deploys cleanly.
- Failure blast radius is per-subgraph: independent stacks are unaffected by a
  failing chain (unlike the NixOS-rebuild failure, which aborts the whole run
  because every stack may depend on the host configuration).
- A long chain behind a paused dependency accumulates in the pending registry —
  the queue drawer shows the entire held-back chain with reasons, which is the
  desired visibility, not a leak. The flip side: while anything is blocked or
  queued, `LastDeployedCommit` stays pinned at the last fully-deployed commit
  (correct for diffs and rollback, but a permanently failing dependency keeps
  the base — and the shown diffs — growing until it is fixed).
- While a dependency stays broken, each reconcile tick re-emits the
  dependency's `failed` and the dependents' `blocked` events into the history —
  the same recurrence a persistently failing stack already has today, just with
  more rows per tick. Notifications do not repeat (only the dependency's own
  failure pages, and repeat pages are the operator's existing reconcile-tick
  behaviour).
- **Use edges sparsely.** A hub edge like "every stack depends_on traefik"
  makes one bad ingress deploy block the entire host: dozens of pending
  entries, a pinned diff base for everything, and no availability gain — a
  blocked stack just keeps running its old version, which is also what it
  would have done deploying normally. Declare an edge only where deploying
  *before* the dependency is actually wrong, not wherever a runtime arrow
  exists (reverse-proxy discovery, scraping, dashboards are runtime couplings
  and need no edge).
- Config validation rejecting cycles/unknown names means a bad `depends_on`
  stops skipper from starting. On hosts where skipper.yml is produced by the
  NixOS module and shipped via skipper's own nixos-rebuild, that would brick
  the CD loop until a manual rebuild — the module should therefore validate
  the names at eval time (assertion over the stack list) so a typo fails
  `nix build`, never the running service.
- Self-heal is unaffected: `HealStack` restores the *currently deployed*
  version of a single stack, so ordering (a property of applying *new*
  versions) does not apply.
- Slightly more state per run: the orchestration loop tracks per-stack outcomes
  to evaluate edges; the graph itself is computed once per run from config.

## Alternatives considered

- **Numeric waves (`wave: 0/1/2`, ArgoCD sync-wave style).** Rejected: an
  integer orders but does not explain — the DB-app relationship is invisible,
  every new stack forces the operator to reason about the global numbering, and
  there is no natural edge along which to propagate failure ("block everything
  in later waves" punishes unrelated stacks; "block nothing" loses the point).
- **Document config-list order as the contract, add nothing.** Rejected: the
  order is already deterministic today, so the only thing this would add is
  prose. It leaves both gaps open — no declared intent, no failure edge.
- **Deploy dependents anyway, log a warning.** Rejected: ordering without
  gating breaks the operator's natural reading of `depends_on` — the one
  scenario that motivates the feature (failed DB migration, app deploys against
  the old schema) would still happen, now with a log line.
- **Abort the whole run on any failure (NixOS-rebuild style).** Rejected: too
  coarse — independent stacks would be held hostage by an unrelated failure.
  The rebuild aborts everything because the host system underlies every stack;
  no compose stack has that blast radius by default.
- **Let dependents overtake a queued dependency.** Rejected: autosync-pausing a
  database stack is precisely the "hold this back deliberately" signal; letting
  the app's change through inverts the declared order at the moment it matters
  most.
- **Runtime-health edge (block when the dependency is currently unhealthy,
  even if unchanged).** Rejected for this ADR: it entangles ordering with the
  health poller and duplicates self-heal's territory (ADR-0029). Deploy-outcome
  edges are decidable from the run itself, with no poller dependency and no
  UI-gating questions.
