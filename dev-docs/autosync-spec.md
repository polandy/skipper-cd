# skipper-cd Autosync — Specification

Autosync governs whether a detected change is deployed automatically. It is
modelled on ArgoCD's auto-sync: deployment can be paused **globally** or **per
stack**, changes that arrive while paused are **queued** and deployed **as soon
as sync resumes**. Autosync is **enabled everywhere by default** — with no
configuration, every stack deploys automatically exactly as before.

This document is the authoritative spec for the feature; the user-facing summary is [`docs/autosync.md`](../docs/autosync.md). UI behaviour is
specified in [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md).

---

## Two layers: config-as-code and runtime overrides

An autosync setting exists at two scopes (**global** and **per-stack**) and in
two layers:

- **Config-as-code** — declared in `skipper.yml`, loaded once at startup,
  immutable at runtime. This is the source of truth a restart returns to.
- **Runtime override** — set from the web UI, held **in memory only**, and
  **never persisted**. A `skipper-cd` restart drops all overrides and the
  config-as-code values apply again (see [Restart behaviour](#restart-behaviour)).

### Resolution

The effective autosync state for a stack resolves per layer, then per scope. A
per-stack setting **overrides** the global one:

```
globalResolved = uiGlobalOverride ?? config.autosync ?? true
stackResolved  = uiStackOverride[name] ?? stack.autosync          // may be nil
effective(name) = stackResolved != nil ? stackResolved : globalResolved
```

| global | stack     | effective | note                          |
|--------|-----------|-----------|-------------------------------|
| on     | *unset*   | **sync**  | default everywhere            |
| off    | *unset*   | paused    | global pause                  |
| off    | on        | **sync**  | per-stack override wins       |
| on     | off       | paused    | per-stack override wins       |

`??` is "first non-nil". Both config values are `*bool`: a missing global key
means `true`; a missing per-stack key means *inherit* (fall through to global).

### Override collapse

A per-stack **UI override is an exception to the baseline, never a permanent
pin**. The *baseline* is what a stack would resolve to without its UI override —
its config value if set, otherwise the effective global:

```
baseline(name) = uiStackOverride ignored: stackConfig[name] ?? globalResolved
```

The controller holds a per-stack UI override **only while it differs from the
baseline**; the moment it would equal the baseline, the override is dropped and
the stack inherits again. This is enforced at the two moments they can coincide:

- **Setting a stack override** to the value it would inherit anyway (or to `nil`)
  clears it instead of storing a redundant pin. Toggling a stack back to its
  baseline value is therefore the UI's natural "return to inherit" gesture — a
  two-position switch needs no separate reset control.
- **Toggling global** collapses every per-stack UI override that now equals its
  baseline; a genuine exception (still differing) survives.

Config-as-code values (`stacks[].autosync`) never collapse — they are durable
exceptions and pin the baseline, so a global toggle does not touch config-pinned
stacks. Only in-memory UI overrides are soft. Consequently the global switch acts
as a true master: it moves every inheriting stack and leaves only genuine
exceptions standing. A UI pause does **not** survive a global off→on cycle (its
override collapses while global is off, then the stack resumes with global);
durable, latch-style pauses belong in `skipper.yml`.

---

## Configuration (`skipper.yml`)

```yaml
autosync: true            # optional, global default, default: true

stacks:
  - name: traefik         # inherits global (syncs)
  - name: gitea
    autosync: false       # this stack is paused until re-enabled
  - name: monitoring
    autosync: true        # explicit; syncs even if global is turned off
```

| Scope     | Key                | Type   | Default | Meaning                                                            |
|-----------|--------------------|--------|---------|--------------------------------------------------------------------|
| global    | `autosync`         | bool   | `true`  | Default sync state for every stack that does not set its own.       |
| per-stack | `stacks[].autosync`| bool   | *inherit* | Overrides the global state for this stack (in both directions).   |

Omitting both keys reproduces the pre-autosync behaviour exactly.

---

## The queue

Two mechanisms cooperate: the **hash state is the source of truth for what
redeploys**, and an **in-memory registry** provides the visible, ordered queue.

### Deferral (leave-dirty)

Change detection is hash-based: a stack deploys only when its tracked files'
SHA-256 hashes differ from the last recorded deploy (see
[State File](../docs/state.md)). When a stack has changes but
its autosync is **not** effective, skipper-cd:

1. emits a `queued` [event](#events-and-logging) (persisted and logged),
2. **does not deploy**, and
3. **does not record the new hashes**.

Because the hashes are not advanced, the stack stays "dirty" — every later sync
re-detects the same change. This *is* the queue: there is no separate persisted
queue structure to keep in step with reality.

### Draining

Re-enabling autosync (globally or for a stack) via the UI triggers a full
`SyncAndDeployAll` run. Every still-dirty stack that is now effective is
deployed, in the normal deploy order, and its hashes are recorded — clearing it
from the queue. Turning autosync **off** never triggers a run.

A webhook that arrives while a stack is paused still runs `SyncAndDeployAll`; the
paused stack is deferred again (re-emitting `queued`) while other, un-paused
stacks deploy as usual.

### Pending registry (the ordered, real-time queue)

An in-memory registry mirrors the deferral decisions so the UI can show a count
and an ordered list without re-hashing on demand:

- One entry per pending stack: `{ changed_files, since, reason }`.
- Maintained at the same decision points every run already reaches: a deferral
  **marks** the stack; a successful deploy or an unchanged skip **clears** it. So
  after each run the registry reflects exactly `(changed && !effective)`.
- **`reason`** is `stack` when the stack's own setting causes the pause
  (`stackResolved == false`), otherwise `global`.
- **Order = deploy order.** Positions mirror what `DeployAllStacks` will do on
  resume: `_nixos` first (when pending), then stacks in `skipper.yml` order.
  Position is derived from that order, not arrival time, so the list is a
  truthful preview of the drain.
- **Count** = number of entries. Multiple pushes to a paused stack collapse into
  a single pending deploy (the stack is either dirty or not).
- In-memory only, like the overrides: after a restart the startup
  `SyncAndDeployAll` re-populates it.

### `_nixos`

The NixOS rebuild pseudo-stack (reserved key `_nixos`) participates like any
other stack: it is covered by global autosync and is independently toggleable.
When it is paused and nix files changed, skipper-cd emits `queued` for `_nixos`,
**keeps the previous nix hashes** (does not pre-save the new ones), and **still
runs the Docker stack deploys** in the same pass. This is distinct from
`nixos_rebuild.enabled: false`, which is a hard off (the rebuild is never
considered); a paused `_nixos` is merely deferred and drains on resume.

---

## Events and logging

A deferred deploy is a first-class event with a new status:

| Status   | Meaning                                                              |
|----------|---------------------------------------------------------------------|
| `queued` | A change was detected but not deployed because autosync is paused.   |

- The event carries `changed_files` (what is waiting), like `deploying`.
- `queued` events are **persisted to the deploy history** and replayed to the UI
  (only `skipped` is excluded from history).
- Each deferral also logs a line at INFO:
  `deploy deferred: autosync paused stack=<name> changed_files=[...]`.

---

## HTTP API

All endpoints share the webhook server's trust boundary (behind the same edge
auth as the UI) and are only registered when `ui_enabled: true`.

### `GET /api/autosync`

Returns the current autosync snapshot.

```json
{
  "global": true,
  "global_config": true,
  "global_overridden": false,
  "stacks": [
    { "name": "gitea", "effective": false, "config": false, "overridden": false, "pending": true },
    { "name": "traefik", "effective": true, "config": null, "overridden": false, "pending": false }
  ]
}
```

`config` is the config-as-code value (`null` = unset/inherit); `effective` is the
resolved state; `overridden` is `true` when a UI override is currently in force.

### `POST /api/autosync`

Sets a runtime override. Body:

```json
{ "scope": "global", "enabled": false }
{ "scope": "stack", "stack": "gitea", "enabled": true }
```

Returns the new snapshot (same shape as `GET`). When the change **enables** sync,
a `SyncAndDeployAll` run is triggered to drain the queue.

### `GET /api/queue`

Returns the ordered pending list, for initial paint.

```json
{
  "count": 2,
  "pending": [
    { "position": 1, "stack": "_nixos", "reason": "global", "changed_files": ["flake.lock"], "since": "2026-07-11T14:00:00Z" },
    { "position": 2, "stack": "gitea", "reason": "stack", "changed_files": ["docker-compose.yml"], "since": "2026-07-11T14:03:00Z" }
  ]
}
```

### SSE events (`GET /api/events`)

Besides the existing `deploy` event, the same stream carries two more named
events so open UIs update in real time without polling:

| Event      | Payload                       | Emitted when                                   |
|------------|-------------------------------|------------------------------------------------|
| `autosync` | the `GET /api/autosync` shape | on every toggle                                |
| `queue`    | the `GET /api/queue` shape    | whenever the registry changes (defer, deploy, toggle-triggered run) |

On connect, after replaying the deploy history, the current `autosync` and
`queue` snapshots are sent so a fresh tab paints immediately.

---

## Metrics

Exposed on `/metrics` alongside the existing deploy metrics:

| Metric                          | Type    | Description                                                        |
|---------------------------------|---------|-------------------------------------------------------------------|
| `skipper_deploys_queued_total`  | counter | Deploys deferred because autosync was paused, labelled by `stack`. |
| `skipper_autosync_enabled`      | gauge   | Effective per-stack autosync (`1`/`0`), labelled by `stack` (incl. `_nixos`). |
| `skipper_autosync_global`       | gauge   | Effective global autosync (`1`/`0`).                              |
| `skipper_autosync_pending`      | gauge   | Number of stacks currently queued (queue depth).                  |

---

## Restart behaviour

Runtime overrides and the pending registry are in-memory only. On restart:

1. Config-as-code (`autosync` values in `skipper.yml`) applies again — any UI
   override is gone.
2. The startup `SyncAndDeployAll` re-evaluates every stack: still-dirty stacks
   whose autosync is now effective deploy immediately; those still paused are
   re-queued.

In production, the NixOS module regenerates `skipper.yml` and restarts the
service on every rebuild, so a rebuild is also the moment overrides reset to the
declared configuration.

---

## Edge cases

- **Change then revert while paused:** if a paused stack's files change and then
  return to the last-deployed content before resuming, the hashes match again and
  nothing deploys — correct, the stack is back in sync.
- **Pausing alone never queues:** a stack only becomes pending when a change is
  detected *while* it is paused. Pausing a stack with no pending changes leaves
  the queue untouched.
- **Global off + per-stack on:** the per-stack override wins; that stack keeps
  syncing while everything else is paused. Because it still differs from the
  baseline (`on` vs global `off`), it is a genuine exception and survives global
  toggling until it coincides with the baseline.
- **Re-enable a stack you just paused (UI):** pausing then resuming a stack via
  the UI leaves no override — the resume equals the baseline and
  [collapses to inherit](#override-collapse), so the stack again follows the
  global switch. It is not silently pinned.
- **UI pause across a global cycle:** a stack paused only via the UI does not stay
  paused across a global off→on cycle. Turning global off makes its baseline
  `off`; the override (`off`) collapses; turning global on resumes it with the
  rest. To hold a stack paused durably, declare `autosync: false` in `skipper.yml`.
