# End-to-End Test Specification

Status: living reference — kept in sync with the suite as masks land.

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
> **UC9** (queued row + `paused:` tag, superseded on resume), the
> override-collapse cases **UC10**/**UC11**/**UC12**, **UC13** (a late
> snapshot never overwrites a newer one), **UC14** (the drawer is inert until it
> has server state) and **UC15** (a re-render keeps the switch nodes).
> Mask D (global chrome) is
> likewise complete — **UD1** (theme toggle + persistence + no-flash), the
> theme-picker cases **UD6**/**UD6b** (per-browser override + auto-hiding
> mismatch notice, switcher on) and **UD7** (switcher off by default: picker
> hidden, saved override ignored), **UD2** (connection indicator
> connected→reconnecting→connected, driven by a kill/relaunch of the binary on
> the same port), **UD10** (recovery from a *fatal* stream error the browser
> won't retry), **UD3** (deploy indicator names the active stack while held),
> **UD4** (responsive ≤700px), **UD5** (build identity label), **UD8** (view-options popover), **UD9** (theme glyph), and **UD11** (tap-tip opt-in on non-header controls). All four masks' behaviour is landed, **and the visual-snapshot
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
> (an exited on-demand container reads stopped + labelled, ADR-0027 amendment),
> **UH7** (routine restart folds into `up in Xs` + strip + raw-list toggle) —
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
- **Scriptable stub `docker`** on a temp dir prepended to `PATH`. It lives in
  **one file, `e2e/fixtures/docker-stub.sh`**, that both harnesses install — the
  Go one embeds it, the Playwright one reads it — so a change reaches both
  suites instead of one copy quietly drifting from the other. Behaviour via env
  vars, so one stub drives every UI status:
  - always appends `"$@"` to `$DOCKER_LOG` (one line per invocation);
  - `STUB_DOCKER_UI=1` → enable the branches only the UI suite needs (orphan
    listings, container logs, per-stack health `ps`, the app-link detector's
    labelled `ps` + label `inspect`). The Playwright harness sets it; the Go
    harness does not, and without it the stub behaves exactly as that suite's
    own copy did;
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
- **Failure attachments.** A failed test attaches the instance's stdout/stderr
  (`skipper-output`) *and* the UI's own diagnostics (`ui-notes`, read from
  `window.__uiNotes`). Both are collected **after** the test finishes, never
  subscribed to while it runs: attaching a console or network listener is itself
  enough to change the timing of a race, which is how the UC11 investigation
  repeatedly lost its own flake (T8).
- **Stolen ports retry the launch.** The harness reserves ports by binding
  port 0 and releasing again, so between the release and skipper's (or a peer
  stub's) own bind another process can take the port — they come from the same
  ephemeral range every outbound connection draws from, and parallel workers
  make that a real, ~once-per-thousand event. `Skipper.start` detects the theft
  deterministically (skipper exits with its bind error — `waitFor` fails fast
  on a dead process instead of spinning out its deadline — or a stub's listen
  rejects with `EADDRINUSE`) and relaunches on fresh ports, up to three
  attempts; any other failure propagates immediately. `relaunch()` deliberately
  keeps its ports (the browser's origin must survive), so it carries the
  residual risk.
- **Fonts settle before interaction.** The `page` fixture awaits
  `document.fonts.ready` after every navigation (`goto` and `reload`): the
  web fonts' `font-display: swap`
  is the page's one late reflow, and a swap between Playwright computing a click
  point and dispatching it moves the target out from under the click (the UC11
  root cause — the queue empty-note wraps to a second line when JetBrains Mono
  lands). Interacting with the autosync drawer additionally waits for its
  `data-settled` attribute (the open transition's end, see `UI_SPEC.md`).
- **Lint** (`make e2e-ui-lint`): type-aware ESLint over the suite. The rule that
  earns it is `no-floating-promises` — a forgotten `await` on an assertion makes
  a test pass without checking anything, and a vacuously green test is worse
  than none. Deliberately no Prettier: like `app.css`, the suite keeps its
  hand-written style.
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
| `rolled_back_unhealthy` | like `rolled_back`, but the stack has a `deploy_health_check:` section and `STUB_DOCKER_FAIL_NTH_UP` lists the deploy **and** rollback `up` (e.g. `2,3` counting the startup deploy) — the health-gated rollback `up` fails too |
| `queued` | pause autosync (config-as-code or `POST /api/autosync`), then commit + webhook |

**Selector prerequisite (`data-testid`).** The UI has stable `id`s but no
`data-testid`, and rows are keyed by dynamic `event_id`. Add a refactor-proof
`data-testid` set and record it in `UI_SPEC.md`. Playwright selects only on
these, never on text/CSS. Minimum set:

- Deploys: `deploy-row` (+ `data-stack`, `data-status`), `status-badge`,
  `files-pill`, `files-panel`, `diff-panel`, `error-panel`, `empty-state`,
  `stack-icon`, and a `time-cell`/`duration-cell` marker for snapshot masking.
- Logs: `logs-panel`, `logs-live`, `logs-stat`, `log-line` (+ `data-level`),
  `level-badge`, `stack-prefix`, `cmd-prefix`, `diff-pill`, `log-search`,
  `log-wrap`, `follow-logs`, `log-fs`.
- Autosync: `autosync-btn`, `pending-pill`, `autosync-drawer`, `global-switch`,
  `stack-switch` (+ `data-stack`), `queue-item` (+ `wait-cell` for masking),
  `stack-filter`.
- Chrome: `view-toggle`, `view-options` (+ `time-mode`), `theme-toggle`,
  `conn-indicator` (+ `data-state`), `deploy-indicator`.

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
- **UB8 — Panel controls, no popover.** The view is styled as a page-sized
  `clog-panel` (same header chrome as the Stacks/Deploys container-log popup —
  see §4.21), so `log-search` / `log-wrap` / `log-fs` sit directly in its own
  header and are visible without opening anything (`view-options` stays
  hidden). Search reveals `log-filter-wrap` via type-to-search or by clicking
  the tool; clicking the tool again closes it and clears the query (no separate
  clear button, unlike `deploy-filter`). Wrap/fullscreen light their own `.on`
  state on the tool itself; `Esc` exits fullscreen.
- **UB9 — Live/pause pill.** `logs-live` freezes the pane without dropping
  anything — unlike the container-log panel's pause, which drops lines
  outright. Two lines are streamed in (real backend, bad-signature webhooks)
  while paused and proven absent from the DOM (`log-line` count unchanged);
  going live again catches both up in one render. `logs-stat` tracks
  live/paused text in the panel footer.
- **UB10 — Live/pause pill, keyboard.** `logs-live` is a `role="button"` span
  (tabindex 0), so it must activate on Enter and Space, not only a mouse click.
  The pill is focused, `Enter` pauses it (`paused` class + `logs-stat` text) and
  `Space` resumes it — proving both activation keys are wired.

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
- **UC13 — A late snapshot never overwrites a newer one.** Autosync state reaches
  the UI over two channels (the toggle's `POST` response and the `autosync` SSE
  event), which can overtake each other. The reversal is *forced*, not awaited:
  `page.route` lets the first `POST` through to the server but holds its response
  until the DOM shows a newer state (applied over SSE from a second client).
  Releasing the stale response must not move the switch — the UI keys on the
  snapshot's `version`, not on arrival order
  ([autosync-spec.md](autosync-spec.md#snapshot-ordering)).
  **"The switch did not move" is an absence**, and asserting it right after the
  response is delivered passes *before* the page has handled the payload — green
  with or without the guard (verified: the first draft of this test passed against
  a build without the fix, and so did its inverse). The test therefore waits for
  the **drop announcement** the UI emits on the console, which is issued in the
  same synchronous step in which the switch would otherwise have been repainted;
  only then are the DOM assertions safe. Reach for the same shape whenever a test
  must observe that something *did not* happen.
- **UC14 — The drawer waits for server state.** Between page load and the first
  `autosync` snapshot the drawer has only the markup's optimistic defaults, so
  `autosync-drawer` carries `data-ready="false"` and `global-switch` is
  `aria-disabled` and does not accept a click (`pointer-events: none`). The test
  stalls `/api/events` with `page.route` to hold that window open, clicks the
  switch while it is inert, then releases the stream: the click must have *waited*
  and then landed (switch `false`, header `data-global="false"`), not been
  swallowed. Every other drawer case opens through the `openDrawer` helper, which
  waits for `data-ready="true"` — the deterministic seam that replaces hoping the
  snapshot won the race.
- **UC15 — A re-render keeps the switch nodes.** Every `autosync`/`queue` event
  repaints the drawer's lists; rebuilding them wholesale replaced the switch
  nodes, and a switch replaced between mousedown and mouseup takes the `click`
  with it (the browser fires it on the common ancestor, where the delegated
  handler finds no switch). With two stacks, an `elementHandle` is taken for
  `web`'s switch, a *different* stack is paused from a second client, and — once
  `api`'s switch has visibly flipped, proving the repaint ran — `web`'s node must
  still be `isConnected` and still toggle. This is what makes UC5/UC10/UC11's
  clicks land under load.

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
  hidden; the **table** collapses to its two-line layout (name/status over
  time/version), the Duration and Files columns hide, and tapping a row with
  changed files expands the panel. The 1280px control asserts
  the wordmark is visible with likewise no sideways scroll. *Snapshot: mobile
  layout.*
- **UD8 — View-options popover.** The view-specific toggle (`time-mode`, shared
  by deploys/stacks) lives in the `view-options` popover opened from the
  *active* view button, not the header row — so switching views never surfaces
  or hides a header control. It stays hidden until the popover opens; the
  toggle works from inside it, and Esc / outside-click dismiss it. Logs carries
  no group at all — its controls live inline in its own panel header instead
  (see UB8/UB9) — so a click on the already-active logs button opens nothing.
- **UD9 — Theme glyph.** The header `theme-toggle` is glyph-only on every
  viewport; the `.tg-moon` / `.tg-sun` glyphs reflect the mode (moon in dark →
  sun in light) and flip on toggle.
- **UD5 — Version label.** The header `brand-version` shows the deployed version
  as `v<semver>`. `globalSetup` injects the version via `-ldflags` from
  `.release-please-manifest.json` (the same source as the Docker/Nix builds), so
  the case asserts the ldflags → `GET /api/version` → header render through-line
  against the exact shipped version (release-proof: both sides read the manifest).
- **UD11 — Tap-tip opt-in on non-header controls.** A leaf-level `data-taptip`
  control (the deploy row's `clog-btn`) flashes `.tap-tip` on a synthetic touch
  `pointerdown` and auto-hides after its 1600ms timer (`page.clock`); the same
  dispatch with `pointerType: 'mouse'` shows nothing. A second case opens the
  `view-options` popover and confirms `time-mode` still stays silent even
  though it sits under the opted-in `<header>` — the `.view-options` exclusion
  overrides an opted-in ancestor.
- **UD12 — State published while the stream connects is not lost.** The stream
  carries its own baseline, so there is no window between "read the baseline"
  and "start listening". Forced, not awaited: the `/api/events` request is held
  before it reaches the server, a deploy is queued while nothing is subscribed,
  and only then is it released — the pending pill must still appear. Fails
  against a build that fetches the baseline separately, where the queued deploy
  reached nobody and was never re-sent (that gap is what made **UC8** flaky).

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
- **UH7 — Folded timeline.** A routine restart cycle (`ps` flips healthy →
  starting → healthy, each accepted phase awaited via the pill) does **not**
  grow a `starting` line: the timeline folds it into the current healthy line
  as `· up in Xs` (two `health-phase` rows, not three), the `health-strip`
  renders one segment per phase, and the `health-fold-toggle` (`all 3 phases`)
  swaps the folded view for the verbatim `.hp-raw` list — starting in the
  middle — and back (UI_SPEC "Status history").

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
UM5/UM6 cover the panel's fold — runs of routine outcomes collapse into one
summary line, with the verbatim list behind a toggle (UI_SPEC "Deploy history").

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
- **UM5 — Routine outcomes fold.** Five deploys of `web` (startup plus four
  image bumps, awaited through `/api/audit`): the panel shows the newest record
  as one `audit-row` and the four older ones as a single `audit-fold` line
  (`4 more successful deploys since …`, three `audit-fold-commit` chips). The
  `audit-fold-toggle` (`all 5 deploys` ⇄ `fold routine outcomes`) swaps in the
  verbatim `.ap-raw` list of five rows, flips `aria-expanded`, and its
  `aria-controls` target is visible; clicking again folds back.
- **UM6 — Nothing to fold, no control.** With only two records the panel renders
  both as `audit-row`s and carries neither an `audit-fold` line nor the toggle —
  the fold must not appear on stacks where it collapsed nothing.
- **UM7 — A repeated failure keeps its newest row.** With `STUB_DOCKER_FAIL_ON:
  up` every deploy fails, so the failing startup deploy plus three failing bumps
  give four records with one status and one error. The panel shows the newest
  failure as a full `audit-row` *including* its `.ar-err` line, and its three
  repeats as an `audit-fold` (`N more identical failures since …`,
  `data-status="failed"`); expanded rows plus folded counts account for all
  four, and the toggle still reveals them verbatim. The fold count is
  deliberately not pinned — the startup deploy fails *differently* (no previous
  commit to restore), and a different cause never merges.

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
stack dirs are the stack set (`stack_discovery: true`), and the per-stack
overrides go into the host config's `stacks:` list (ADR-0043; the option's
`repoConfig` map shape is folded into it). Behaviour-only (no snapshot) — the
disabled line is hidden when empty, so the existing visual baselines are
untouched.

- **UO1 — Disabled line.** With `wip` parked via `disabled: true`, the deploys
  view shows no `wip` row but a `disabled-stacks` line with one `wip` chip;
  the line is hidden in the logs view and returns with the deploys view.
- **UO2 — Leftover repo config.** Pushing a leftover in-repo `skipper.yaml`
  (`setRepoConfig`) — no longer read as of ADR-0043 — produces a `failed` row
  for the reserved `_config` stack whose error panel explains the file is no
  longer read, so un-migrated config never silently reverts to defaults.

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
- **UP2 — The tag yields before the stack name.** `.stack-name` carries
  `overflow: hidden`, which resolves its automatic flex minimum to zero, so it
  used to be the *only* shrinkable item in the cell while the `nowrap` tag
  refused to give way — a long dependency name clipped the stack name past its
  own ellipsis (measured: 20px against a 47px need, still clipped at a 1400px
  viewport). The name now stays whole at 1400, 1100 and 900px; the tag
  ellipsises instead and is dropped below the 1000px breakpoint, where it had
  nothing useful left to say — it shrank to a one-letter stub, which reads worse
  than absent.

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
mode with `stacks: ['api', 'web', 'wip']`, a host-config override parking
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
- **UR6 — An open panel survives a republish.** A `stacks` snapshot lands after
  every run and rebuilds the roster. With a row expanded, the panels come back
  on the same row carrying the **new** snapshot — asserted end to end: the
  change-detection lead reads `Unchanged since the last deploy.` before a push
  and `Unchanged since <sha>.` after the pushed change deployed, with no click
  in between. Without the re-open the panel is simply gone (the assertion fails
  on a missing element), which is what made UAJ1b flaky.

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

A live `docker compose logs` panel opened from a logs icon, per stack (merged)
and per container. Boots with `healthPoll: 1` and `setStackHealth` so the
per-container icons appear on the health-panel service lines and the `?services=`
selection validates; the stub `docker` answers `compose … logs` with a fixed
backlog (a single service drops the compose prefix, the whole stack and a
multi-service subset keep `<stack>-1  | `). Behaviour-only (no new snapshot
beyond the shared deploy-table baselines the row icon already shifts).

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
  (UT5/UT6, the Logs view's own search/wrap/fullscreen/live controls, moved to
  §4.3 as UB8/UB9 once the view became a page-sized `clog-panel` with those
  controls inline in its own header instead of a popover.)
- **UT7 — Per-service filter.** On a two-service stack, the funnel tool reveals
  the `clog-svcs` chip row (`all` + one per service). Selecting one service names
  the scope (`web / app`) and drops the compose prefix; adding a second gives a
  subset scope (`web / app + db`) with the prefix back; `all` returns to the
  merged stream. A single-service stack shows no filter tool (UT1 asserts its
  absence).

- **UT8 — A refused stream reads as closed.** The container-log request is
  intercepted in the browser and answered `429` (the stream cap's refusal), so
  the terminal case is staged without a server seam and without waiting on the
  clock. The footer must say `stream closed`, never `reconnecting` — `EventSource`
  does not retry a non-2xx — and the live/pause pill must follow it: `.dead`,
  labelled `closed`, and inert, so a click cannot put `live · streaming` back on
  a stream that is gone.

### 4.22 UI — Maske U: Deploy hooks (ADR-0038)

The hooks UI surface: the per-stack **hooks badge** + command panel, the hook
output attributed in the log, and the **running-hook phase** + inline hook log.
Boots in **discovery mode** with a host-config override that declares hooks for
`web` — harmless `echo`s (they succeed and their stdout is attributed to the
stack), plus a hook that blocks on the harness's hook-hold file for the
running-hook cases, so the phase stays observable until the test calls
`releaseHook()` — the same file-gate idiom as `hold()`/`release()` on `up`
(§4.7), just for a hook command instead of `compose up`. The running-hook
masks use `readiness: 'listening'` so the page loads while the deploy is
parked in the held hook. No real docker — hooks run via real `sh -c`.
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
- **UU3 — Running phase + inline log.** With the held hook in flight, the
  deploying `web` row shows the `hook-phase` (`pre_deploy hook …`) and a pulsing
  `hooks-badge` (`data-hook-active`). The phase's logs icon opens the
  container-logs panel **in skipper mode** inline (the `deploys-table` stays
  visible — no page jump), streaming the stack-filtered `/api/logs` (the
  `clog-body` shows the hook output). The test calls `releaseHook()` once done
  so the deploy settles before teardown.
- **UU4 — Phase in the roster.** The same running-hook phase renders on the
  Stacks `roster-row` for `web`, identical to the Deploys view.

### 4.23 UI — Maske V: Multi-host federated UI (ADR-0048)

The read-data fan-in: the primary (`host_name: host-a`) merges each peer's
deploys, stacks and health into one UI. The harness stands up a **reachable stub
peer** (`host-b`) — a small HTTP server serving its `/api/v1/snapshot` (curated
to `stacks`/`health`/`app_links`) and `/api/audit` — plus an **unreachable
peer** (`host-c`, a dead port), via the `peers` / `hostName` start options. No
real second skipper; the stub is enough to exercise the merge, filter and
offline paths. Behaviour-only: the multi-host surface only appears when peers are
configured, so no existing snapshot shifts, and the colour assignment is
unit-tested (`app-helpers.test.js`).

- **UV1 — Merged feed + host chips.** The `hosts-btn` is enabled and reports
  `3/3`; the feed interleaves local (`host-a`) and peer (`host-b`) rows, each
  tagged with a `host-mono` chip. Peer rows are read-only mirrors — a chip but no
  `history-btn` / `jump-btn`. The two hosts' dots render distinct palette slots
  (`data-host-color`) and distinct computed colours (the merged view's point;
  the per-theme adjacent-slot distinctness is unit-tested in `app-helpers.test.js`).
- **UV2 — Chip = click-to-filter toggle.** Clicking a `host-b` chip isolates the
  feed to `host-b` (local rows hidden, `deploys-table` gains `host-filter-active`,
  the badge reads `1/3`); clicking a dot again clears back to `3/3` and all rows.
- **UV3 — Hosts drawer multi-select.** The drawer lists one `host-row` per host
  (self + 2 peers); deselecting `host-b` drops its rows while local rows stay;
  `hosts-all-btn` ("Select all") restores every host.
- **UV4 — Offline peer flagged, not blanked.** The unreachable `host-c` gives the
  control the `has-offline` state and shows the `host-stale-banner` naming it; in
  the drawer `host-c`'s reachability dot reads `down` and `host-b`'s reads `up`, a
  peer carries an "open its own UI" `host-link` and `self` does not.
- **UV5 — Merged roster.** The Stacks view lists local and peer stacks together,
  host-tagged; a peer `roster-row` shows its status badge and host chip but no
  `jump-btn` / `clog-btn`, and the same chip-filter isolates the roster to one host.

- **UV6 — Peer row never dead-ends.** A peer row is a read-only mirror, but
  clicking it opens a `peer-detail` panel (commit + file count + status) and
  loads the peer's diff inline through the
  primary's proxy (`GET /api/peers/{name}/events/{id}/diffs`, stub-served by the
  harness `diffs`), rendered as a normal `diff-panel`. This is the case behind
  the "clicking the peer `_nixos` row does nothing" report.
- **UV7 — Filter persists across a reload.** Narrowing to `host-b` via the
  drawer, then reloading, restores the same narrowed view once the peers
  snapshot re-arrives (`host-a` rows stay hidden, badge reads `1/3`);
  clicking "Select all" and reloading again restores the unfiltered `3/3`
  view, proving the cleared filter (`localStorage` key removed) also
  survives a reload rather than resurrecting a stale narrow filter.

### 4.24 UI — Maske W: `_nixos` rebuild row (ADR-0025)

The NixOS-rebuild pseudo-stack's deploy row. The harness (`nixosRebuild`) commits
a `configuration.nix` and stubs `systemd-run` / `systemctl` to a fast success, so
the startup sync runs a (fake) `nixos-rebuild` and emits a real `_nixos` row;
`setNixConfig` + a webhook then pushes a second rebuild carrying a real git diff.
No real switch. Behaviour-only.

- **UW1 — Affordances.** The `_nixos` row has **no** `jump-btn` (it is not in the
  Stacks roster) and **no** container-logs `clog-btn` (it is not a compose
  project) — only its git diff and deploy history apply.
- **UW2 — Click never dead-ends.** A diff-bearing `_nixos` row (`data-has-diffs=1`
  + a `files-pill`) opens its diff/files panel on a row-body click. (A row with
  nothing hashed falls back to the history panel — the general never-dead-end
  rule, unit-covered by the click handler; here the realistic diff case is
  asserted.)

### 4.25 UI — Maske X: Peer health parity (ADR-0048)

The fan-in curates each peer's `health` / `healthwatch` / `app_links` (not just
its roster + deploys), so a peer's rows reach the same at-a-glance detail as a
local stack. The harness stub peer (`host-b`) serves those states in its
snapshot; local health is enabled with `healthPoll` so the parity shows on both
sides. Behaviour-only (peer rows appear only when peers are configured, so no
snapshot shifts).

- **UX1 — Inline health pill on peer deploy rows.** A peer `deploy-row` carries a
  `health-pill` with the fanned-in status (`gitea` healthy, `postgres` unhealthy)
  without expanding — the same pill local rows carry.
- **UX2 — Inline pill + app-link on peer roster rows.** In the Stacks view a peer
  `roster-row` shows the `health-pill` and an `app-link-btn` inline; local roster
  rows now carry the pill too (the roster surfaced no live health for anyone
  before).
- **UX3 — Peer expand → containers.** Clicking a peer row opens the `peer-detail`
  with the peer's `health-panel` inline (its `health-service` lines from the
  fanned-in health), each carrying a per-service `clog-btn` (its streaming
  behaviour is Maske Y).
- **UX4 — Healthwatch timeline + pill routing.** Clicking a peer row's
  `health-pill` opens its containers detail (never the primary's own local health
  panel); the fanned-in `healthwatch` renders the `health-history` /
  `health-phase` status timeline.

### 4.26 UI — Maske Y: Peer container logs (ADR-0048)

The last local-only affordance closed. The browser can't reach a peer
cross-origin, so the primary proxies the peer's container-logs SSE stream at `GET
/api/peers/{name}/container-logs/{stack}` (service selection rides `?services=`;
the streaming sibling of the diff proxy). The peer's containers panel now carries
the same per-service log button local stacks have. The harness stub peer
(`host-b`) streams canned SSE frames for `gitea` so the proxy has something to
forward. Behaviour-only.

- **UY1 — Peer container log streams through the proxy.** Expanding a peer row and
  clicking a container's `clog-btn` opens the live `clog-panel`; its `clog-body`
  fills with the peer's log lines, forwarded frame-by-frame through the proxy.
- **UY2 — Toggle closed.** A second click on the same peer `clog-btn` closes the
  panel, like a local container log.

### 4.27 UI — Maske Z: Unhealthy-stack visibility (ADR-0027 extension)

The header **health beacon** and the Deploys **attention band** lift a currently-
`unhealthy` stack out of the chronological log (where its row-bound pill can sit
far down or age out). Both read the same live `health` snapshot via the pure
`attentionStacks()` helper; the harness enables the poller (`healthPoll`) and
scripts each stack's `docker compose ps` output. Behaviour-only: both surfaces
are hidden whenever nothing is unhealthy, so no snapshot baseline shifts.

- **UZ1 — Beacon counts unhealthy stacks.** With two of three stacks unhealthy,
  the `health-beacon` is visible with a `health-beacon-count` of `2` and a
  pluralised `aria-label`; its popover lists exactly those two stacks
  (`health-beacon-item`), never the healthy one.
- **UZ2 — Band jumps to the stack.** The `attention-band` lists the unhealthy
  stack (with its `health-pill`) above the log; clicking the `attention-row`
  lands on that stack's newest `deploy-row` (flashed via `.jump-target`).
- **UZ3 — Cross-view presence + recovery.** The header beacon survives a switch
  to the Stacks view while the Deploys-scoped band hides with it; when the last
  unhealthy stack recovers, both the beacon and the band disappear.
- **UZ4 — Stacks floats + marks unhealthy first.** The Stacks view has no band;
  instead the unhealthy roster row sorts to the top (a stable sort keeps the
  healthy remainder alphabetical) and wears a `data-health="unhealthy"` marker
  (driving the severity bar + tint) a healthy row lacks; a beacon jump from the
  Stacks view lands on that `roster-row` (flashed via `.jump-target`) rather than
  switching to Deploys.

### 4.28 UI — Maske AA: Accessibility sweep (Tier 2)

The keyboard/AT-facing guarantees the a11y sweep added (see UI_SPEC.md
"Accessibility"). Behaviour-only — the contrast retune (T2.5) and the
transparent touch-target overlays (T2.9) have no behavioural surface, so they
carry no assertion here (contrast is verified by construction against WCAG, the
overlays leave the rendered pixels unchanged).

- **UAA1 — Keyboard focus ring.** The first keyboard `Tab` lands on a header
  control that must show a non-`none` outline with a positive width — the
  `:focus-visible` ring that was absent on the glyph-only controls before.
- **UAA2 — Drawer focus management.** Opening the autosync drawer from the
  keyboard pulls focus onto its first control (`global-switch`); `Escape` closes
  it and returns focus to the opener (`autosync-btn`). The drawer is `aria-modal`.
- **UAA3 — Live region.** The `a11y-announce` region is empty at boot (the
  startup deploy is history), then a live webhook deploy fills it with the spoken
  outcome (`web deployed successfully`) — proving live outcomes announce and the
  history replay stays silent. Between the two, the test waits for the region's
  `data-announce-ready` to reach `1` rather than sleeping past the post-connect
  gate: the gate publishes its own state precisely so this stays an assertion.
  The suite contains no `waitForTimeout`, and `make check-no-sleeps` keeps it
  that way — if a wait seems unavoidable, the missing piece is a signal in the
  UI, not a longer timeout.
- **UAA4 — Host-row checkbox (multi-host).** Each drawer `host-row` is
  `role="checkbox"` with `aria-checked` = in-view; `Space` on the focused row
  toggles it (narrowing the merged feed, `host-filter-active`) and focus is
  restored onto the same host's rebuilt row.
- **UAA5 — Host-mono chip keyboard (multi-host).** The per-row identity chip is a
  `role="button"` quick filter; `Enter` on a focused chip isolates the view to
  that host, like a click.

### 4.29 UI — Maske AB: Always-visible search trigger (T3.11)

The header magnifier (`stack-search-btn`) that makes the desktop stack filter
discoverable — before this it was type-to-search only, an easter egg. Behaviour
-only. Three startup deploys (`web`/`api`/`db`) give rows to filter.

- **UAB1 — Deploys open/close.** The magnifier is visible on desktop deploys;
  clicking it reveals the filter, focuses the input, and sets `.active` +
  `aria-expanded="true"`; a second click folds the bar away and clears both.
- **UAB2 — Stacks.** On the Stacks view the same magnifier opens the roster
  filter (`roster-filter-wrap`) and focuses it — the trigger is view-aware.
- **UAB3 — Logs.** On the Logs view the same magnifier opens the **in-log
  search**: the bar reveals, the input focuses, typing narrows the log, and a
  second click folds it away and clears the query. Back on Deploys it drives the
  deploy filter again. (It used to be hidden here — the one view where the
  header's search glyph vanished.)
- **UAB4 — Type-to-search parity.** Typing still reveals the bar and seeds the
  first character, and the magnifier reflects a bar opened by typing (`.active`
  + `aria-expanded`); `Esc` folds it away and resets the trigger.

### 4.30 UI — Maske AC: View-toggle active-bar + options caret (T3.12)

The two cues on the active view button. A **top bar** (an intentional `::before`
rectangle) marks the active view on every view, Logs included — it replaces the
earlier accidental bar, where the a11y sweep's touch-target `::after` box had
reshaped a caret's `border-top` into a full-width bar. A **caret** (the real
`.vt-caret` child) marks that the view has an options popover: present only on
views that have one (deploys/stacks, not Logs) and flipped up while open.
Behaviour + computed-style only (no snapshot).

- **UAC1 — Both cues on the active view only.** On default deploys the active
  button shows the bar (`::before`, 3px) and a visible caret; the inactive
  stacks button shows neither (no bar, caret hidden).
- **UAC2 — Logs bar-only.** The active Logs button shows the bar (it is a valid
  active view) but no caret (it has no popover — the false-affordance fix),
  while the active Stacks button shows both; the caret gate is Logs-specific.
- **UAC3 — Flip on open.** Opening the popover flips the caret (transform
  `matrix(1,…)` → `matrix(-1,…)`), keyed off the active button's
  `aria-expanded`; closing returns it to rest. The flip transition is disabled
  in-test so the transform reads at its settled value, never mid-tween.
- **UAC4 — Honest `aria-expanded`.** Opening on Deploys then switching to Stacks
  closes the popover and leaves both buttons `aria-expanded="false"` (no stale
  `true`, so the caret never reads open on a non-active button).

### 4.31 UI — Maske AD: deploy-row ⋯ overflow menu (T3.13)

The newest row per stack collapses its secondary actions — deploy history
(§4.14), container logs (§4.21) and deploy hooks (§4.22) — behind a single `⋯`
button (`more-btn`), so the resting row is identity + status + the still-visible
cross-view jump action instead of a cluster of look-alike glyphs. The relocated
action buttons keep their own testids and handlers inside the menu, so Maskes
M/T/U open it (`openRowMenu`) before reaching them. Boots in discovery mode with
a hooks-declaring `web` stack so all three actions appear. Behaviour-only.

- **UAD1 — Collapsed + labelled.** The row shows a `more-btn` and the jump
  button, with no history/clog/hooks glyph loose in the stack cell; the menu is
  hidden (`aria-expanded="false"`) until clicked, then lists the three actions as
  labelled rows (Deploy history / Container logs / Deploy hooks).
- **UAD2 — Select runs + closes.** Picking Deploy history opens its `audit-panel`
  (the row gains `audit-open`) and closes the menu.
- **UAD3 — Container logs.** Picking Container logs opens the `clog-panel` and
  closes the menu — the relocated clog button keeps its handler.
- **UAD4 — Dismissal.** The menu closes on an outside click and on `Esc`, like
  the app-link popover.
- **UAD5 — Portrait safety.** On a 400px portrait viewport the open popover stays
  within the viewport bounds (it flips right-aligned near the edge) — the density
  fix never reintroduces an off-screen/overlapping menu.

### 4.32 UI — Maske AE: Stacks/roster row inline actions

The Stacks/roster row (§4.19) surfaces its secondary actions inline in the stack
cell, beside the cross-view jump and the app-link: the container-logs icon
(§4.21) always, the deploy-hooks badge (§4.22) only for a hooks-declaring stack.
Earlier these were folded behind a `⋯` overflow menu (as the deploy row still is,
§4.31), but on the roster the row-body click already opens the health + history
panel, so the menu usually wrapped a single action (logs) — an extra click for no
density gain. No `more-btn` on the roster. Boots in discovery mode with a
hooks-declaring `web` stack and a plain `api` stack. Behaviour-only.

- **UAE1 — Inline, no menu.** The `web` row shows the jump button, the
  container-logs icon and the hooks badge all directly in the stack cell, and no
  `more-btn`. The hooks-less `api` row shows the logs icon but no hooks badge.
- **UAE2 — Container logs.** Clicking the inline logs icon opens the `clog-panel`
  directly (one click, no menu step).
- **UAE3 — Deploy hooks.** Clicking the inline hooks badge opens the bound
  `hooks-panel` (the row gains `hooks-open`); a second click toggles it closed.
- **UAE4 — Portrait safety.** On a 400px portrait viewport the inline action
  cluster stays within the row bounds and the log panel opens below it.
- **UAE5 — Wraps as one cluster, version stays put.** On an iPad-mini-width
  viewport (744px) a stack carrying the full glyph set (jump, app link, logs,
  hooks) cannot fit them behind its name: they move to a second line
  *together* (the positive signal that a wrap really happened is asserted
  first), every glyph shares one line, and the **version chip stays on the
  name's line** rather than sliding to the middle of the now taller row. Before
  `.row-actions` each glyph was its own flex item and the row broke between two
  icons; before the top-aligned cells the version dropped below the name it
  describes. The fixture also asserts its own shape — the name shares the first
  line with the host chip — since a name long enough to take a line of its own
  is a different case the alignment check would not describe.
- **UAE6 — A re-polled app link stays in the cluster.** With an app link seeded
  (`appLinks` start option, detection rides the health poll) the icon is rebuilt
  on every poll long after the row rendered: it must land back inside the
  cluster ahead of the logs icon, leaving no glyph as a direct child of the
  stack cell. Appending it to the cell would strand it on its own line again.
- **UAE7 — An over-long name clips instead of moving.** At the same width a
  29-character stack name stays on the host chip's line, ellipsised, with the
  version beside it and the full name in the element's `title`. Ungrouped, the
  name took a line of its own below the chip and the version — aligned to the
  row's first line — ended up above it.

### 4.33 UI — Maske AF: Status-badge icons + solid worst states (T3.14)

Every status badge (§3, [Status badges](../internal/ui/UI_SPEC.md)) leads with an
icon (`svg.badge-ico`) from the pure `statusIcon` helper, and the two worst
terminal states — `rolled_back_unhealthy` / `heal_exhausted` — render as a solid
danger chip instead of the smallest 9px two-line text, correcting the inverted
hierarchy where the most attention-critical states were the quietest (T3.14). The
spec drives the real self-heal loop (like §4.12) so `success`, `healed` and
`heal_exhausted` badges all surface in one run. Behaviour-only; the solid look is
also captured by the regenerated §5 snapshots.

- **UAF1 — Success carries an icon.** The startup `success` badge contains one
  `svg.badge-ico`; its text stays exactly `success` (the icon adds no text node,
  so the existing text assertions across masks are unaffected).
- **UAF2 — Healed carries an icon.** After the stack turns unhealthy the
  corrective `healed` badge also leads with a `badge-ico`.
- **UAF3 — Worst state is a solid two-line chip.** The `heal_exhausted` badge
  carries the warning `badge-ico`, still shows both stacked label lines
  (`self-heal` / `failed` inside `.badge-lbl`), and reads its wording.
- **UAF4 — Solid vs dim.** The `heal_exhausted` badge's settled
  `background-color` is opaque (alpha ≈ 1) while a dim badge (`success`) is
  translucent (alpha < 0.9) — a deterministic computed-style read, no timing.

### 4.34 UI — Maske AG: First-run header tour (T3.15)

The glyph-only header teaches its controls once on a fresh browser: a caption
under each control plus a dismiss banner (`data-testid="header-tour"`), gated on
the `localStorage` key `headerTourSeen` and applied pre-paint as
`.header-tour-seen` on `<html>` (§3, [First-run header tour](../internal/ui/UI_SPEC.md#first-run-header-tour)).
Purely storage-gated — no timers — so shown/dismissed is deterministic. This spec
opts out of the shared fixture's default `seedTourSeen` so it lands on a genuinely
fresh browser; every other spec keeps the seed, so the steady-state header (not
the one-time tour) is what all other baselines and behaviour tests see — which is
why §5 needed no baseline churn.

- **UAG1 — Fresh browser shows the tour.** On first load the banner and the
  per-control captions (`Deploys`, `Autosync`) are visible, and `<html>` is not
  yet marked `header-tour-seen`.
- **UAG2 — Got it dismisses and persists.** Clicking **Got it** hides the banner
  and captions, marks `<html>.header-tour-seen`, and sets `headerTourSeen='1'`; a
  reload of the same context never re-shows the tour.
- **UAG3 — Esc dismisses.** Pressing `Esc` is an equivalent dismiss (a keyboard
  user is never trapped), and focus lands back on the active view button.
- **UAG4 — Returning browser skips it.** With `headerTourSeen` pre-seeded the
  tour never shows — not even for a frame — because the pre-paint class applies
  before the header paints.
- **UAG5 — Suppressed on mobile.** At ≤ 700 px the tour (banner + captions) is
  hidden even on a fresh browser and leaves `headerTourSeen` unset; widening the
  same session back to desktop reveals it (a desktop/tablet affordance).

### 4.35 UI — Maske AH: Feedback & error states (T4.16 / T4.17)

Two feedback gaps, both driven deterministically (route control, no timers) — see
[Loading vs. empty](../internal/ui/UI_SPEC.md#event-lifecycle-sse) and
[Failed detail fetches](../internal/ui/UI_SPEC.md#failed-detail-fetches). For T4.17,
holding `/api/events` holds the `synced` marker, so the loading skeleton stays
up until the test releases the gate; that marker is what then reveals the empty
state or the rows. For
T4.16, a route that answers the first hit with an error status then continues
exercises both the failure line and the Retry recovery in one flow.

- **UAH1 — Skeleton → genuine-empty.** A stack-free instance: while the stream
  is held, `loading-state` is visible and both the table and `empty-state` are
  suppressed; on release the empty (`synced`) history flips it to `No deployments
  yet`, table still hidden.
- **UAH2 — Skeleton → table.** With a deploy present, the released history paints
  the row and shows the table — the empty state never flashes.
- **UAH3 — Audit history load-error + retry.** A `500` on `/api/audit` shows the
  amber `load-error` (`Couldn't load deploy history`, not `No recorded deploys`);
  Retry re-fetches and lists the record.
- **UAH4 — Diff load-error + retry.** A `500` on `/api/events/*/diffs` shows the
  `load-error` instead of a silent drop to the file list; Retry loads the real
  diff panel.
- **UAH5 — Peer-diff load-error + retry.** A `502` on the peer-diff proxy keeps
  the peer detail's read-only facts and shows `Couldn't reach <host> for the
  diff.`; Retry loads the peer diff inline.

Behaviour-only (no snapshot): the states are structural (`data-testid` + text),
and the skeleton shimmer / spinner are animations a visual baseline would have to
freeze anyway.

### 4.36 UI — Maske AI: Per-service image delta

A deploy carries `image_changes` (service, old, new) on its SSE payload; the row
surfaces which service updated — and to what — in its **Version column** (a
full-width row on a phone), without opening the diff. See
[Image delta](../internal/ui/UI_SPEC.md#image-delta). Each case rewrites a stack's
compose (`setStackImage` / `setStackServices`) and fires a signed webhook, so the
deploy runs for real against stub docker and the delta renders from the actual
image change — no hand-fed events.

- **UAI1 — Tag bump.** A one-service `nginx:1.25 → 1.26` bump shows the service
  name (`app`) with the old tag struck through and the new tag in the add colour,
  in the Version column — right of the Stack cell and flush with the `Version`
  header (the alignment that makes a column worth it) — plus the `aria-label`
  (`app updated from 1.25 to 1.26`) and the full-reference `title`.
- **UAI2 — Service always labelled.** A lone service named after its own stack
  (`worker` in stack `worker`) still shows its service label — the chip names
  which service moved, not the stack — rendering `worker 2.0 → 2.1`.
- **UAI3 — Every changed service listed.** Four changed services render four
  chips, stacked one per line (asserted by bounding box: each below the previous,
  sharing a left edge) — nothing is hidden behind a count.
- **UAI4 — Digest-only rebuild.** A same-tag digest change (`nginx:1.25@sha256:…`)
  shows the shared tag `1.25` plus a `↻` rebuilt marker (no raw digest pair); the
  full digest is on the chip's `title`.
- **UAI5 — Responsive.** On a 390 px viewport the row is two lines and the
  Version cell shares the second one with the time — below the name, beside the
  time (asserted by bounding box, all boxes read in **one** measurement pass so
  a prepended row cannot have two reads describe different rows). Duration and
  Files are hidden; the name still renders in full (no ellipsis).
- **UAI7 — Tablet.** On a 744 px viewport (iPad-mini portrait) **Duration and
  Files give up their tracks** — hidden in the rows *and* in the header — so
  Version keeps the first line: level with the name, right of the stack cell,
  ending before the status cell begins, chips still stacked one per line. Seeded
  health (`healthPoll: 1` + `initialHealth`) gives the status cell its second
  line, the widest state the row reaches. The stack name is asserted present:
  squeezing all six columns rendered an icon cluster with no stack behind it.
- **UAI6 — View-options toggle.** The Deploys popover's **Version changes** toggle
  (on by default) collapses the whole column when flipped off — no chips, no
  `Version` header, and no row separators (they exist only because the column
  makes rows variable-height); the off choice persists across a reload
  (`localStorage`), and toggling back on restores all three.

Behaviour-only (no snapshot): the delta is structural (`data-testid="svc-delta"` +
per-part text), and the per-reference token logic is exhaustively covered by the
`app-helpers` unit layer (`imageDelta` / `parseImageRef` / `shortImageTag`).

### 4.37 UI — Maske AK: Service versions in the Stacks view

`compose ps` reports the image each container runs, which rides the `health`
snapshot: a roster row's **Version** cell names the service the stack is named
after plus its running version, and the expanded containers panel carries every
service's version — the same chip the Deploys Version column renders (Maske AI).
See [Service versions](../internal/ui/UI_SPEC.md#service-versions). Health is
seeded per stack (`initialHealth` / `setStackHealth` now carry `Image`), so the
versions render from a real snapshot.

- **UAK1 — Lead version on the row.** A three-service `immich` shows one chip:
  `immich-server v1.119.0` (the shorter of the two name matches, never the
  alphabetically-first `database`) plus `+2`, with the `aria-label`
  (`immich-server running v1.119.0`), the full-reference `title`, and no `→`
  (a running version is a fact, not a change). Its own column between Stack and
  Status, asserted by bounding box.
- **UAK2 — No arbitrary lead.** A role-named stack (`monitoring` over
  prometheus/grafana) gets no chip at all — the cell reads `2 services` and defers
  to the panel.
- **UAK3 — Every version in the panel.** Expanding lists all three versions
  (`data-testid="health-version"`) beside the state/status the panel already
  showed, the panel carrying `has-versions`; the chip drops its service label
  there, since the line already names it.
- **UAK4 — Degrades to nothing.** A snapshot without `Image` (an older skipper, or
  a peer of one) leaves the cell empty and the panel without the version column —
  never an empty chip or an empty track.
- **UAK5 — Patched in place.** A later poll carrying a new image updates the cell
  (`v1.120.0`, count gone) while the row's open panel survives — the versions
  arrive with health, not with the roster snapshot.
- **UAK6 — Tablet.** On an 820 px viewport **Commit** gives up its column —
  header label and cells — so Version keeps the row's first line beside the
  name (asserted by bounding box, read in one measurement pass). The stack name
  is asserted present and non-zero: `immich`'s cell also carries a change chip,
  an app link and the log/jump glyphs, which wrap to a second line inside the
  cell rather than squeezing the row's identity away.

Behaviour-only (no snapshot): the cell is structural, and the lead-service and
token logic are covered by the `app-helpers` unit layer (`rosterVersion` /
`imageRepoName` / `shortImageTag`).

### 4.38 UI — Maske AJ: Roster change detection

skipper redeploys a stack only when one of its hashed inputs changes
(Invariant 2), so the most common operator question is not "what happened" but
**"why did nothing happen"**. The roster's third expand panel answers it in
place: which inputs are watched, and the commit nothing has changed since. See
[Change detection](../internal/ui/UI_SPEC.md#change-detection). Data rides the
`stacks` snapshot, so there is no fetch and no loading state.

- **UAJ1 — Watched inputs + since-commit.** Expanding a cleanly deployed stack
  adds `watched-panel` as the **last** card of the expand stack (asserted by
  bounding box: below `audit-panel`), leads with `Unchanged since <7-char sha>.`,
  and lists the stack's compose file **repo-relative** (the path the operator
  edits and commits, not the clone's absolute location). Closing the row removes
  it with the rest of the card. The stack's hashed *config* renders as its own
  non-path entry (`watched-config`), never as a file. The lead's reference point
  is the deploy itself on a stack's *first* deploy (no prior commit to diff
  against).
- **UAJ1b — Since-commit after a real change.** A pushed compose bump deploys
  for real, and the lead then names the short SHA the stack is at — the actual
  answer to "I pushed, why is this stack quiet". Gated on **row counts**, not on
  a wait: the stack's success rows must reach 1 (the startup deploy settled)
  *before* the push, and 2 afterwards (the pushed change got its own run). Only
  a push-driven deploy records a commit, so a change pushed into the run already
  in flight leaves the lead at `the last deploy` — the flake this replaced.
- **UAJ2 — Parked stack.** A stack with `disabled: true` reports that skipper
  neither watches nor deploys it, and lists no files — rather than showing
  whatever its last deploy happened to record.
- **UAJ3 — Filter parity.** The panel hides and reappears with its row under the
  roster search filter, like every other trailing panel.

Behaviour-only (no snapshot): the panel is structural text, and the lead-line
phrasing across every deploy status (a `failed`/`queued`/`blocked` stack has a
change *pending* and must never claim "unchanged") is exhaustively covered by
the `app-helpers` unit layer (`watchedSummary`).

### 4.39 UI — Maske AL: Commit SHAs link to the forge

A SHA on its own is a dead end — it names a commit the operator then has to go
find. Every SHA the UI prints is therefore a link to that commit on the forge
(`repo_web_url`, or one derived from `repo_url`). See
[Commit links](../internal/ui/UI_SPEC.md#commit-links). The harness clones from a
local path, which no forge URL can be derived from, so it sets `repo_web_url`
explicitly (`FORGE_URL`); the degradation case opts back out with
`repoWebURL: null`.

The roster cases run at the default desktop viewport on purpose: below the
tablet breakpoint the Commit column is dropped entirely (Maske AK, UAK6), so
there is no cell to link there.

- **UAL1 — The roster Commit cell links.** The cell is an `<a>` whose `href` is
  `<forge>/commit/<full sha>` — the **full** 40-char SHA, though the cell prints
  the 7-char form (a short SHA is a display convention some forges will not
  resolve). It opens in a new tab (`target="_blank"`, `rel` carrying `noopener`),
  since the UI is a live SSE stream that navigating away would cost.
- **UAL2 — The link does one thing.** The SHA sits in a row whose body toggles
  the expand panel; clicking the link must not also open it. The navigation is
  cancelled in the capture phase, so what is asserted is purely the row
  handler's guard — and the row still expands when clicked anywhere else.
- **UAL3 — Panels, prose and the diff header too.** The deploy-history rows
  (`.ar-sha`), the diff panel's commit header (`commit-sha`) and the
  change-detection lead's `Unchanged since <sha>` link the same way: one helper
  renders every SHA, so they either all link or none do. The lead is prose, so
  the assertion also pins the sentence around the link.
- **UAL4 — A peer links to its own forge.** A peer's roster row uses the
  `repo_web_url` from *that peer's* fanned-in snapshot, not the primary's —
  each host tracks its own deploy repo, so the primary's forge never had those
  commits.
- **UAL5 — Degrades to plain text.** With no forge configured (and none
  derivable), the SHA renders as the inert `<span>` it was before links existed
  — never a dead link.

Behaviour-only (no snapshot): the link is an attribute change, and the URL
building itself (trailing slashes, a missing base or SHA, a non-http(s) base
that must never reach an `href`) is covered by the `app-helpers` unit layer
(`commitURL`) and, server-side, by `git.WebURL`.

### 4.42 UI — Maske AO: registry update check

The read-only update check (ADR-0054) annotates the Stacks view's version
chips with what upstream offers. The harness stands up a **local registry
stub** (tags/list + manifest HEAD; the `{{registry}}` token in composes and
health images resolves to its host:port) and the shared docker stub answers
`image inspect` from seeded `rd-*.json` files — so the real pipeline runs:
running images recorded at deploy → checker → `stacks` snapshot → chips. The
harness config disables the check for **every other mask**
(`interval_seconds: 0`), so no test phones a real registry for its fake images
or renders unexpected markers; this mask opts in via `updateCheck`.

- **UAO1 — Newer tag.** The row chip reads `1.22.3 ⇡ 1.22.6` (amber `.td-upd`),
  the tooltip names the available version.
- **UAO2 — Panel.** The containers panel marks the affected service's chip and
  its head shows `⇡ 1 update · checked … ago`.
- **UAO3 — Rebuilt.** Nothing newer in the tag list, but the upstream digest
  differs from the local RepoDigests: the chip reads `v3.1 ⇡ rebuilt`.
- **UAO4 — Unmarked control.** A stack whose tag list has nothing newer and
  whose digest matches shows no marker and no head summary. Asserted only after
  UAO1's marker is visible — the check publishes one snapshot for all stacks,
  so the marked stack is the positive signal that the absence is a result, not
  a not-yet-checked false green.

The markers arrive via the post-deploy nudge (the check re-runs when a deploy
changed what runs), an SSE republish after startup — every case awaits the
marker itself, never a wall-clock delay.

### 4.43 UI — Maske AP: an unreachable deploy stream

The loading skeleton deliberately does not settle on a failed connection, so a
transient outage never reads as "no deployments" (Maske AH). Left alone that
turns into promising forever: a page with no route to the server — an installed
PWA opened off the network, which the service worker still paints from its
cached shell — sat on `Connecting to deployment stream…` with the indicator on
`reconnecting` and no way out, reloading included. This mask pins the honest
end of it.

`/api/events` is answered `503` rather than aborted: a non-2xx is **fatal** to
`EventSource` (it closes for good instead of retrying itself), so the page is
driven purely by its own retry — the path under test. Flipping the stub back to
`continue()` stands in for a server that came back.

- **UAP1 — It says so.** After the failures cross the threshold the skeleton
  gives way to the amber load-error line reading `Can't reach skipper — the
  deploy stream is offline.`, and the indicator reads `reconnecting`. Asserts the
  skeleton is *gone* — it means "rows are on their way" — that the genuine-empty
  state stayed hidden, and that the line sits outside any `aria-hidden` subtree:
  the skeleton is decoration the connection indicator speaks for, so a focusable
  Retry inside it would be tabbable but invisible to assistive tech.
- **UAP2 — Retry recovers in place.** With the server back, the notice's Retry
  connects, the table appears and the notice retracts. A marker set on `window`
  before the click is asserted afterwards: if Retry ever became a reload it would
  not survive, so the test fails instead of passing on a fresh document.
- **UAP3 — Counter-check.** A reachable server shows no notice. Without it UAP1
  would still pass if the line were rendered unconditionally — a worse bug than
  the one fixed.

Each case awaits the notice or the table itself, never a wall-clock delay: the
notice is rendered on the failure that crosses the threshold, so the assertion
cannot land early.

**Not covered here:** the wake-up wiring (`visibilitychange` / `online`
reconnecting a page that returns to the foreground). Observing it needs a
connection attempt to be attributed to the event rather than to the retry timer
already armed, which means either racing that timer or subscribing to network
events mid-test — and the fixture warns that such a listener changes the timing
of the very race it observes (T8). It is covered deterministically at the unit
layer instead (`makeReconnector` `resume`, injected timer).

### 4.44 UI — Maske AQ: after-the-fact rollback visibility

The 2026-08-05 incident: a rollback was recorded everywhere — event ring,
audit log, notification — and visible nowhere five minutes later, because the
successful retry became every surface's newest word on the stack. The mask
drives that exact sequence against one instance (`STUB_DOCKER_FAIL_NTH_UP=2`:
startup `up` succeeds, the deploy's `up` fails, the rollback `up` succeeds →
`rolled_back`; a second push then retries successfully) and asserts each
surface keeps the rollback readable
([UI_SPEC](../internal/ui/UI_SPEC.md#rollback-linkage)):

- **UAQ1 — Retry note.** The retry's success row (and only it) carries the
  `↺ after rollback` note with the rollback's event id; activating it flashes
  the rollback's own row (`.jump-target`). The pairing reads in both
  directions instead of the success papering over the rollback.
- **UAQ2 — Status filter.** The Deploys filter bar's status chips carry
  per-status counts, narrow the log to the selected statuses (`1/3` count) and
  clear together with the query on Escape. The chip click lands from a focused
  input — the blur that folds an idle bar must not collapse the chip row out
  from under the click.
- **UAQ3 — Incident badge.** The header badge counts the window's bad
  outcomes, survives a view switch, and its click lands on the Deploys view
  with the bar revealed and the four bad-outcome chips pre-selected — showing
  exactly the rows the count promised. A click from another view re-applies
  that preset; a second click on the Deploys view clears it and folds the bar
  away — the badge is the way out as well as the way in.
- **UAQ4 — Roster.** The stack's roster row shows the outcome strip
  (oldest → newest: success, rolled_back, success) and the last-incident line
  naming the rollback the retry papered over.
- **UAQ5 — Logs.** The severity chips are thresholds and narrated outcome
  lines classify by outcome: the WARN-level `rolled back` outcome line stays
  visible under the `errors` chip (hiding it there is the incident's exact
  failure mode), while an ordinary INFO line is narrowed away.

Behaviour-only, no snapshot. The pinning exemption (no-op run summaries are
not pinned) is covered at the unit layer on both sides (`internal/logbuf`,
`app-helpers`), where eviction can be driven precisely.

### 4.45 UI — Maske AR: the Logs view fits the display

The 2026-08-05 report from a tablet: the log panel hung off the right edge of
the screen. The cause was layout, not content — `main`'s auto side margins
suppress the flex stretch in the Logs view's column, leaving it shrink-to-fit,
i.e. no narrower than its min-content, and one pre-formatted diff line inside
the pane is wider than a tablet. The mask pushes a change whose diff carries a
deliberately long image reference, then asserts the column stays inside the
viewport and the wide line scrolls where it belongs. It also pins the chrome the
same report reshaped ([UI_SPEC](../internal/ui/UI_SPEC.md#log-view)):

- **UAR1 — Tablet (744x1133).** `main` and the panel both end at or before the
  viewport's right edge, and the diff block scrolls horizontally by itself — a
  positive signal that the wide content is really rendered, not absent.
- **UAR2 — Phone (390x844).** The same guarantee where the chrome row wraps,
  plus the search hand-off: the header magnifier is hidden at this width, so the
  panel's own `log-search` tool is shown and is the way into the in-log search.
- **UAR3 — One chrome row.** The panel has no `clog-head`; the live pill and the
  wrap/auto-scroll/fullscreen tools sit in the filter row, and pausing from
  there still reports paused in the footer. No second magnifier at this width.
- **UAR4 — Fullscreen covers the header.** With fullscreen on, the element
  painted at the top-centre of the viewport is the panel (not the header), its
  box is the whole viewport, and the panel's own search tool is back because the
  header's magnifier is unreachable; `Esc` gives the header back. Second case:
  fullscreen on, `Esc`, switch to Deploys — the panel is hidden, its
  `clog-fullscreen` class cleared and the deploy table visible. It used to stay
  on top of whichever view followed.
- **UAR5 — The header never scrolls sideways.** `scrollWidth <= clientWidth` on
  a 375 px phone with the view switch still reachable, and on a 744 px tablet
  with the first-run tour showing — the two cases that used to overflow. Both
  run against a deliberately loaded instance (fanned-in peer, unhealthy stack,
  theme picker), because an empty header fits anywhere and would assert nothing.

Behaviour-only, no snapshot.

## 5. Visual snapshot strategy

Snapshots are Playwright `toHaveScreenshot` baselines, deliberately scoped to a
lean set of high-value per-mask anchors, not every case.

They are tracked with **Git LFS** ([ADR-0052](adr/0052-binary-assets-out-of-git-history.md)):
a UI tweak regenerates up to six full PNGs and a PNG never delta-compresses, so
history keeps a pointer and the bytes live in LFS.

`git-lfs` on PATH (`nix develop` provides it) is needed in exactly two situations:
**regenerating** a baseline — `git add` fails without it — and running with
`RUN_SNAPSHOTS=1`, which compares against the real PNGs. A plain local
`make e2e-ui` leaves the compares off (below), so pointer files are harmless
there. To get the real files: `git lfs install && git lfs pull`. CI fetches them
in a small `baselines` job and hands them to `e2e-ui` as an artifact, because
Playwright's pinned container ships no git-lfs (§7).

The landed baselines:

| Baseline | Anchor case | Target | Masked |
| --- | --- | --- | --- |
| `deploys-table.png` | UA1 | `deploys-table` | `time-cell`, `duration-cell` |
| `diff-panel.png` | UA8 | `diff-panel` | — (static diff) |
| `autosync-drawer.png` | UC6 | `autosync-drawer` | `wait-cell` |
| `theme-dark.png` / `theme-light.png` | UD1 | full page | `time-cell`, `duration-cell` |
| `mobile-layout.png` | UD4 | full page (390px) | `time-cell` (the duration is hidden at this width) |

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
  # The version/commit MUST match the harness constants (buildVersion/buildCommit
  # in fixtures/harness.ts), not the release manifest — the header label is baked
  # into the full-page snapshots, and UD5 asserts it against those fixed values.
  CGO_ENABLED=0 go build -buildvcs=false -ldflags "-X main.version=10.10.10 -X main.commit=e2ee2ee" -o .pw-bin/skipper ./cmd/skipper
  docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$PWD":/work -w /work/e2e/ui \
    -e SKIPPER_E2E_BIN=/work/.pw-bin/skipper -e CI=1 -e RUN_SNAPSHOTS=1 \
    mcr.microsoft.com/playwright:v1.61.1-noble \
    sh -c "npm ci && npx playwright test --update-snapshots"
  ```

  Then review the changed PNGs before committing. (`.pw-bin/` is gitignored.)

### 4.40 UI — Maske AM: A refused autosync write is announced

A failed toggle is invisible in the interface: the switch simply does not move,
which looks exactly like a click that never landed. That ambiguity cost real
time chasing a UC11 flake, so the write path now reports its failure on the
console — the same treatment a dropped stale snapshot gets (UC13). See
[Autosync](../internal/ui/UI_SPEC.md#autosync).

- **UAM1 — A refusing server.** The `POST /api/autosync` is fulfilled with 503.
  The console announcement names the scope and the status; the switch keeps
  showing *server* state, because the write did not happen and rendering it as
  though it had would be the worse lie.
- **UAM2 — A request that never arrives.** The same POST is aborted
  (`connectionfailed`), covering the transport failure rather than the refusal.

Both wait on the announcement rather than on the DOM: it is emitted in the same
step the switch would otherwise have been repainted in, so "the switch did not
move" cannot pass early. Counter-probed against the pre-fix build, where both
cases fail.

### 4.41 UI — Maske AN: the web-font swap causes no layout shift

The self-hosted webfonts use `font-display: swap`, so on a slow connection the
page first renders in a local fallback font. Historically that render occupied
*different* space — the container's Ubuntu Mono (0.5em advance) kept the queue
empty-note on one line where JetBrains Mono (0.6em) wraps it to two — so the
swap dropped everything below it ~19px: the UC11 flake for the suite, a
one-time CLS for real users. `app.css` now declares metric-compatible local
fallback faces (`size-adjust` + `ascent/descent/line-gap-override`, computed
from the real font metrics) for the common Ubuntu/DejaVu/Liberation candidates,
so the pre-swap render already occupies the loaded face's exact space. See
[Fonts](../internal/ui/UI_SPEC.md).

- **UAN1 — Blocked vs loaded geometry.** The autosync drawer is rendered twice:
  once with every `/fonts/**` request aborted (the slow-connection render — the
  spec asserts via `document.fonts.check` that the webfont really is absent,
  the positive signal that the fallback was measured), once normally. The
  empty-note's box and the first stack switch's height must match within 1px;
  the regression it guards was a ~19px hop. Counter-probed against the pre-fix
  CSS, where the note's height differs by a full line.

Deterministic by construction: both measurements happen after
`document.fonts.ready` (the fixture's navigation wrapper) and the drawer's
`data-settled` — settled states, no waiting on a swap to *probably* have
happened.

### 5.1 Docs screenshots — rendered, never committed

The two images the docs landing page embeds
(`docs/assets/screenshots/{deploys,stacks}.png`) are **not** in git. A screenshot
of the UI is derived from the UI, and a PNG never delta-compresses — committing
one on every UI change would grow the repo without bound. They are rendered from
a seeded instance instead, by `screenshots/docs-shots.spec.ts` under its own
config (`screenshots/shots.config.ts`, kept out of the suite's `testDir`):

```sh
make docs-screenshots     # builds the binary, renders into docs/assets/screenshots/
```

`.github/workflows/docs.yml` runs the same renderer in Playwright's pinned
container, hands the PNGs to the `build` job as an artifact, and only then runs
`mkdocs build --strict` — so a missing or broken render fails the docs gate. The
workflow's path filter therefore covers `internal/ui/**` and `e2e/ui/**` too: a UI
change must refresh the published images.

It is a **renderer, not a test** — it asserts only enough to know the page is
ready to photograph. What it stages, and why:

- Six stacks named after real self-hosted apps, whose logos it fetches from the
  icon set skipper auto-matches against (a fetch failure is not fatal — the rows
  fall back to monogram chips rather than breaking the docs build).
- `initialCompose` gives each stack a realistic multi-service compose from the
  start, so the first deploy is already the real thing rather than a
  placeholder-to-real migration.
- `commitAuthor` makes the pushes Renovate-authored, since the diff panel names
  the author and a bot-driven bump is the loop skipper exists for.
- Three staged pushes: a version bump whose row's diff is the focal point, a
  second push **held** at `compose up` so the top row stays `deploying`, then the
  remaining stacks bumped so every roster row carries a commit — with each bumped
  stack's `setStackHealth` updated to match, so the Stacks view's versions agree
  with its commits.

Because the images are gitignored, a local `mkdocs build --strict` fails on the
missing files until `make docs-screenshots` has run once.

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
| Logs view panel controls, no popover | **UB8** |
| Live/pause pill freezes without dropping | **UB9** |
| Live/pause pill keyboard-operable (Enter/Space) | **UB10** |
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
| A late autosync snapshot never overwrites a newer one | **UC13** |
| The drawer is inert until it has server state | **UC14** |
| A re-render never swaps a switch out from under a click | **UC15** |
| Enable drains, disable does not | **UC8** |
| Queued row + `paused:` tag | **UC9** |
| Theme toggle + persistence + no-flash | **UD1** |
| Theme picker override (switch/persist/notice/clear), switcher on | **UD6** |
| Mismatch notice auto-hide (virtual clock) | **UD6b** |
| Theme switcher off (default): picker hidden, override ignored | **UD7** |
| Connection indicator states | **UD2** |
| Connection indicator recovers from a fatal stream error | **UD10** |
| State published while the stream connects is not lost | **UD12** |
| Deploy indicator active/idle | **UD3** |
| Tap-tip opt-in on non-header controls (touch flash / mouse silent / view-options excluded) | **UD11** |
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
| Stack health: folded timeline — routine start absorbed, strip, raw-list toggle | **UH7** |
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

- **baselines**: fetches the LFS-tracked visual baselines on a runner that has
  git-lfs and uploads them as an artifact, which `e2e-ui` unpacks over the pointer
  files its in-container checkout wrote (ADR-0052).

A third job lives in `.github/workflows/docs.yml`, not here:

- **screenshots**: renders the docs landing-page images (§5.1) in the same pinned
  container and hands them to the docs build as an artifact. It asserts nothing
  about the product — it exists so those PNGs never have to be committed.
