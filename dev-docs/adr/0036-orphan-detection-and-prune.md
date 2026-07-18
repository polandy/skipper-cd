# ADR-0036: Orphan detection and optional prune

Status: accepted
Date: 2026-07-18

## Context

Stack discovery (ADR-0034) made the deploy repo the source of truth for stack
*membership*: the set of stacks is exactly the discovered directories under
`stacks_base_dir`. That closes the last gap standing between skipper and
ArgoCD's "prune / orphaned resources" model — until now, a stack whose
directory was removed from the repo kept running forever and nothing surfaced
it. skipper deployed the stack once, then simply stopped mentioning it.

Two independently valuable layers close the gap:

1. **Detection (viz-only)** — show compose projects running on the host that
   skipper's current stack set does not account for.
2. **Prune (opt-in)** — `compose down` the projects whose stack directory was
   removed from the repo.

This ADR records the detection layer and the shape prune takes; prune ships as
a follow-up on the same design.

Non-goals: pruning projects skipper never deployed (manually started compose
projects, skipper's own container), volume deletion, image cleanup, and any
manual "delete" button — deploys stay git-driven ([[project-scope-visualization-not-trigger]]).

## Decision

### Identity is the working_dir label, not the compose file path

Classification matches a running project to a stack by its
`com.docker.compose.project.working_dir` label. A rollback deploy runs with a
temp compose file in `/tmp` but keeps `--project-directory` at the original dir
(Invariant 3), so the compose-file path is unstable while the working_dir is
the durable identity. Every successful deploy records that dir in state
(`project_dirs` map), so a removed stack whose `working_dir` pointed *outside*
`stacks_base_dir` is still recognized as formerly managed.

### Enumeration: one `docker ps`, not `compose ls` + a label query

The proposal named `docker compose ls`, which does not carry the working_dir
label — it would need a second per-project query. Instead a single
`docker ps -a --filter label=com.docker.compose.project` with a tab-separated
`--format` yields, one line per container, the project name, working_dir,
config-files label, container name, service, image, state, status and ports.
That is enough to group into projects *and* populate the UI's per-orphan
container expansion (which containers an orphan owns, running or stopped, on
which ports) in one call — no JSON label-string parsing.

A second best-effort `docker volume ls --format '{{project}}\t{{name}}'` maps
compose-created named volumes to their project, so the expansion shows the data
an orphan holds — tagged "kept on prune" because prune never passes
`--volumes`. A failure of that query omits the note but never blocks detection.

### Classification

For every running project, matched by working_dir:

| working_dir matches …                                   | class     | prunable |
|---------------------------------------------------------|-----------|----------|
| an active discovered stack's project dir                | managed   | — (normal row) |
| a `disabled: true` stack's project dir                  | managed   | — (hands-off) |
| under `stacks_base_dir`, or a dir recorded in state, with no matching stack | **orphaned** | yes (opt-in) |
| anything else                                           | unmanaged | never |

A stale state entry (recorded stack, no matching stack, nothing running) is a
state-only orphan so prune can clean the entry. The reserved `_nixos`/`_config`
state keys are never compose projects, so they never carry a `project_dir` and
never classify.

### Detection wiring: piggyback on the health poll, no new timer

Detection runs on the existing health-poll cadence and is viz-only, so it is
gated on a subscribed UI client (`HasSubscribers`) — it skips the headless
`AlwaysPoll` ticks self-heal/healthwatch drive. Results ride the SSE stream as
an `orphans` snapshot; detection emits no deploy events (no history/notification
spam). A docker failure leaves the last snapshot in place rather than blanking
the section. Headless prune (the follow-up) is instead carried by the reconcile
loop, since it must run unattended.

### Prune (follow-up, opt-in)

Global `prune: true` (default false) with a per-stack `*bool` override in the
repo `skipper.yaml`. Only the **orphaned** class is pruned — never unmanaged,
never disabled. It runs inside `SyncAndDeployAll` under the deploy mutex,
*after* all stack deploys (so a directory rename never has old and new down at
once): `docker compose -p <project> down --remove-orphans`, no `--volumes`
(homelab data safety). Success emits a `pruned` event and drops the state
entry; failure retries next sync. If the sync's stack phase aborted (`_config`
file-level failure), prune does not run — an unparseable config must never read
as "all stacks are gone".

## Consequences

- New `internal/orphans` package: pure `Classify` + a `Detector` behind
  `command.Runner`. State grows a `project_dirs` map; the deployer exposes
  `CurrentProjectDirs()` alongside `CurrentStacks()`/`CurrentDisabledStacks()`.
- A disabled stack with a `working_dir` override outside `stacks_base_dir` is
  matched only via `stacks_base_dir/<name>` (the override is not exposed to the
  disabled set); such a project could misclassify as orphaned. Acceptable v1
  limitation — disabled stacks are hands-off and never pruned regardless.
- Legacy (host `stacks:` list) mode works too, but with `stacks_base_dir` often
  empty its `under-base` heuristic is inert; matching then rests on active dirs
  and recorded state. Orphan detection is most meaningful in discovery mode.
