# ADR-0013: Child process output is tee'd through the log pipeline

Status: accepted
Date: 2026-07-10

## Context

The web UI gains a live log view streaming skipper-cd's `slog` output. The
most valuable lines for diagnosing a failed deploy are what `docker
compose`, `git`, and `nixos-rebuild` actually print — but `ShellRunner`
wires the child's stdout/stderr straight to the process stdout/stderr,
bypassing the logger entirely. Capturing them requires touching the exec
layer, which every deploy runs through.

## Decision

`ShellRunner` gets an optional consumer-side sink interface:

- `command.LineSink` (`ChildLine(cmd, stream, line string)`) is defined in
  `internal/command`; the package stays free of any log-buffer knowledge.
  `internal/logbuf.Log` implements it (entries at level INFO with `cmd` and
  `stream` attrs — many tools write normal progress to stderr, so stderr
  lines are not errors; the attr preserves the distinction).
- With a sink set (`NewShellRunnerWithSink`), `Run` tees the child's
  stdout/stderr via `io.MultiWriter` through a line-splitting writer into
  the sink. The output still reaches the process stdout/stderr, so
  journald logging is unchanged.
- `Output` tees stderr only: its stdout is data returned to the caller
  (e.g. `git show`), not log output.
- The `cmd` attr carries the command name only (`docker`, `git`,
  `nixos-rebuild`) — full argv could leak tokens embedded in URLs or env.
- The line writer's `Write` never returns an error and caps unterminated
  lines at 8 KiB: `io.MultiWriter` aborts the child's writes on the first
  error, and a logging problem must never fail a deploy.
- Capture is wired up only when `ui_enabled: true`; the buffer is
  in-memory only (bounded ring, no persistence). `GET /api/logs` shares
  `/api/events`' trust level — unauthenticated. Child output can be more
  sensitive than deploy events; this is accepted for a LAN tool.

## Consequences

- The `Runner` interface and all test fakes are unchanged; only the
  `ShellRunner` constructors of `internal/git` and `internal/deploy` grow a
  trailing sink parameter (nil = previous behavior).
- Slow SSE subscribers drop lines under bursty child output (non-blocking
  publish); the bounded backlog replay recovers recent context.
- A sink implementation must never block or log via `slog` — it runs on
  the deploy path and inside the tee handler's world (recursion risk,
  documented in `internal/logbuf`).
