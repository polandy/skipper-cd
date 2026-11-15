# Feature Spec: Orphan Detection & Optional Prune

Status: detection layer implemented (ADR-0036). Prune layer **discarded
2026-07-19** (not needed); the design below is kept for the record only.
Assumes stack discovery (ADR-0034, `stack-discovery-spec.md`) — the discovered
set is the authoritative "managed" set, which is what makes this spec simple.
Date: 2026-07-18 (rewritten for discovery mode; updated for implementation)

## Goal

Close the ArgoCD "prune / orphaned resources" gap: today a stack whose
directory is removed from the repo keeps running forever and nobody notices.
Two layers, independently valuable:

1. **Detection (viz-only)** — show compose projects running on the host that
   skipper does not manage, in the UI. **Implemented.**
2. **Prune (opt-in)** — automatically `compose down` projects whose stack
   directory was removed from the repo. **Follows on the same design.**

Non-goals: pruning projects skipper never deployed (manually started compose
projects, skipper's own container in the Docker trial), volume deletion,
image cleanup, any manual "delete" button (deploys stay git-driven).

## Orphan classification

Source of truth is a single `docker ps -a --filter
label=com.docker.compose.project` with a Go-template format that emits
`project<TAB>working_dir`, one line per container. Matching uses the
**working_dir label**, not `config_files`: rollback deploys run with a temp
compose file in `/tmp` but keep `--project-directory` at the original dir
(Invariant 3), so `working_dir` is the stable identity. (The original proposal
named `docker compose ls`; that call lacks the working_dir label, so one
`docker ps` — which also yields the container count — replaced it.)

Each discovered stack's expected project dir is computable (its `project_directory`
override or `stacks_base_dir/<name>`). For every running project:

| working_dir label matches …                        | class    | shown as        | prunable |
|----------------------------------------------------|----------|-----------------|----------|
| project dir of a discovered stack                  | managed  | (normal row)    | no |
| project dir of a `disabled: true` stack            | managed  | disabled line   | no — disabled means hands-off |
| a dir under `stacks_base_dir`, or a `project_dir` recorded in state, with no discovered stack | **orphaned** | orphan section | yes (opt-in) |
| anything else                                      | unmanaged | orphan section, `unmanaged` tag | never |

The state entry's `project_dir` field (written on every successful deploy,
`persistedState.ProjectDirs`) covers the one heuristic remainder: a removed
stack whose `project_directory` override pointed outside `stacks_base_dir` is still
recognized as formerly managed.

Stale `state.yaml` entries (recorded stack, no discovered stack, nothing
running) are also surfaced as orphans ("state only") and cleaned up by prune.

## Detection behavior (implemented)

- Runs in the existing health-poll cycle (UI-gated, same interval) — no new
  timer. The detection sink is gated on `HasSubscribers`, so the headless
  `AlwaysPoll` ticks self-heal/healthwatch drive do not run it. Headless prune
  (below) is instead carried by the reconcile loop.
- Results are the `orphans` SSE snapshot; detection emits **no** deploy events
  (no event spam). A docker failure keeps the last snapshot (logged).
- UI: a collapsed "Orphans" section under the deploy table; one row per orphan
  with class tag, project name, working_dir, and container count (or "state
  only"). Hidden when empty. See `internal/ui/UI_SPEC.md` §4.15.

## Prune behavior (opt-in) — DISCARDED 2026-07-19

> Not built. The prune layer was scoped but dropped as unneeded — detection
> alone covers the homelab case. The design below is retained for reference in
> case it is revisited.

```yaml
# host skipper.yml
prune: true                # global default, false if omitted
```
```yaml
# repo skipper.yaml (per-stack override, *bool inherits global)
stacks:
  media:
    prune: false
```

- Only the **orphaned** class is pruned, never unmanaged, never disabled.
  `_nixos`/`_config` are reserved state keys, never projects — untouched by
  definition.
- Executed during `SyncAndDeployAll` (under the deploy mutex), **after** all
  stack deploys — deploys first, prune last, so a directory rename never has
  old and new down at once: `docker compose -p <project> down
  --remove-orphans`. **No** `--volumes` — homelab data safety; volumes of a
  pruned stack stay and their cleanup is manual.
- On success: new `pruned` deploy event, state entry deleted. On failure:
  `failed` event; the orphan stays listed and retries next sync.
- Removal race for the per-stack override: a removed stack's repo entry is
  gone with it, so the state entry also records `prune: false` if the stack
  had opted out — a stack that never wanted pruning is not pruned after
  removal either.
- Safety interlock with discovery: if the sync's stack phase aborted
  (`_config` file-level failure — see `stack-discovery-spec.md`), prune does
  **not** run; an unparseable config must never read as "all stacks are gone".

## Package layout

- `internal/orphans`: pure `Classify` + a `Detector` (docker query + parse +
  publish) behind `command.Runner`; `Managed` supplies the expected set.
- Wiring in `cmd/skipper/main.go`; prune (follow-up) runs inside the
  `internal/deploy` post-run path (it must hold the deploy mutex).

## Failure & edge cases

- `docker ps` failing → detection skipped this cycle, logged, last snapshot
  stays.
- Compose project name vs stack name can differ (`COMPOSE_PROJECT_NAME`,
  `-p`); all matching is via the working_dir label, names are display-only.
- A disabled stack with a `project_directory` override outside `stacks_base_dir` is
  matched only via `stacks_base_dir/<name>`; it is never pruned regardless.

## Open questions (resolved)

1. Prune headless via the reconcile loop by default? **Yes** — prune runs on
   every sync regardless of trigger.
2. Grace period (N syncs) before pruning? **No** — git is the source of truth,
   a revert restores the stack.
