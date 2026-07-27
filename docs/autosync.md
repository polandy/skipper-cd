# Autosync & Queue

Autosync governs whether a detected change is deployed automatically. Deploys can be paused **globally** or **per stack**; changes that arrive while paused are **queued** and deploy as soon as sync resumes. Autosync is **on everywhere by default** — with no configuration, every stack deploys automatically.

## Configuration

```yaml
autosync: true            # optional, global default, default: true

stacks:
  - name: traefik         # inherits global (syncs)
  - name: gitea
    autosync: false       # this stack is paused until re-enabled
  - name: monitoring
    autosync: true        # explicit; syncs even if global is turned off
```

| Scope | Key | Default | Meaning |
|---|---|---|---|
| global | `autosync` | `true` | Default sync state for every stack that does not set its own. |
| per-stack | `stacks[].autosync` | *inherit* | Overrides the global state for this stack (in both directions). |

## Pausing from the UI

The web UI has toggles for the global switch and each stack. These are **runtime overrides, in memory only** — a restart drops them and the `skipper.yml` values apply again. Toggling a stack back to the value it would inherit anyway returns it to "follow global" (there is no hidden pin). For a durable pause, set `autosync: false` in `skipper.yml`.

## The queue

- A change to a paused stack is **not deployed and not marked done** — it shows up as `queued` (in the UI, the event history, and the logs) and stays pending.
- Re-enabling sync (UI toggle, or config + restart) deploys everything pending, in the normal deploy order.
- Multiple pushes to a paused stack collapse into **one** pending deploy of the latest state.
- The NixOS rebuild (`_nixos`) participates like a stack: independently pausable; while paused, nix changes queue and the Docker stacks still deploy.

## API

Available when `ui_enabled: true`, behind the same edge auth as the UI:

| Endpoint | Purpose |
|---|---|
| `GET /api/autosync` | Current global/per-stack autosync state, with a `version` that advances on every change. |
| `POST /api/autosync` | Set an override: `{"scope": "global", "enabled": false}` or `{"scope": "stack", "stack": "gitea", "enabled": true}`. Enabling triggers a run that drains the queue. |
| `GET /api/queue` | The ordered pending list with changed files and queue time. |

The `/api/events` SSE stream carries `autosync` and `queue` events so open UIs update in real time. A client that reads autosync state from both the stream and its own `POST` response should ignore any snapshot whose `version` is below the last one it applied — the two channels can overtake each other. The version restarts at `0` with the process.

## Metrics

| Metric | Type | Description |
|---|---|---|
| `skipper_deploys_queued_total` | counter | Deploys deferred because autosync was paused, labelled by `stack`. |
| `skipper_autosync_enabled` | gauge | Effective per-stack autosync (`1`/`0`), labelled by `stack` (incl. `_nixos`). |
| `skipper_autosync_global` | gauge | Effective global autosync (`1`/`0`). |
| `skipper_autosync_pending` | gauge | Number of stacks currently queued. |
