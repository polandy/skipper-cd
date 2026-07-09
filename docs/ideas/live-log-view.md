# Feature idea: live log view in the web UI

Status: idea (documented 2026-07-09, not yet planned/implemented)

## Motivation

The web UI today only shows deploy events (status, changed files, diffs).
To diagnose why a deploy failed, or to see what `docker compose` or
`nixos-rebuild` actually printed, you currently have to SSH into the host and
read `journalctl`. The UI should therefore gain a second view ("screen") that
shows all lines logged by skipper-cd, live.

## Requirements

- A toggle in the existing UI: "Deploys" (today's view) ↔ "Logs".
- Shows all `slog` output from skipper-cd (level, timestamp, message,
  structured attributes).
- Live updates without reload; new lines appear immediately (like deploy
  events do today via SSE).
- Opening the view shows a bounded backlog (ring buffer, e.g. the last
  500–1000 lines), then switches to the live stream.
- No JS framework; the UI stays a single embedded `index.html` (invariant
  from `internal/ui/UI_SPEC.md`).

## Rough implementation sketch (fitting the existing architecture)

- Capture: a custom `slog.Handler` acting as a tee — keeps writing to
  stdout/journal and additionally writes into a new log buffer. Set as the
  default logger in `main.go`.
- Distribution: reuse the pattern from `internal/events` (broadcaster with
  non-blocking publish + bounded history). Either generalize it or add a
  parallel `LogBroadcaster`/`LogBuffer` pair; unlike deploy history,
  persistence is probably not needed (in-memory ring only).
- Transport: a new SSE endpoint, e.g. `GET /api/logs` in `internal/ui`,
  analogous to `SSEHandler` (replay the buffer, then live events, keepalive,
  Last-Event-ID).
- UI: tab/view toggle in `index.html`; monospaced log lines, level coloring,
  auto-scroll with a "follow" toggle; optionally a level filter.

## Open questions (to resolve before implementation)

- Buffer size, and whether log history should survive a restart (probably
  not).
- Should the raw stdout/stderr of child processes (`docker compose`, `git`,
  `nixos-rebuild`) be captured too? Today they bypass the logger and go
  straight to stdout/stderr (`ShellRunner`) — capturing them would require
  the runner to pipe that output through the logger/buffer. This is likely
  the most valuable part of the feature, but also the most invasive.
- Access control: the UI is unauthenticated today; logs can be more
  sensitive than deploy events.

## Working approach

Implement test-first (specify handler/broadcaster behavior as tests, as in
`internal/events`); record architecture decisions (e.g. runner output
capture) as an ADR in `docs/adr/`.
