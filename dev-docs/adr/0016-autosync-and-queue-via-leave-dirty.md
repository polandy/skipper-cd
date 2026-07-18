# ADR-0016: Autosync with a leave-dirty queue and non-persistent overrides

Status: accepted
Date: 2026-07-11

## Context

skipper-cd deploys unconditionally: every webhook (and the startup sync) runs
`SyncAndDeployAll` → `DeployAllStacks` → per-stack `deployStackIfChanged`, which
deploys any stack whose tracked-file hashes changed since the last deploy
(ADR-0002). There was no way to pause deployment for a stack or globally, the way
ArgoCD's auto-sync can be turned off so changes accumulate until a human resumes.

We want:

- Pause sync **globally** and **per stack**, declared in config-as-code
  (`skipper.yml`) and default-on so existing setups are unchanged.
- A temporary **UI override** of those toggles that does **not** survive a
  restart — config-as-code must win again on restart.
- Changes that arrive while paused must be **remembered** and deployed **when
  sync resumes**, marked (`queued`) and logged.
- A **visible, ordered queue**: how many deploys are waiting and in what order
  they will drain.

Two design questions dominated: how to represent the queue, and where the
override state lives.

## Decision

### Queue = leave the stack "dirty"; a registry mirrors it for the UI

Change detection is already hash-based, and a stack is redeployed whenever its
hashes differ from `state.yaml`. So the queue needs no new persisted structure:
when a stack has changes but its autosync is not effective, `deployStackIfChanged`
emits a `queued` event, **skips the deploy, and does not record the new hashes**.
The stack stays dirty, so the next `SyncAndDeployAll` re-detects it. Re-enabling
sync triggers a run that drains it. The hash state remains the single source of
truth for what redeploys — there is no second structure that can disagree with
reality (contrast the coalescing `pending` flag rejected in ADR-0010; this is a
different mechanism and does not touch the single-flight mutex or invariant 7).

For the UI's count and ordered list we keep an **in-memory pending registry**,
updated at the same decision points every run already reaches: a deferral marks
the stack, a successful deploy or an unchanged skip clears it. After each run the
registry reflects exactly `(changed && !effective)`. Its order is *deploy order*
(`_nixos` first, then config order), so it is a truthful preview of the drain,
not arrival order. The registry is derived state; if it were ever lost, the hash
state still deploys the right thing.

### Resolution: per-stack overrides global

`effective(stack) = stackSetting ?? globalSetting ?? true`, where a per-stack
value (config or UI override) wins over the global one in both directions. A
missing global is `true` (default-on); a missing per-stack value inherits global.
This lets "pause everything" and "keep just this one syncing" coexist.

### Overrides are in-memory only

Config-as-code values load once into the config; UI overrides live in a
thread-safe controller and are **never written to disk**. A restart drops them
and reloads config-as-code — the required non-persistent semantics fall out for
free, with no persistence code and no reconciliation. In production the NixOS
module regenerates `skipper.yml` and restarts the service on every rebuild, so a
rebuild is also the reset point. The startup sync then drains any stack that is
dirty and now effective.

### `_nixos` participates like a stack

The NixOS rebuild pseudo-stack is covered by the global toggle and independently
toggleable. When paused with nix changes, it emits `queued`, **keeps the previous
nix hashes** (does not pre-save per ADR-0005), and returns success so Docker
stack deploys still run. This is separate from `nixos_rebuild.enabled: false`,
which remains a hard off.

## Consequences

- No new persisted state and no change to the deploy lock: invariant 7 and
  ADR-0010 stand. The queue is emergent from existing hash tracking.
- The pending registry, the `queued` event/label, and the `autosync`/`queue` SSE
  events are the visible surface; the metrics `skipper_deploys_queued_total`,
  `skipper_autosync_enabled`, `skipper_autosync_global`, and
  `skipper_autosync_pending` make queue depth and pause state alertable.
- A UI override cannot outlive the process; there is deliberately no way to
  persist a pause from the UI — durable pauses belong in `skipper.yml`.
- A paused stack whose files change and then revert before resume deploys
  nothing (hashes match again) — correct "back in sync" behaviour.
- Full behaviour and the API contract live in
  [`dev-docs/autosync-spec.md`](../autosync-spec.md); the UI surface in
  [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md).
