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

## Amendment (2026-08-02): the console prints each changed file's diff

The decision above narrates *what happened*; it stops at the file name
(`↳ nextcloud/docker-compose.yml`). The web UI does not stop there — a
`deploy complete` line carries the event id and opens the recorded diff on
demand (`GET /api/events/{id}/diffs`). The console has no such affordance:
`journalctl` cannot fetch. So for the one surface that cannot ask for the
detail, the detail is printed:

```
14:32:08    ↳ nextcloud/docker-compose.yml
      @@ -8,7 +8,7 @@ services:
         app:
      -    image: nextcloud:30.0.4-apache
      +    image: nextcloud:30.0.6-apache
           restart: unless-stopped
```

The block is indented past the timestamp column so it reads as that file's
detail rather than as further log lines, and uses the add/remove/hunk
colours already in the palette. `+++`/`---` headers are matched before the
plain `+`/`-` cases so they render as metadata rather than as one huge
addition and removal.

### The diff travels as a log attr, and each sink decides

`internal/deploy` attaches the (already truncated) diff to the `file changed`
record rather than handing it to prettylog by some side channel — a display
layer must not gain its own data path into the core. The consequence is that
every sink sees it, and each one answers for itself:

- **prettylog** renders the block. This is what it is for.
- **`internal/logbuf`** clamps it. The ring is bounded (2000 entries) and
  every entry is streamed to every connected browser over `/api/logs`;
  10 KB payloads would evict real history and push kilobytes per line down
  the stream. The rule this establishes: **the ring carries messages, not
  payloads** — a payload has its own endpoint. The clamp counts *lines* for
  a multi-line value, because that is the unit its reader thinks in
  ("12 lines omitted"), with a byte ceiling so one runaway line cannot slip
  past the line budget.
- **text/json** carry it verbatim. A structured log is the complete record
  by definition, and a shipper that does not want it can drop the field;
  silently emitting less than the pretty format does would be the surprising
  choice.

### Bounds

Nothing new is stored or computed: the content is the diff the deploy event
already collected, under the caps that already applied to it — 10 KB per
file, 50 KB per deploy (`internal/deploy/events.go`). A test pins the logged
form to the stored one, so the console can never print more than the event
kept.

## Amendment (2026-08-02): the web UI's log view mirrors this rendering

This ADR built the narrative for the console and said it mirrors the web
UI's *Deploys* view. The web UI's own **Logs** view was left out, and it
showed the raw record — `INFO deploy complete stack=nextcloud
event_id=412`, and a run summary that was six sevenths `=0`. Two surfaces
onto the same records, one narrating and one not.

The Logs view now renders the same narrative: a status glyph in place of the
level badge, the stack, the narrated text, the detail dimmed; the run
summary lists only non-zero outcomes and takes its glyph from the worst one
present. A changed file's diff is rendered inline beneath its line, as the
console prints it.

### Two tables, on purpose

The obvious objection is that the message→narrative mapping now exists
twice: `internal/prettylog/render.go` and `internal/ui/static/app-helpers.js`.
Sharing one source would mean either shipping the table to the browser from
Go (a build step this UI does not have — ADR-0035) or letting the UI import
Go strings (it cannot). The alternative — a *different* idiom in each
surface — is what this amendment exists to remove.

So the duplication is deliberate and bounded by the rule this ADR already
set for prettylog: **a display layer owns its own strings and must not gain
influence over the core packages' log wording.** Both tables fall back the
same way — a message that drifts stops matching and renders raw, never
dropped — so drift degrades to the old rendering on one surface rather than
breaking either.

### Where the two deliberately differ

- The `[stack]` prefix keeps its brackets in the UI: there it is also the
  control that filters the log to that stack, and it must look the same on
  narrated and unnarrated lines. A synthesised label (`peer argoneon`) is
  not a control and is rendered plain.
- The console prints a file's whole diff (capped at 10 KB); the UI renders
  the copy the log ring clamped (~40 lines) and reaches the rest through the
  deploy's diff pill. The console has no pill — that asymmetry is the reason
  it prints the full one.
