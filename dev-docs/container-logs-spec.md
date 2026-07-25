# Feature Spec: Container Logs in the UI

Status: accepted (ADR-0037)
Date: 2026-07-18 (rewritten 2026-07-19: snapshot-only → live-streamed)

## Goal

When a service is unhealthy, answer "why" without ssh-ing to the host: show
its container logs in the UI, live. Pure visualization — deploys stay
git-driven. ArgoCD equivalent: pod logs on the resource tree.

Non-goals: log persistence/indexing (Loki's job), download/export, full history
beyond the tail buffer, log-based alerting, merged multi-stack views.

## User model

A terminal/console **logs icon** opens the log on every log-bearing surface, in
both views:

- **Deploys view** — per **stack** on the deploy row (all services merged,
  service-prefixed lines); per **container** on each service line of the health
  panel.
- **Stacks view** — per **stack** on the card head (merged); per **container**
  on each container row.

Opening a log shows a **backlog** (last N lines) immediately, then the tail
**follows live**. **Only one log is open at a time** — opening another closes
the previous one (and stops its stream).

Every open log offers the same controls:

- **Live / pause** — the stream runs by default; pause freezes it.
- **Auto-scroll** — follow the tail (stay pinned to the bottom as lines arrive).
- **Line-wrap** — toggle soft-wrap for long lines.
- **In-log search** — a search box that filters *within the open log*:
  non-matching lines are hidden, matches highlighted and counted. On mobile the
  existing header search icon retargets to the open log while one is open.
- **Fullscreen** — an overlay filling the viewport with its own live stream +
  search + wrap + auto-scroll; Esc closes.
- **Backlog selector** — 50 / 200 / 1000 (default 200, hard cap 1000).

The existing "Logs" view (skipper's own process logs) gains the same
fullscreen + search + line-wrap; it already has auto-scroll and oldest-first.
One control set across all log surfaces.

## HTTP surface (only when `ui_enabled: true`)

```
GET /api/container-logs/{stack}?tail=200[&since=<ts>]                       # SSE, services merged
GET /api/container-logs/{stack}?tail=200&services=api[,db][&since=<ts>]      # SSE, selected subset
```

- `text/event-stream`; each `data:` frame is one log line (timestamps included,
  rendered dimmed by the UI). The backlog replays as the first frames, then live
  lines follow.
- Invocation via the `LogStreamer` seam (ADR-0037):
  `docker compose -f <composePath> --project-directory <dir> logs
  --no-color --no-log-prefix --tail N --timestamps --follow <service>`. The
  whole-stack endpoint drops `--no-log-prefix` so compose labels each line with
  its service. Compose file from the clone, `--project-directory` from
  `project_directory` — same `stackRun` helper as deploy (Invariant 1).
- **Validation before argv**: `{stack}` in the current stack set
  (`CurrentStacks`, discovery-aware — Invariant 8); `{service}` present in the
  stack's current health snapshot; `tail` clamped to [1, 1000]. Otherwise 404.
- **`since`** (RFC3339) resumes after a reconnect: server passes `--since <ts>`
  instead of `--tail N` so the backlog is not re-sent and no lines are lost.
- Read-only: no deploy-mutex interaction, no state writes.

## Lifecycle

- **One follow child per viewer.** The "one log open at a time" rule means the
  server runs at most one `docker compose logs --follow` per client. Opening a
  new log cancels the previous request context → its child is killed. No
  concurrent-stream limit or queue needed.
- **Disconnect** cancels the request context (context exec), killing the child.
- **Shutdown** never blocks on a stream: the child is tied to the request
  context and abandoned (ADR-0014 rule).

## Security

- Logs may contain secrets an app prints at startup. Accepted for the target
  deployment (UI behind front-auth, LAN-only); nothing stored server-side —
  skipper pipes and forgets. Called out in `docs/`.
- Endpoints exist only with `ui_enabled: true`.

## Package layout

- `internal/containerlogs` — the `LogStreamer` seam (consumer-side interface +
  an `os/exec` implementation), request validation, argv building, tail
  clamping, and the SSE handler. Compose-path/project-dir derivation reuses the
  deploy path's `stackRun` so invariants can't drift. Not `internal/logs` — too
  close to `internal/logbuf` (skipper's own-log ring).
- Wired under the `ui_enabled` block in `cmd/skipper/main.go`.

## Testing

- `internal/containerlogs`: table tests with a fake `LogStreamer` — exact argv
  (per-service and merged), `--tail` clamping, `--since` on reconnect, unknown
  stack → 404, unknown service → 404, `ctx` cancellation stops the stream, SSE
  framing of backlog-then-live lines.
- Node unit layer: the pure UI helpers (search filter/highlight, line
  classification) in `static/app-helpers.js`.
- UI e2e (**Mask S**, behaviour-only, after a manual eyeball): open a log →
  fake backlog renders and live frames append; only-one-open; search filters
  in-log; wrap toggles; fullscreen opens/closes.

## UI integration

- One log-panel component reused across the four entry points, plus a
  fullscreen overlay. Monospace, theme-aware, no JS deps — consistent with the
  self-contained UI (ADR-0035). `internal/ui/UI_SPEC.md` gets the panel + its
  controls before implementation (UI-change rule).
- The client tracks the last-seen line timestamp and passes it as `since` on
  EventSource reconnect.
