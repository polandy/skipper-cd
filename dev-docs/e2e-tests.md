# End-to-End Test Specification

Authoritative spec for skipper-cd's end-to-end (E2E) tests. Read this before
adding or changing E2E coverage. Pure logic lives in unit tests (see below) —
E2E owns only the **wiring and the through-line journeys** the unit tests
cannot see.

**Primary goal: quality-assure the Web UI requirements.** The UI is a
self-contained app shell (`internal/ui/static/index.html` plus same-origin
embedded assets — fonts and the extracted `app-helpers.js`, ADR-0035) whose
contract is
[`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md).
The pure, DOM-free helpers in `app-helpers.js` have their own fast unit layer —
`node --test` (`make ui-unit`, its own CI job) — so E2E need not re-verify
formatting/classification edge cases. A dependency/version bump
or an edit to that file can silently break a control, an SSE→DOM render, a
badge, or the drawer. The UI E2E layer exercises the **real rendered UI against
the real backend** so those breaks fail CI. Coverage spans all four UI masks,
asserting **behaviour + visual snapshots**.

> Status: **Go layer landed; Playwright UI project scaffolded, UA1 green.** The Go
> pipeline harness and P1–P10 (§4.1) exist under `e2e/` behind the `e2e` build tag,
> with a dedicated `e2e` CI job (§7). The UI product-code prerequisites are done and
> recorded in `UI_SPEC.md`: the `data-testid` set (§3) and the embedded self-hosted
> fonts (§5). The Playwright project (`e2e/ui/`) is scaffolded — a Node twin of the
> Go harness drives the real binary — with **UA1** (row lifecycle), **UA2** (all
> six rendered status badges), **UA3** (skipped deploys never render a row), **UA4** (time-mode
> toggle + persistence), **UA5** (stack icon + monogram fallback), **UA7** (files-pill panel toggle),
> **UA8** (diff-panel fetch + colouring), **UA9** (error-panel tied to the
> failed row with its message), **UA10** (empty-state placeholder for a
> stack-free, event-free instance), **UD5** (header version label from
> `/api/version`), **UB1** (deploys↔logs view toggle + persistence), **UB2**
> (log lines + INFO/WARN/ERROR level badges), **UB3** (sort toggle
> newest↔oldest + persistence), **UB4** (follow toggle autoscroll +
> persistence), **UB5** (stack-prefix on deploy lines + `[docker]`
> cmd-prefix on captured child output), **UB6** (diff pill on `deploy
> complete` lines expanding the diff panel below the line), and **UB7** (log
> stream recovers from a fatal error via the page's own backoff retry) passing, plus **UC1**
> (header autosync control mirrors global state live from the `autosync` SSE
> event) and **UC2** (pending pill). Mask C is now complete — **UC3**
> (drawer open/close), **UC4** (global switch → POST + live header mirror),
> **UC5** (per-stack switch → POST, reflected on reopen), **UC6** (queued list in
> deploy order with position/reason/count/wait), **UC7** (stack filter:
> substring/clear/Esc), **UC8** (enable drains the queue, disable runs no deploy),
> **UC9** (queued row + `paused:` tag, superseded on resume), and the
> override-collapse cases **UC10**/**UC11**/**UC12**. Mask D (global chrome) is
> likewise complete — **UD1** (theme toggle + persistence + no-flash), the
> theme-picker cases **UD6**/**UD6b** (per-browser override + auto-hiding
> mismatch notice, switcher on) and **UD7** (switcher off by default: picker
> hidden, saved override ignored), **UD2** (connection indicator
> connected→reconnecting→connected, driven by a kill/relaunch of the binary on
> the same port), **UD10** (recovery from a *fatal* stream error the browser
> won't retry), **UD3** (deploy indicator names the active stack while held),
> **UD4** (responsive ≤700px), **UD5** (build identity label), **UD8** (view-options popover), **UD9** (theme glyph). All four masks' behaviour is landed, **and the visual-snapshot
> baselines (§5) too**: a lean set of six baselines (deploys table, diff panel,
> autosync drawer, both themes, mobile layout) generated and compared in
> Playwright's pinned container, gated by `RUN_SNAPSHOTS`. The `e2e-ui` CI job now
> runs inside that container. A later **Mask E** (§4.6) adds the PWA update-banner
> journey — **UE1** (accept → reload onto the new build) and **UE2** (dismiss
> keeps the current version) — driven by relaunching onto a second binary whose
> service worker carries a new version. A later **Mask F** (§4.7) covers the
> upcoming-deploys look-ahead — **UF1** (trail), **UF2** (run panel), **UF3**
> (mobile `+N` chip) — driven by a multi-stack run held on its first stack. **Mask
> G** (§4.8) covers the deploys stack search — **UG1** (type-to-reveal + filter +
> Esc fold), **UG2** (no-match note), **UG3** (mobile popover entry) — a
> client-side filter over the startup deploy rows. **Mask H** (§4.9) covers the
> stack-health pill — **UH1** (pill per stack), **UH2** (per-service panel),
> **UH3** (newest row per stack), **UH4** (health-watch status history:
> age + phase timeline + deploy-correlated commit chip, ADR-0031), **UH5**
> (an exited on-demand container reads stopped + labelled, ADR-0027 amendment) —
> driven by a health poller scripted through the stub docker's `ps` output. **Mask I** (§4.10) covers the diff panel's commit
> metadata + variant-A row binding — **UI1** (commit header), **UI2** (row/panel
> binding), **UI3** (multi-commit range) — driven by webhook image bumps. **Mask
> J** (§4.11) covers the rolled-back error panel — **UJ1** (error box bound to its
> row), **UJ2** (diff carried on the rolled-back event, bar continuous through
> row → panel → error) — driven by a failing `up` that rolls back. **Mask K**
> (§4.12) covers self-heal — **UK1** (a degraded stack is restored, `healed`
> row), **UK2** (a stack that stays unhealthy exhausts into `heal_exhausted`) —
> driving the real self-heal loop off the scripted health poller. **Mask L**
> (§4.13) covers the one-open-panel-per-row rule — **UL1** (the health panel and
> the files/diff panel swap each other out, in both click orders). **Mask M**
> (§4.14) covers the per-stack deploy-history panel (audit log, ADR-0033) —
> **UM1** (the history button opens a panel of the stack's terminal outcomes
> from `/api/audit`), **UM2** (the button is on the newest row per stack only),
> **UM3** (the panel is mutually exclusive with the diff panel) — driven by real
> deploys (startup + a webhook bump). **Mask N** (§4.15) covers the self-heal row
> detail (ADR-0029 amendment) — **UN1** (a `healed` row carries a self-heal badge,
> not a files pill; clicking it opens a panel that notes there is no diff and
> lists the drifted service but not the healthy one), **UN2** (the heal panel
> obeys the one-open-panel-per-row rule against the health panel) — driving the
> real self-heal loop with a mixed healthy/unhealthy service snapshot.

## 1. Scope & boundaries

**Layered coverage.** Each layer owns what only it can prove; no layer
re-tests a lower layer's logic.

| Layer | Owns | Where |
| --- | --- | --- |
| Unit | Pure logic, exact docker/git argv via injected `Runner` fakes | `internal/**/*_test.go` |
| Integration | Real `git` against a local repo; real `os/exec` boundary | `internal/git/integration_test.go`, `internal/command/runner_test.go` |
| E2E (pipeline) | The real compiled binary wiring: config → servers → webhook → git sync → change detection → docker invocation → `state.yaml` → SSE/metrics/healthz | `e2e/` |
| **E2E (UI)** — *primary* | The embedded Web UI rendering live SSE state in a real browser: every mask's behaviour + visual baseline | `e2e/ui/` (Playwright) |

**In scope**
- The real `skipper` binary as a subprocess, real `git` from a local origin,
  a **scriptable stub `docker`** on `PATH` (see §3) — the backend that the UI
  is driven against.
- Every UI mask (Deploys, Logs, Autosync drawer, global chrome), asserted at
  two levels: **behaviour** (DOM state, persisted `localStorage`, server state
  via `/api/*`) and **visual snapshots** (per-mask baseline images).
- Deterministic generation of every deploy status the UI renders
  (`deploying`/`success`/`failed`/`rolled_back`/`skipped`/`queued`) via the
  scriptable stub docker + git commits (recipes in §3).

**Out of scope (owned elsewhere or excluded)**
- Re-asserting exact docker/git argv permutations — unit tests own this.
- A real Docker daemon / real containers — replaced by the stub docker.
- `nixos-rebuild` — needs a NixOS host + transient systemd unit; unit-tested
  only (`internal/nixos`); **n/a** for E2E.
- Real webhook providers — payloads are crafted and HMAC-signed directly.

## 2. Tool choice & rationale

**Backend driver: a Go test that runs the real binary as a subprocess.**
No new Go dependency; matches the repo's "real commands are the deliberate
exception" stance. Running the compiled binary (not an in-process mux) exercises
`main.go` flag parsing, config load, startup sync, and graceful shutdown. It
serves double duty: it is both the pipeline E2E suite (§4 P-cases) and the
backend the Playwright UI suite drives.

- **Stub `docker` on `PATH`, real `git`.** A generated `docker` script records
  argv and exits with a scriptable outcome (§3), keeping E2E hermetic and fast.
  `git` runs for real against a local origin repo. The exact argv is already
  unit-covered; E2E's value is the wiring and the UI it feeds.

**UI: Playwright + visual snapshots.** The UI's whole job is rendering live SSE
state and interactive, `localStorage`-persisted controls — genuinely
browser-shaped and unassertable at the HTTP layer. Playwright drives it against
the real binary. Snapshots additionally guard pure CSS/theme regressions a
behaviour assertion would miss. Snapshot flakiness is controlled per §5.

## 3. Test environment & fixtures

Shared harness (Go `e2e` package + a Playwright project pointed at the same
binary):

- **Origin repo** (`t.TempDir()`): `git init -b main`, one or more stack dirs
  each with a `docker-compose.yml`, committed. Tests advance it with new commits
  to simulate pushes. (Mirrors `makeOriginRepo` in `git/integration_test.go`.)
- **Scriptable stub `docker`** on a temp dir prepended to `PATH`. Behaviour via
  env vars, so one stub drives every UI status:
  - always appends `"$@"` to `$DOCKER_LOG` (one line per invocation);
  - `STUB_DOCKER_FAIL_ON=<subcmd>` → exit non-zero when args contain that
    subcommand (e.g. `up`);
  - `STUB_DOCKER_FAIL_NTH_UP=<n>[,<n>…]` → fail on the listed `compose … up`
    invocations (one value lets a rollback `up` succeed while the deploy `up`
    fails → `rolled_back`; a list also fails a health-gated rollback `up` →
    `rolled_back_unhealthy`);
  - `STUB_DOCKER_HOLD_UP=<path>` → block on `up` until the file appears, so the
    `deploying` state can be observed, then released.
  - `STUB_DOCKER_ECHO=<line>` → print `<line>` to stdout on `up`, so the
    captured child-process output reaches the log ring (drives the `cmd-prefix`).
- **Config file** (`e2e.yml`, temp dir): `repo_url` → local origin,
  `repo_dir`/`stacks_base_dir` → temp dirs, `ui_enabled: true`, free
  `port`/`metrics_port`, known `webhook_secret`, small
  `command_timeout_seconds`. State dir is `filepath.Dir(repo_dir)`.
- **Binary**: built once (`go build -o` a temp path), launched with
  `-config e2e.yml`; readiness gated by polling `/healthz`; torn down with
  SIGTERM + wait.
- **Signed-webhook helper**: HMAC-SHA256 hex over the JSON body →
  `X-Gitea-Signature`.

**Deterministic status recipes** (how each UI status is produced through the
real backend):

| Status | Recipe |
| --- | --- |
| `success` | commit a stack change → signed webhook → stub `up` exits 0 |
| `skipped` | webhook again with no new commit |
| `deploying` | `STUB_DOCKER_HOLD_UP` set → row stays `deploying` until released |
| `failed` | first deploy of a stack, `STUB_DOCKER_FAIL_ON=up` (no prior commit → rollback unavailable → `failed`) |
| `rolled_back` | one successful deploy first (sets `LastDeployedCommit`), then a change with `STUB_DOCKER_FAIL_NTH_UP=1` (initial `up` fails, rollback `up` succeeds) |
| `rolled_back_unhealthy` | like `rolled_back`, but the stack has a `health_check:` section and `STUB_DOCKER_FAIL_NTH_UP` lists the deploy **and** rollback `up` (e.g. `2,3` counting the startup deploy) — the health-gated rollback `up` fails too |
| `queued` | pause autosync (config-as-code or `POST /api/autosync`), then commit + webhook |

**Selector prerequisite (`data-testid`).** The UI has stable `id`s but no
`data-testid`, and rows are keyed by dynamic `event_id`. Add a refactor-proof
`data-testid` set and record it in `UI_SPEC.md`. Playwright selects only on
these, never on text/CSS. Minimum set:

- Deploys: `deploy-row` (+ `data-stack`, `data-status`), `status-badge`,
  `files-pill`, `files-panel`, `diff-panel`, `error-panel`, `empty-state`,
  `stack-icon`, and a `time-cell`/`duration-cell` marker for snapshot masking.
- Logs: `log-line` (+ `data-level`), `level-badge`, `stack-prefix`,
  `cmd-prefix`, `diff-pill`.
- Autosync: `autosync-btn`, `pending-pill`, `autosync-drawer`, `global-switch`,
  `stack-switch` (+ `data-stack`), `queue-item` (+ `wait-cell` for masking),
  `stack-filter`.
- Chrome: `view-toggle`, `view-options` (+ `time-mode`, `log-sort`,
  `follow-logs`), `theme-toggle`, `conn-indicator` (+ `data-state`),
  `deploy-indicator`.

## 4. Test cases (Given/When/Then)

### 4.1 Pipeline (Go, `//go:build e2e`)

Backend wiring, independent of the browser. These also validate the harness the
UI suite reuses.

- **P1 — Smoke: webhook triggers a real deploy** *(harness proof)*. Given the
  binary runs against a local origin with one changed stack. When a signed
  `POST /webhook` for `main` arrives. Then `202`; stub docker log shows
  `compose … up -d` for the stack; `state.yaml` records its hashes.
- **P2 — Unchanged stack skipped.** After P1, an identical webhook → no new
  `up -d`; SSE event `skipped`.
- **P3 — Startup sync.** A pending change present at boot deploys on startup
  without any webhook.
- **P4 — Invalid signature → 401**, stub log empty.
- **P5 — Wrong branch → 200 ignored**, no deploy.
- **P6 — `/healthz`** 200 on good sync, 503 with the error on a failing sync.
- **P7 — `/metrics`** exposes `skipper_webhooks_received_total` and a
  deploy-triggered counter for the stack.
- **P8 — Rollback** (`STUB_DOCKER_FAIL_NTH_UP=1`): stub log shows the rollback
  `up -d` against the previous compose; SSE event `rolled_back`.
- **P9 — Autosync-paused → queued**: SSE `queued`, no `up -d`, `state.yaml`
  unchanged for the stack, `/api/queue` lists it.
- **P10 — Health watch journey** (ADR-0031, `STUB_DOCKER_PS_FILE`): with a
  `health_watch` block and a local generic target, the baseline observation
  never alerts; flipping the stub's `compose ps` output to `unhealthy` POSTs a
  `"type": "health"` alert (deploy-correlated via the startup deploy), flipping
  back POSTs the recovery, and `healthwatch.yaml` records the phases. This is
  the config→wiring→watcher→alerter through-line the unit tests cannot see.

### 4.2 UI — Maske A: Deploys-View (Playwright)

- **UA1 — Row lifecycle.** Given the UI is open. When a stack deploys
  (`deploying` held, then released to `success`). Then a `deploy-row` for the
  stack appears newest-first, shows `deploying`, then mutates in-place to
  `success` (same row, not a duplicate). *Snapshot: table after success.*
- **UA2 — Status badges.** Drive each rendered status via §3 recipes. Then each
  `deploy-row` carries the correct `data-status` + `status-badge`
  (`success`/`failed`/`rolled_back`/`rolled_back_unhealthy`/`queued`/`deploying`).
  (No snapshot — the six statuses need separate instances; the badges are
  behaviour-asserted here and the table's dark/light rendering is snapshotted by
  UA1/UD1.)
- **UA3 — Skipped deploys never render.** An unchanged stack emits a `skipped`
  event, but no `deploy-row` is created for it (proven by ordering against a
  later real deploy); there is no skip-filter control.
- **UA4 — Time mode.** Toggle switches Time cells relative↔absolute and persists
  across reload (`localStorage timeMode`).
- **UA5 — Stack icon + monogram fallback.** A resolvable icon renders in
  `stack-icon`; a stack whose icon 404s falls back to the monogram chip (no
  broken image).
- **UA7 — Files pill.** For an event with `changed_files`, clicking `files-pill`
  inserts `files-panel` below the row; clicking again removes it.
- **UA8 — Diff panel.** For a `has_diffs` event, clicking the pill fetches
  `/api/events/{id}/diffs` and renders `diff-panel` with additions/deletions/
  hunk colouring. *Snapshot: diff panel.*
- **UA9 — Error detail.** A `failed` event shows `error-panel` with the message.
- **UA10 — Empty state.** A fresh UI with no events shows `empty-state`.

### 4.3 UI — Maske B: Logs-View

- **UB1 — View toggle.** `deploys`↔`logs` switches the visible view and persists
  (`localStorage activeView`).
- **UB2 — Log lines + level badges.** Log SSE lines render as `log-line` with the
  correct `data-level` / `level-badge`. Driven end-to-end against the real
  backend: a failing startup deploy yields INFO + ERROR lines and a bad-signature
  webhook adds a WARN. DEBUG is out of scope — the default slog handler filters
  below INFO and skipper has no log-level toggle, so it can never reach the ring.
  (No snapshot — real log output is nondeterministic; see §5.)
- **UB4 — Follow toggle.** Following (the default) pins the pane to the newest
  edge when a fresh line streams in; unfollowing leaves the scroll position
  alone. Driven against the real backend: the ring is filled so the pane
  overflows, then live lines (bad-signature webhooks) are streamed and the pane
  is asserted to snap to `scrollTop 0` only while following. Persists
  (`localStorage followLogs`).
- **UB5 — Prefixes.** A structured deploy line with a `stack` attr renders the
  accent `stack-prefix` next to its level badge; a child-process line
  (`cmd`+`stream`) renders the muted `cmd-prefix` and no level badge. Both come
  from one failing startup deploy against the real backend: `STUB_DOCKER_ECHO`
  makes the stub `docker` print a line on `up` (captured as a `[docker]` child
  line, `data-level="cmd"`), while the same failing `up` emits the `[web]`
  stack-tagged ERROR line — the two prefix shapes are asserted end-to-end.
- **UB6 — Diff pill in logs.** A `deploy complete` line carries the deploy event's
  `event_id`, rendered as a `diff-pill`; clicking it fetches
  `/api/events/{id}/diffs` and inserts the same `diff-panel` directly below the log
  line, and clicking again collapses it — the log-view twin of UA8. Driven against
  the real backend: a second deploy (webhook bumping the compose image) is the first
  with a prior commit to diff against, so its `deploy complete` line — the newest,
  hence topmost in the fixed newest-first order — is the one whose pill expands a
  populated panel (`nginx:1.26`), not the plain "No diff recorded" note.
- **UB7 — Fatal-stream recovery.** The `/api/logs` stream has no connection
  indicator, so a fatal error must recover silently. A `page.route` fulfils
  `/api/logs` with a 503 so the reconnect closes `EventSource` for good; after a
  kill/relaunch the pane resumes only if the page's own capped-backoff retry
  re-opens the stream. Proven by the WARN-line count *growing* past the pre-drop
  line (the persisted old line alone would pass a mere presence check). Fails
  without the manual retry — the browser never comes back from CLOSED. The events
  twin is UD10.

### 4.4 UI — Maske C: Autosync-Drawer

- **UC1 — Header state.** The `autosync-btn` header control shows global autosync
  on/off, exposed as `data-global` ("true"/"false") — the machine-readable twin of
  its amber "paused" styling. Driven against the real backend: global sync is
  flipped via `POST /api/autosync` from a separate client, so the browser learns of
  it only through the server's broadcast `autosync` SSE event; the header mirrors it
  live and again after a reload, proving it reflects server state, never
  `localStorage`.
- **UC2 — Pending pill.** The amber `pending-pill` is hidden when nothing is
  queued. Pausing autosync and then pushing a change defers the paused stack, so
  the server registers it as pending and broadcasts a `queue` event; the pill
  appears carrying the live queue count. Resuming autosync drains the queue and
  the pill hides again — it tracks server queue depth over SSE, not a client
  guess.
- **UC3 — Drawer open/close.** Clicking the control opens `autosync-drawer`;
  `Esc` and outside-click close it.
- **UC4 — Global switch.** Toggling `global-switch` posts
  `POST /api/autosync {scope:"global"}`; the header mirrors the new state live.
- **UC5 — Per-stack switch.** Toggling a `stack-switch` posts
  `{scope:"stack",stack}`; state reflected on reopen.
- **UC6 — Queued list.** Paused + change → `queue-item`s appear in deploy order
  (`_nixos` first) with position, name, reason chip (`global`/`stack`), file
  count, and wait time. *Snapshot: drawer with queued items (wait cell masked).*
- **UC7 — Stack filter.** Typing filters the stack list (case-insensitive
  substring); a clear button appears; `Esc` clears the field first, then closes
  the drawer; a no-match state shows when everything is excluded.
- **UC8 — Enable drains, disable does not.** Enabling (global or stack) triggers
  a deploy run that drains the queue (stub `up` runs, pending→0); disabling only
  updates state (no `up`).
- **UC9 — Queued row + tag.** A `queued` event yields a `deploy-row` with the
  `queued` badge and a `paused:` tag; a later real deploy supersedes it.
- **UC10 — Re-enable does not pin (override collapse).** Global on. Pausing a
  stack via its `stack-switch` and then resuming it must leave **no** sticky
  override: a subsequent global-off pauses that stack along with the rest,
  proving the resume collapsed the override back to inherit rather than pinning
  an explicit `true`. Driven entirely through the rendered switches.
- **UC11 — UI pause does not survive a global off→on cycle.** A stack paused only
  via the UI (`stack-switch`) resumes after the global switch is turned off and
  back on: turning global off makes its baseline `off`, the override collapses,
  and global-on resumes it. The chosen master-switch semantic
  ([ADR-0019](adr/0019-autosync-ui-overrides-collapse-to-inherit.md)) — a UI
  pause is an exception to the current global baseline, not an independent latch.
- **UC12 — Collapse through the stack filter.** With several stacks and a
  `stack-filter` query narrowing the list, toggling a *filtered* stack targets
  the right stack and preserves the query and the matched subset (the other
  stacks stay hidden). Flipping global from a separate client re-renders the
  filtered list live over SSE without dropping the filter, and the collapse holds
  through the filtered/live view: a stack paused while filtered resumes after a
  global off→on cycle once the filter is cleared.

### 4.5 UI — Maske D: Global chrome

- **UD1 — Theme toggle + no-flash.** `theme-toggle` switches the configured
  palette's dark↔light variant and persists (`localStorage colorScheme`); after
  reload the root `light` class is applied before first paint (no flash). Runs
  with the default config (theme switcher off), so the snapshots capture the
  fixed-theme header. *Snapshots: dark + light.*
- **UD6 — Theme picker + per-browser override.** With `ui_theme_switcher`
  enabled, `theme-select` switches the active palette instantly (a plain
  `data-theme` attribute change, no reload) and persists a non-default choice as
  a local `themeOverride`; the choice survives a reload, `data-server-theme`
  (the configured value) is never touched, and picking the configured theme
  again clears the override. While an override is active a dismissible
  `theme-notice` names the mismatch. Started with `themeSwitcher: true`.
- **UD6b — Mismatch notice auto-hide.** Playwright's virtual clock
  (`page.clock`) fast-forwards past the notice's 6s timer to assert it hides
  itself, deterministically and without a real wait. Switcher enabled.
- **UD7 — Theme switcher off (default).** With `ui_theme_switcher` unset the
  `theme-select` is absent and a `themeOverride` seeded in `localStorage` stays
  dormant: after a reload `data-theme` remains on the configured theme and no
  notice appears, so a locked-down deployment keeps its at-a-glance colour.
- **UD2 — Connection indicator.** `conn-indicator` shows `connecting`→
  `connected`; killing/restarting the binary drives `reconnecting`→`connected`.
- **UD10 — Fatal-stream recovery.** Where UD2 exercises the browser's built-in
  retry after a transient drop, this covers a *fatal* stream error: a
  `page.route` fulfils `/api/events` with a 503 so the reconnect closes
  `EventSource` for good (the browser stops retrying). The indicator holds
  `reconnecting`; lifting the route and relaunching the binary must let the
  page's own capped-backoff retry re-open the stream → `connected`. Fails
  without the manual retry (the browser never comes back from CLOSED).
- **UD3 — Deploy indicator.** Shows the active stack name(s) while a deploy is
  held, `idle` otherwise.
- **UD4 — Responsive ≤700px.** At a 390px viewport the **compact header**
  (UI_SPEC §Responsive) does not overflow the viewport width
  (`documentElement.scrollWidth ≤ clientWidth`) and the `brand-name` wordmark is
  hidden; the **table** collapses to the 2×2 layout, the Files column hides, and
  tapping a row with changed files expands the panel. The 1280px control asserts
  the wordmark is visible with likewise no sideways scroll. *Snapshot: mobile
  layout.*
- **UD8 — View-options popover.** The view-specific toggles (`time-mode` for
  deploys; `log-sort` + `follow-logs` for logs) live in the `view-options`
  popover opened from the *active* view button, not the header row — so
  switching views never surfaces or hides a header control. They stay hidden
  until the popover opens, which shows only the active view's group; the toggle
  works from inside it, and Esc / outside-click dismiss it.
- **UD9 — Theme glyph.** The header `theme-toggle` is glyph-only on every
  viewport; the `.tg-moon` / `.tg-sun` glyphs reflect the mode (moon in dark →
  sun in light) and flip on toggle.
- **UD5 — Version label.** The header `brand-version` shows the deployed version
  as `v<semver>`. `globalSetup` injects the version via `-ldflags` from
  `.release-please-manifest.json` (the same source as the Docker/Nix builds), so
  the case asserts the ldflags → `GET /api/version` → header render through-line
  against the exact shipped version (release-proof: both sides read the manifest).

### 4.6 UI — Maske E: PWA update banner

The installable PWA prompts to reload when a newer version has been deployed
(ADR-0023). Driving this end-to-end needs a *second* build: the banner fires only
when the browser installs a service worker whose bytes differ, and the build
identity is baked into the `sw.js` cache name. So `beforeAll` builds an updated
binary (`-ldflags` version/commit differing from `globalSetup`'s) with `go`, and
the cases relaunch onto it on the same origin/ports. When `go` is unavailable
(the snapshot-regeneration container mounts a prebuilt `SKIPPER_E2E_BIN`), the
cases `test.skip` — they carry no snapshots, so nothing is lost there. Both start
stack-free (`stacks: []`): the PWA surface needs no deploys.

- **UE1 — Accept + reload.** Open the app on the shipped build: a service worker
  takes control, no `update-banner` shows, and `brand-version` reads the shipped
  `v<ver> · <commit>`. Relaunch onto the updated build and call
  `registration.update()` (the same check the app polls on load / visibility
  regain). Then the `update-banner` appears with its "A new version…" text;
  clicking `update-banner-reload` posts `SKIP_WAITING`, and the resulting
  `controllerchange` reloads the page **once** onto the new build — asserted by
  `brand-version` flipping to the updated identity and the banner clearing.
- **UE2 — Dismiss.** Same journey to the visible banner; clicking
  `update-banner-close` hides it and, crucially, leaves `brand-version` on the
  **old** identity — no `SKIP_WAITING`, so the worker stays waiting and the page
  never reloads.

### 4.7 UI — Maske F: Upcoming-deploys look-ahead

The header surfaces the stacks that will deploy *next in the current run*
(ADR-0024): a look-ahead trail beside the active stack, and a read-only run
panel. Driving this needs a **multi-stack run held mid-flight**: the three stacks
(`web`, `api`, `db`) are all changed and pushed together, then the stub docker's
`up` is held (`hold()`), so the run blocks on the first stack (`web`) with the
rest still to come. The `upcoming` snapshot is emitted by the real backend, so
nothing is faked. Behaviour-only (no snapshot), like Maske E.

- **UF1 — Look-ahead trail.** While the run is held, the indicator's `aria-label`
  reads `deploying web · next api, db` and the visible `deploy-next` trail names
  the upcoming stacks. Releasing the `up` drains the run: the trail empties and
  the indicator returns to `idle`.
- **UF2 — Run panel.** Clicking the indicator while idle does nothing (the panel
  has nothing to show). During the held run, clicking it opens `run-drawer`
  listing the run in deploy order — the active `web` row carries `.active` and
  "deploying now", `api`/`db` follow. Ending the run (release) closes the panel.
- **UF3 — Mobile fallback.** At 390px the verbose trail is hidden and the
  upcoming stacks collapse to the `deploy-count` `+N` chip (`+2`); the names stay
  in `aria-label`.

### 4.8 UI — Maske G: Deploys stack search

A client-side type-to-search filter over the deploy rows by stack name. Three
stacks (`web`, `api`, `db`) deploy on startup, giving three success rows to
filter; nothing is faked. Behaviour-only (no snapshot). The reveal state is read
from `deploy-filter-wrap` (collapsed to zero height until revealed).

- **UG1 — Desktop type-to-search.** The bar (`deploy-filter-wrap`) starts hidden.
  Typing `api` reveals it, focuses `deploy-filter` with the seeded query, and
  leaves only the `api` row visible (`web`/`db` hidden). First `Esc` clears the
  field (all rows back) but keeps the bar open; a second `Esc` folds it away.
- **UG2 — No match.** Typing `zzz` hides every row and shows the
  `deploy-filter-empty` note echoing the query; clearing the field restores the
  rows and hides the note.
- **UG3 — Mobile entry point.** On desktop the deploys view-options popover has
  no `deploy-search` row (type-to-search covers it). At 390px the row appears;
  tapping it closes the popover, reveals the bar, and focuses the field, after
  which typing `db` filters exactly as on desktop.

### 4.9 UI — Maske H: Stack health (ADR-0027)

The live per-stack health pill. The poller is enabled with `healthPoll: 1` and
each stack's `docker compose ps --format json` output is scripted through the
stub docker (`skipper.setStackHealth`), so it is deterministic and offline — no
real docker, no real containers. Behaviour-only (no snapshot).

- **UH1 — Health pill per stack.** Three stacks (`web`, `db`, `cache`) get
  scripted `ps` output — running/healthy, running/unhealthy, exited(0) — and each
  stack's newest row shows a `health-pill` whose `data-health` is the rolled-up
  status (`healthy` / `unhealthy` / `stopped`).
- **UH2 — Per-service panel.** A stack with a healthy and an unhealthy service
  rolls up to `unhealthy`; clicking the pill opens the `health-panel` listing
  every `health-service`, and clicking again removes it.
- **UH3 — Newest row per stack.** Health is a current value, so with the pill on
  a stack's row, a pushed change that prepends a second row leaves exactly one
  pill — on the newest (first) row, not the older one.
- **UH4 — Status history (ADR-0031).** With the health watch on (`healthWatch:
  true`, debounce 1, riding `healthPoll: 1`): a service with only its baseline
  phase shows the inline age but **no** `health-history`; after a webhook deploy
  (records the commit context) and a `ps` flip to unhealthy, reopening the panel
  shows the timeline — two `health-phase` rows newest-first (`unhealthy`,
  `healthy`) with a 7-hex `health-phase-commit` chip on the deploy-correlated
  unhealthy phase only (the commit-less baseline carries none).
- **UH5 — On-demand container (ADR-0027 amendment).** A stack with
  `on_demand_containers` (`onDemand` start option) whose scripted `ps` shows the
  on-demand container `exited` with code 137 next to a healthy sibling: the
  pill stays `healthy` (the intended idle never degrades the rollup), and the
  panel shows the service as `stopped` with `exited · on-demand` in its state
  cell while the sibling's state stays plain. Covers the
  config→StackRef→probe→snapshot→label through-line the unit tests cannot see.
- **UH6 — Keyboard operability.** The pill is a real `<button>`: it takes focus
  (`Tab`-reachable) and `Enter` toggles the per-service panel open and closed —
  the keyboard twin of UH2's clicks.

### 4.10 UI — Maske I: Diff-panel commit metadata + row binding

The diff panel's commit header and its variant-A binding to the deploy row. A
webhook that bumps a stack's image commits against the startup commit, so the
deploy carries a real diff **and** commit metadata; the harness commits as author
`e2e` with message `bump <stack> to <tag>`, making the header deterministic.
Behaviour-only (no snapshot — UA8 already snapshots the diff colouring).

- **UI1 — Commit header.** After a bump deploy, opening the diff panel shows a
  `diff-head` that echoes the row (`web` + `deploy diff` + a `success` pill) and
  the deployed commit's subject (`bump web to 1.26`), author (`e2e`) and 7-char
  short SHA.
- **UI2 — Variant-A binding.** Opening the panel marks the row `diff-open` and the
  panel `bound` with `data-status="success"`, and the panel is the row's direct
  sibling (shared, unbroken left bar); clicking again clears both.
- **UI3 — Multi-commit range.** Two commits before a single deploy render the
  newest as the headline (`bump web to 1.27`) and a `commits-pill` reading
  `2 commits` that toggles the collapsed `diff-commit-list` (both commits listed).

### 4.11 UI — Maske J: rolled-back error panel binding + diff

The rolled-back error box's variant-A binding and the diff now carried on the
terminal event. Startup up#1 succeeds (sets `LastDeployedCommit`); the deploy's
up#2 fails and the rollback up#3 succeeds → `rolled_back` (`STUB_DOCKER_FAIL_NTH_UP=2`).
Behaviour-only (no snapshot).

- **UJ1 — Error box binding.** The rolled-back row's error panel carries
  `data-status="rolled_back"` and is the row's direct sibling, so the shared left
  bar is unbroken (the message reads as attached to its row, not a floating card).
- **UJ2 — Diff carried + continuous bar.** The rolled-back event now reports its
  diff, so the row is `data-has-diffs="1"`; opening the files pill inserts a
  `bound` `diff-panel` (`data-status="rolled_back"`) between the row and the error
  panel, so the DOM order is row → `diff-panel` → `error-panel` — one unbroken bar.

### 4.12 UI — Maske K: Self-heal (ADR-0029)

The real self-heal loop, driven end-to-end through the running binary. The health
poller (`healthPoll: 1`) reports a stack degraded via the stub docker's scripted
`ps` output, and self-heal (`selfHeal: true`) restores it with a corrective `up`.
`initialHealth` seeds each stack healthy *before* boot so the first poll does not
read the freshly-deployed-but-unscripted stack as `stopped` and heal it
spuriously; `self_heal_min_unhealthy_polls` / `_cooldown_seconds` are lowered so
the loop resolves in a couple of polls. Behaviour-only (no snapshot — UA2 already
snapshots badge colouring, and the two new badges are covered there in spirit).

- **UK1 — Restore.** A stack seeded healthy turns unhealthy; within a poll
  self-heal runs a corrective `up` (a real extra `docker … up`, asserted via
  `dockerUps`) and a `healed` row appears. `max_attempts: 5` so it never exhausts.
- **UK2 — Give up.** A stack that stays unhealthy after its one allowed heal
  (`max_attempts: 1`) trips the circuit breaker: a single `heal_exhausted` row
  with the give-up error in its `error-panel`, emitted once (not per poll).

### 4.13 UI — Maske L: one open panel per deploy row

The health panel and the files/diff panel are mutually exclusive on a row:
opening one closes the other, so the layout never depends on click order and the
variant-A row binding is never split across two panels. A webhook image bump
puts a diff on the newest row while the health poller (scripted stub) puts the
health pill on the same row. Behaviour-only (no snapshot).

- **UL1 — Mutual exclusion, both orders.** With the health panel open, clicking
  the files pill swaps in the diff panel (row `diff-open`, not `health-open`);
  clicking the health pill swaps the health panel back (row `health-open`, not
  `diff-open`). The surviving panel is always the row's direct sibling.

### 4.14 UI — Maske M: per-stack deploy history (ADR-0033)

The newest row per stack carries a history button that opens a panel of the
stack's durable terminal deploy outcomes, fetched from `/api/audit`. The panel
joins the one-open-panel-per-row rule (Maske L). The harness runs the real
backend, so the records come from real deploys: the startup deploy plus a
webhook image bump give two `success` records. Behaviour-only (no snapshot).

- **UM1 — History panel content.** Clicking the newest row's `history-btn` opens
  the `audit-panel` (row gains `audit-open`, panel is its direct sibling) with
  two `audit-row`s, newest first, each `data-status="success"` and carrying the
  deployed commit's short SHA. A second click closes it.
- **UM2 — Newest row only.** With two `web` rows, only the newest carries the
  `history-btn`; the older row has none (the button is a current per-stack value,
  like the health pill).
- **UM3 — Mutual exclusion with the diff panel.** Opening the history panel then
  the files/diff panel swaps in the diff panel (row `diff-open`, not
  `audit-open`); opening the history button again swaps it back.
- **UM4 — No orphaned panel on a queued row.** With autosync paused, a pushed
  change renders a queued `web` row (the stack's newest, so it carries the
  history button); the panel is opened on it, and resuming autosync drains the
  queue — the queued row is superseded by the real deploy and the open
  `audit-panel` is removed with it instead of stranding in the table.

### 4.15 UI — Maske N: self-heal row detail (ADR-0029 amendment)

A `healed` row is not a git deploy, so it has no changed files and no diff. Its
files cell instead carries a teal **self-heal badge** (`heal-pill`) that expands a
detail panel explaining the corrective redeploy and listing the services that had
drifted when it ran (from the `heal_drift` carried on the event). Builds on the
real self-heal loop of Maske K: `initialHealth` seeds two services healthy before
boot, then one service (`app`) is turned unhealthy while the other (`db`) stays
healthy — the rollup goes unhealthy so self-heal fires, but only the degraded
service is listed. Behaviour-only (no snapshot).

- **UN1 — Badge + drift panel.** The healed row carries a `heal-pill` (not a
  `files-pill`); clicking it opens the bound `heal-panel` (`data-status="healed"`)
  whose text notes there is no diff and lists the drifted service `app`
  (`unhealthy`) but not the healthy `db`. A second click toggles it closed.
- **UN2 — One panel per row.** With the health panel open on the healed row,
  clicking the self-heal badge swaps in the heal panel — the health panel is
  gone, never two panels under one row (the rule of Maske L).

### 4.16 UI — Maske O: stack discovery surface (ADR-0034)

The harness boots in discovery mode (`discovery` start option): the origin's
stack dirs are the stack set, `stack_discovery: true` replaces the `stacks:`
list, and the repo-root `skipper.yaml` (committed via the option's
`repoConfig`, mutated later with `setRepoConfig`) carries the per-stack
overrides. Behaviour-only (no snapshot) — the disabled line is hidden when
empty, so the existing visual baselines are untouched.

- **UO1 — Disabled line.** With `wip` parked via `disabled: true`, the deploys
  view shows no `wip` row but a `disabled-stacks` line with one `wip` chip;
  the line is hidden in the logs view and returns with the deploys view.
- **UO2 — Broken repo config.** Pushing a `skipper.yaml` with a syntax error on
  line 3 produces a `failed` row for the reserved `_config` stack whose error
  panel carries the parse error plus the marked excerpt (`> 3 |`).

### 4.17 UI — Maske P: blocked rows + hostile-name escaping (ADR-0032)

The first UI coverage of the `blocked` status, doubling as the escaping guard
for repo-controlled stack names (in stack-discovery mode a stack name comes
from the deploy repo, so it must never be interpreted as markup). `app` depends
on a stack literally named `dep<img>x` (`dependsOn` start option, the Node twin
of the Go harness's `startSkipperOrdered`); `STUB_DOCKER_FAIL_NTH_UP: '3'`
fails the dependency's redeploy after the two startup deploys. Behaviour-only
(no snapshot).

- **UP1 — Blocked row renders the reason as text.** A webhook run that changes
  both stacks fails the dependency and blocks `app`: the blocked row's tag reads
  the literal `blocked by dep<img>x` and contains no injected `<img>` element,
  and the dependency's own row shows the hostile name verbatim in its stack
  cell.

### 4.18 UI — Maske Q: orphan detection (ADR-0036)

The UI coverage of the Orphans section. The instance boots in discovery mode
(`web` + `api` are the stack set) with the health poll on, since detection rides
that cadence and is UI-gated (`HasSubscribers`). The stub's `docker ps -a` /
`docker volume ls` listing is scripted with the new `setOrphans`/`setVolumes`
harness helpers, keyed off `skipper.stacksBaseDir` so the working_dir
classification is deterministic: the two active stacks (managed), a removed
stack still running under `stacks_base_dir` (orphaned, two containers), and a
hand-started project outside it (unmanaged). Behaviour-only (no snapshot).

- **UQ1 — Detection lists orphaned + unmanaged, expandable.** The section
  appears with a count of 2 — the managed `web`/`api` are matched by working_dir
  and excluded. Opening it shows the two items with the right `data-class`;
  expanding the orphaned row reveals its two containers and the data-safety facts
  (compose path, named volumes tagged `kept on prune`).
- **UQ2 — The deploy search scans orphans.** A term only an orphan *container*
  carries (the redis image) auto-opens the section, auto-expands the matching
  orphan with the hit, and hides the non-matching one; the count badge shows `1`
  and the search's hits/total counts orphans among the searchable elements
  (`1/4`). Switching the query to the unmanaged project's name flips the match;
  a term nothing matches drops the badge to `0`.
- **UQ3 — Expansion survives a refresh.** A manually expanded orphan is not
  re-collapsed when fresh orphan data lands: the section re-renders every poll,
  but the expansion is tracked client-side (`orphansOpen`), so a scripted status
  change re-renders the row and it stays open.

### 4.19 UI — Maske R: Stacks roster view (stack-roster-spec)

The third top-level view: an inventory of the full stack set skipper owns (stack
discovery, ADR-0034) with each stack's last outcome — as opposed to the deploy
table's event log — rendered as an aligned table that reuses the deploy table's
row/column/expand language (`dev-docs/ui-design-concept.md`). Boots in discovery
mode with `stacks: ['api', 'web', 'wip']`, a repo-root `skipper.yaml` parking
`wip` (`disabled: true`), and `healthPoll` + `initialHealth` for api/web so the
containers panel is populated. Behaviour-only (no new snapshot; the default
deploy view is unchanged, so the full-page `ud-chrome` baselines still hold).
The `never deployed` state is unit-tested (`internal/roster`) — in discovery mode
the first sync deploys every stack, so it is not deterministically seedable in
e2e — and shares its `.roster-flag` rendering with the disabled row.

- **UR1 — Aligned inventory table.** The stacks view shows an aligned column
  header (`Stack · Status · Last deploy · Commit`, no count/title line) and one
  row per declared stack (api, web deployed → success badge; wip parked →
  `.disabled` row with the `disabled` flag and no badge), enabled sorted before
  disabled; the deploy table is hidden and restored on switching back.
- **UR2 — Click a row for containers + history.** Clicking a row expands the
  stack into its containers panel (`health-panel` with a `health-service`, from
  the health snapshot) above its deploy-history panel (`data-audit-for` = stack);
  clicking again closes both, and opening another row closes the first (one stack
  at a time).
- **UR3 — Search.** A printable key reveals the filter and seeds it; a substring
  match narrows the rows with a `shown/total` count; a no-match query shows the
  empty note; first `Esc` clears, second folds the bar away.
- **UR4 — Mobile search entry.** On a narrow viewport the stacks view-options
  popover carries a desktop-hidden "Search stacks" row that reveals and focuses
  the filter, which then narrows the rows the same way.
- **UR5 — Shared time mode.** The stacks popover's `Absolute time` toggle (the
  shared `timeMode`) switches the roster's relative times to absolute.

### 4.20 UI — Maske S: cross-view stack jump

The compass jump button beside every stack name — deploy row and roster row
alike — that switches between the Deploys and Stacks views and lands on that
stack's row there (`internal/ui/UI_SPEC.md#cross-view-stack-jump`). Two
`test.describe` blocks: the default two-stack boot (`api`, `web`) for the
landing/regression cases, and a discovery+disabled boot (mirroring Maske O/R's
`wip` fixture) for the no-landing-target case. Behaviour-only — no new
snapshot, but the jump-btn's footprint on every row required regenerating
`deploys-table.png` and the full-page `theme-dark`/`theme-light`/
`mobile-layout` baselines (§5).

- **US1 — Deploys → Stacks.** Clicking a deploy row's jump button switches to
  the Stacks view and its (sole) roster row for that stack is visible and
  briefly carries `.jump-target`, which clears again on its own.
- **US2 — Stacks → Deploys lands on the newest row.** With two deploys of the
  same stack (two rows, newest first), jumping from the roster flashes
  `.jump-target` on the first (newest) row only — never the older one.
  Where the roster view is *inventory* (one row per stack), the deploy table
  is a *log*, so "the stack's row" is ambiguous there and the newest wins.
- **US3 — The jump doesn't also open the row it sits on.** A regression guard:
  the jump button and the row's own click-to-open-panel handler are on the
  same delegated listener, so the jump must pre-empt it. Neither `diff-open`
  (deploy row) nor `audit-open` + a rendered `audit-panel` (roster row) appear
  after a jump click.
- **US4 — No landing target.** A parked (`disabled: true`) stack has a roster
  row and a jump button, but no deploy row (it has never deployed). Jumping
  from it still switches to the Deploys view; there is simply nothing to land
  on or flash.
- **US5 — A leftover filter in the target view doesn't hide the landing row.**
  Checked in both directions: filter the target view down to a *different*
  stack, leave it filtered, switch views by hand (not by jumping), then jump
  to the filtered-out stack from the other view. The jump clears the stale
  filter so the landing row is actually visible, not `.filtered-out`.

### 4.21 UI — Maske T: Container logs (ADR-0037)

A live `docker compose logs` panel opened from a console icon, per stack (merged)
and per container. Boots with `healthPoll: 1` and `setStackHealth` so the
per-container icons appear on the health-panel service lines and the `{service}`
segment validates; the stub `docker` answers `compose … logs` with a fixed
backlog (a single service drops the compose prefix, the whole stack keeps
`<stack>-1  | `). Behaviour-only (no new snapshot beyond the shared deploy-table
baselines the row icon already shifts).

- **UT1 — Per-stack panel.** The row's `clog-btn` opens a `clog-panel` that
  streams the backlog (the merged view keeps the `web-1` service prefix) and
  shows the `clog-live` pill; clicking the icon again closes it.
- **UT2 — Per-container panel.** Opening the `health-panel` (health pill) and
  clicking a `health-service` line's `clog-btn` opens that one service's log
  (scope `web / app`, no compose prefix).
- **UT3 — Single log open.** Opening a second log (another stack's row icon)
  closes the first — at most one `clog-panel` exists at a time.
- **UT4 — In-log type-to-search.** With a log open, typing routes into its search
  (the `deploy-filter` is *not* revealed): the matching line highlights
  (`.clog-hit`), the rest hide, and the hit count shows.
- **UT5 — Logs-view controls in the popover.** The Logs view carries
  `log-search` / `log-wrap` / `log-fs` in the view-options popover; typing
  reveals the `log-filter` bar (like the deploys/stacks views).
- **UT6 — Logs-view wrap + fullscreen.** `log-wrap` and `log-fs` are popover
  toggles that light (`.active`) when engaged.

### 4.22 UI — Maske U: Deploy hooks (ADR-0038)

The hooks UI surface: the per-stack **hooks badge** + command panel, the hook
output attributed in the log, and the **running-hook phase** + inline hook log.
Boots in **discovery mode** with a repo `skipper.yaml` that declares hooks for
`web` — harmless `echo`s (they succeed and their stdout is attributed to the
stack), plus a `sleep` for the running-hook cases so the phase is observable.
The running-hook masks use `readiness: 'listening'` so the page loads while the
deploy is still in the hook (a deterministic window; the `sleep` is well under
the 30s command timeout). No real docker — hooks run via real `sh -c`.
Behaviour-only: the visual-snapshot masks configure no hooks, so the badge/phase
never appear in a baseline (and the status cell only stacks `:has(.hook-phase)`),
so no baseline shifts and no new snapshot is added.

- **UU1 — Badge + panel.** `web`'s `hooks-badge` shows the split `2+1` count and
  a `pre-deploy hook: 2` title; `api` (no hooks) has none. Clicking opens the
  `hooks-panel` listing the three `hooks-cmd` lines verbatim; clicking again
  closes it.
- **UU2 — Log attribution.** In the Logs view, filtering to `web` shows the
  hook's `echo` output as a `log-line` with a `[web]` `stack-prefix` — the
  attribution that lets the hook log filter by stack.
- **UU3 — Running phase + inline log.** With a `sleep` hook in flight, the
  deploying `web` row shows the `hook-phase` (`pre_deploy hook …`) and a pulsing
  `hooks-badge` (`data-hook-active`). The phase's console icon opens the
  container-logs panel **in skipper mode** inline (the `deploys-table` stays
  visible — no page jump), streaming the stack-filtered `/api/logs` (the
  `clog-body` shows the hook output).
- **UU4 — Phase in the roster.** The same running-hook phase renders on the
  Stacks `roster-row` for `web`, identical to the Deploys view.

## 5. Visual snapshot strategy

Snapshots are Playwright `toHaveScreenshot` baselines, deliberately scoped to a
lean set of high-value per-mask anchors, not every case. The landed baselines:

| Baseline | Anchor case | Target | Masked |
| --- | --- | --- | --- |
| `deploys-table.png` | UA1 | `deploys-table` | `time-cell`, `duration-cell` |
| `diff-panel.png` | UA8 | `diff-panel` | — (static diff) |
| `autosync-drawer.png` | UC6 | `autosync-drawer` | `wait-cell` |
| `theme-dark.png` / `theme-light.png` | UD1 | full page | `time-cell`, `duration-cell` |
| `mobile-layout.png` | UD4 | full page (390px) | `time-cell`, `duration-cell` |

The **Logs pane (UB2) is deliberately not snapshotted**: real deploy log output
is nondeterministic (line count, tmp paths, commit SHAs), so even with the text
masked the pane's layout diffs run-to-run. UB2's value — the level-badge mapping
— is fully covered by its behaviour assertions.

- **Determinism controls:** CSS animations/transitions are disabled globally
  (`toHaveScreenshot: { animations: 'disabled' }` in `playwright.config.ts`),
  viewports are pinned, and dynamic regions (`time-cell`, `duration-cell`, queue
  `wait-cell`) are covered with Playwright's `mask` option so relative times /
  durations never diff. (The old header `LIVE` pulse was removed, so it no longer
  needs masking.)
- **Opt-in via `RUN_SNAPSHOTS`.** The pixel compare only runs when
  `RUN_SNAPSHOTS` is set — the `e2e-ui` CI job (and the baseline-generation run)
  set it. A local host run (`PW_CHROMIUM_EXECUTABLE` pointing at a system
  Chromium) leaves it unset, so the behaviour assertions still run but the
  screenshot is skipped: local runs compare behaviour, CI compares pixels. The
  gate lives in `e2e/ui/fixtures/snapshot.ts` (`visualSnapshot`).
- **Fonts** are **embedded and self-hosted** (`woff2` files under `static/fonts`,
  served same-origin from `/fonts/` and preloaded — ADR-0035), so there is no
  external request and no font load-timing / offline nondeterminism.
  Even so, baselines are generated and compared **in Playwright's pinned Docker
  container** (`mcr.microsoft.com/playwright:v1.61.1-noble`, matching
  `@playwright/test` in the lockfile) to fix OS-level font rasterisation.
- **Baselines** live under `e2e/ui/__screenshots__/` (via `snapshotPathTemplate`)
  and are reviewed like code; updates are explicit, never automatic.
- **Regenerating baselines** (after an intentional UI change) — run the suite in
  the same pinned container so pixels match CI. Build a static binary and mount
  it in, then update snapshots:

  ```sh
  VER=$(jq -r '."."' .release-please-manifest.json)
  CGO_ENABLED=0 go build -ldflags "-X main.version=$VER -X main.commit=e2ee2ee" -o .pw-bin/skipper ./cmd/skipper
  docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$PWD":/work -w /work/e2e/ui \
    -e SKIPPER_E2E_BIN=/work/.pw-bin/skipper -e CI=1 -e RUN_SNAPSHOTS=1 \
    mcr.microsoft.com/playwright:v1.61.1-noble \
    sh -c "npm ci && npx playwright test --update-snapshots"
  ```

  Then review the changed PNGs before committing. (`.pw-bin/` is gitignored.)

## 6. Traceability

Every UI_SPEC requirement maps to a case, or is marked already-unit-tested /
n/a. Pipeline invariants continue to map to §4.1.

| UI_SPEC requirement | Case |
| --- | --- |
| SSE row lifecycle: deploying→success in place, newest-first | **UA1** |
| Status badges (success/failed/rolled_back/queued/deploying) | **UA2**, UC9 |
| Skipped deploys never render a row | **UA3** |
| Time mode toggle + persistence | **UA4** |
| Stack icon chip + monogram fallback | **UA5** |
| Files pill expandable panel | **UA7** |
| Diff panel fetch + colouring | **UA8** |
| Error detail panel | **UA9** |
| Empty state | **UA10** |
| View toggle deploys/logs + persistence | **UB1** |
| Log lines + level badges | **UB2** |
| Sort toggle + persistence | **UB3** |
| Follow toggle + persistence | **UB4** |
| Stack prefix / child `[docker]` prefix | **UB5** |
| Diff pill on `deploy complete` line | **UB6** |
| Log stream recovers from a fatal error | **UB7** |
| Header autosync control reflects server state | **UC1** |
| Pending count pill appears/hides | **UC2** |
| Drawer open/close (click/Esc/outside) | **UC3** |
| Global autosync switch → POST, live mirror | **UC4** |
| Per-stack switches → POST | **UC5** |
| Queued list in deploy order (reason/count/wait) | **UC6** |
| Stack filter (substring/clear/Esc) | **UC7** |
| Per-stack switch is an exception, not a pin (override collapse) | **UC10**, UC11 |
| Global switch is a true master (collapse on global toggle) | **UC11** |
| Override collapse through the stack filter | **UC12** |
| Enable drains, disable does not | **UC8** |
| Queued row + `paused:` tag | **UC9** |
| Theme toggle + persistence + no-flash | **UD1** |
| Theme picker override (switch/persist/notice/clear), switcher on | **UD6** |
| Mismatch notice auto-hide (virtual clock) | **UD6b** |
| Theme switcher off (default): picker hidden, override ignored | **UD7** |
| Connection indicator states | **UD2** |
| Connection indicator recovers from a fatal stream error | **UD10** |
| Deploy indicator active/idle | **UD3** |
| Upcoming look-ahead trail (active + next) | **UF1** |
| Run panel (open on click, ordered run, close on end) | **UF2** |
| Upcoming mobile `+N` count chip | **UF3** |
| Deploys search: type-to-reveal + filter + Esc fold | **UG1** |
| Deploys search: no-match empty note | **UG2** |
| Deploys search: mobile popover entry (desktop-hidden) | **UG3** |
| Stacks roster: aligned inventory table (deployed/disabled rows + column header) | **UR1** |
| Stacks roster: click a row for its containers + deploy-history panels | **UR2** |
| Stacks roster: search (type-to-reveal, filter, empty note, Esc fold) | **UR3** |
| Stacks roster: mobile popover search entry | **UR4** |
| Stacks roster: shared time-mode toggle (relative → absolute) | **UR5** |
| Stacks roster: never-deployed synthetic state | Unit `roster` — not e2e-seedable |
| Cross-view jump: Deploys → Stacks lands on the roster row | **US1** |
| Cross-view jump: Stacks → Deploys lands on the newest row | **US2** |
| Cross-view jump: pre-empts the row's own panel-open click | **US3** |
| Cross-view jump: no landing target (never-deployed stack) | **US4** |
| Cross-view jump: clears a leftover filter that would hide the landing row | **US5** |
| Stack health: rolled-up pill per stack (healthy/unhealthy/stopped) | **UH1** |
| Stack health: per-service panel toggle | **UH2** |
| Stack health: pill on the newest row per stack | **UH3** |
| Stack health: status history — age, ≥2-phase timeline, deploy-correlated commit chip | **UH4** |
| Stack health: exited on-demand container reads stopped + on-demand label | **UH5** |
| One open panel per row (health ↔ files/diff mutually exclusive) | **UL1** |
| Responsive ≤700px: header no-overflow + wordmark hidden + table collapse + tap-to-expand | **UD4** |
| Header version label (`v<semver>` from `/api/version`) | **UD5** |
| PWA update banner: prompt on a new version, reload onto it | **UE1** |
| PWA update banner: dismiss keeps the current version | **UE2** |
| Diff API 404 / truncation limits | Unit `ui`/`deploy` — **n/a** for E2E |
| Log windowing (500 +500), buffer trim | **n/a** in v1 (fiddly; low bump-risk) |
| Favicon `prefers-color-scheme` | **n/a** (browser chrome, not page DOM) |

## 7. CI

Two opt-in jobs added to `.github/workflows/ci.yml`, pinned the way the repo
already pins actions:

- **e2e**: `go test -tags e2e ./e2e` on `ubuntu-latest` (stub docker on PATH, git
  preinstalled). Uploads the stub docker log + skipper stderr on failure.
- **e2e-ui**: runs the UI project inside Playwright's pinned container
  (`mcr.microsoft.com/playwright:v1.61.1-noble`) — which ships Node + the
  browsers — with only Go installed on top for `globalSetup`'s binary build.
  `RUN_SNAPSHOTS=1` turns on the pixel compares against the committed baselines
  (§5). Uploads the Playwright HTML report + `test-results/` (traces and snapshot
  diffs) on failure.
