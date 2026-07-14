# End-to-End Test Specification

Authoritative spec for skipper-cd's end-to-end (E2E) tests. Read this before
adding or changing E2E coverage. Pure logic lives in unit tests (see below) —
E2E owns only the **wiring and the through-line journeys** the unit tests
cannot see.

**Primary goal: quality-assure the Web UI requirements.** The UI is one
embedded `internal/ui/static/index.html` whose contract is
[`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md). A dependency/version bump
or an edit to that file can silently break a control, an SSE→DOM render, a
badge, or the drawer. The UI E2E layer exercises the **real rendered UI against
the real backend** so those breaks fail CI. Coverage spans all four UI masks,
asserting **behaviour + visual snapshots**.

> Status: **Go layer landed; Playwright UI project scaffolded, UA1 green.** The Go
> pipeline harness and P1–P9 (§4.1) exist under `e2e/` behind the `e2e` build tag,
> with a dedicated `e2e` CI job (§7). The UI product-code prerequisites are done and
> recorded in `UI_SPEC.md`: the `data-testid` set (§3) and the embedded self-hosted
> fonts (§5). The Playwright project (`e2e/ui/`) is scaffolded — a Node twin of the
> Go harness drives the real binary — with **UA1** (row lifecycle), **UA2** (all
> five rendered status badges), **UA3** (skipped deploys never render a row), **UA4** (time-mode
> toggle + persistence), **UA5** (stack icon + monogram fallback), **UA6** (icon
> refresh: POST + cache-busted reload), **UA7** (files-pill panel toggle),
> **UA8** (diff-panel fetch + colouring), **UA9** (error-panel tied to the
> failed row with its message), **UA10** (empty-state placeholder for a
> stack-free, event-free instance), **UD5** (header version label from
> `/api/version`), **UB1** (deploys↔logs view toggle + persistence), **UB2**
> (log lines + INFO/WARN/ERROR level badges), **UB3** (sort toggle
> newest↔oldest + persistence), **UB4** (follow toggle autoscroll +
> persistence), **UB5** (stack-prefix on deploy lines + `[docker]`
> cmd-prefix on captured child output), and **UB6** (diff pill on `deploy
> complete` lines expanding the diff panel below the line) passing, plus **UC1**
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
> the same port), **UD3** (deploy indicator names the active stack while held),
> **UD4** (responsive ≤700px), **UD5** (build identity label), **UD8** (view-options popover), **UD9** (theme glyph). All four masks' behaviour is landed, **and the visual-snapshot
> baselines (§5) too**: a lean set of six baselines (deploys table, diff panel,
> autosync drawer, both themes, mobile layout) generated and compared in
> Playwright's pinned container, gated by `RUN_SNAPSHOTS`. The `e2e-ui` CI job now
> runs inside that container. The suite is complete.

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
  - `STUB_DOCKER_FAIL_NTH_UP=<n>` → fail only on the Nth `compose … up`
    (lets a rollback `up` succeed while the initial `up` fails → `rolled_back`);
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

### 4.2 UI — Maske A: Deploys-View (Playwright)

- **UA1 — Row lifecycle.** Given the UI is open. When a stack deploys
  (`deploying` held, then released to `success`). Then a `deploy-row` for the
  stack appears newest-first, shows `deploying`, then mutates in-place to
  `success` (same row, not a duplicate). *Snapshot: table after success.*
- **UA2 — Status badges.** Drive each rendered status via §3 recipes. Then each
  `deploy-row` carries the correct `data-status` + `status-badge`
  (`success`/`failed`/`rolled_back`/`queued`/`deploying`). (No snapshot — the
  five statuses need separate instances; the badges are behaviour-asserted here
  and the table's dark/light rendering is snapshotted by UA1/UD1.)
- **UA3 — Skipped deploys never render.** An unchanged stack emits a `skipped`
  event, but no `deploy-row` is created for it (proven by ordering against a
  later real deploy); there is no skip-filter control.
- **UA4 — Time mode.** Toggle switches Time cells relative↔absolute and persists
  across reload (`localStorage timeMode`).
- **UA5 — Stack icon + monogram fallback.** A resolvable icon renders in
  `stack-icon`; a stack whose icon 404s falls back to the monogram chip (no
  broken image).
- **UA6 — Icon refresh.** The `i` hotkey issues `POST /api/icons/refresh`
  and reloads icons with a cache-busting param (there is no header button).
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
- **UB3 — Sort toggle.** The `log-sort` toggle flips the rendered order
  newest-first↔oldest-first and persists it (`localStorage logSort`). Driven
  against real replayed log output: the rendered `log-line` sequence is
  fingerprinted and the toggle must reverse it exactly, with a reload preserving
  the chosen order.
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
  hence topmost under the default descending sort — is the one whose pill expands a
  populated panel (`nginx:1.26`), not the plain "No diff recorded" note.

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
- **Fonts** are **embedded** in `index.html` (self-hosted `@font-face`, `woff2`
  as `data:` URIs), so there is no font load-timing / offline nondeterminism.
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
| Icon refresh via `i` hotkey | **UA6** |
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
| Deploy indicator active/idle | **UD3** |
| Responsive ≤700px: header no-overflow + wordmark hidden + table collapse + tap-to-expand | **UD4** |
| Header version label (`v<semver>` from `/api/version`) | **UD5** |
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
