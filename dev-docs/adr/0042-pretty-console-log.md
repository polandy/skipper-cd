# ADR-0042: Pretty console output (default `log_format`)

Status: accepted
Date: 2026-07-20

## Context

The web UI's Deploys view already narrates a run clearly (status pills,
hooks badge, per-stack timeline), but a headless/console operator watching
`journalctl -u skipper` or a plain terminal only ever saw plain logfmt:
`msg="deploying stack" stack=nextcloud changed_files=[...]`, one line per
`slog` call, no visual structure, and — since `skipping stack, no changes
detected` was logged at Debug — no visible confirmation a stack was even
considered. There was also no single place showing what skipper is watching
(stack names, hooks, watch dirs) short of reading the config/repo directly.

Non-goals: replacing `text`/`json` (both stay, for log shippers and other
machine consumers — Loki, journald structured fields); streaming raw
`docker compose` stdout line-by-line (that already exists for the UI via
`internal/containerlogs`, a separate live-follow mechanism, not this log
stream); a TUI or any interactive console.

## Decision

### A third `log_format` value, `pretty`, becomes the default

`internal/config`: `LogFormatPretty = "pretty"` joins `text`/`json`;
`Config.LogFormat` defaults to it instead of `text`. `text` and `json` are
unchanged and remain first-class — swapping the default doesn't touch their
handlers.

### A dedicated `slog.Handler`, not a template over the existing ones

New package `internal/prettylog`. `Handler.Handle` renders one colored,
icon-led line per `slog.Record`. A small table (`anchors` in `render.go`)
recognizes skipper's own deploy-lifecycle message strings — `"deploying
stack"`, `"deploy complete"`, `"deploy failed but rolled back"`,
`"running deploy hook"`, `"self-heal: stack restored"`, and so on — and gives
each a custom icon, color, and a hand-picked subset of its attrs (e.g.
`deploying stack` shows the stack name and a file-count summary, not the
noisy `dir`/`project_dir` paths). Any message *not* in the table — which is
most of the codebase, including every `internal/rollout` canary line, every
`internal/webhook`/`internal/git` line, anything a future package adds —
still renders cleanly via a level-based fallback (icon by Debug/Info/Warn/
Error) and indents under its stack if it carries a `stack` attr. A log line
is never dropped or garbled just because it isn't in the table.

The message text is intentionally duplicated as map keys rather than shared
via constants imported into `internal/deploy`/`internal/git`/etc.: this is a
display-layer package, and it must not gain influence over core packages'
log wording. A message that drifts out of sync simply falls through to the
generic renderer — a cosmetic regression, not a broken log line — so the
coupling is one-directional and low-risk.

Color is basic 16-color ANSI (`\x1b[3Xm`), not 256-color: the viewer's own
terminal theme remaps the 16-color palette (so it looks right against
whichever theme — catppuccin, gruvbox, ... — the operator actually runs),
where a fixed 256-color/hex palette would just as often clash. Color
auto-disables (icons stay) when the destination isn't a terminal
(`os.ModeCharDevice`) or `NO_COLOR` is set (https://no-color.org) — a file
redirect or `journalctl > file` never gets raw escape codes.

### Two new narration points, wired from `cmd/skipper/main.go`, not `internal/deploy`

1. **Stack roster.** `logStackRoster` (`cmd/skipper/roster_log.go`) logs one
   `"stacks resolved"` header plus one `"stack discovered"` line per stack
   (hook counts, watch dirs), plus — when any exist — one `"stacks disabled"`
   line naming the stacks parked via `disabled: true` (stack-discovery mode
   only; a static host `stacks:` list has no such concept). These are new
   `prettylog.Msg*` messages, exported as constants so the emitter (`main.go`)
   and the matcher (`prettylog/render.go`) can't drift apart (unlike the
   core-package anchors above, this vocabulary is owned by the pretty feature
   itself, so sharing it costs nothing). Fired once: a static host `stacks:`
   list is known at process start, so `main` calls it immediately; in
   stack-discovery mode (ADR-0034, Invariant 8) neither the stack set nor the
   disabled names are known until the first sync resolves them, so a
   `sync.Once`-guarded call (fed by `deployer.CurrentStacks()` and
   `CurrentDisabledStacks()`) sits in `PostRunHook` instead, making every call
   after the first a no-op in both modes.
2. **Run summary.** A new `runTally` (`cmd/skipper/runtally.go`) is wired as
   an unconditional `eventSinks` consumer (independent of `UIEnabled`, same
   reasoning as notifications/audit — ADR-0020) and counts each run's
   terminal per-stack statuses. `PostRunHook` flushes it into one
   `prettylog.MsgRunComplete` line: `"2 deployed · 1 rolled back · 1
   skipped"`, tone (✓/↺/✗) driven by the worst outcome present. Self-heal's
   `Healed`/`HealExhausted` are deliberately never counted — that path never
   runs through `DeployAllStacks`/`PostRunHook` (it's driven by the health
   poller, serialized on the same mutex but a separate call path) and already
   has its own `self-heal: ...` log lines, so folding it in here would either
   double-narrate it or, worse, get attributed to the wrong run. The two
   reserved pseudo-stacks (`ConfigStateKey`, `NixosStateKey`) get a narrow,
   documented exclusion for the same reason: a stack-discovery config failure
   or a NixOS rebuild failure both abort `DeployAllStacks` *before* it reaches
   `PostRunHook` (Invariants 4/8), so an uncounted failure for either would
   otherwise sit in the tally and land in whichever *later* run happens to
   flush next — `runTally.observe` special-cases exactly those two escape
   paths (verified against every `emit`/`emitDeployFailure` call site; not a
   guess) and otherwise counts `NixosStateKey` like any stack.

Neither addition touches `internal/deploy`'s control flow — both are
built from data already flowing out of it (`stacksNow()`, the existing
`eventSinks`/`PostRunHook` seams), keeping the pretty-log feature purely
additive at the `cmd/skipper` wiring layer. The one exception is a one-line
change in `internal/deploy/deploy.go`: `"skipping stack, no changes
detected"` moves from `slog.Debug` to `slog.Info`, so a skip is visible by
default in every format (not pretty-specific — `text`/`json` gain the same
visibility).

## Consequences

- New `internal/prettylog` package (`Handler`, `render.go`'s anchor table,
  `attrs.go`'s ordered flattening) plus `cmd/skipper/roster_log.go` and
  `cmd/skipper/runtally.go`.
- `log_format` defaults to `pretty` for new installs; existing configs that
  set `log_format: text` or `log_format: json` explicitly are unaffected —
  only the *unset* default changes.
- One behavior change outside the new code: the "no changes" skip line is
  now Info, not Debug, in every format, matching the pitch of always being
  able to see what a run considered without waiting for a real deploy.
- No new dependency: color/TTY detection is `os.File.Stat().Mode()`, no
  `golang.org/x/term` (keeps `vendor/` unchanged, `vendorHash = null` intact).

## Amendment (2026-08-02): the idle-run narrative moves back to Debug; no verbosity key

The decision above promoted `skipping stack, no changes detected` from Debug
to Info so a run's consideration of every stack was always visible. Measured
against a real instance (29 stacks, `reconcile_interval_seconds: 300`), that
made the *idle* case dominate the log: of 573 buffered entries over 75
minutes, 464 (81%) were skip lines and another 64 the per-tick sync/run
header pair — 92% of the log described nothing happening, and the two lines
that carried real signal were one eviction away from falling out of the ring
behind `/api/logs`.

The premise was right for a single run watched live and wrong for a process
that reconciles on a timer. So `skipping stack, no changes detected`,
`starting deploy run` and `pulling latest commits` return to `slog.Debug`,
and `git reset --hard` gains `--quiet`, which suppresses its per-sync
`HEAD is now at <sha> <subject>` child line. An idle run now costs one line:
the `run complete` summary, which already carries the skip count.

### No `log_level` key

The obvious companion — a `log_level` config key restoring the full
narrative — was built and then deliberately dropped. Reasons:

- **The level is a property of what skipper has to say, not of the host.** A
  key makes the deploy narrative depend on how an instance happens to be
  configured, so the same run reads differently on two hosts and every
  consumer (console, journald, the web UI, a screenshot in an issue) has to
  ask which setting produced it.
- **The web UI is the better control surface.** It filters what it already
  holds, per viewer, with no restart — where a config key costs a config
  change plus a service restart to answer "show me only the failures", and
  is global rather than per-viewer.
- **Turning it *up* would undo the fix.** The ring behind `/api/logs` is the
  UI's whole history; a host left at debug refills it with idle-run chatter
  and evicts the deploys again. The measurement above is the argument
  against making that a switch someone can flip.

The four demoted lines stay `slog.Debug` rather than being deleted, matching
what the codebase already does with its other stand-down diagnostics
(`reconcile tick skipped`, `self-heal skipped`, `healthwatch baseline`):
below the fixed threshold, but present for anyone attaching a lower-level
handler in a test or a debugging build. The prettylog anchor table keeps its
entries for all four for the same reason.

What is *not* changed: the skip is still reported everywhere it was before
this ADR — as a `skipped` deploy event, in the run tally, and in the UI's
per-stack state. Only the per-stack log line moved.
