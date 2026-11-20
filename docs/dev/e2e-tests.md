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
`node --test` (`make ui-unit`, its own CI job, which also reports its line,
branch and function coverage and fails below the floors set in the `Makefile`)
— so E2E need not re-verify formatting/classification edge cases. A dependency/version bump
or an edit to that file can silently break a control, an SSE→DOM render, a
badge, or the drawer. The UI E2E layer exercises the **real rendered UI against
the real backend** so those breaks fail CI. Coverage spans all four UI masks,
asserting **behaviour + visual snapshots**.

> Status: **Go layer landed; Playwright UI project scaffolded, UA1 green.** The Go
> pipeline harness and P1–P31 (§4.1) exist under `e2e/` behind the `e2e` build tag,
> with a dedicated `e2e` CI job (§7). The UI product-code prerequisites are done and
> recorded in `UI_SPEC.md`: the `data-testid` set (§3) and the embedded self-hosted
> fonts (§5). The Playwright project (`e2e/ui/`) is scaffolded — a Node twin of the
> Go harness drives the real binary — with **UA1** (row lifecycle), **UA2** (all
> six rendered status badges), **UA3** (skipped deploys never render a row), **UA4** (time-mode
> toggle + persistence), **UA5** (stack icon + monogram fallback), **UA7** (row click toggles the deploy detail),
> **UA8** (diff fetch + colouring in the deploy detail), **UA9** (a failed
> row's deploy detail: verdict before facts, the error in the Output tab), **UA10** (empty-state placeholder for a
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
> **UC9** (queued row + `autosync paused` cause line, superseded on resume), the
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
> **UD4** (responsive ≤700px), **UD5** (build identity label), **UD8** (view-options popover), **UD9** (theme glyph), **UD11** (tap-tip opt-in on non-header controls), **UD13** (OS colour scheme) and **UD14** (mobile theme row). All four masks' behaviour is landed, **and the visual-snapshot
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
> stack-health pill — **UH1** (pill per stack), **UH2** (the Containers tab lists
> every service), **UH3** (deploy rows carry no health pill), **UH4** (health-watch status history:
> age + phase timeline + deploy-correlated commit chip, ADR-0031), **UH5**
> (an exited on-demand container reads stopped + labelled, ADR-0027 amendment),
> **UH7** (routine restart folds into `up in Xs` + strip + raw-list toggle) —
> driven by a health poller scripted through the stub docker's `ps` output. **Mask I** (§4.10) covers the diff panel's commit
> metadata in the deploy detail — **UI1** (Commit fact), **UI2** (row/panel
> binding), **UI3** (Commits tab for a multi-commit range) — driven by webhook image bumps. **Mask
> J** (§4.11) covers the rolled-back deploy detail — **UJ1** (opens on click for
> its row), **UJ2** (diff carried on the rolled-back event, the
> rollback verdict before it) — driven by a failing `up` that rolls back. **Mask K**
> (§4.12) covers self-heal — **UK1** (a degraded stack is restored, `healed`
> row), **UK2** (a stack that stays unhealthy exhausts into `heal_exhausted`) —
> driving the real self-heal loop off the scripted health poller. **Mask L**
> (§4.13) covers the one-open-detail-per-table rule — **UL2** (a panel on another
> row closes the open one). **Mask M**
> (§4.14) covers the per-stack deploy-history panel (audit log, ADR-0033) —
> **UM1** (the Stacks detail's History tab lists the stack's terminal outcomes
> from `/api/audit`) — driven by real
> deploys (startup + a webhook bump). **Mask N** (§4.15) covers the self-heal row
> detail (ADR-0029 amendment) — **UN1** (a `healed` row has an empty Change cell;
> clicking it opens the deploy detail on its Drift tab, which lists the drifted
> service but not the healthy one) — driving the
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
  - `STUB_DOCKER_ROLLOUT_FILE=<path>` → a rollout service's container table,
    answered to `compose ps --format json`: the first plain `up` starts `old`, a
    `--scale` up adds a `canary` (healthy, or `STUB_DOCKER_CANARY_HEALTH`), `rm -- <id>` drops one (P24, P25).
  - `STUB_DOCKER_READINESS_FILE=<path>` → once the file exists, answers the
    readiness wait's `compose ps --all` with its JSON lines; absent, the read is
    empty and counts as ready (P26).
- **Failure attachments.** A failed test attaches the instance's stdout/stderr
  (`skipper-output`) *and* the UI's own diagnostics (`ui-notes`, read from
  `window.__uiNotes`). Both are collected **after** the test finishes, never
  subscribed to while it runs: attaching a console or network listener is itself
  enough to change the timing of a race, which is how the UC11 investigation
  repeatedly lost its own flake (T8).
- **Teardown kills the whole process group.** Each skipper starts as the
  leader of its own process group, and `stop()` SIGKILLs the group and waits
  until no member is left running (`fixtures/procgroup.ts`). Killing only
  skipper orphaned a stub `docker` or hook that kept writing into the temp tree
  while it was removed (`ENOTEMPTY` in the leak check). Zombies don't count: a
  container whose PID 1 does not reap keeps them forever.
- **No retries.** `retries` is 0 everywhere, CI included: a test that fails
  only sometimes is missing a deterministic seam, and a re-run would hide it.
  A failed test keeps its trace (`retain-on-failure`).
- **Stolen ports retry the launch.** The harness reserves ports by binding
  port 0 and releasing again, so between the release and skipper's (or a peer
  stub's) own bind another process can take the port — they come from the same
  ephemeral range every outbound connection draws from, and parallel workers
  make that a real, ~once-per-thousand event. `Skipper.start` detects the theft
  deterministically (skipper exits with its bind error — `waitFor` fails fast
  on a dead process instead of spinning out its deadline — or a stub's listen
  rejects with `EADDRINUSE`) and relaunches on fresh ports, up to three
  attempts; any other failure propagates immediately. Each attempt also gets a
  freshly scaffolded workspace (`retryOnFreshWorkspace`): skipper runs its
  startup deploy before it binds the port, so a reused state dir would make
  the relaunched instance skip every stack as unchanged. `relaunch()` deliberately
  keeps its ports (the browser's origin must survive), so it carries the
  residual risk.
- **Fonts settle before interaction.** The `page` fixture awaits
  `document.fonts.ready` after every navigation (`goto` and `reload`): the
  web fonts' `font-display: swap`
  is the page's one late reflow, and a swap between Playwright computing a click
  point and dispatching it moves the target out from under the click (the UC11
  root cause — the queue empty-note wraps to a second line when JetBrains Mono
  lands). Interacting with the autosync or Hosts drawer additionally waits for its
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
| `rolled_back_unhealthy` | like `rolled_back`, but the stack has a `deploy_health_check:` section and `STUB_DOCKER_FAIL_NTH_UP` lists the deploy, rollback **and** roll-forward `up` (e.g. `2,3,4` counting the startup deploy) — the health-gated rollback `up` fails, and so does the one roll-forward |
| `queued` | pause autosync (config-as-code or `POST /api/autosync`), then commit + webhook |

**Selector prerequisite (`data-testid`).** The UI has stable `id`s but no
`data-testid`, and rows are keyed by dynamic `event_id`. Add a refactor-proof
`data-testid` set and record it in `UI_SPEC.md`. Playwright selects only on
these, never on text/CSS. Minimum set:

- Deploys: `deploy-row` (+ `data-stack`, `data-status`), `status-badge`,
  `cause-line`, `change-more`, `row-sha`, `detail-panel`, `files-panel`, `diff-panel`, `empty-state`,
  `stack-icon`, and a `time-cell`/`duration-cell` marker for snapshot masking.
- Logs: `logs-panel`, `logs-live`, `logs-stat`, `log-line` (+ `data-level`),
  `level-badge`, `stack-prefix`, `cmd-prefix`, `diff-pill`, `log-search`,
  `log-wrap`, `follow-logs`, `log-fs`.
- Autosync: `autosync-btn`, `pending-pill`, `autosync-drawer`, `global-switch`,
  `stack-switch` (+ `data-stack`), `queue-item` (+ `wait-cell` for masking),
  `stack-filter`.
- Chrome: `view-toggle`, `view-options` (+ `time-mode`), `theme-toggle`,
  `conn-indicator` (+ `data-state`), `deploy-indicator`.

### Temp dirs never outlive the run

Anything a test, script or CI helper creates under the OS temp dir is removed on
success, failure and interrupt — a leak exhausts `/tmp`'s inodes for every other
job on the host.

- Go tests use `t.TempDir()`; `e2e`'s `TestMain` builds the binary into a dir its
  deferred removal clears on every exit path.
- Playwright: `fixtures/tempdirs.ts` (`TempDirs`) records every dir it creates.
  The `skipper` fixture calls `dispose()` in a `finally` and then fails the test
  if `workspaces.leaked()` is non-empty; `globalSetup` builds the binary into a
  per-run dir that its returned teardown removes; `ue-pwa` removes its updated
  binary in `afterAll`. `fixtures/tempdirs.test.ts` (run by `make ui-unit`)
  proves removal after a success and after a forced failure.
- `scripts/ui-preview.mjs` removes its tree on `exit` and on SIGINT/SIGTERM/SIGHUP;
  `make ui-preview-smoke` runs it with a fresh `TMPDIR` and fails if anything is left.

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
- **P13 — Held change waits for a retry** (ADR-0062, `STUB_DOCKER_FAIL_NTH_UP=2`):
  after a `rolled_back`, a webhook for the same commit reports SSE `held` with
  no new `up -d` and `skipper_stack_held{stack}` = 1; `POST
  /api/stacks/<name>/retry` → `202`, the change deploys (`success`), the gauge
  is deleted, and a further retry answers `409`.
- **P14 — A failed build names its cause** (ADR-0063, `STUB_DOCKER_FAIL_BUILDS=2`):
  a pushed Dockerfile whose `RUN` step fails emits a `failed` event whose error
  carries the apt error line and BuildKit's verdict, not only `exit status 1`;
  a second attempt (a retry, since the failure backs off — ADR-0064), whose stub
  output differs only in the elapsed stamp, emits the identical text.
- **P15 — A failure before start backs off, then holds** (ADR-0064,
  `STUB_DOCKER_FAIL_BUILDS=3`): after the first failed build a webhook for the
  same commit reports SSE `held` with no new `build` and no
  `skipper_stack_held`; two retries are the second and third failures, after
  which the gauge is `1` and a webhook again builds nothing; a push that fixes
  the Dockerfile deploys (`success`) and the gauge is deleted.
- **P16 — A failing health probe rolls back unhealthy** (ADR-0022, ADR-0072): a
  stack with `deploy_health_check: {url, timeout_seconds: 1}` deploys while a
  local probe server answers 2xx; once it answers `503`, a pushed change fails
  the gate, the rollback reruns it and fails too, and so does the one
  roll-forward → SSE `rolled_back_unhealthy` (never `rolled_back`), with exactly
  four `up -d` (startup, deploy, rollback, roll-forward).
- **P17 — The deploy gate is inferred from a compose `healthcheck:`** (ADR-0046,
  ADR-0072): with no `deploy_health_check`, a pushed compose that gains a
  `healthcheck:` is followed by skipper's own readiness read (`compose ps
  --all`; the startup deploy has none), while a sibling stack without one is
  never read; no `up` carries compose's `--wait`.
- **P18 — `deploy_health_check: false` suppresses the inferred gate** (ADR-0049):
  with that key on a stack, a pushed compose that gains a `healthcheck:` still
  deploys with a plain `up -d` and no readiness read, unlike P17.
- **P19 — `rollback: false` leaves the failure held** (ADR-0050,
  `STUB_DOCKER_FAIL_NTH_UP=2`): with that key on a stack, a pushed change whose
  `up` fails emits SSE `failed` (never `rolled_back`), runs exactly two `up -d`
  (startup, deploy) and no `down`, and `skipper_stack_held{stack}` = 1.
- **P20 — The bootstrap run skips `pull` and `build --pull`** (ADR-0051): with an
  empty state, a fresh instance with an image stack and a `build:` stack starts
  with no `pull` and a `build` that carries no `--pull`; a pushed image bump and
  a pushed base-image change then deploy with `pull` and `build --pull`.
- **P21 — A revert releases a held change** (ADR-0068, `STUB_DOCKER_FAIL_NTH_UP=2`):
  after a rollback holds the change, a pushed revert converges `web` with exactly
  one more `up -d --remove-orphans` and `skipper_stack_held{stack}` is deleted.
- **P22 — A failed converge keeps the hold** (ADR-0068,
  `STUB_DOCKER_FAIL_NTH_UP=2,4`): the revert's converge `up` fails, SSE `failed`
  is emitted and `skipper_stack_held{stack}` stays 1.
- **P23 — A failing `post_deploy` hook holds the change** (ADR-0062): with every
  `up` succeeding, a pushed change whose `post_deploy` hook fails emits SSE
  `rolled_back` and raises `skipper_stack_held{stack}` = 1 with exactly three
  `up -d` (startup, deploy, rollback); a second webhook for the same commit
  emits `held` and runs no further `up`.
- **P24 — A rollout cuts over without a gap** (ADR-0040,
  `STUB_DOCKER_ROLLOUT_FILE`): a stack with `rollout: {services: [app]}` whose
  service is routed by Traefik labels (no host port, a `healthcheck:`) starts
  with a plain `up`; a pushed image bump then runs exactly `up --no-recreate
  --scale app=2`, `stop -- old` and `rm -- old`, in that order, and reports
  `success` (never `rolled_back`).
- **P25 — An unhealthy canary keeps the old container** (ADR-0040,
  `STUB_DOCKER_CANARY_HEALTH=unhealthy`): the same stack with
  `health_timeout_seconds: 1` runs `up --no-recreate --scale app=2`, then
  `stop -- canary` and `rm -- canary` and nothing else — the old container is
  never stopped, no git-restore `up` — emits `rolled_back`, and the container
  table holds only `old`.
- **P26 — A still-starting container is held, not rolled back** (ADR-0072,
  `STUB_DOCKER_READINESS_FILE`): with `deploy_health_check.timeout_seconds: 1`
  and the container scripted `starting`, a pushed bump emits `failed` whose
  error says `still starting`, `not rolled back` and names
  `POST /api/stacks/web/retry`; no rollback or roll-forward `up` runs and
  `skipper_stack_held{web}` is 1. Once the container reads `healthy`, a retry
  deploys it (`success`).
- **P27 — A success is notified** (ADR-0020): with a `generic` notification
  target at a local recorder, the startup deploy and a pushed image bump each
  POST a `success` payload for `web`.
- **P28 — A failure is notified** (ADR-0020, `STUB_DOCKER_FAIL_NTH_UP=2`): with
  `rollback: false`, the failed deploy POSTs one `failed` payload carrying the
  error and no `rolled_back`.
- **P29 — A rollback is notified** (ADR-0020, `STUB_DOCKER_FAIL_NTH_UP=2`): the
  failed deploy POSTs one `rolled_back` payload, no `failed`, and no second
  `success` beyond the startup one.
- **P30 — A newer tag is notified** (ADR-0054): `web` runs `<loopback
  registry>/app:1.0` and the local registry lists `1.1`; with
  `update_check.notify: true` one `"type": "update"` payload (`latest: 1.1`) is
  POSTed to the generic target.
- **P31 — Updates do not notify by default** (ADR-0054): same setup without
  `notify`; the gauge `skipper_updates_available` reaches 1 (the check ran) and
  nothing is POSTed.
- **P32 — `allowed_hosts` guards the UI, not the core routes** (ADR-0067): with
  `allowed_hosts: [skipper.example]`, `GET /` and `GET /api/audit` answer 200
  for that Host and 421 for `evil.example`, while `/healthz` stays 200 and an
  unsigned `POST /webhook` gets 401 (the handler, not the Host guard) under the
  foreign Host.
- **P33 — An unknown config key fails startup** (ADR-0043): a `skipper.yml`
  with `no_such_setting: true` makes the binary exit 1 and name the key in its
  output.
- **P10 — Health watch journey** (ADR-0031, `STUB_DOCKER_HEALTH_FILE`): with a
  `health_watch` block and a local generic target, the baseline observation
  never alerts; flipping the stub's health table to `unhealthy` POSTs a
  `"type": "health"` alert (deploy-correlated via the startup deploy), flipping
  back POSTs the recovery, and `healthwatch.yaml` records the phases. This is
  the config→wiring→watcher→alerter through-line the unit tests cannot see.
- **P11 — Health watch alert cooldown** (ADR-0031 amendment): with
  `alert_cooldown_seconds: 3600`, the first unhealthy alert and its recovery are
  both POSTed; a repeat failure inside the cooldown is persisted in
  `healthwatch.yaml` as `suppressed: true` while the recorder still counts
  exactly two alerts.
- **P12 — Dependency ordering** (ADR-0032): with `app` depending on `db`, a push
  changing both fails `db`'s `up` (`STUB_DOCKER_FAIL_NTH_UP=3`); `app` gets a
  `blocked` event and no `up`, `/api/queue` lists it, and
  `skipper_deploys_blocked_total{stack="app"}` is at least 1.

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
- **UA7 — Row detail.** For an event with `changed_files`, clicking the row
  opens its deploy detail (`detail-panel`, in the side column at the suite's
  1280 px) on its
  Changes tab with the `files-panel`; clicking again closes it.
- **UA8 — Diff panel.** For a `has_diffs` event, clicking the row fetches
  `/api/events/{id}/diffs` and renders `diff-panel` inside the deploy detail with
  additions/deletions/hunk colouring. *Snapshot pending the owner's look at the
  new panel.*
- **UA9 — Failed deploy detail.** Clicking a `failed` row opens its deploy
  detail: `detail-verdict` (`Failed at docker compose up`) comes before
  `detail-facts`, and the Output tab's `deploy-output` carries the error.
- **UA10 — Empty state.** A fresh UI with no events shows `empty-state`.
- **UA11 — Queued row replaced on resume.** With autosync paused globally, a
  pushed image bump leaves a `queued` row beside the startup success; resuming
  drains the queue and the stack deploys, so `web` shows two `success` rows and
  no `queued` one — the stale row is replaced, not left standing.
- **UA12 — Queued row diff.** The paused stack's `queued` row is flagged
  `data-has-diffs`; clicking it fetches `/api/events/{id}/diffs` and opens the
  `diff-panel` (not the plain `files-panel`) with `nginx:1.25` as the deleted
  and `nginx:1.26` as the added line.
- **UA13 — Deploy detail tabs by keyboard.** A failed row's deploy detail
  follows the ARIA tabs pattern like UBC9: End selects and focuses the last tab
  and the `tabpanel` is `aria-labelledby` it; ArrowRight wraps to the first.
  Verified to fail without the deploy panel's keydown handler.
- **UA14 — Deploy row by keyboard.** A failed row's `.row-toggle` (its stack
  name, `role=button`) opens the deploy detail on Enter and flips
  `aria-expanded`; Escape from a detail tab closes it, resets `aria-expanded`
  and returns the focus to the toggle; reopened, Escape on the toggle itself
  closes it too.

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
- **UC9 — Queued row + cause.** A `queued` event yields a `deploy-row` with the
  `queued` badge and an `autosync paused for every stack` cause line; a later real deploy supersedes it.
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
- **UC16 — The drawer opens at its top.** In the Logs view on a 744×620
  viewport with ten stacks and the open transition slowed to 3s, opening the
  drawer lands focus on `global-switch` with the drawer's `scrollTop` still 0;
  once `data-settled`, the global row lies inside the drawer, not clipped above
  its edge.

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
  hidden; the **table** collapses to its two-line layout (name/time over
  change/result), the When cell's duration hides, and tapping a row with
  changed files opens its deploy detail as the phone sheet (Maske BF). The 1280px control asserts
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
- **UD13 — OS colour scheme.** Run under an emulated light OS (the suite
  itself emulates dark, which the baselines show): with no saved choice the
  page opens light, follows an `emulateMedia` flip to dark live, and once the
  toggle saves a choice that choice wins over the OS, across a reload too.
- **UD14 — Mobile theme row.** At 390 px the header `theme-toggle` is hidden
  and the `view-options` popover shows `deploy-theme-toggle` (hidden on
  desktop); tapping it switches to light, marks it `.active` and saves
  `colorScheme`.
- **UD5 — Version label.** The header `brand-version` shows the deployed version
  as `v<semver>`. `globalSetup` injects the version via `-ldflags` from
  `.release-please-manifest.json` (the same source as the Docker/Nix builds), so
  the case asserts the ldflags → `GET /api/version` → header render through-line
  against the exact shipped version (release-proof: both sides read the manifest).
- **UD11 — Tap-tip opt-in on non-header controls.** A leaf-level `data-taptip`
  control (a service's `clog-btn` in the Stacks detail's Containers tab) flashes `.tap-tip` on a synthetic touch
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
each stack's container state is scripted through the stub docker
(`skipper.setStackHealth` writes the health table the batched `docker ps` +
`docker inspect` read, ADR-0069), so it is deterministic and offline — no
real docker, no real containers. Behaviour-only (no snapshot).

- **UH1 — Health pill per stack.** Three stacks (`web`, `db`, `cache`) get
  scripted `ps` output — running/healthy, running/unhealthy, exited(0) — and each
  stack's Stacks `roster-row` shows a `health-pill` whose `data-health` is the rolled-up
  status (`healthy` / `unhealthy` / `stopped`).
- **UH2 — Per-service panel.** A stack with a healthy and an unhealthy service
  rolls up to `unhealthy`; the Containers tab of the stack's Stacks detail shows
  the `health-panel` listing every `health-service`, and closing the detail
  removes it.
- **UH3 — Deploy rows carry no health pill.** Health is a current value and a
  deploy row is history: a pushed change that prepends a second deploy row
  leaves no `health-pill` on either, while the Stacks row keeps its pill.
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
- **UH6 — retired.** The pill is no longer a control (the roster row opens the
  detail), so there is no keyboard path of its own to pin.
- **UH7 — Folded timeline.** A routine restart cycle (`ps` flips healthy →
  starting → healthy, each accepted phase awaited via the pill) does **not**
  grow a `starting` line: the timeline folds it into the current healthy line
  as `· up in Xs` (two `health-phase` rows, not three), the `health-strip`
  renders one segment per phase, and the `health-fold-toggle` (`all 3 phases`)
  swaps the folded view for the verbatim `.hp-raw` list — starting in the
  middle — and back (UI_SPEC "Status history").

### 4.10 UI — Maske I: Diff-panel commit metadata + row binding

The deploy detail's commit metadata and its binding to the deploy row. A
webhook that bumps a stack's image commits against the startup commit, so the
deploy carries a real diff **and** commit metadata; the harness commits as author
`e2e` with message `bump <stack> to <tag>`, making the facts deterministic.
Behaviour-only (no snapshot).

- **UI1 — Commit fact.** After a bump deploy, the deploy detail's head names
  `web` with a `success` badge, and its Commit fact shows the 7-char short SHA
  and the subject (`bump web to 1.26`), loaded lazily with the diff.
- **UI2 — Row binding.** Opening the detail marks the row `audit-open`, the panel
  carries `data-status="success"` and sits in the side column; the head's
  `detail-close` closes it and clears the row.
- **UI3 — Multi-commit range.** Two commits before a single deploy put the
  newest (`bump web to 1.27`) in the Commit fact and add a Commits tab counting
  `2`, whose `diff-commit-list` lists both commits.

### 4.11 UI — Maske J: rolled-back deploy detail

The rolled-back deploy detail: the rollback verdict first, then the diff now
carried on the terminal event. Startup up#1 succeeds (sets `LastDeployedCommit`); the deploy's
up#2 fails and the rollback up#3 succeeds → `rolled_back` (`STUB_DOCKER_FAIL_NTH_UP=2`).
Behaviour-only (no snapshot).

- **UJ1 — Opens on click.** No error box rides the list: the rolled-back row
  stays closed until clicked, then its deploy detail (`data-status="rolled_back"`)
  sits in the side column and the row is `audit-open`.
- **UJ2 — Verdict before diff.** The rolled-back event reports its diff, so the
  row is `data-has-diffs="1"`; clicking it opens one deploy detail whose
  verdict (`Rolled back to app 1.25`, naming `docker compose up`) precedes the
  `diff-panel` in document order.

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
  `dockerUps`); once the stack reports healthy again, exactly one `healed` row
  appears (a heal is reported on recovery, ADR-0029 amendment). `max_attempts:
  5` so it never exhausts.
- **UK2 — Give up.** A stack that stays unhealthy after its one allowed heal
  (`max_attempts: 1`) trips the circuit breaker: a single `heal_exhausted` row
  whose deploy detail reads `Self-heal gave up` with the give-up error in its
  Output tab, emitted once (not per poll), and
  no `healed` row — the redeploy that did not restore it is no heal.

### 4.13 UI — Maske L: one open detail per deploy table

A deploy row opens exactly one panel, its deploy detail (the health panel moved
to the Stacks detail), and a detail opened on another row closes the open one
(UI_SPEC "Expandable panels", detail selection), so the variant-A row binding is
never split across two panels. Behaviour-only (no snapshot).

- **UL1 — retired.** It pinned the health panel against the deploy detail on one
  row; deploy rows no longer carry a health pill.
- **UL2 — Across rows.** With two stacks bumped in one run, opening `api`'s row
  while `web`'s deploy detail is open closes it: `web` loses
  `audit-open`, one deploy detail remains, and it is `api`'s, in the side column.

### 4.14 UI — Maske M: per-stack deploy history (ADR-0033)

The History tab of a stack's Stacks detail lists the stack's durable terminal
deploy outcomes, fetched from `/api/audit`. The harness runs the real
backend, so the records come from real deploys: the startup deploy plus a
webhook image bump give two `success` records. Behaviour-only (no snapshot).
UM5/UM6 cover the panel's fold — runs of routine outcomes collapse into one
summary line, with the verbatim list behind a toggle (UI_SPEC "Deploy history").

- **UM1 — History tab content.** The History tab of `web`'s Stacks detail shows
  the `audit-panel` with two `audit-row`s, newest first, each
  `data-status="success"` and carrying the deployed commit's short SHA; closing
  the detail removes it.
- **UM2–UM4 — retired.** They pinned the deploy-row `history-btn` (newest row
  only, exclusion with the deploy detail, a queued row's orphaned panel); the
  history lives only in the Stacks detail now.
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
- **UM7 — A repeated failure is one counted record, split by cause.** With
  `STUB_DOCKER_FAIL_ON: up` every deploy fails, so the failing startup deploy
  plus three failing bumps give four *attempts*. Since ADR-0056 the repeats
  collapse where the records are stored, so the panel holds **two** rows, both
  with their `.ar-err` line: the startup failure (no previous commit to restore,
  so it fails with its own message — a different cause is a separate incident)
  and the run of three, whose row carries `repeat-note` `×3`. No `audit-fold`
  appears: there is nothing left for the display-time fold to do. The loop
  settles on **occurrences**, not record count — once repeats collapse the
  record count stops growing, so counting records would let the next push land
  mid-run and be coalesced away (Invariant 7).

### 4.15 UI — Maske N: self-heal row detail (ADR-0029 amendment)

A `healed` row is not a git deploy, so it has no changed files and no diff: its
Change cell stays empty, and clicking the row opens a deploy detail explaining the corrective redeploy and listing the services that had
drifted when it ran (from the `heal_drift` carried on the event). Builds on the
real self-heal loop of Maske K: `initialHealth` seeds two services healthy before
boot, then one service (`app`) is turned unhealthy while the other (`db`) stays
healthy — the rollup goes unhealthy so self-heal fires, but only the degraded
service is listed. Once the corrective `up` is seen in `dockerUps`, the stack is
turned healthy again — the healed row appears only on that recovery (ADR-0029
amendment of 2026-09-29). Behaviour-only (no snapshot).

- **UN1 — Drift detail.** The healed row's Change cell is empty; clicking the
  row opens the deploy detail on its Drift tab, whose verdict notes there is no
  diff and whose `heal-drift` lists the drifted service `app` (`unhealthy`) but
  not the healthy `db`. A second click toggles it closed.
- **UN2 — retired.** It pinned the health panel against the deploy detail on
  the healed row; deploy rows no longer carry a health pill.

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
  for the reserved `_config` stack whose deploy detail explains the file is no
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
  both stacks fails the dependency and blocks `app`: the blocked row's
  `cause-line` (text and `title`) reads the literal `blocked by dep<img>x` and
  contains no injected `<img>` element, and the dependency's own row shows the
  hostile name verbatim in its stack cell.
- **UP2 — The cause sits under the stack name.** The cause is its own line below
  the name rather than a tag beside it, so at 1400, 900 and 390px the name stays
  whole (`.stack-name` is not clipped) and the cause stays visible beneath it,
  ellipsising inside its own line.

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
  and excluded. It opens itself (an `orphaned` project is present) with the count
  pill tinted, and the header still toggles it shut and open by hand; the two
  items carry the right `data-class`;
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
row/column/expand language (`docs/dev/ui-design-concept.md`). Boots in discovery
mode with `stacks: ['api', 'web', 'wip']`, a host-config override parking
`wip` (`disabled: true`), and `healthPoll` + `initialHealth` for api/web so the
containers panel is populated. Behaviour-only (no new snapshot; the default
deploy view is unchanged, so the full-page `ud-chrome` baselines still hold).
The `never deployed` state is unit-tested (`internal/roster`) — in discovery mode
the first sync deploys every stack, so it is not deterministically seedable in
e2e — and shares its `.roster-flag` rendering with the disabled row.

- **UR1 — Aligned inventory table.** The stacks view shows an aligned column
  header (`Stack · Health · Version · Last deploy`, no count/title line) and one
  row per declared stack (api, web deployed → success badge; wip parked →
  `.disabled` row with the `disabled` flag and no badge), enabled sorted before
  disabled; the deploy table is hidden and restored on switching back.
- **UR2 — Click a row for its detail panel.** Clicking a row opens its
  `detail-panel` on the Containers tab (`health-panel` with a `health-service`,
  from the health snapshot); the History tab swaps in the deploy-history panel
  (`data-audit-for` = stack) in place of the containers. Clicking the row again
  closes the panel, and opening another row moves it there (one stack at a
  time). The panel itself is Maske BC (§4.56).
- **UR3 — Search.** A printable key reveals the filter and seeds it; a substring
  match narrows the rows with a `shown/total` count; a no-match query shows the
  empty note; first `Esc` clears, second folds the bar away.
- **UR4 — Mobile search entry.** On a narrow viewport the stacks view-options
  popover carries a desktop-hidden "Search stacks" row that reveals and focuses
  the filter, which then narrows the rows the same way.
- **UR5 — Shared time mode.** The stacks popover's `Absolute time` toggle (the
  shared `timeMode`) switches the roster's relative times to absolute.
- **UR6 — An open panel survives a republish.** A `stacks` snapshot lands after
  every run and rebuilds the roster. With a row's panel open on its Triggers tab,
  the panel comes back on the same row and tab carrying the **new** snapshot —
  asserted end to end: the
  change-detection lead reads `Unchanged since the last deploy.` before a push
  and `Unchanged since <sha>.` after the pushed change deployed, with no click
  in between. Without the re-open the panel is simply gone (the assertion fails
  on a missing element), which is what made UAJ1b flaky.

### 4.20 UI — Maske S: cross-view stack jump

The compass jump button — in the head of a row's detail panel, in both views
(§4.56) — that switches between the Deploys and Stacks views and lands on that
stack's row there (`internal/ui/UI_SPEC.md#cross-view-stack-jump`). Two
`test.describe` blocks: the default two-stack boot (`api`, `web`) for the
landing/regression cases, and a discovery+disabled boot (mirroring Maske O/R's
`wip` fixture) for the no-landing-target case. Behaviour-only (no snapshot).

- **US1 — Deploys → Stacks.** Clicking the jump button in a deploy row's detail
  switches to the Stacks view; the (sole) roster row for that stack is visible,
  briefly carries `.jump-target`, which clears again on its own, and has its
  detail open.
- **US2 — Stacks → Deploys lands on the newest row.** With two deploys of the
  same stack (two rows, newest first), jumping from the roster flashes
  `.jump-target` on the first (newest) row only — never the older one.
  Where the roster view is *inventory* (one row per stack), the deploy table
  is a *log*, so "the stack's row" is ambiguous there and the newest wins.
- **US3 — The jump leaves the panel it sits in open.** A jump only switches
  views: the deploy detail it was clicked in is still there, and the roster
  detail panel stays open on its tab (`data-tab=triggers`) on the way back to
  Stacks.
- **US4 — No landing target.** A parked (`disabled: true`) stack has a roster
  row and a jump button in its detail panel, but no deploy row (it has never deployed). Jumping
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
per-container icons appear on the Containers tab's service lines and the `?services=`
selection validates; the stub `docker` answers `compose … logs` with a fixed
backlog (a single service drops the compose prefix, the whole stack and a
multi-service subset keep `<stack>-1  | `). Behaviour-only (no snapshot).

- **UT1 — Per-stack panel.** The Logs action (`clog-btn`) in the head of the
  newest row's deploy detail opens a `clog-panel` that streams the backlog (the
  merged view keeps the `web-1` service prefix) and shows the `clog-live` pill;
  clicking the action again closes it.
- **UT2 — Per-container panel.** On the Containers tab of the stack's Stacks
  detail, clicking a `health-service` line's `clog-btn` opens that one service's
  log (scope `web / app`, no compose prefix).
- **UT3 — Single log open.** Opening a second log (another stack's detail
  action) closes the first — at most one `clog-panel` exists at a time.
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

- **UU1 — Badge + panel.** The `hooks-badge` is an action in the head of a
  stack's Stacks detail: `api` (no hooks) has none; `web`'s shows the split
  `2+1` count and a `pre-deploy hooks: 2` title. Clicking opens the
  `hooks-panel` listing the three `hooks-cmd` lines verbatim; clicking again
  closes it.
- **UU2 — Log attribution.** In the Logs view, filtering to `web` shows the
  hook's `echo` output as a `log-line` with a `[web]` `stack-prefix` — the
  attribution that lets the hook log filter by stack.
- **UU3 — Running phase + inline log.** With the held hook in flight, the
  deploying `web` row shows the `hook-phase` (`pre_deploy hook …`) and no
  `hooks-badge` (the pulse rides the Stacks detail, UU4). The phase's logs icon opens the
  container-logs panel **in skipper mode** inline (the `deploys-table` stays
  visible — no page jump), streaming the stack-filtered `/api/logs` (the
  `clog-body` shows the hook output). The test calls `releaseHook()` once done
  so the deploy settles before teardown.
- **UU4 — Phase in the roster.** The same running-hook phase renders on the
  Stacks `roster-row` for `web`, identical to the Deploys view. The row carries
  no hooks control; once its detail panel opens, the panel's `hooks-badge`
  action carries the pulse (`data-hook-active`) at once — a panel opened
  mid-hook must not wait for the next hookrun frame.

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
  tagged with a `host-mono` chip. The two hosts' dots render distinct palette slots
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
  the drawer `host-c`'s reachability dot reads `down` beside the visible
  `host-reach` label `unreachable` (also in the row's `aria-label`) and `host-b`'s
  dot reads `up` with no label, a
  peer carries an "open its own UI" `host-link` and `self` does not.
- **UV5 — Merged roster.** The Stacks view lists local and peer stacks together,
  host-tagged; a peer `roster-row` shows its status badge and host chip but no
  `jump-btn` / `clog-btn`, and the same chip-filter isolates the roster to one host.

- **UV6 — Peer row never dead-ends.** A peer row is a read-only mirror, but
  clicking it opens the same deploy detail as a local row (`data-host` = the
  peer; Commit and Files facts from the audit record) and loads the peer's diff
  into its Changes tab through the
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

- **UW1 — Affordances.** The `_nixos` row's deploy detail has a close button but
  **no** `jump-btn` (it is not in the Stacks roster) and **no** container-logs
  `clog-btn` (it is not a compose project).
- **UW2 — Click never dead-ends.** A diff-bearing `_nixos` row (`data-has-diffs=1`)
  opens its diff/files panel on a row-body click. (A row with
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

- **UX1 — retired.** It pinned the health pill on peer deploy rows; deploy rows
  no longer carry one, so the fanned-in health shows on the roster (UX2).
- **UX2 — Inline pill on peer roster rows, app-link in the panel.** In the
  Stacks view a peer `roster-row` shows the `health-pill` inline and no
  `app-link-btn`; the link rides the head of its `detail-panel.peer-panel`. Local
  roster rows carry the pill too (the roster surfaced no live health for anyone
  before).
- **UX3 — Peer deploy → containers.** A peer row's deploy detail carries **Show
  in Stacks** (`jump-btn`), which lands on the peer stack's Stacks
  `detail-panel` (`data-host="host-b"`) with its `health-panel` (`health-service`
  lines from the fanned-in health), each carrying a per-service `clog-btn` (its
  streaming behaviour is Maske Y).
- **UX4 — Healthwatch timeline.** **Show in Stacks** from the failed peer
  `postgres` row's deploy detail opens the peer stack's Stacks detail (never the
  primary's own local health panel); its Containers tab renders the fanned-in
  `healthwatch` as the `health-history` / `health-phase` status timeline.

### 4.26 UI — Maske Y: Peer container logs (ADR-0048)

The last local-only affordance closed. The browser can't reach a peer
cross-origin, so the primary proxies the peer's container-logs SSE stream at `GET
/api/peers/{name}/container-logs/{stack}` (service selection rides `?services=`;
the streaming sibling of the diff proxy). The peer's containers panel now carries
the same per-service log button local stacks have. The harness stub peer
(`host-b`) streams canned SSE frames for `gitea` so the proxy has something to
forward. Behaviour-only.

- **UY1 — Peer container log streams through the proxy.** Opening a peer row's
  containers (**Show in Stacks** from its deploy detail) and clicking a container's `clog-btn` opens the live `clog-panel`; its `clog-body`
  fills with the peer's log lines, forwarded frame-by-frame through the proxy.
- **UY2 — Toggle closed.** A second click on the same peer `clog-btn` closes the
  panel, like a local container log.
- **UY3 — Survives the peers poll.** Every peers poll rebuilds the open peer
  panel; the open `clog-panel` moves into the rebuilt panel, still streaming and
  with its `clog-btn` still marked on, instead of vanishing with the old one.

### 4.27 UI — Maske Z: Unhealthy-stack visibility (ADR-0027 extension)

The header **health beacon** and the Deploys **attention band** lift a currently-
`unhealthy` stack out of the chronological log (where its newest row can sit far
down or age out). Both read the same live `health` snapshot via the pure
`attentionStacks()` helper; the harness enables the poller (`healthPoll`) and
scripts each stack's container state. Behaviour-only: both surfaces
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
overlays leave the rendered pixels unchanged); the 44px touch-screen targets do
(UAA7).

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
- **UAA6 — Drawer close button.** The autosync drawer's `drawer-close` is named
  `Close`; opening still focuses `global-switch` (not the close button), and
  `Enter` on the close button closes the drawer and returns focus to `autosync-btn`.
- **UAA7 — Touch targets.** Under `hasTouch` (so `pointer: coarse` matches — the
  test asserts it), every `.dp-act` and `.dp-tab` of an open deploy detail and the
  drawer close button measure at least 44px.

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

### 4.31 UI — Maske AD: deploy-row ⋯ overflow menu (retired)

UAD1–UAD5 covered the `⋯` menu (`more-btn`) that collapsed the newest deploy
row's history, container-logs and hooks actions. Deploy rows no longer carry
controls: history and hooks live in the Stacks detail, the Logs and Show in
Stacks actions in the head of the deploy detail. The mask is retired with the
menu and its IDs are not reused.

### 4.32 UI — Maske AE: the Stacks row's identity cell

UAE1–UAE6 covered the roster row's inline action cluster (jump, app link,
logs, hooks). The row no longer carries controls — they are the head of the
stack's detail panel, Maske BC (§4.56) — so those cases are retired with it
and their IDs are not reused. What remains is the row's identity on a tablet.
Behaviour-only.

- **UAE7 — An over-long name clips instead of moving.** At the same width a
  29-character stack name stays on the host chip's line, ellipsised, with the
  version beside it and the full name in the element's `title`. Ungrouped, the
  name took a line of its own below the chip and the version — aligned to the
  row's first line — ended up above it.

### 4.33 UI — Maske AF: Status-badge icons + solid worst states (T3.14)

Every status badge (§3, [Status badges](../../internal/ui/UI_SPEC.md)) leads with an
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
- **UAF2 — Healed carries an icon.** After the stack turns unhealthy, is
  redeployed and recovers, the `healed` badge also leads with a `badge-ico`. A
  second outage the one allowed redeploy cannot fix then yields the
  `heal_exhausted` badge below.
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
`.header-tour-seen` on `<html>` (§3, [First-run header tour](../../internal/ui/UI_SPEC.md#first-run-header-tour)).
Purely storage-gated — no timers — so shown/dismissed is deterministic. This spec
opts out of the shared fixture's default `seedTourSeen` so it lands on a genuinely
fresh browser; every other spec keeps the seed, so the steady-state header (not
the one-time tour) is what all other baselines and behaviour tests see — which is
why §5 needed no baseline churn.

- **UAG1 — Fresh browser shows the tour.** On first load the banner and the
  per-control captions (`Autosync`, `Theme`) are visible, and `<html>` is not
  yet marked `header-tour-seen`. The view switch has **no** caption — its buttons
  carry their own labels at this width — which the test asserts positively, so
  the caption row cannot creep back and double-label them.
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
[Loading vs. empty](../../internal/ui/UI_SPEC.md#event-lifecycle-sse) and
[Failed detail fetches](../../internal/ui/UI_SPEC.md#failed-detail-fetches). For T4.17,
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
  `load-error` inside the deploy detail's Changes tab (its facts stay visible)
  instead of a silent drop to the file list; Retry loads the real diff panel.
- **UAH5 — Peer-diff load-error + retry.** A `502` on the peer-diff proxy keeps
  the peer deploy detail's facts and shows `Couldn't reach <host> for the
  diff.` in its Changes tab; Retry loads the peer diff there.

Behaviour-only (no snapshot): the states are structural (`data-testid` + text),
and the skeleton shimmer / spinner are animations a visual baseline would have to
freeze anyway.

### 4.36 UI — Maske AI: Per-service image delta

A deploy carries `image_changes` (service, old, new) on its SSE payload; the row
surfaces which service updated — and to what — in its **Change column** (its own
line on a phone), without opening the diff. See
[Image delta](../../internal/ui/UI_SPEC.md#image-delta). Each case rewrites a stack's
compose (`setStackImage` / `setStackServices`) and fires a signed webhook, so the
deploy runs for real against stub docker and the delta renders from the actual
image change — no hand-fed events.

- **UAI1 — Tag bump.** A one-service `nginx:1.25 → 1.26` bump shows the service
  name (`app`) with the old tag struck through and the new tag in the add colour,
  in the Change column — right of the Stack cell and flush with its header (the
  alignment that makes a column worth it) — plus the `aria-label`
  (`app updated from 1.25 to 1.26`) and the full-reference `title`.
- **UAI2 — Service always labelled.** A lone service named after its own stack
  (`worker` in stack `worker`) still shows its service label — the chip names
  which service moved, not the stack — rendering `worker 2.0 → 2.1`.
- **UAI3 — One chip plus a count.** Four changed services render one chip (`app`)
  and a muted `change-more` `+3`, like the Stacks Version column; the row's
  deploy detail lists all four in its Change fact.
- **UAI4 — Digest-only rebuild.** A same-tag digest change (`nginx:1.25@sha256:…`)
  shows the shared tag `1.25` plus a `↻` rebuilt marker (no raw digest pair); the
  full digest is on the chip's `title`.
- **UAI5 — Responsive.** On a 390 px viewport the row is two lines: name and time
  on the first, the Change cell below the name and the result to its right on
  the second (asserted by bounding box, all boxes read in **one** measurement
  pass so a prepended row cannot have two reads describe different rows). The
  duration and SHA are hidden; the name still renders in full (no ellipsis).
- **UAI7 — Tablet.** On a 744 px viewport (iPad-mini portrait) the four columns
  stay on one line in column order — name · result · change · time — with
  `+1` beside the chip; the When cell drops its duration so the name keeps its
  floor rather than being squeezed away.
- **UAI6 — View-options toggle.** The Deploys popover's **Changes column** toggle
  (on by default) collapses the whole column when flipped off — no chips and no
  `Change` header; the off choice persists across a reload (`localStorage`), and
  toggling back on restores both. The row hairline is asserted present in **both**
  states: rows sit flush on one sheet, so the line is the sheet's structure, not
  the column's — the assertion is what keeps it from being tied back to the
  column.

Behaviour-only (no snapshot): the delta is structural (`data-testid="svc-delta"` +
per-part text), and the per-reference token logic is exhaustively covered by the
`app-helpers` unit layer (`imageDelta` / `parseImageRef` / `shortImageTag`).

### 4.37 UI — Maske AK: Service versions in the Stacks view

The health poll reports the image each container runs, which rides the `health`
snapshot: a roster row's **Version** cell names the service the stack is named
after plus its running version, and the expanded containers panel carries every
service's version — the same chip the Deploys Version column renders (Maske AI).
See [Service versions](../../internal/ui/UI_SPEC.md#service-versions). Health is
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
- **UAK6 — Tablet.** On an 820 px viewport the Last deploy cell gives up its
  SHA, so Version keeps the row's first line beside the name (asserted by
  bounding box, read in one measurement pass); the header keeps its Version
  label. The stack name is asserted present and non-zero.

Behaviour-only (no snapshot): the cell is structural, and the lead-service and
token logic are covered by the `app-helpers` unit layer (`rosterVersion` /
`imageRepoName` / `shortImageTag`).

### 4.38 UI — Maske AJ: Roster change detection

skipper redeploys a stack only when one of its hashed inputs changes
(Invariant 2), so the most common operator question is not "what happened" but
**"why did nothing happen"**. The Triggers tab of a roster row's detail panel
(§4.56) answers it in place: which inputs are watched, and the commit nothing has changed since. See
[Change detection](../../internal/ui/UI_SPEC.md#change-detection). Data rides the
`stacks` snapshot, so there is no fetch and no loading state.

- **UAJ1 — Watched inputs + since-commit.** Opening a cleanly deployed stack
  with no health data opens its panel on the Triggers tab (`data-tab=triggers`,
  no `audit-panel` beside it — one tab at a time); `watched-panel` leads with
  `Unchanged since <7-char sha>.`,
  and lists the stack's compose file **repo-relative** (the path the operator
  edits and commits, not the clone's absolute location). Closing the row removes
  it with the panel. The stack's hashed *config* renders as its own
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
[Commit links](../../internal/ui/UI_SPEC.md#commit-links). The harness clones from a
local path, which no forge URL can be derived from, so it sets `repo_web_url`
explicitly (`FORGE_URL`); the degradation case opts back out with
`repoWebURL: null`.

The roster cases run at the default desktop viewport on purpose: below the
tablet breakpoint the row's SHA is dropped (Maske AK, UAK6), so there is no
cell to link there.

- **UAL1 — The roster Commit cell links.** The cell is an `<a>` whose `href` is
  `<forge>/commit/<full sha>` — the **full** 40-char SHA, though the cell prints
  the 7-char form (a short SHA is a display convention some forges will not
  resolve). It opens in a new tab (`target="_blank"`, `rel` carrying `noopener`),
  since the UI is a live SSE stream that navigating away would cost.
- **UAL2 — The link does one thing.** The SHA sits in a row whose body toggles
  the detail panel; clicking the link must not also open it. The navigation is
  cancelled in the capture phase, so what is asserted is purely the row
  handler's guard — and the row still opens its panel when clicked anywhere
  else.
- **UAL3 — Panels, prose and the diff header too.** The deploy-history rows
  (`.ar-sha`, History tab), the diff panel's commit header (`commit-sha`) and the
  change-detection lead's (Triggers tab) `Unchanged since <sha>` link the same way: one helper
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

### 4.40 UI — Maske AM: A refused autosync write is announced

A failed toggle is invisible in the interface: the switch simply does not move,
which looks exactly like a click that never landed. That ambiguity cost real
time chasing a UC11 flake, so the write path now reports its failure on the
console — the same treatment a dropped stale snapshot gets (UC13). See
[Autosync](../../internal/ui/UI_SPEC.md#autosync).

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
[Fonts](../../internal/ui/UI_SPEC.md).

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
([UI_SPEC](../../internal/ui/UI_SPEC.md#rollback-linkage)):

- **UAQ1 — Retry verdict.** The retry's success detail opens with the
  `after_rollback` verdict line; its Open rollback action (`rollback-link`)
  carries the rollback's event id and flashes the rollback's own row
  (`.jump-target`). The pairing reads in both directions instead of the
  success papering over the rollback.
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
- **UAQ4 — Detail panel.** The Last deploy fact of the stack's detail panel
  (`detail-facts`) shows the outcome strip (oldest → newest: success,
  rolled_back, success) and the last-incident line naming the rollback the
  retry papered over.
- **UAQ5 — Logs.** The severity chips are thresholds and narrated outcome
  lines classify by outcome: the WARN-level `rolled back` outcome line stays
  visible under the `errors` chip (hiding it there is the incident's exact
  failure mode), while an ordinary INFO line is narrowed away.

Behaviour-only, no snapshot. The pinning exemption (no-op run summaries are
not pinned) is covered at the unit layer on both sides (`internal/logbuf`,
`app-helpers`), where eviction can be driven precisely.
- **UAQ6 — A clean window costs nothing.** Its own instance, with no bad outcome
  at all: the badge takes up no space, measured (`offsetWidth 0`) rather than
  assumed. The component's `display` beats the UA `[hidden]` rule, so hiding at
  zero has to be said explicitly in CSS — without that line an empty
  rollback-tinted pill sits in the header of every instance with a clean 24 h.
  Verified to fail against the unfixed CSS.
- **UAQ7 — A live deploy keeps the open panel whole.** With the stack's detail
  panel open and the next `up` held, a push turns the roster row `deploying`
  through the per-row live refresh (no `stacks` snapshot rebuilds it while the
  deploy is in flight); the panel stays open with its outcome strip and
  last-incident line. The refresh rewrites only the row's status cell.

### 4.45 UI — Maske AR: the Logs view fits the display

The 2026-08-05 report from a tablet: the log panel hung off the right edge of
the screen. The cause was layout, not content — `main`'s auto side margins
suppress the flex stretch in the Logs view's column, leaving it shrink-to-fit,
i.e. no narrower than its min-content, and one pre-formatted diff line inside
the pane is wider than a tablet. The mask pushes a change whose diff carries a
deliberately long image reference, then asserts the column stays inside the
viewport and the wide line scrolls where it belongs. It also pins the chrome the
same report reshaped ([UI_SPEC](../../internal/ui/UI_SPEC.md#log-view)):

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

### 4.46 UI — Maske AS: the rows line up on a phone

The 2026-08-10 report from a phone: the Stacks rows looked untidy. The cause was
the status column — an `auto` track whose contents were left-aligned, so the
widest element of each row set that row's width. The last-incident line
(`↺ rolled back · unhealthy · 8d21h ago`, nowrap) is by far the widest thing the
cell can hold: measured at 448 px it grew the track from 92 px to 231 px, which
left the version cell 0 px and cut the version chip off mid-glyph. The mask
drives a real rollback-then-retry — the sequence that leaves a papered-over
incident behind — at Pixel-9-Pro width and asserts the row's geometry
([UI_SPEC](../../internal/ui/UI_SPEC.md#responsive--700-px)). The incident line
has since moved into the detail panel (§4.56), so the row is two lines — name
and health pill, then version and last deploy — and UAS1/UAS2 assert that shape:

- **UAS1 — One right edge.** The health pill (first line) and the Last deploy
  cell — badge and time — (second line) end at the same x (±1 px). Left-aligned
  they only agreed when one happened to be as wide as the other.
- **UAS2 — The version keeps its width.** The version cell sits on the second
  line beside the last deploy (the badge's centre falls inside it, the cell
  ends before the badge), inside the row's box, with a real width — it was 0
  when the incident line sized the status column. The row's SHA is hidden.
- **UAS3 — A long chip keeps its change whole.** In the Deploys Change column
  (the narrow one) a long service name plus long tags at the same width: the
  label is really clipped (`scrollWidth > clientWidth` — a positive signal, so
  the assertion cannot hold trivially), the chip stays one line, and the change
  tokens end inside it. Split or cut, `1.5 → 1.6` reads as two versions.

- **UAS4 — The Deploys row shares the edge.** The same assertion on a deploy
  row, whose time sits on the first line and status cell (badge plus any note)
  on the second: the two views are one look, so repairing the roster alone would have left
  the sibling view ragged in exactly the way the report was about. It reads the
  edges only once the row has no `new-row` class: the entrance scales the row,
  so two reads during it land on different frames (the 1.7 px flake).
- **UAS5 — A live row's entrance plays once.** After the rollback-then-retry and
  a round trip through the Stacks view, no Deploys row carries `new-row` or a
  running animation. The class used to stay on, so every return to the view slid
  all live rows in again — the cause of UAS4's flake.

UAS1–UAS4 fail against the unfixed layout (three on geometry, UAS3 on the
missing token group); UAS5 against the build that never dropped `new-row`.
Behaviour-only, no snapshot.

### 4.47 UI — Maske AT: a stack removed from the repo (ADR-0036 amendment)

Detection answers *what is orphaned now*; this mask covers the other half — the
Deploys view recording *when* a stack left, against the commit that removed it.
The instance boots in discovery mode (`web` + `blog`) with the health poll on,
since the orphan half rides that cadence. The new `removeStack` harness helper
deletes the stack's directory in the origin repo and commits it; the containers
it leaves behind are scripted with `setOrphans`. Behaviour-only (no snapshot).

- **UAT1 — The removal is its own row, and what it left behind is not hidden.**
  Before the push nothing is orphaned, so the section is not even shown. After
  deleting `blog` and pushing, a `data-status="removed"` row for `blog` carries
  the `removed` badge, and the still-running project surfaces as `orphaned` with
  the section **opened by itself** — the surviving `web` stays managed and out of
  it.
- **UAT2 — Announced once, not per sync.** Every later sync re-reads a repo that
  still lacks the stack, so a naive comparison would post a row per reconcile
  tick. A second push that bumps `web` gives the positive signal that the run
  really happened and re-evaluated the set (its new success row — two now, with
  the startup deploy's), while the removed row count stays at 1.

### 4.48 UI — Maske AU: a standing failure does not flood the history (ADR-0056)

The shape this covers was measured on a live host: one stale override made every
five-minute reconcile fail identically for 16½ hours, which filled that stack's
whole 200-record audit window (one real deploy survived) and took 91 of the 100
slots in the global event ring. The instance boots one stack with
`STUB_DOCKER_FAIL_ON: 'up'` and `readiness: 'listening'`, so every deploy
produces the same `failed` outcome. Each webhook carries a new image tag: a
change that failed after it started is held rather than retried (ADR-0062), so
what repeats here is a new change failing the same way. Behaviour-only
(no snapshot).

- **UAU1 — The repeats become a count, not more rows.** The startup failure
  carries no marker (one occurrence). Each further webhook is settled through the
  count before the next is sent — deploys serialize on one mutex, so a burst
  would not produce one run each (Invariant 7) — and the row goes `×2`, `×3`
  while its count stays 1. The count is the positive signal the "no second row"
  assertion rests on: without the collapse it never appears while rows pile up.
  The marker's `title` names the run and its span.
- **UAU2 — It survives a reload.** The collapse lives in the stores, not in the
  rendering, so a reload rebuilds from the persisted history with the same single
  counted row.
- **UAU3 — The stack's durable history reads the same.** The deploy-history panel
  — the store the flood used to empty — holds one record for the run, carrying
  the same marker, rather than one row per attempt.

### 4.49 UI — Maske AV: the Stacks view's updates filter (ADR-0054 amendment)

Maske AO covers the per-chip `⇡` marker — "does *this* stack have an update".
This one covers the fleet-level surface on top of it: the always-visible header
badge counting the **stacks** an update is waiting on, and the single
updates-only toggle in the Stacks filter bar it presets and clears. The instance
reuses AO's registry stub, with one deliberate addition: `monitoring` (over
prometheus + grafana) updates **grafana**, a non-lead service. That is the case
that made the badge un-countable before the amendment — the row showed only
`2 services`, so the badge claimed a stack no marker on screen accounted for.
Behaviour-only (no snapshot).

- **UAV1 — The badge counts stacks, not services.** `monitoring` contributes one
  though only one of its two services updates; the pluralised
  `title`/`aria-label` says so in words. The badge reaching its final count is
  the positive signal every case below waits on — the check publishes after the
  startup deploys, so an assertion made earlier would pass against a UI that
  never counted anything.
- **UAV2 — Activating it lands on exactly what it promised.** The Stacks view,
  the bar revealed, the chip pressed, and only the two updated stacks standing.
  The control stack is hidden **by** the filter, not missing: UAV3's unfiltered
  count is what makes that absence meaningful.
- **UAV3 — The way out is the way in.** A second activation clears the filter,
  folds the bar and brings every row back.
- **UAV4 — The count is countable off the list.** Every row the filter leaves
  standing carries a visible `.td-upd`. `gitea` keeps its lead service (`server`,
  the one updating); `monitoring` steps off the lead to `grafana` with a `+1`
  pointer. Run against the pre-amendment build this fails on the `monitoring`
  cell, which reads `2 services` — the regression guard for the 3-against-2 the
  feature was reported over.
- **UAV5 — The toggle and the query narrow independently.** A query narrows
  within the updated set; a query no updated stack matches empties the list and
  the note generalises to *Nothing matches the active filters.* rather than
  blaming the name. `Esc` clears both, then folds.
- **UAV6 — The chip stands on its own.** Revealed by the header search trigger,
  toggled by hand, with the badge's `.active` state tracking the same state in
  both directions — including over a view switch: the lit state means "the
  roster below is the list this badge points at", so it goes out on the
  Deploys view and comes back with the narrowing intact.
- **UAV7 — Nothing to report costs nothing.** Its own instance, with no update
  check running: the badge takes up no space and the chip row is empty. The
  component's `display` beats the UA `[hidden]` rule, so hiding at zero has to
  be said explicitly in CSS — without that line an empty amber pill sits in the
  header of every fully-up-to-date instance, which is how it was caught (200 px
  of the `theme-dark` baseline). Verified to fail against the unfixed CSS.

### 4.50 UI — Maske AW: one open header pop-out at a time

The header carries five pop-out surfaces — the autosync, hosts and run drawers,
the view-options popover and the health-beacon popover — and they all park under
the header at the same anchor, so two open at once is never a layout the UI
means to show. Maske L pins the same rule one level down, on a deploy row's two
panels. Behaviour-only (no snapshot).

Every surface registers with the chrome, which both dismisses it on an outside
click and closes it when another one opens. Those are two different paths, and
the beacon is what separates them: its own click handler stops propagation, so
a click on it never reaches the shared outside-click dismissal. A surface that
relied on that dismissal alone therefore stayed open behind the beacon popover.

- **UAW1 — Opening the beacon popover closes an open drawer.** With the autosync
  drawer open, a click on the beacon leaves the popover showing and the drawer
  both closed and no longer reporting `aria-expanded="true"` — the drawer's
  button is the positive signal that the close actually ran, rather than the
  drawer merely being painted over. Verified to fail against the unfixed build.
- **UAW2 — And the other direction.** Beacon popover first, then the autosync
  button: the drawer opens and the popover is hidden with the beacon back to
  `aria-expanded="false"`.
- **UAW3 — The two header drawers replace each other, both orders.** Hosts then
  autosync, then hosts again: the one opening second is open, the first is
  closed, and its button reports it. Neither drawer named the other before the
  registry, so this is the pair the refactor must keep working.

### 4.51 UI — Maske AX: which containers a change reached (ADR-0059)

A deploy that moves no image still redeploys the whole stack, and the row could
say only how many files changed. The event now carries `file_changes` — per
changed file its kind and the services it reached — and the Changes column
renders a chip per kind. Behaviour-only (no snapshot; the column's own baseline
belongs to UA1).

Every case pushes for real: the harness commits the change and posts a webhook,
so the attribution comes out of the backend's comparison of the deployed
revision against the pushed one, not a fixture. The fixture stacks are a `web`
with an `app` service and a `web-restic` sidecar, plus a shared `shared.env`
both stacks list under `env_files`.

- **UAX1 — The row names the container the change reached.** The sidecar gains
  two environment variables and nothing else: the row shows one `compose
  web-restic` chip, in the Changes column aligned with its header, and does not
  name `app`. One change gives one chip, so no `change-more` count rides beside
  it.
- **UAX2 — A project-wide input reads stack-wide.** A change to the shared env
  file compose passes for the whole project renders `env stack-wide` with no
  service name and the matching screen-reader phrasing: naming two of the
  services would be a guess.
- **UAX3 — An image bump grows no second chip.** The compose file changed, but
  the version chip already names that service, so the row must look exactly as
  it did before this feature: the `app 1.25 → 1.26` chip is the cell's only one
  and no kind chip sits beside it. The version chip is the positive signal —
  without asserting it, "no kind chip" would also pass on an empty column.
  Verified to fail against the unfixed build (suppression removed).
- **UAX4 — The panel attributes each file.** Opening the row's diff panel, the
  `docker-compose.yml` file header carries the same `web-restic` note plus the
  title spelling out the kind, so a reader in the diff does not have to carry
  the row's chip in their head.

### 4.52 UI — Maske AY: the project_directory checkout is kept current (ADR-0060)

Compose serves a stack's relative bind mounts out of `--project-directory`, not
out of the clone the compose file was read from, so a project-directory checkout
nobody advances mounts stale content behind a green deploy.
`project_directory_sync` makes skipper fast-forward that checkout once per run,
before the stack phase — and refuse when the tree is dirty, which is the state
the operator who edits that tree leaves it in. Behaviour-only (no snapshot).

The fixture (`projectDirSync: true`) gives the instance a second clone of the
origin as `project_directory_base`; `dirtyProjectDir()` edits a tracked file in
it and `projectDirHead()` reads the commit it sits on.

- **UAY1 — A working fast-forward reports nothing.** A pushed compose change
  deploys and leaves no `_project_dir` row: the phase is plumbing, like the
  clone sync at the top of every run. The stack's own second row is the
  positive signal that the run really ran.
- **UAY2 — A dirty checkout is reported without blocking the deploys.** The
  refusal renders as a `failed` `_project_dir` row whose deploy detail names the
  uncommitted changes, the stack still deploys in the same run, and
  `projectDirHead()` proves the checkout did not move.
- **UAY3 — A standing refusal is reported once.** Two more pushes over a
  still-dirty tree add two stack rows and no second phase row (ADR-0055). The
  climbing stack-row count is the positive signal that the extra runs happened.
- **UAY4 — The phase row detail carries no container-logs or jump action.**
  `_project_dir` names a run phase, not a Compose project, so its deploy detail
  has neither `clog-btn` nor `jump-btn` — the same treatment `_nixos` and
  `_config` get (`isPseudoStack`). The stack row's detail, which does carry a
  `jump-btn`, is the positive signal that the omissions are omissions.

### 4.53 UI — Maske AZ: a held change and its retry (ADR-0062)

A change whose new version failed after it started is held: it waits for a new
commit or an operator retry instead of failing again every reconcile tick. The
instance boots `web` with `STUB_DOCKER_FAIL_NTH_UP: '2'`, so the first pushed
change rolls back (up#2 fails, up#3 restores) and the retry's up#4 succeeds.
Behaviour-only (no snapshot).

- **UAZ1 — The chip explains the hold, retry releases it.** After the rollback
  the Stacks row carries `held-chip`. Its `title` names the failure and says a
  new commit releases it. Opening the row puts an enabled `retry-btn` in its
  detail panel's verdict line. A click deploys the change once more. The next
  roster snapshot drops both, the badge reads `success`, and the Deploys view
  gains a second success row.
- **UAZ2 — A refused retry shows why and hands the button back.** With the
  POST intercepted as `409`, the click is announced (`retry: refused for web …
  409`). That console line is the positive signal the assertions rest on: the
  button is enabled again and reads `Retry once`, the line's `retry-msg`
  (`role="status"`) reads the server's reason, and the chip stays. The intent note
  `retry: requesting web` proves the click reached the handler.
- **UAZ3 — A pushed revert releases the hold.** After the rollback, a revert
  commit and webhook converge the stack (up#4); the chip and the retry button
  disappear without a retry click.
- **UAZ4 — A failed converge keeps the hold.** With `STUB_DOCKER_FAIL_NTH_UP:
  '2,4'` the revert's converge fails: the Deploys view shows a `failed` row (the
  positive signal) and the Stacks row keeps `held-chip`.
- **UAZ5 — A failing `post_deploy` hook holds the change.** Every `up` succeeds;
  the stack's `post_deploy` hook (`[ ! -f "$SKIPPER_E2E_HOOK_HOLD" ]`) passes at
  startup and fails once the test creates the hook-hold file. The pushed change
  rolls back and the Stacks row carries `held-chip`, with the retry button in
  its detail panel.

### 4.54 UI — Maske BA: a change that failed before it started backs off, then is held (ADR-0064)

A change that fails before any container is touched (`pre_deploy`, `pull`,
`build`) is retried after a growing wait and held after its third failure.
The instance boots `web` with `STUB_DOCKER_FAIL_BUILDS: '3'`; the test pushes a
Dockerfile, so the first three builds fail the way BuildKit reports a failed
`RUN` step. Each retry click is one attempt, which keeps the test off the
clock. Behaviour-only (no snapshot).

- **UBA1 — The chip reads `backoff`, then `held`.** After the first failure the
  Stacks row's `held-chip` reads `backoff`; its `title` says the change failed
  before it started (`1 attempt so far`), when the next attempt is due, and that
  it is held if it keeps failing. A retry click (in the row's detail panel) is
  the second failure (`2 attempts so
  far`, still `backoff`); a second click is the third, and the chip reads
  `held` with a `title` counting the 3 failures and the usual release text.

### 4.55 UI — Maske BB: the app runs under its strict CSP (ADR-0067)

Every main-port response carries a `Content-Security-Policy` with
`script-src 'self'`, so a stray inline script or handler would silently break
the page. A `securitypolicyviolation` listener is installed before any page
script. Behaviour-only (no snapshot).

- **UBB1 — Headers are set and no view trips the policy.** The shell response
  carries the CSP (`script-src 'self'`, `frame-ancestors 'none'`),
  `X-Frame-Options: DENY`, `nosniff` and `Referrer-Policy: same-origin`; a
  saved light scheme is applied (the external pre-paint script ran). After a
  deploy, its diff panel, the Stacks, Logs and Deploys views, no violation is
  recorded. The test then injects one inline script: exactly one
  `script-src-elem` violation is recorded and the script does not run, which
  proves the recorder works.

### 4.56 UI — Maske BC: the Stacks detail panel

A roster row carries no controls: clicking it opens one `detail-panel` under it
— head (name and actions), verdict, facts, and one tab of Containers · History ·
Triggers ([UI_SPEC](../../internal/ui/UI_SPEC.md#stack-detail-panel)). The main
instance boots discovery mode with a hooks-declaring `web` (with an app link),
a plain `api` (both with seeded health) and a parked `wip`; the held, phone and
peer cases take their own. Behaviour-only (no snapshot).

- **UBC1 — The default tab answers the state.** A healthy stack opens on
  Containers (`aria-selected`, `health-panel`), a parked one on Triggers
  (`watched-panel`, the `parked` verdict). Verified to fail with
  `stackDefaultTab` returning History for a healthy stack.
- **UBC2 — A held stack opens on History.** After a rollback holds `web`
  (`STUB_DOCKER_FAIL_NTH_UP: '2'`), its panel opens on History with the
  `audit-panel`, and the `held` verdict line (tone `rollback`) carries
  `Retry once`.
- **UBC3 — One order for the head.** `web`'s actions read app link · Logs ·
  Hooks (`1+1`) · Show in Deploys · close, in DOM order, once the app-link
  poll has landed (the positive signal the comparison waits on); `api`'s read
  Logs · Show in Deploys · close — an action that does not apply is absent.
- **UBC4 — Logs and Hooks from the head.** Logs opens the `clog-panel`. Hooks
  swaps the pane for the `hooks-panel` (`data-tab=hooks`, `aria-pressed=true`,
  no tab selected); a second click hands back the tab it came from, and
  choosing a tab while the pane is open leaves it.
- **UBC5 — Tabs, verdict and facts.** A healthy `api` reads the `ok` verdict
  (`Its service is healthy.`) and the facts `Running · Last deploy · Health ·
  Hooks` with `1 of 1 healthy`. History and Triggers each replace the one pane.
- **UBC6 — Close, and one at a time.** Opening a second row replaces the open
  panel (one `detail-panel`); `detail-close` removes it and the row's
  `audit-open`.
- **UBC7 — Phone.** At 448 px the four facts sit in two columns of two
  (asserted by bounding box: equal tops per pair, aligned left edges), the head
  actions drop their labels, and every action and fact stays inside the
  viewport.
- **UBC8 — A peer's panel is read-only.** A held peer stack's
  `detail-panel.peer-panel` shows the `held` verdict (the positive signal) with
  no `retry-btn` and no `jump-btn`, and keeps its close button.
- **UBC9 — The ARIA tabs pattern.** Each tab names the pane it controls
  (`aria-controls`) and the pane is labelled by the selected tab
  (`aria-labelledby`); only the selected tab has `tabindex=0`. ArrowRight,
  End, ArrowLeft and Home move both selection and focus, wrapping at the ends,
  and the pane follows. Verified to fail without the panel's keydown handler.
- **UBC10 — The Hooks pane is named.** With the head's Hooks pane open no tab is
  selected, so the pane carries `aria-label="Hooks"` and the first tab stays the
  strip's one Tab stop.
- **UBC11 — A row by keyboard.** Space on `api`'s `.row-toggle` opens its
  panel and sets `aria-expanded=true`; Escape from a tab closes it, resets
  `aria-expanded` and focuses the toggle; Enter reopens it and the close button
  (pressed by keyboard) hands the focus back to the toggle as well.
- **UBC12 — The hash follows the detail.** Opening `api` writes
  `#stacks/api`, choosing History adds `?tab=history`; the Deploys view clears
  the hash and coming back restores it with the kept panel; closing clears it.
- **UBC13 — A link opens its stack.** Loading `/#stacks/api?tab=history` lands
  on the Stacks view with `api`'s panel on History; editing the hash to
  `#stacks/wip?tab=nope` opens `wip` on its default Triggers tab and the hash
  drops the unknown tab. Verified to fail without the bootstrap's
  `followDeepLink` and without the `replaceState` write.
- **UBC14 — A peer link waits for its row.** Loading `/#stacks/host-b/cache`
  opens the peer panel (its `held` verdict the positive signal) once the peer
  poll brings the row; UBC8 checks a click on it writes that hash.

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

### 4.57 UI — Maske BD: day separators and the fold of routine deploys

The Deploys list stays flat; a day separator opens each local day and three or
more consecutive routine deploys of one stack fold behind one line
([UI_SPEC](../../internal/ui/UI_SPEC.md#day-separators-and-folding)). Every
webhook carries a new image tag, so each deploy is a `success` with an image
change. The pure rules (threshold, breaks, filter-before-fold, midnight) are
unit-tested in `app-helpers.test.js`. Behaviour-only (no snapshot); the
separator is masked in the full-page baselines, since its word depends on the
clock.

- **UBD1 — Fold and unfold.** Six deploys show the newest row plus the
  `deploy-fold` line (`↻ N more successful deploys of web`, `aria-expanded`
  false) with the older rows hidden; a click shows them, a second click folds
  them again.
- **UBD2 — Day separator.** The rows sit under a `deploy-day` line reading
  Today (Yesterday only if the run crosses midnight).
- **UBD3 — Filter before fold.** A name filter that matches nothing leaves no
  fold line on screen; the empty note is the positive signal that the filter ran.

### 4.58 UI — Maske BE: the rolling sub-label of a zero-downtime rollout (ADR-0040)

While a rollout cuts a service over, the `deploying` row carries a
`rollout-phase` sub-label (`rolling app`), identically in the Deploys view and
the Stacks roster ([UI_SPEC](../../internal/ui/UI_SPEC.md#rolling-service-live)).
The stack is routed by Traefik labels with a `rollout:` override; the stub's
container table (`STUB_DOCKER_ROLLOUT_FILE`) scripts the canary. A canary that
reads `starting` parks the cutover in its health wait until the test rewrites
the table, so the label stays observable with no wall-clock race. Behaviour-only
(no snapshot).

- **UBE1 — The label names the rolled service in both views.** A pushed bump
  with a `starting` canary shows `rolling app` on the deploy row and, after a
  switch to Stacks, on the roster row; once the canary reads `healthy` the
  label is gone and the deploy settles as `success`.
- **UBE2 — A failed cutover leaves no label.** A canary that stays `unhealthy`
  (`health_timeout_seconds: 1`) ends as a `rolled_back` row with no
  `rollout-phase` anywhere (the rolled_back row is the positive signal; the Go
  path is P25).

### 4.59 UI — Maske BF: the detail beside the list and the phone sheet (ADR-0073)

At a viewport of 1200 px or wider an open detail sits in its view's side
column (`detail-side`), right of a list narrowed to the rows' identity and
status; between 701 and 1199 px it stays inline right after its row, and at
700 px or narrower the side column is a full-screen sheet
([UI_SPEC](../../internal/ui/UI_SPEC.md#detail-placement)). The suite's
default 1280 px viewport is in side mode, so the masks that check a detail's
binding assert it there (`detailHome` in `fixtures/stack-detail.ts`); the
inline placement is asserted here at 1100 px. A resize
fires the breakpoint's `matchMedia` change, which re-places the open detail
before the assertion reads it. Behaviour-only (no snapshot).

- **UBF1 — Deploys, wide.** At 1280 px the deploy detail opens in the side
  column, to the right of its row; the row keeps its status and hides its When
  cell, which returns once the head's close button closes the detail.
- **UBF2 — Deploys, narrower.** At 1100 px the detail is the row's next sibling
  and the side column is empty.
- **UBF3 — Stacks, wide.** The roster detail opens in the Stacks side column;
  the row keeps its last-deploy badge and hides its time and Version cell.
- **UBF4 — Resize.** An open detail moves from the side column to inline and
  back as the viewport crosses 1200 px, staying open on its row.
- **UBF5 — Controls from the side.** Escape inside the side panel closes it and
  focuses the row's toggle; the head's Jump action lands on the Stacks detail,
  again in the side column.
- **UBF6 — A filtered-out detail crosses the breakpoint.** At 1100 px a roster
  filter hides the open row and its inline panel; widened to 1280 px the panel
  sits in the side column, still hidden; clearing the filter shows it there,
  beside the list.
- **UBF7 — Stacks, phone sheet.** At 390 px with 14 stacks the roster is
  scrolled to a low row, which opens its detail as a sheet covering the
  viewport and the header; wheeling over the sheet leaves the page's `scrollY`
  alone, and close returns to the same `scrollY` with the row in view.
- **UBF8 — Deploys, phone sheet.** The deploy detail takes the same sheet and
  the focus moves onto the panel; Escape closes it and focuses the row's
  toggle.
- **UBF9 — Resize from the phone.** Widening to 1100 px moves the open sheet
  inline under its row; narrowing again turns it back into the sheet.

### 4.60 UI — Maske BG: an open detail follows the live state

An open detail re-renders in place on a live update
([UI_SPEC](../../internal/ui/UI_SPEC.md#expandable-panels)). The health poller
runs every second over the stub's scripted container state
(`setStackHealth`); each test tags the open panel element, so an in-place
re-render is told apart from a closed and reopened panel. Behaviour-only (no
snapshot).

- **UBG1 — Stacks, Containers tab.** A detail open on Containers for a healthy
  one-service stack shows the stack unhealthy with two services once the
  health changes, on the same panel and tab.
- **UBG2 — Deploys, Logs action.** A stack seeded with no containers opens its
  deploy detail without Logs, after a second stack's health marker proves a
  snapshot landed; once the stack reports a container the same panel gains
  Logs.
- **UBG3 — Focus.** A focused Containers tab stays focused across the
  re-render a health change triggers.
- **UBG4 — Container log.** A service's log opened from the Containers tab
  is still open, once, inside the panel after a health change re-renders it.

### 5.1 Docs screenshots — rendered, never committed

The two images the docs landing page embeds
(`docs/user/assets/screenshots/{deploys,stacks}.png`) are **not** in git. A screenshot
of the UI is derived from the UI, and a PNG never delta-compresses — committing
one on every UI change would grow the repo without bound. They are rendered from
a seeded instance instead, by `screenshots/docs-shots.spec.ts` under its own
config (`screenshots/shots.config.ts`, kept out of the suite's `testDir`):

```sh
make docs-screenshots     # builds the binary, renders into docs/user/assets/screenshots/
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

Rebuilt from the suite, not from UI_SPEC: every case ID that appears in a test
title under `e2e/ui/tests/` or in a Go test's doc comment under `e2e/` has one
row, naming the file that runs it. §4 holds the Given/When/Then for each.

### 6.1 Pipeline (Go)

| Case | Test | File |
| --- | --- | --- |
| **P1** | `TestE2E_WebhookTriggersDeploy` | `pipeline_test.go` |
| **P2** | `TestE2E_UnchangedStackSkipped` | `pipeline_test.go` |
| **P3** | `TestE2E_StartupSyncDeploys` | `pipeline_test.go` |
| **P4** | `TestE2E_InvalidSignatureRejected` | `pipeline_test.go` |
| **P5** | `TestE2E_WrongBranchIgnored` | `pipeline_test.go` |
| **P6** | `TestE2E_HealthzReflectsSync` | `pipeline_test.go` |
| **P7** | `TestE2E_MetricsExposeCounters` | `pipeline_test.go` |
| **P8** | `TestE2E_RollbackOnFailedUp` | `pipeline_test.go` |
| **P9** | `TestE2E_PausedStackQueued` | `pipeline_test.go` |
| **P10** | `TestP10_HealthWatchAlertsOnUnhealthyAndRecovery` | `healthwatch_test.go` |
| **P11** | `TestP11_HealthWatchCooldownSuppressesRepeatFlap` | `healthwatch_test.go` |
| **P12** | `TestE2E_DependencyOrdering` | `pipeline_test.go` |
| **P13** | `TestE2E_HeldChangeWaitsForRetry` | `pipeline_test.go` |
| **P14** | `TestE2E_FailedBuildNamesItsCause` | `pipeline_test.go` |
| **P15** | `TestE2E_FailedBuildBacksOffThenHolds` | `pipeline_test.go` |
| **P19** | `TestE2E_RollbackDisabledLeavesFailureHeld` | `pipeline_test.go` |
| **P20** | `TestE2E_BootstrapRunSkipsPull` | `pipeline_test.go` |
| **P21** | `TestE2E_RevertReleasesHeldChange` | `pipeline_test.go` |
| **P22** | `TestE2E_RevertKeepsHoldWhenConvergeFails` | `pipeline_test.go` |
| **P23** | `TestE2E_FailedPostDeployHookHoldsChange` | `pipeline_test.go` |
| **P24** | `TestE2E_RolloutCutoverDrainsOldAfterCanary` | `pipeline_test.go` |
| **P25** | `TestE2E_RolloutUnhealthyCanaryKeepsOldContainer` | `pipeline_test.go` |
| **P26** | `TestE2E_StillStartingHoldsWithoutRollback` | `pipeline_test.go` |
| **P27** | `TestE2E_NotifiesSuccessToGenericTarget` | `notifications_test.go` |
| **P28** | `TestE2E_NotifiesFailureToGenericTarget` | `notifications_test.go` |
| **P29** | `TestE2E_NotifiesRollbackToGenericTarget` | `notifications_test.go` |
| **P30** | `TestE2E_UpdateCheckNotifiesNewerTag` | `updatecheck_test.go` |
| **P31** | `TestE2E_UpdateCheckDoesNotNotifyByDefault` | `updatecheck_test.go` |
| **P32** | `TestE2E_AllowedHostsGuardUIButNotWebhookOrHealthz` | `config_test.go` |
| **P33** | `TestE2E_UnknownConfigKeyFailsStartup` | `config_test.go` |

### 6.2 UI (Playwright)

The title is the case's test or `describe` title; the spec files sit in
`e2e/ui/tests/`.

| Case | Title | Spec |
| --- | --- | --- |
| **UA1** | deploy row transitions deploying → success in place | `ua-deploys.spec.ts` |
| **UA2** | status badges | `ua-deploys.spec.ts` |
| **UA3** | skipped deploys never render a row | `ua-deploys.spec.ts` |
| **UA4** | time mode | `ua-deploys.spec.ts` |
| **UA5** | stack icon + monogram fallback | `ua-deploys.spec.ts` |
| **UA7** | row detail | `ua-deploys.spec.ts` |
| **UA8** | diff panel | `ua-deploys.spec.ts` |
| **UA9** | error detail | `ua-deploys.spec.ts` |
| **UA10** | empty state | `ua-deploys.spec.ts` |
| **UA11** | the queued row is replaced when autosync resumes | `ua-deploys.spec.ts` |
| **UA12** | queued row diff | `ua-deploys.spec.ts` |
| **UA13** | deploy detail tabs by keyboard | `ua-deploys.spec.ts` |
| **UA14** | deploy row detail by keyboard | `ua-deploys.spec.ts` |
| **UB1** | view toggle | `ub-logs.spec.ts` |
| **UB2** | log lines + level badges | `ub-logs.spec.ts` |
| **UB4** | follow toggle | `ub-logs.spec.ts` |
| **UB5** | log-line prefixes | `ub-logs.spec.ts` |
| **UB6** | diff pill in logs | `ub-logs.spec.ts` |
| **UB7** | log stream reconnects after a fatal error and resumes live lines | `ub-logs.spec.ts` |
| **UB8** | Logs view panel controls (no popover) | `ub-logs.spec.ts` |
| **UB9** | Logs view live/pause pill | `ub-logs.spec.ts` |
| **UB10** | Logs view live/pause pill — keyboard | `ub-logs.spec.ts` |
| **UC1** | header state | `uc-autosync.spec.ts` |
| **UC2** | pending pill | `uc-autosync.spec.ts` |
| **UC3** | drawer open/close | `uc-autosync.spec.ts` |
| **UC4** | global switch | `uc-autosync.spec.ts` |
| **UC5** | per-stack switch | `uc-autosync.spec.ts` |
| **UC6** | queued list | `uc-autosync.spec.ts` |
| **UC7** | stack filter | `uc-autosync.spec.ts` |
| **UC8** | enable drains, disable does not | `uc-autosync.spec.ts` |
| **UC9** | queued row + cause | `uc-autosync.spec.ts` |
| **UC10** | re-enable does not pin | `uc-autosync.spec.ts` |
| **UC11** | UI pause does not survive a global cycle | `uc-autosync.spec.ts` |
| **UC12** | collapse through the stack filter | `uc-autosync.spec.ts` |
| **UC13** | a late snapshot never overwrites a newer one | `uc-autosync.spec.ts` |
| **UC14** | the drawer waits for server state | `uc-autosync.spec.ts` |
| **UC15** | a re-render keeps the switch nodes | `uc-autosync.spec.ts` |
| **UC16** | the drawer opens at its top | `uc-autosync.spec.ts` |
| **UD1** | theme toggle switches dark<->light, persists, and applies before paint | `ud-chrome.spec.ts` |
| **UD2** | connection indicator tracks connected→reconnecting→connected | `ud-chrome.spec.ts` |
| **UD3** | deploy indicator names the active stack while held, idle otherwise | `ud-chrome.spec.ts` |
| **UD4** | responsive ≤700px | `ud-chrome.spec.ts` |
| **UD5** | header shows the deployed build identity | `ud-chrome.spec.ts` |
| **UD6** | theme picker switches live, persists a local override, and surfaces a mismatch notice | `ud-chrome.spec.ts` |
| **UD6b** | theme mismatch notice auto-hides a few seconds after it appears | `ud-chrome.spec.ts` |
| **UD7** | with the switcher off, the picker is hidden and a saved override is ignored | `ud-chrome.spec.ts` |
| **UD8** | view-specific options live in a popover opened from the active view button | `ud-chrome.spec.ts` |
| **UD9** | the theme toggle glyph shows a moon in dark and a sun in light | `ud-chrome.spec.ts` |
| **UD10** | indicator recovers after a fatal (non-200) stream error the browser will not retry | `ud-chrome.spec.ts` |
| **UD11** | tap-tip opt-in on non-header controls | `ud-chrome.spec.ts` |
| **UD12** | a state change published while the stream connects still reaches the UI | `ud-chrome.spec.ts` |
| **UD13** | light/dark follows the OS until a choice is saved | `ud-chrome.spec.ts` |
| **UD14** | on mobile the view-options popover carries the light/dark toggle | `ud-chrome.spec.ts` |
| **UE1** | the update banner reloads onto the new version | `ue-pwa.spec.ts` |
| **UE2** | dismissing the banner keeps the current version | `ue-pwa.spec.ts` |
| **UF1** | the indicator shows the active stack and the upcoming trail | `uf-upcoming.spec.ts` |
| **UF2** | clicking the indicator opens the run panel with the ordered run | `uf-upcoming.spec.ts` |
| **UF3** | the trail collapses to a +N count chip on mobile | `uf-upcoming.spec.ts` |
| **UG1** | typing reveals the filter, narrows the rows, and Esc folds it away | `ug-search.spec.ts` |
| **UG2** | a non-matching query shows the empty note | `ug-search.spec.ts` |
| **UG3** | the mobile popover "Search stacks" row reveals and focuses the filter | `ug-search.spec.ts` |
| **UH1** | health pill per stack | `uh-health.spec.ts` |
| **UH2** | per-service panel | `uh-health.spec.ts` |
| **UH3** | deploy rows carry no health pill | `uh-health.spec.ts` |
| **UH4** | status history (health watch) | `uh-health.spec.ts` |
| **UH5** | on-demand container reads stopped, not unhealthy | `uh-health.spec.ts` |
| **UH7** | routine start folds away, raw list behind the toggle | `uh-health.spec.ts` |
| **UI1** | commit facts | `ui-diff-commit.spec.ts` |
| **UI2** | row binding | `ui-diff-commit.spec.ts` |
| **UI3** | multi-commit range | `ui-diff-commit.spec.ts` |
| **UJ1** | the detail opens on click for its row | `uj-rollback-error.spec.ts` |
| **UJ2** | the rollback verdict comes before the diff, in one panel | `uj-rollback-error.spec.ts` |
| **UK1** | self-heal restores a degraded stack | `uk-selfheal.spec.ts` |
| **UK2** | self-heal gives up after repeated failures | `uk-selfheal.spec.ts` |
| **UL2** | a panel on another row closes the open one | `ul-panel-exclusive.spec.ts` |
| **UM1** | per-stack deploy history | `um-audit.spec.ts` |
| **UM5** | routine outcomes fold, verbatim list behind the toggle | `um-audit.spec.ts` |
| **UM6** | a short history keeps every row and offers no toggle | `um-audit.spec.ts` |
| **UM7** | a repeated failure is one counted record, split by cause | `um-audit.spec.ts` |
| **UN1** | a healed row opens the drift detail | `un-heal-detail.spec.ts` |
| **UO1** | disabled-stacks line | `uo-discovery.spec.ts` |
| **UO2** | leftover repo config | `uo-discovery.spec.ts` |
| **UP1** | blocked row renders the dependency reason as text | `up-blocked.spec.ts` |
| **UP2** | the cause line sits under the stack name | `up-blocked.spec.ts` |
| **UQ1** | lists orphaned + unmanaged projects, expandable to containers and data-safety facts | `uq-orphans.spec.ts` |
| **UQ2** | the deploy search scans orphans — matches auto-expand, non-matches hide, counts follow | `uq-orphans.spec.ts` |
| **UQ3** | a manually expanded orphan stays open across an orphan-data refresh | `uq-orphans.spec.ts` |
| **UR1** | lists the full set as an aligned table and replaces the deploy table | `ur-roster.spec.ts` |
| **UR2** | clicking a row shows the stack containers and deploy history | `ur-roster.spec.ts` |
| **UR3** | typing filters the roster and shows the empty note on no match | `ur-roster.spec.ts` |
| **UR4** | the mobile popover "Search stacks" row reveals and focuses the filter | `ur-roster.spec.ts` |
| **UR5** | the time-mode toggle switches roster times to absolute | `ur-roster.spec.ts` |
| **UR6** | an open panel survives a roster republish and carries the new data | `ur-roster.spec.ts` |
| **US1** | Deploys -> Stacks jump lands on and flashes the roster row | `us-jump.spec.ts` |
| **US2** | Stacks -> Deploys jump lands on the newest row, not an older one | `us-jump.spec.ts` |
| **US3** | the jump leaves the panel it sits in open | `us-jump.spec.ts` |
| **US4** | jump with no landing target | `us-jump.spec.ts` |
| **US5** | a leftover filter in the target view does not hide the landing row | `us-jump.spec.ts` |
| **UT1** | per-stack log panel | `ut-container-logs.spec.ts` |
| **UT2** | per-container log panel | `ut-container-logs.spec.ts` |
| **UT3** | single log open | `ut-container-logs.spec.ts` |
| **UT4** | in-log type-to-search | `ut-container-logs.spec.ts` |
| **UT7** | per-service filter | `ut-container-logs.spec.ts` |
| **UT8** | a refused stream | `ut-container-logs.spec.ts` |
| **UU1** | hooks badge + panel | `uu-hooks.spec.ts` |
| **UU2** | hook output attributed in the log | `uu-hooks.spec.ts` |
| **UU3** | running-hook phase + inline log | `uu-hooks.spec.ts` |
| **UU4** | running phase in the Stacks roster | `uu-hooks.spec.ts` |
| **UV1** | merged deploy feed shows peer rows with host dots | `uv-multi-host.spec.ts` |
| **UV2** | clicking a host dot filters to that host and toggles back | `uv-multi-host.spec.ts` |
| **UV3** | the Hosts drawer multi-select filters the feed | `uv-multi-host.spec.ts` |
| **UV4** | an unreachable peer shows the offline banner and a down dot | `uv-multi-host.spec.ts` |
| **UV5** | the merged roster shows peer stacks, read-only | `uv-multi-host.spec.ts` |
| **UV6** | clicking a peer row opens a read-only detail with the peer diff inline | `uv-multi-host.spec.ts` |
| **UV7** | the host filter persists across a reload | `uv-multi-host.spec.ts` |
| **UW1** | the _nixos detail has no jump or container-logs action | `uw-nixos.spec.ts` |
| **UW2** | clicking the _nixos row opens a panel, never nothing | `uw-nixos.spec.ts` |
| **UX2** | peer roster rows show an inline health pill; the panel carries the app-link | `ux-peer-health.spec.ts` |
| **UX3** | a peer deploy detail shows its containers in Stacks | `ux-peer-health.spec.ts` |
| **UX4** | peer containers show the healthwatch timeline | `ux-peer-health.spec.ts` |
| **UY1** | a peer container log button streams the peer log through the proxy | `uy-peer-logs.spec.ts` |
| **UY2** | clicking the peer container log button again closes the panel | `uy-peer-logs.spec.ts` |
| **UY3** | the peer log panel survives the peers poll rebuilding its row | `uy-peer-logs.spec.ts` |
| **UZ1** | header beacon counts unhealthy stacks | `uz-unhealthy-visibility.spec.ts` |
| **UZ2** | attention band jumps to the stack | `uz-unhealthy-visibility.spec.ts` |
| **UZ3** | cross-view presence and recovery | `uz-unhealthy-visibility.spec.ts` |
| **UZ4** | Stacks roster floats unhealthy first | `uz-unhealthy-visibility.spec.ts` |
| **UAA1** | keyboard focus paints a visible ring on the focused control | `uaa-a11y.spec.ts` |
| **UAA2** | opening a drawer moves focus in and Escape returns it to the opener | `uaa-a11y.spec.ts` |
| **UAA3** | terminal deploy outcomes are announced live, not from history | `uaa-a11y.spec.ts` |
| **UAA4** | host rows are keyboard-operable checkboxes with aria-checked | `uaa-a11y.spec.ts` |
| **UAA5** | the host-mono chip filters from the keyboard | `uaa-a11y.spec.ts` |
| **UAA6** | a drawer closes from its close button and returns focus | `uaa-a11y.spec.ts` |
| **UAA7** | touch targets | `uaa-a11y.spec.ts` |
| **UAB1** | the header magnifier opens and closes the deploys filter | `uab-search-trigger.spec.ts` |
| **UAB2** | the magnifier opens the stacks (roster) filter | `uab-search-trigger.spec.ts` |
| **UAB3** | the magnifier opens the in-log search on the Logs view | `uab-search-trigger.spec.ts` |
| **UAB4** | type-to-search still reveals the bar and the magnifier shows open | `uab-search-trigger.spec.ts` |
| **UAC1** | the active view shows the bar and the caret; inactive views show neither | `uac-view-caret.spec.ts` |
| **UAC2** | Logs shows the bar but no caret; Stacks shows both | `uac-view-caret.spec.ts` |
| **UAC3** | the caret flips while the popover is open | `uac-view-caret.spec.ts` |
| **UAC4** | switching views leaves no button stuck aria-expanded | `uac-view-caret.spec.ts` |
| **UAE7** | tablet — an over-long name ellipsises instead of taking a line | `uae-roster-identity.spec.ts` |
| **UAF1** | success badge carries an icon | `uaf-status-icons.spec.ts` |
| **UAF2** | healed badge carries an icon | `uaf-status-icons.spec.ts` |
| **UAF3** | heal_exhausted is a two-line chip with a warning icon | `uaf-status-icons.spec.ts` |
| **UAF4** | heal_exhausted is opaque, a dim badge translucent | `uaf-status-icons.spec.ts` |
| **UAG1** | a fresh browser shows the banner and control captions | `uag-header-tour.spec.ts` |
| **UAG2** | Got it dismisses the tour and it stays gone after reload | `uag-header-tour.spec.ts` |
| **UAG3** | Esc dismisses the tour and returns focus to the header | `uag-header-tour.spec.ts` |
| **UAG4** | a returning browser never shows the tour | `uag-header-tour.spec.ts` |
| **UAG5** | the tour is suppressed on the compact mobile header | `uag-header-tour.spec.ts` |
| **UAH1** | loading skeleton yields to the empty state | `uah-feedback-states.spec.ts` |
| **UAH2** | loading skeleton yields to the deploy table | `uah-feedback-states.spec.ts` |
| **UAH3** | audit history load-error + retry | `uah-feedback-states.spec.ts` |
| **UAH4** | diff load-error + retry | `uah-feedback-states.spec.ts` |
| **UAH5** | peer-diff load-error + retry | `uah-feedback-states.spec.ts` |
| **UAI1** | tag bump | `uai-changed-services.spec.ts` |
| **UAI2** | service always labelled | `uai-changed-services.spec.ts` |
| **UAI3** | one chip plus a count | `uai-changed-services.spec.ts` |
| **UAI4** | digest-only rebuild | `uai-changed-services.spec.ts` |
| **UAI5** | responsive | `uai-changed-services.spec.ts` |
| **UAI6** | view-options toggle | `uai-changed-services.spec.ts` |
| **UAI7** | tablet | `uai-changed-services.spec.ts` |
| **UAJ1** | a deployed stack shows what is watched and since which commit | `uaj-change-detection.spec.ts` |
| **UAJ1b** | after a deployed change the lead names the commit | `uaj-change-detection.spec.ts` |
| **UAJ2** | a parked stack reports that nothing is watched | `uaj-change-detection.spec.ts` |
| **UAJ3** | the panel hides with its row under an active filter | `uaj-change-detection.spec.ts` |
| **UAK1** | a roster row shows its lead service version and a count for the rest | `uak-service-versions.spec.ts` |
| **UAK2** | a stack with no lead service shows only its service count | `uak-service-versions.spec.ts` |
| **UAK3** | the containers panel shows every service version | `uak-service-versions.spec.ts` |
| **UAK4** | a snapshot carrying no images shows no version at all | `uak-service-versions.spec.ts` |
| **UAK5** | a new image on the next poll updates the cell without dropping a panel | `uak-service-versions.spec.ts` |
| **UAK6** | the SHA gives up its place so Version keeps the first line | `uak-service-versions.spec.ts` |
| **UAL1** | the roster Commit cell links to the commit on the forge | `ual-commit-links.spec.ts` |
| **UAL2** | following the commit link does not also expand the row | `ual-commit-links.spec.ts` |
| **UAL3** | deploy-history and deploy-detail SHAs link to the same forge | `ual-commit-links.spec.ts` |
| **UAL4** | a peer row links its commit to the peer own forge | `ual-commit-links.spec.ts` |
| **UAL5** | without a forge the SHA stays plain text | `ual-commit-links.spec.ts` |
| **UAM1** | a refusing server is announced, switch keeps server state | `uam-autosync-write-failure.spec.ts` |
| **UAM2** | a request that never arrives is announced | `uam-autosync-write-failure.spec.ts` |
| **UAN1** | font-swap layout stability | `uan-font-fallback.spec.ts` |
| **UAO1** | a roster row chip carries the amber marker for a newer tag | `uao-update-check.spec.ts` |
| **UAO2** | the containers panel carries per-service markers and the head summary | `uao-update-check.spec.ts` |
| **UAO3** | a republished tag reads as ⇡ rebuilt | `uao-update-check.spec.ts` |
| **UAO4** | an up-to-date stack shows no marker and no head summary | `uao-update-check.spec.ts` |
| **UAP1** | stops promising a connection and offers a retry | `uap-stream-offline.spec.ts` |
| **UAP2** | the retry reconnects in place, with no reload | `uap-stream-offline.spec.ts` |
| **UAP3** | a reachable server never shows the notice | `uap-stream-offline.spec.ts` |
| **UAQ1** | the retry's detail pairs the success with its rollback | `uaq-rollback-visibility.spec.ts` |
| **UAQ2** | status chips narrow the deploy log and Esc clears them | `uaq-rollback-visibility.spec.ts` |
| **UAQ3** | the incident badge toggles the pre-filtered Deploys view | `uaq-rollback-visibility.spec.ts` |
| **UAQ4** | outcome strip and last-incident line in the detail panel | `uaq-rollback-visibility.spec.ts` |
| **UAQ5** | the errors chip keeps the rollback outcome line | `uaq-rollback-visibility.spec.ts` |
| **UAQ6** | the incident badge occupies no space with nothing to report | `uaq-rollback-visibility.spec.ts` |
| **UAQ7** | a deploying stack keeps its open panel and outcome strip | `uaq-rollback-visibility.spec.ts` |
| **UAR1** | tablet | `uar-logs-fit.spec.ts` |
| **UAR2** | phone | `uar-logs-fit.spec.ts` |
| **UAR3** | one chrome row | `uar-logs-fit.spec.ts` |
| **UAR4** | fullscreen covers the header | `uar-logs-fit.spec.ts` |
| **UAR5** | the header never scrolls sideways | `uar-logs-fit.spec.ts` |
| **UAS1** | health pill and last deploy share the row right edge | `uas-roster-phone-align.spec.ts` |
| **UAS2** | the version cell keeps its width beside the last deploy | `uas-roster-phone-align.spec.ts` |
| **UAS3** | the label gives way and the change tokens stay on one line | `uas-roster-phone-align.spec.ts` |
| **UAS4** | the deploy row status cell shares the same right edge | `uas-roster-phone-align.spec.ts` |
| **UAS5** | returning to the Deploys view does not replay a row's entrance | `uas-roster-phone-align.spec.ts` |
| **UAT1** | a removed stack lands in the deploy history and opens the Orphans section | `uat-removed-stack.spec.ts` |
| **UAT2** | the removal is recorded once, not on every later sync | `uat-removed-stack.spec.ts` |
| **UAU1** | a repeated failure collapses into one row that counts it | `uau-repeated-failure.spec.ts` |
| **UAU2** | the collapsed count survives a reload | `uau-repeated-failure.spec.ts` |
| **UAU3** | the deploy-history panel marks the repeat too | `uau-repeated-failure.spec.ts` |
| **UAV1** | the header badge counts the stacks an update is waiting on | `uav-updates-filter.spec.ts` |
| **UAV2** | the badge opens the Stacks view filtered to the stacks with updates | `uav-updates-filter.spec.ts` |
| **UAV3** | a second activation clears the filter and folds the bar away | `uav-updates-filter.spec.ts` |
| **UAV4** | every row the filter leaves standing carries a visible update marker | `uav-updates-filter.spec.ts` |
| **UAV5** | the toggle combines with the name query, and Esc clears both | `uav-updates-filter.spec.ts` |
| **UAV6** | the chip works from the search trigger, and the badge follows its state | `uav-updates-filter.spec.ts` |
| **UAV7** | the badge occupies no space when nothing has an update | `uav-updates-filter.spec.ts` |
| **UAW1** | the beacon popover and a drawer never sit open together | `uaw-surface-exclusive.spec.ts` |
| **UAW2** | opening a drawer closes the beacon popover | `uaw-surface-exclusive.spec.ts` |
| **UAW3** | the hosts and autosync drawers never sit open together | `uaw-surface-exclusive.spec.ts` |
| **UAX1** | a change with no new image | `uax-change-attribution.spec.ts` |
| **UAX2** | a project-wide input | `uax-change-attribution.spec.ts` |
| **UAX3** | an image bump grows no second chip | `uax-change-attribution.spec.ts` |
| **UAX4** | the panel attributes each file | `uax-change-attribution.spec.ts` |
| **UAY1** | a working fast-forward reports nothing | `uay-project-dir-sync.spec.ts` |
| **UAY2** | a dirty checkout is reported without blocking the deploys | `uay-project-dir-sync.spec.ts` |
| **UAY3** | a standing refusal is reported once | `uay-project-dir-sync.spec.ts` |
| **UAY4** | the phase row detail carries no container-logs or jump action | `uay-project-dir-sync.spec.ts` |
| **UAZ1** | the held chip names the failure and retry releases it | `uaz-held-change.spec.ts` |
| **UAZ2** | a refused retry shows why and re-enables the button | `uaz-held-change.spec.ts` |
| **UAZ3** | a pushed revert releases the held chip | `uaz-held-change.spec.ts` |
| **UAZ4** | a failed converge keeps the held chip | `uaz-held-change.spec.ts` |
| **UAZ5** | a failing post_deploy hook holds the change | `uaz-held-change.spec.ts` |
| **UBA1** | backoff names the attempts and the next try, then turns into held | `uba-backoff-change.spec.ts` |
| **UBB1** | headers are set and no view trips the policy | `ubb-csp.spec.ts` |
| **UBC1** | a healthy stack opens on Containers, a parked one on Triggers | `ubc-stack-detail.spec.ts` |
| **UBC2** | a held stack opens on History with its retry | `ubc-stack-detail.spec.ts` |
| **UBC3** | the head actions keep their order, absent when they do not apply | `ubc-stack-detail.spec.ts` |
| **UBC4** | Logs opens the log panel; Hooks toggles the hooks pane | `ubc-stack-detail.spec.ts` |
| **UBC5** | the tabs switch the pane; verdict and facts read the stack | `ubc-stack-detail.spec.ts` |
| **UBC6** | the close button closes; one panel at a time | `ubc-stack-detail.spec.ts` |
| **UBC7** | the facts sit in two columns and the actions drop their labels | `ubc-stack-detail.spec.ts` |
| **UBC8** | a peer panel shows the hold without retry or jump | `ubc-stack-detail.spec.ts` |
| **UBC9** | the arrow keys switch tabs and the pane names its tab | `ubc-stack-detail.spec.ts` |
| **UBC10** | the Hooks pane is named and the strip stays reachable | `ubc-stack-detail.spec.ts` |
| **UBC11** | a row opens by keyboard and Escape closes it | `ubc-stack-detail.spec.ts` |
| **UBC12** | the hash follows the open detail and its tab | `ubc-stack-detail.spec.ts` |
| **UBC13** | loading a link opens the stack it names | `ubc-stack-detail.spec.ts` |
| **UBC14** | a peer link opens once the peer row arrives | `ubc-stack-detail.spec.ts` |
| **UBD1** | six routine deploys fold into the newest row plus a fold line | `ubd-deploy-fold.spec.ts` |
| **UBD2** | rows sit under a Today separator | `ubd-deploy-fold.spec.ts` |
| **UBD3** | a name filter that hides the rows takes the fold line with them | `ubd-deploy-fold.spec.ts` |
| **UBE1** | the rolling sub-label names the rolled service in both views and clears | `ube-rollout-phase.spec.ts` |
| **UBE2** | a failed cutover settles as rolled_back with no label | `ube-rollout-phase.spec.ts` |
| **UBF1** | at 1280 px the deploy detail opens beside the list | `ubf-side-panel.spec.ts` |
| **UBF2** | at 1100 px the deploy detail opens under its row | `ubf-side-panel.spec.ts` |
| **UBF3** | at 1280 px the Stacks detail opens beside the roster | `ubf-side-panel.spec.ts` |
| **UBF4** | a resize moves the open detail between side and inline | `ubf-side-panel.spec.ts` |
| **UBF5** | Jump and Escape work from the side panel | `ubf-side-panel.spec.ts` |
| **UBF6** | a detail filtered out inline shows in the side column once the filter clears | `ubf-side-panel.spec.ts` |
| **UBF7** | on a phone the Stacks detail is a sheet and close keeps the scroll | `ubf-side-panel.spec.ts` |
| **UBF8** | on a phone the deploy sheet takes the focus and closes on Escape | `ubf-side-panel.spec.ts` |
| **UBF9** | a resize moves the open detail between sheet and inline | `ubf-side-panel.spec.ts` |
| **UBG1** | the open Containers tab shows a health change | `ubg-detail-live.spec.ts` |
| **UBG2** | the open deploy detail picks up a health change | `ubg-detail-live.spec.ts` |
| **UBG3** | a live re-render keeps the focused tab focused | `ubg-detail-live.spec.ts` |
| **UBG4** | a live re-render keeps the open container log | `ubg-detail-live.spec.ts` |

### 6.3 Known gaps

- **In the suite, not in §4:** none.
- **In §4, not in the suite:** UH6, UL1, UM2–UM4, UN2, UX1 (retired, noted in §4)
  and UT5, UT6 (moved to UB8–UB10). UB3 (sort toggle) is also gone from the
  suite although §4 and the Logs-view intro still name it.
- **Not e2e, by decision:**

| Requirement | Where it is covered |
| --- | --- |
| Stacks roster: never-deployed synthetic state | Unit `roster` — not e2e-seedable |
| Diff API 404 / truncation limits | Unit `ui`/`deploy` |
| Log windowing (500 +500), buffer trim | n/a in v1 (fiddly; low bump-risk) |
| Favicon `prefers-color-scheme` | n/a (browser chrome, not page DOM) |

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
  Chromium starts with `--disable-gpu` (`playwright.config.ts`). Without it, the
  headless shell's GPU process once crashed the whole browser with a SIGSEGV
  after `SharedImageManager::ProduceMemory … non-existent mailbox` errors. The
  worker's next test then failed in `browser.newContext` before its body ran.
  The baselines render identically with the flag; they are software-rasterised
  either way.

- **baselines**: fetches the LFS-tracked visual baselines on a runner that has
  git-lfs and uploads them as an artifact, which `e2e-ui` unpacks over the pointer
  files its in-container checkout wrote (ADR-0052).

`make check-e2e-trace` (the `ui-unit` job) fails when a test-title ID (`UA1:` in
Playwright, `TestP1_` in Go) has no `- **ID — …` case in this document or is used
by two tests.

A third job lives in `.github/workflows/docs.yml`, not here:

- **screenshots**: renders the docs landing-page images (§5.1) in the same pinned
  container and hands them to the docs build as an artifact. It asserts nothing
  about the product — it exists so those PNGs never have to be committed.
