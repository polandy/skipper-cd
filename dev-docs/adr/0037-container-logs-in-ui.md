# ADR-0037: Container logs in the UI (live-streamed via SSE)

Status: accepted
Date: 2026-07-19

## Context

When a stack is unhealthy the UI shows *that* it is unhealthy (the health
pill, the per-service health panel) but not *why*. Answering "why" means
ssh-ing to the host and running `docker compose logs`. skipper already knows
every stack's compose file, project directory and service list — it can surface
those logs itself. ArgoCD's equivalent is pod logs on the resource tree.

skipper already streams its *own* process logs to the UI: the "Logs" view is a
`logbuf` ring fed by the slog handler and pushed over SSE (`LogsSSEHandler`).
Container logs are the same idea for a different source — an external
`docker compose logs` process instead of skipper's in-process log records.

Non-goals: log persistence/indexing (Loki's job), download/export, full history
beyond the tail buffer, log-based alerting, a merged multi-stack view.

## Decision

### Live-streamed, hybrid backlog + follow

A log surface opens with a **backlog** (the last N lines, one bounded read)
for an instant picture, then **follows** live. Both come from
`docker compose logs`: the backlog is `--tail N` run to completion, the follow
is `--follow` streamed. Snapshot-only was considered and rejected — the health
poller already refreshes on a cadence, so a static log next to a live health
pill reads as stale, and skipper already has the SSE plumbing to do better.

### Transport: SSE, one endpoint

```
GET /api/container-logs/{stack}?tail=200                          # whole stack, services merged
GET /api/container-logs/{stack}?tail=200&service=api              # one service (compose prefix dropped)
GET /api/container-logs/{stack}?tail=200&service=api&service=db   # a subset (prefix kept)
```

`text/event-stream`; each `data:` frame is one log line. The initial tail is
replayed as the first frames, then live lines follow. `tail` is clamped to
[1, 1000], default 200.

Plain `/api/logs` is **already taken** by skipper's own-process log stream, so
the container-log route is `/api/container-logs`. For the same reason the new
package is `internal/containerlogs`, not `internal/logs` (too close to
`internal/logbuf`).

### One stream per viewer

Only one log can be open in the UI at a time: opening a log closes any other
open one. So a viewer holds **at most one** `docker compose logs --follow`
child. This removes any need for a concurrent-stream limit or queue — the old
stream's context is cancelled (killing its child) the moment a new log opens.

### Streaming seam: a consumer-side interface, not a change to `command.Runner`

`command.Runner.Run` is run-to-completion; nothing else needs a long-lived,
line-delivering exec. Rather than widen that interface, `internal/containerlogs`
defines its own small seam:

```go
// LogStreamer runs name+args in dir and delivers each output line to onLine
// until the process exits or ctx is cancelled.
type LogStreamer interface {
    Stream(ctx context.Context, dir string, name string, args []string, onLine func(line string)) error
}
```

The real implementation wraps `os/exec` (stdout+stderr piped, scanned by line —
the same splitting `command.lineWriter` already does for the deploy sink). Tests
inject a fake that emits canned lines and honours `ctx` cancellation. This keeps
the streaming concern in the package that owns it and keeps the deploy path's
`Runner` untouched.

### Compose invocation reuses the deploy rules

The compose file always comes from the repo clone and `--project-directory`
from `working_dir` (Invariant 1), derived by the **same** helper the deploy
path uses (`stackRun`) so the two cannot drift. Flags:
`logs --no-color --no-log-prefix --tail N --timestamps [--follow] <service>`.
For the whole-stack endpoint `--no-log-prefix` is dropped so compose labels each
line with its service.

### Lifecycle and safety

- **UI-gated**: the endpoints exist only with `ui_enabled: true`. Headless
  instances expose nothing new.
- **Validation before argv**: `{stack}` must be in the current stack set
  (`CurrentStacks`, discovery-aware — Invariant 8); `{service}` must appear in
  the stack's current health snapshot; `tail` is clamped. Anything else → 404.
  Nothing user-supplied reaches the argv unvalidated.
- **Disconnect** cancels the request context, which kills the child (context
  exec) — no orphaned `logs -f`.
- **Shutdown** never blocks on a log stream: the child is tied to the request
  context and abandoned, consistent with the "never block shutdown on a child"
  rule (ADR-0014). Read-only — no interaction with the deploy mutex or state.
- **Reconnect**: EventSource reconnects automatically; the client passes the
  last seen timestamp so the server resumes with `--since <ts>`, avoiding a
  re-replayed backlog and duplicate lines.

### Secrets

Logs can contain secrets an app prints at startup. Accepted for the target
deployment (UI already behind front-auth, LAN-only); nothing is stored
server-side — skipper pipes and forgets. Called out in `docs/`.

### UI surface

A terminal/console icon opens the log on every log-bearing surface, in **both**
views:

- **Deploys** — per stack on the deploy row (merged, service-prefixed); per
  container on each service line of the health panel.
- **Stacks** — per stack on the card head (merged); per container on each
  container row.

Every open log offers the same controls: **live/pause**, **auto-scroll**
(follow the tail), **line-wrap** toggle, **in-log search**, and **fullscreen**.
Backlog selector 50/200/1000. When a log is open the search box filters *within
it* — non-matching lines hidden, matches highlighted and counted (on mobile the
existing search icon retargets to the open log). The existing "Logs" view gains
the same fullscreen + search + line-wrap (it already has auto-scroll and
oldest-first), so all log surfaces share one control set.

## Consequences

- A new `internal/containerlogs` package (validation + argv + clamp + the
  `LogStreamer` seam + the SSE handler) and one UI panel type reused across
  four entry points plus a fullscreen overlay.
- The "one log at a time" rule bounds server load to one follow child per
  viewer with no bookkeeping.
- No new config beyond the existing `ui_enabled` gate.
- Phase-2 space intentionally left: download/export and a merged multi-stack
  view remain out; Loki stays the answer for retention and search-at-scale.

## Amendment (2026-07-25): multi-service filter

The per-service scope moved from a `/{service}` path segment to a repeatable
`?service=` query param, so a viewer can narrow the merged stream to **several**
services at once (`docker compose logs` accepts multiple service args), not just
one. The prefix rule generalises: exactly one selected service drops
`--no-log-prefix` (unambiguous scope), while zero or several keep the compose
prefix so each line stays labelled. Query over path means the multi-host peer
proxy forwards the selection for free (it already passes the query string
through), and it collapses the previous path-form + whole-stack routes into one
endpoint. The UI surfaces it as a collapsible chip row (`clog-svcs`) behind a
funnel tool, suppressed for stacks with fewer than two services.
