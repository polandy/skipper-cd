# skipper-cd Web UI — Specification

Single-page application served at `/` when `ui_enabled: true`. No external JS dependencies. Real-time deployment events via SSE (`/api/events`); real-time log lines via SSE (`/api/logs`). The `/api/events` stream also carries `autosync` and `queue` events that drive the autosync controls and the queue drawer live (see [Autosync](#autosync)), and a `health` snapshot that drives the per-stack [Stack health](#stack-health) pill.

---

## Design

Nautical-industrial theme, built on a **configurable palette** (`ui_theme` in `skipper.yml`, see [`docs/configuration.md`](../../docs/configuration.md#web-ui-theme)): **Catppuccin** (Mocha dark / Latte light) is the default, plus four built-ins — **Nord**, **Solarized**, **Gruvbox**, **Rosé Pine** — each with its own dark and light variant. The theme is a per-deployment choice, baked server-side into `<html data-theme="…">` at serve time (`ui.IndexHandler`); it never flashes because it is present in the initial HTML, not applied by JS. When enabled (`ui_theme_switcher`, off by default), the header **theme picker** additionally lets a single browser override it locally without touching the deployment (see [Theme override](#theme-override)). Dark/light is a separate, per-browser axis: the header toggle only flips a `.light` class on top of whichever theme is active (`localStorage` key `colorScheme` = `light`/`dark`; a pre-paint inline script in `<head>` applies the class before first render — dark is the stylesheet default per theme, so a missing preference can never flash). `color-scheme` follows the active variant.

One semantic token layer consumes the palette; all tints, borders and glows are derived from it via `color-mix()`, so no component rule names a raw colour:

| Token | Meaning |
|---|---|
| `--accent` | Active deploy, brand accent, active toggles, connecting |
| `--success` | Success, connected |
| `--danger` | Failed, errors, reconnecting, diff deletions |
| `--rollback` | Rolled back |
| `--skip` | DEBUG log level (aliases the muted text tier) |
| `--queued` | Queued/deferred deploy, pending count, autosync paused (shares its colour with `--hunk`, distinct semantic) |
| `--diff-add` | Diff additions |
| `--hunk` | Diff hunk headers, WARN log level |

Each theme maps these tokens to its own raw colours (`--raw-accent`, `--raw-success`, …) under `:root[data-theme="<name>"]` / `:root[data-theme="<name>"].light`; adding a theme is a self-contained CSS block, nothing else in the page references a theme name. Background depth: `crust` (sunken — log pane, diff/files panels) → `mantle` (page) → `base` (header glass, cards) → `surface0` (raised — tags, toggle tracks). Text: `text` / `subtext0` / `overlay1` (primary / secondary / muted).

Fonts: **DM Sans** (UI) + **JetBrains Mono** (timestamps, stack names, badges). Both are **self-hosted and embedded** in `index.html` as `@font-face` rules with the `woff2` (latin subset, weights 400/500/600) inlined as `data:` URIs — there is no external Google Fonts request, so the page is fully self-contained, works offline, and renders deterministically for visual snapshots (see [`docs/e2e-tests.md`](../../docs/e2e-tests.md) §5). Background: mantle with subtle grid overlay and peach radial glow at top centre.

---

## Layout

Sticky frosted-glass header (56 px) + centred main (max 1040 px).

**Header — left:** skipper-cd container-ship logo (inline SVG, 32 px — a hull with wave carrying three container boxes: one in `--accent`, one in `--success`, one outlined; hull, outline and wave follow `--text-primary` via `currentColor`, so the logo tracks the theme toggle) and, stacked beside it, a text column: the `skipper-cd` wordmark (accent `-cd`) over a small muted **version label** showing the deployed build identity (`v<semver> · <commit>`, or the branch name for feature builds; local builds without ldflags show `dev`). The label is fetched once on load from `GET /api/version`, left empty until it resolves, and carries a `title` tooltip with the full string. It is a shrinkable flex item (`min-width: 0`) that shows in full whenever the header has room and only clips with an ellipsis when space genuinely runs out — no fixed character cap. The favicon is the same ship as an SVG data URI, its colours drawn from the configured theme, with a `prefers-color-scheme` media query switching between its light and dark variant (favicons cannot follow the in-page toggle).

**Header — right.** The controls are **icon/glyph-only** on every viewport (no text labels or switch tracks in the header row). Each glyph follows the theme via `currentColor`, is muted by default, and only takes on colour to signal a real state. Pointer users get the native `title` tooltip on hover; since touch never shows `title` tooltips, a tap on any header control flashes its label in a small **tap-reveal bubble** (the control's action still fires). In order:

- **Deploy indicator** — an **anchor** glyph (muted) at rest, swapping to a **ship** (accent, pulsing) while deploying. The glyph leads; to its right comes the active stack name(s) and then a dimmed **look-ahead trail** — `→ a · b · +N` — naming the stacks that will deploy *next in the same run*, capped at three names plus a `+N` overflow. The trail's source is the [`upcoming`](#event-lifecycle-sse) SSE event (distinct from the autosync pending queue: this is the *active run's* remaining work). `idle` / `deploying <stacks> · next <stacks>` is mirrored into `title` + `aria-label`. While a run is active the indicator is a **button** (`role="button"`, `Enter`/`Space`): it toggles the [Run panel](#run-panel). Visible in both views.
- **View toggle** — segmented control of two icons: a rows/table glyph (`deploys`) and a terminal glyph (`logs`). Default: `deploys`. State persisted in `localStorage` key `activeView`. The **active** button carries a small `▾` and opens the [View-options popover](#view-options-popover); the other button switches views.
- **Autosync control** — a single sync-arrows glyph showing global autosync state; **muted by default**, turning `--queued` (amber) when paused and `--accent` while its drawer is open. When deploys are queued it also shows an amber **pending count** pill (hidden at zero). Not a `localStorage` preference — it reflects server state from the `autosync`/`queue` SSE events. Click (or `Enter`/`Space`) toggles the [Autosync drawer](#autosync). Visible in both views.
- **Theme picker** — a palette glyph over a transparent native `<select>` of the five built-in palettes (see [Theme override](#theme-override)); clicking opens the native option list. **Opt-in**: present only when `ui_theme_switcher: true` (see [`docs/configuration.md`](../../docs/configuration.md#web-ui-theme)); off by default, so the deployed theme is fixed. Desktop only (hidden ≤ 700 px). Visible in both views.
- **Theme toggle** — a moon (dark, default) / sun (light) glyph switching between the configured theme's dark and light variant. State persisted in `localStorage` key `colorScheme` (`dark` / `light`). Visible in both views.
- **Connection indicator** — a **chain-link** glyph: a closed link in `--success` when `connected`, a closed link pulsing `--accent` while `connecting`, and a **broken link** pulsing `--danger` while `reconnecting`. State is on `data-state`. Bound to `/api/events`; the log stream has no own indicator.

### View-options popover

The view-specific toggles live in a small popover anchored under the view toggle (styled like the [Autosync drawer](#autosync)), **not** in the header row — so switching views never makes a header control appear or disappear. It is opened by clicking the already-active view button (the `▾` hint), and dismisses on outside-click or `Esc`; it and the Autosync drawer are mutually exclusive. Inside, each option is a full row (glyph + label + switch track). Contents by active view:

- **Deploys** → **Time mode** — switches the Time column between relative (`Xs ago`) and absolute (`toLocaleString()`). Default: inactive (relative). `localStorage` key `timeMode`. On **mobile only**, the group is preceded by a **"Search stacks"** action row that reveals and focuses the [Deploys filter](#deploys-filter) (the touch entry point for type-to-search).
- **Logs** → **Sort** — reverses log order; default inactive (newest first), active flips to oldest-first (terminal semantics). `localStorage` key `logSort` (`desc` / `asc`); flipping resets the visible window to one page. And **Auto-scroll (Follow)** — auto-scrolls the log pane to the newest line on every append; default active. `localStorage` key `followLogs`.

### Theme override

The theme picker is **opt-in**: it renders only when `ui_theme_switcher: true`. By default the flag is off — the picker is absent, the pre-paint script ignores any saved `themeOverride`, and the deployed `ui_theme` is fixed and cannot be switched from the browser. The switcher exists mainly to try palettes out. When enabled, it lets a **browser** show a different palette than the one `ui_theme` configures for the environment, without touching the deployment. This is purely a local, client-side preference:

- **Gating** — the flag is baked into `<html data-theme-switcher="on|off">` at serve time (`ui.IndexHandler`). The pre-paint script and the picker JS both read it: with the switcher off the `<select>` is hidden (CSS) and no override or mismatch notice is applied, so a `themeOverride` left in `localStorage` from a time the flag was on lies dormant until it is re-enabled.

- **Applying** — selecting a theme sets `document.documentElement`'s `data-theme` attribute immediately (every theme's CSS is always present in the stylesheet, so there is no reload and no flash). `<html>` carries a second, immutable attribute, `data-server-theme`, holding the value the environment is actually configured with — set once at serve time by `ui.IndexHandler` and never touched by JS. It is the reference the mismatch notice compares against.
- **Persisting** — a non-default choice is saved to `localStorage` key `themeOverride`. Picking the theme that matches `data-server-theme` again removes the override, so the page goes back to following whatever the environment is configured for (including across a `ui_theme` change on a later deploy). A pre-paint inline script in `<head>` re-applies a saved override before first render, the same no-flash approach the colour-scheme toggle uses.
- **Mismatch notice** — whenever `themeOverride` is set and differs from `data-server-theme` (which, by the rule above, is whenever an override exists at all), a dismissible notice appears under the header: *"Showing `<override theme>` in this browser — this environment is configured for `<server theme>`."* It auto-hides after 6 seconds, or immediately on clicking its close button; the check re-runs (and the notice reappears if still applicable) on every page load and every theme-picker change, but never nags mid-session beyond that.
- **Scope** — the override never reaches the server: there is no endpoint to change `ui_theme` from the UI. It only ever affects the browser that set it.

---

## Deploy table

5-column grid (`160px 1fr 110px 80px 100px`): **Time · Stack · Status · Duration · Files**

Rows are prepended (newest first) with a slide-in animation. Time cells show relative or absolute time depending on the header toggle. Relative times refresh every 30 s; tooltip always shows the other format.

### Stack icons

The Stack cell carries a small icon chip (18 px, fixed box, `object-fit: contain`) left of the name for recognition. The image is served same-origin from `GET /api/icons/<stack>` (no CSP concern); on any load error the chip swaps to a **monogram** — the stack's first letter on an accent-tinted chip — via the `<img>` `error` handler, so a broken image never shows. Icons are resolved server-side (repo `icon.svg`/`icon.png` override → configured `icon:` slug → auto-match on the stack name → 404 → monogram) and cached on the host; see the README "Service Icons" section.

**Refreshing icons.** The server-side icon cache is cleared by `POST /api/icons/refresh` (e.g. `curl -X POST …/api/icons/refresh`); the next load of each icon then picks up renamed stacks and newly published icons. It is an **ops endpoint** — there is deliberately no header control and no keyboard trigger (the single-key `i` hotkey was removed so type-to-search in the deploys view can own printable keys — see [Deploys filter](#deploys-filter)).

### Stack health

The Stack cell also carries a **health pill** (`data-testid="health-pill"`) right of the stack name — a small dot + label showing the stack's *current* runtime health, ArgoCD-style. It reflects the live [`health`](#event-lifecycle-sse) SSE snapshot, not the deploy outcome, and is a distinct axis from the [Status badge](#status-badges): a `success` deploy can still be `unhealthy` now (a container crash-looped afterwards), and a `deploying` stack can be `starting`. This is skipper-cd's own-stack health only (see [ADR-0027](../../dev-docs/adr/0027-live-stack-health-in-ui.md)); it is read-only and never restarts or redeploys anything.

| Health | Colour | Meaning |
|---|---|---|
| `healthy` | `--h-healthy` (green) | Every service running; every service with a healthcheck healthy |
| `unhealthy` | `--h-unhealthy` (red) | Any service `unhealthy`, `restarting`, or unexpectedly `exited`; dot pulses |
| `starting` | `--h-starting` (amber) | Any service still `starting` and none unhealthy; dot pulses |
| `stopped` | `--h-stopped` (grey) | No running containers for the project |
| `unknown` | `--h-unknown` (grey, dashed border) | Health could not be read (`ps` failed) — never a false `unhealthy` |

The health colours are their own semantic tier (`--h-*`), kept a distinct hue from `--accent` and the teal `--success` so the pill never reads as a deploy status.

**Placement — newest row per stack only.** The deploy table is an event log (many rows per stack over time) while health is a single current per-stack value, so the pill renders **only on the topmost (most recent) row for each stack**; older rows of the same stack carry no pill. This keeps the pill reading as "the current state of this stack" rather than implying it belonged to a past deploy, and keeps the 5-column grid intact (no dedicated Health column).

**Per-service breakdown.** Clicking the pill inserts a sibling panel directly below the row (`data-testid="health-panel"`, same expand pattern as the files/diff/error panels), listing each service (`data-testid="health-service"`) with its container state and per-service health. Clicking again removes it. On mobile the pill stays tappable in the collapsed 2×2 row. **One panel per row:** the health panel and the files/diff panel are mutually exclusive — opening one closes the other, so the open panel is always the row's direct sibling and the row's tint/bar is never bound to two panels at once (a click order must not change the layout).

**Status history (health watch).** When the [health watch](../../docs/configuration.md#health-watch) is configured (ADR-0031), the panel additionally shows *how long* each status has held and *what came before*: the service line gains the current phase's age (`unhealthy · for 6h12m`), and below it a compact timeline (`data-testid="health-history"`) lists the service's last accepted status phases (≤ 10, newest first) — one line per phase (`data-testid="health-phase"`) with a status-coloured dot, the status, when it began (local time), how long it held, and — when the phase began within the attribution window after a deploy — the deploy's short commit as a chip (`data-testid="health-phase-commit"`). Only debounce-accepted phases appear, so the timeline shows real transitions, not probe blips. The data comes from the `healthwatch` SSE snapshot; the panel renders whatever snapshot is current at open time (like the service list itself). **Graceful degradation:** with the watchdog off there is no `healthwatch` snapshot and the panel renders exactly as before — no age, no timeline.

**Row binding.** So the open panel always reads as tied to its row (whatever its position, and with several panels open), the pill click also **tints the parent row and the panel** in the stack's health colour and draws a shared 3px left bar down both, and the panel leads with a **header echoing the stack name + status pill**. The tint/bar colour is the stack's rolled-up status — the worst (least-healthy) service, the same colour as the pill: `unhealthy` (red) dominates, else `starting` (amber), else `healthy` (green), else `stopped` (grey). The parent row carries `health-open` + `data-health` while its panel is open (both cleared on close); the panel carries `data-health` for the same colour (ADR-0027, variant A).

### Self-heal

Where the [Stack health](#stack-health) pill only *reports* a degraded stack, **self-heal** (opt-in, per stack) automatically restores it with a corrective `docker compose up -d` — the runtime-drift counterpart to the [periodic reconcile](configuration.md#periodic-reconcile) loop's git-drift correction. It surfaces in the deploy log as two statuses (see [Status badges](#status-badges)): a `healed` row each time a corrective redeploy runs, and a single `heal_exhausted` row when skipper gives up after repeated redeploys fail to restore the stack (also the default `heal_exhausted` notification). The action is backend-only — there is no UI control to trigger or configure it; it is driven entirely by `self_heal` config. See [ADR-0029](../../dev-docs/adr/0029-runtime-drift-self-heal.md).

### Status badges

| Status | Colour | Notes |
|---|---|---|
| `deploying` | `--accent` (peach) | Animated spinner dot |
| `success` | `--success` (teal) | |
| `failed` | `--danger` (red) | Error panel expanded below row |
| `rolled_back` | `--rollback` (maroon) | Deploy failed but old containers restored and verified healthy; error panel shows details |
| `rolled_back_unhealthy` | `--danger` (red) | Rollback ran but the restored version also failed the health gate — badge label stacks "rolled back" / "unhealthy" on two lines at a reduced font size (one line would overflow the status column); error panel shows details |
| `queued` | `--queued` (yellow) | Deploy deferred — autosync paused, change waiting; tinted row with amber left bar and a `paused: <global\|stack>` tag on the stack cell. See [Autosync](#autosync). |
| `healed` | `--success` (teal) | Self-heal restored a degraded stack with a corrective redeploy (not a git deploy — no diffs); tinted row with a teal left bar. See [Self-heal](#self-heal). |
| `heal_exhausted` | `--danger` (red) | Self-heal gave up after repeated redeploys did not restore the stack — badge label stacks "self-heal" / "failed" on two lines (like `rolled_back_unhealthy`); tinted row with a red left bar; error panel shows details. See [Self-heal](#self-heal). |

### Expandable panels

- **Files pill** — shown when `changed_files` is non-empty. Click inserts a full-width panel as a sibling element directly below the row (same pattern as the error detail panel). Clicking again removes the panel. The plain file list binds to its row (variant A, `data-status` + `bound`) exactly like the diff panel, so its status left bar stays continuous with the row above and the error detail below.
- **Diff panel** — when `has_diffs` is `true` on the event, clicking the files pill fetches diffs from `GET /api/events/{id}/diffs` and renders a syntax-colored diff panel instead of the plain file list. Each file is a collapsible section (single-file diffs default to expanded). Diff lines are colored: additions (green), deletions (red), hunk headers (yellow), metadata (muted). Diffs (and commits, below) are cached client-side after the first fetch.
  - **Row binding (variant A)** — a diff panel opened from a deploy row binds to it exactly like the [health panel](#stack-health): the open row gains `diff-open` and the panel gains `bound` + `data-status`, so both share a **status-coloured left bar** (`inset 3px`) and tint (`--dc` from the deploy status: success→green, failed / rolled_back_unhealthy→red, rolled_back→rosé, queued→amber, deploying→accent). Closing the panel clears `diff-open`; an empty-diff fallback swaps in a **bound file list** that keeps the row open and shares the same bar. Opening the files/diff panel closes an open [health panel](#stack-health) on the same row and vice versa (one panel per row).
  - **Commit header** (`data-testid="diff-head"`) — above the file sections. When bound, an **echo line** repeats the stack name + `deploy diff` label + a status pill, so the panel names its own row even when scrolled away from it. Below that, when the diffs API returns `commits`, the **newest commit**'s subject (message first line), then a meta line of `author · relative time (full timestamp in title) · short SHA`. For a **multi-commit range** the meta line instead shows `oldestSHA → newestSHA`, an `N commits` pill (`data-testid="commits-pill"`) toggling a collapsible per-commit list (`data-testid="diff-commit-list"`, collapsed by default), and the newest commit's author/time. A diff panel opened from a **log line** is unbound (no row to tie to) and shows the commit header without the echo line.
- **Error detail** — shown for `failed`, `rolled_back`, `rolled_back_unhealthy`, and `heal_exhausted` events with an `error` field. Monospace, `pre-wrap`. **Row binding (variant A)** — like the diff/health panels, the error box binds to the row above it: it carries `data-status` and both share a **status-coloured left bar** (`inset 3px`) and tint (`--ec`: failed / rolled_back_unhealthy / heal_exhausted→red, rolled_back→rosé). Its top margin is negative and its top corners square off, and the row squares its bottom corners while the error box trails it (`.event-row:has(+ .error-detail)`), so the message reads as attached to its deploy row rather than a card floating between rows. When a files/diff panel is open on an errored row, that panel sits between the row and the error and squares its own bottom (`.bound:has(+ .error-detail)`), keeping the left bar unbroken across `row → panel → error`.

### Deploys filter

A **type-to-search** filter over the deploy rows by stack name, reusing the [Autosync drawer](#autosync)'s filter styling (magnifier + input + `×` clear). It has **no header control**: the bar is hidden until the user starts typing while the deploys view is active (with at least one row rendered), then it **folds down** above the table (`data-testid="deploy-filter"`). Any printable key reveals it and seeds the first character — this is why the old single-key `i` icon-refresh hotkey was removed (see [Stack icons](#stack-icons)).

- **Matching** — case-insensitive substring on the stack name. Non-matching rows are hidden (`filtered-out`), and any files/diff/error panel trailing a hidden row is hidden with it. A small `shown/total` count sits in the field; an all-hidden result shows a **"No stack matches …"** note (`data-testid="deploy-filter-empty"`).
- **Live rows** — the filter re-applies as new deploy events arrive, so a freshly deploying stack obeys the active query.
- **Dismissing** — `Esc` clears a non-empty field (first press), then folds the bar away (second press); clearing to empty and blurring also folds it away. The query is purely client-side — no request, no persistence.
- **Mobile** — touch has no keyboard to trigger type-to-search, so on mobile (≤ 700 px) the [Deploys view-options popover](#view-options-popover) carries a **"Search stacks"** entry (`data-testid="deploy-search"`, hidden on desktop): tapping it reveals the bar and focuses the field, raising the on-screen keyboard. The type-to-reveal behaviour is desktop-only.

---

## Event lifecycle (SSE)

On connect, history is replayed as `deploy` events, then live events stream in.

| Transition | Behaviour |
|---|---|
| `deploying` | New row prepended, tracked in memory. |
| `success` / `failed` / `rolled_back` / `rolled_back_unhealthy` (deploying row exists) | Existing row mutated in-place; error panel appended if needed. |
| `success` / `failed` / `rolled_back` / `rolled_back_unhealthy` (no existing row) | New row created directly. |
| `skipped` | Dropped — never rendered (an unchanged stack carries no signal). |
| `queued` | Row created with the `queued` badge and a `paused:` tag, **keyed by stack**: a further `queued` for the same stack (another push while paused) replaces it rather than stacking a duplicate. It is removed when the stack next deploys (a `deploying` event supersedes it) or when the stack leaves the pending set in a `queue` snapshot (resumed then found unchanged). Like a deploy, a `queued` event carries `has_diffs`, so the paused row expands the pending diff. |
| `healed` / `heal_exhausted` | New row created directly (self-heal is not preceded by a `deploying` event); `heal_exhausted` carries an error and expands an error panel. See [Self-heal](#self-heal). |

Besides `deploy` events, the stream carries named **state snapshots** — `autosync`, `queue`, `upcoming`, `health`, and `healthwatch` — each replacing the prior snapshot of that name (also sent once on connect as initial state).

- **`upcoming`** `{ "upcoming": ["grafana", "loki"] }` — the stacks that will deploy *after* the one currently deploying, in deploy order. The backend hashes every stack once upfront (after the git sync) to know which will actually deploy this run, then publishes the shrinking list as each stack starts; an empty list is published when the run ends. Drives the [Deploy indicator](#header--right) look-ahead trail and the [Run panel](#run-panel). Distinct from the autosync pending `queue` (deferred, paused stacks). `_nixos` is excluded — the rebuild has no per-stack deploying state.
- **`health`** `{ "stacks": { "gitea": { "status": "healthy", "services": [{ "name": "gitea", "state": "running", "health": "healthy" }] }, … } }` — the current runtime health of skipper-cd's own stacks, driving the [Stack health](#stack-health) pill. The backend polls `docker compose ps` for each stack (only while the UI is on **and** a client is subscribed), rolls each stack up to `healthy`/`unhealthy`/`starting`/`stopped`/`unknown`, and republishes the snapshot on its interval, on connect, and after each deploy run. `_nixos` carries no health (it is not a compose project). See [ADR-0027](../../dev-docs/adr/0027-live-stack-health-in-ui.md).
- **`healthwatch`** `{ "stacks": { "vaultwarden": { "vaultwarden": [{ "status": "unhealthy", "since": "2026-07-16T15:47:05Z", "commit": "a1b2c3d…", "deploy_correlated": true }, …] } } }` — the health watchdog's per-service status history (≤ 10 accepted phases per service, newest first), driving the [Status history](#stack-health) in the per-service panel. Only present when `health_watch` is configured; published on every accepted change and once on connect. `deploy_correlated` is derived by the backend from the attribution window — the UI never computes it. See [ADR-0031](../../dev-docs/adr/0031-notify-on-own-stack-health-change.md).

---

## Autosync

Controls whether detected changes deploy automatically, per stack and globally. Paused stacks queue their changes and deploy them when sync resumes. Behaviour, semantics and the API contract are specified in [`docs/autosync.md`](../../docs/autosync.md); this section covers only the UI surface.

**Header control** — the [Autosync control](#header--right) shows global state and, when deploys are waiting, an amber **pending count** pill (hidden at zero). It is the drawer opener and the "how many are queued" indicator. It reflects server state (the `autosync`/`queue` SSE events), never `localStorage`.

**Autosync drawer** — an on-demand panel anchored under the header, opened by the control. Default hidden; closes on outside-click or `Esc`. Updated in real time from the `autosync` and `queue` SSE events, and painted from `GET /api/autosync` + `GET /api/queue` when opened. Contents, top to bottom:

- **Global autosync** — a switch (the header control mirrors its state). Toggling posts `POST /api/autosync {scope:"global", enabled}`.
- **Queued (N) · drains in this order** — the pending stacks in **deploy order** (`_nixos` first, then `skipper.yml` order), each row: position number, stack name, a `reason` chip (`global` / `stack`), changed-file count, and how long it has waited. Empty/hidden when nothing is queued.
- **All stacks** — one switch per managed stack with its current state, preceded by a **filter field** (case-insensitive substring match on stack name; a clear button appears when non-empty; `Esc` clears the field first, then closes the drawer). A "No stack matches …" state shows when the filter excludes everything. Toggling posts `POST /api/autosync {scope:"stack", stack, enabled}`. The switch reflects `effective`, so it stays correct across a toggle **while a filter is applied** (the toggle re-renders the list but preserves the query and the matched subset).

**Per-stack switch is an exception, not a pin.** A per-stack UI override is held only while it differs from what the stack would inherit; toggling a stack back to its inherited value clears the override (the "return to global" gesture) and toggling the global switch collapses any per-stack override that now matches the baseline. So the global switch behaves as a true master and a UI pause does not survive a global off→on cycle. Full semantics: [`docs/autosync.md`](../../docs/autosync.md#override-collapse) / [ADR-0019](../../docs/adr/0019-autosync-ui-overrides-collapse-to-inherit.md).

**Enable triggers a drain; disable does not.** Enabling sync (global or a stack) triggers a deploy run that drains the queue; disabling only updates state. Switches use the same track/thumb geometry as the header `.filter-toggle`.

---

## Run panel

A read-only panel listing the **current deploy run** in deploy order, opened by clicking the [Deploy indicator](#header--right) while a run is active (closed at rest, since there is nothing to show). Styled like the [Autosync drawer](#autosync) — same glass shell, anchored under the header, closes on outside-click or `Esc`, and mutually exclusive with the Autosync drawer and the view-options popover. Painted from the in-memory deploying rows plus the `upcoming` snapshot; no dedicated endpoint.

- **Header** — title `This deploy run`; sub-line `<b>stack</b> deploying · N more this run` (or `· last in this run`, or `Nothing deploying.`).
- **Rows** — the active deploy first, a pulsing **ship badge** and `deploying now` (accent-tinted row); then the upcoming stacks, each a **position number** badge and `next` / `then`. Accent-tinted (active run) rather than the queue's amber. No switches — unlike the autosync drawer, this panel does not act on anything.

---

## Autosync & queue API

`GET /api/autosync`, `POST /api/autosync`, `GET /api/queue`, and the `autosync` / `queue` SSE events on `/api/events` are specified in [`docs/autosync.md`](../../docs/autosync.md). The `POST` shares the trust level of the other endpoints (unauthenticated at the process; edge auth in front).

---

## Log view

Full-width monospace pane (bounded height, own scrollbar) showing all skipper-cd log output. Default order is **newest-first** (newest line at the top); the header **Sort toggle** flips to oldest-first (newest at the bottom, terminal semantics). Each line: muted timestamp, level badge, optional stack prefix, message, then dim `key=value` attrs.

Timestamps show the time of day; lines from another day get a date prefix, and the full `toLocaleString()` timestamp is always in the tooltip.

Level colours: `ERROR` → red, `WARN` → yellow, `DEBUG` → muted, `INFO` → secondary text. Lines with a `stack` attr (the deploy lifecycle: `deploying stack`, `deploy complete`, failures) render it as an accent-coloured `[gitea]`-style prefix and omit it from the trailing attrs, so what was deployed when is scannable. Child-process lines (attrs contain `cmd` and `stream`) render a muted `[docker]`-style command prefix instead of a level badge and the message in primary text; child output carries no stack attribution (known limitation — the runner does not know which stack it runs for).

`deploy complete` lines carry the deploy event's ID as an `event_id` attr (logged only when an event sink is configured, i.e. `ui_enabled`). The log view renders it as a **diff pill** instead of a plain attr: clicking fetches the deploy's diff from `GET /api/events/{id}/diffs` and inserts the same collapsible diff panel used by the deploy table directly below the line (click again to close; a notice appears when no diff was recorded, e.g. the event fell out of the bounded history).

Received lines are held in a bounded client-side buffer (2000 entries, oldest dropped on overflow) and the pane renders a sliding window of it. The window starts at **500 lines** (the newest 500) and grows by **500** each time the user scrolls to the older edge (the bottom when newest-first, the top when oldest-first); a scroll that reveals older lines preserves the reading position. Live lines are added incrementally at the newest edge, and the rendered window is trimmed back to its size from the older edge — trimmed entries stay in the buffer, so scrolling back reveals them again. Toggling sort rebuilds the pane and resets the window to one page. The `EventSource` for `/api/logs` is created lazily on first activation of the view and kept open afterwards; while the view is hidden, lines are buffered and the pane is rebuilt from the buffer on re-activation.

---

## Log API

`GET /api/logs` — SSE stream of captured log lines, event name `log`, payload:

```json
{"id":1720012345001,"time":"2026-07-10T12:00:00Z","level":"INFO","msg":"deploying stack","attrs":{"stack":"gitea"}}
```

On connect the in-memory backlog (bounded ring, 1000 entries, no persistence across restarts) is replayed — filtered by `Last-Event-ID` on reconnect — then live entries stream in. Entry IDs are seeded from the process start time so they stay monotonic across restarts. Slow consumers have lines dropped rather than blocking the logger. Same trust level as `/api/events` (unauthenticated); child-process output (`docker compose`, `git`, `nixos-rebuild`) is included — see ADR-0013.

---

## Diff API

`GET /api/events/{id}/diffs` — returns `{"diffs": {"filepath": "diff content", ...}, "commits": [{"sha","subject","author","date"}, ...]}` or `{"diffs": null, "commits": null}`. `commits` are the git commits in the range `LastDeployedCommit..HEAD` that touched the event's changed files, newest first (capped at 50). Returns 404 for unknown event IDs.

## Version API

`GET /api/version` — returns the build identity `{"version": "<semver>|dev", "branch": "<name>", "commit": "<short-sha>"}`. Fields are injected at build time via `-ldflags "-X main.version=… -X main.commit=… -X main.branch=…"`:

- `version` — semver from `.release-please-manifest.json` (`dev` for local builds without ldflags).
- `commit` — short git SHA. Injected by the Nix flake (`self.shortRev`) and by Docker/CI; for a local `go build` it is recovered from the Go build info (`-dirty` suffix for an uncommitted tree). May be empty.
- `branch` — git branch name. Only CI/Docker builds know it; the Nix flake and plain local builds leave it empty.

The header label is painted once on load: a **feature-branch** build (branch set and ≠ `main`) shows `branch · commit`; otherwise it shows `v<version> · commit` (or `dev · commit`). The same `version-commit` string is baked into the service worker's cache name (`/sw.js`) so two feature-branch builds that share a release semver still bust the app-shell cache.

Diffs and commit metadata are stored in `deploy-history.yaml` alongside events but are **not** included in SSE payloads (only `has_diffs: true` is sent). This keeps the real-time stream lightweight. Large diffs are truncated at 10 KB per file and 50 KB total per event.

---

## Progressive Web App (PWA)

The UI is an **installable PWA** (full spec: [`docs/pwa.md`](../../docs/pwa.md); decisions: [ADR-0018](../../dev-docs/adr/0018-pwa-installable-ui.md), [ADR-0023](../../dev-docs/adr/0023-pwa-update-prompt.md)). It is an enhancement layer — the page behaves exactly as before in a normal browser tab.

- **Manifest** — `GET /manifest.webmanifest` (`<link rel="manifest">` in `<head>`): `name` `skipper-cd`, `short_name` `skipper`, `display` `standalone`, `start_url`/`scope` `/`, `theme_color` `#1e1e2e` (Mocha base), `background_color` `#181825` (Mocha mantle) for the splash. Icons at 192 and 512 px (`purpose: any`) plus a 512 px `maskable` variant, all rendered from the ship logo and served under `/icons/`.
- **App identity** — the ship logo becomes the app icon (including a maskable variant so Android crops it into the system shape without clipping). Served as PNG because iOS ignores SVG and manifest icons for the home screen; an `apple-touch-icon` link and `apple-mobile-web-app-*` metas cover iOS.
- **Theme colour** — two `<meta name="theme-color" media="(prefers-color-scheme: …)">` tags (dark `#1e1e2e`, light Latte `#eff1f5`) let the OS window/status-bar colour follow the **OS** light/dark preference. Like the favicon, this cannot follow the in-page Mocha/Latte toggle (a platform limitation).
- **Service worker** — `GET /sw.js` (registered after `load`, failure-tolerant). Caches only the **app shell** (`/`, manifest, icons) and serves it **network-first with a cache fallback**, so a reachable server always wins and a just-deployed UI is picked up promptly. **Invariant: live traffic is never cached** — the worker bypasses `/api/*` (incl. the SSE streams `/api/events`, `/api/logs`), `/metrics`, and `/webhook` without `respondWith`, so streaming is untouched. The cache name carries the build version, so a new release changes the served `sw.js` bytes and the browser installs a new worker.
- **Update banner** (ADR-0023) — on an update the new worker does **not** `skipWaiting`; it stays *waiting* so a long-lived standalone window is not silently swapped. When a new worker reaches `installed` while one already controls the page (or is already `waiting` on load), a dismissible **`#update-banner`** (`data-testid="update-banner"`, bottom-centred, `role="status"`) appears: text plus a **Reload** action (`update-banner-reload`) and a **dismiss** (`update-banner-close`). Reload posts `{type:'SKIP_WAITING'}` to the waiting worker; the worker calls `self.skipWaiting()`, and the resulting `controllerchange` reloads the page **once** — when the update was accepted (Reload tapped) or a worker already controlled the page at load (cross-tab), but never on a bare first-install `clients.claim()`. `registration.update()` is polled on load and on `visibilitychange`→visible so a backgrounded app notices a deploy. Prompts are deduped per worker so a dismissed banner can still re-appear for a *later* deploy.
- **Secure context** — installability requires HTTPS (or `localhost`); the usual TLS reverse proxy satisfies this. Over plain HTTP the UI still works but is not installable. No new configuration — the PWA is active whenever `ui_enabled: true`.

---

## Test hooks (`data-testid`)

The E2E UI suite ([`docs/e2e-tests.md`](../../docs/e2e-tests.md)) selects **only** on
`data-testid` — never on `id`, text, or CSS class — so refactoring markup or
styling never breaks the tests. These attributes are a public contract of the
UI; keep them stable when editing `index.html`. Dynamic rows also carry data
attributes (`data-stack`, `data-status`, `data-level`, `data-state`) the tests
assert on.

| `data-testid` | Element | Notes |
|---|---|---|
| `brand-name` | Header `skipper-cd` wordmark | Stacked over the version label; hidden ≤ 700 px (logo alone carries the brand) |
| `brand-version` | Header version label | `v<semver> · <commit>` / branch; `dev` local; empty until `/api/version` resolves; full string in `title`; shown in portrait ≤ 700 px |
| `view-toggle` | Deploys/Logs segmented icon control | Active button (`.active`) opens the view-options popover |
| `deploy-indicator` | Deploy indicator (anchor/ship glyph) | `idle`/`deploying <stacks> · next <stacks>` in `title` + `aria-label`; `role="button"`, opens the run panel while active |
| `deploy-next` | Look-ahead trail beside the active stack | Empty when nothing follows; hidden ≤ 700 px |
| `deploy-count` | Mobile `+N` count chip (upcoming) | Shown only ≤ 700 px; empty when nothing follows |
| `run-drawer` | The run panel (this run's stacks) | `.open` when shown |
| `autosync-btn` | Header autosync control (drawer opener) | `data-global` = `true`/`false` (global autosync state) |
| `pending-pill` | Amber pending-count pill | Hidden at zero |
| `view-options` | View-options popover (opened from the active view button) | `.open` when shown; holds `time-mode` / `log-sort` / `follow-logs` |
| `time-mode`, `log-sort`, `follow-logs` | View-specific toggle buttons | Inside `view-options`; hidden until the popover opens |
| `theme-toggle` | Header theme (dark/light) toggle | Glyph-only; moon in dark, sun in light |
| `theme-select` | Header theme picker (`<select>`) | Present only when `ui_theme_switcher` is enabled; transparent over a palette glyph. See [Theme override](#theme-override) |
| `theme-notice` | Theme mismatch notice | Shown when `themeOverride` differs from `data-server-theme` |
| `theme-notice-close` | Theme mismatch notice's dismiss button | |
| `conn-indicator` | Connection indicator (chain-link glyph) | `data-state` = `connecting`/`connected`/`reconnecting` |
| `empty-state` | Awaiting-events placeholder | |
| `deploys-table` | The deploys view container (header + rows) | Snapshot anchor (UA1) |
| `deploy-row` | A deploy table row | `data-stack`, `data-status` |
| `status-badge` | Status badge inside a row | |
| `stack-icon` | Icon chip in the stack cell | |
| `health-pill` | Stack health pill in the stack cell | Newest row per stack only; `data-health` = `healthy`/`unhealthy`/`starting`/`stopped`/`unknown`; opens `health-panel` |
| `health-panel` | Per-service health breakdown panel below the row | Sibling of the row, like `files-panel`; leads with a stack + status header; carries `data-health` (drives the shared left bar/tint); the open row gets `health-open` + `data-health` |
| `health-service` | A service row inside `health-panel` | `data-health` per service |
| `time-cell`, `duration-cell` | Time / duration cells | Masked in snapshots (dynamic) |
| `files-pill` | Files pill on a row | |
| `files-panel` | Expanded plain file-list panel | |
| `diff-panel` | Expanded diff panel | |
| `error-panel` | Error detail panel under a failed row | |
| `deploy-search` | "Search stacks" row in the deploys view-options popover | Mobile-only entry point; reveals + focuses `deploy-filter` |
| `deploy-filter-wrap` | The filter bar container | Collapsed (height 0) until revealed; the reveal-state hook |
| `deploy-filter` | Deploys type-to-search input | Hidden until the user types (desktop) or taps `deploy-search` (mobile); folds down above the table |
| `deploy-filter-clear` | Deploys filter clear (`×`) button | Shown only when the field is non-empty |
| `deploy-filter-empty` | "No stack matches …" note | Shown when the query hides every row |
| `log-line` | A log line | `data-level` = level (or `cmd` for child output) |
| `level-badge` | Log level badge | |
| `stack-prefix` | `[stack]` prefix on a deploy log line | |
| `cmd-prefix` | `[cmd]` prefix on child-process output | |
| `diff-pill` | Diff pill on a `deploy complete` log line | |
| `autosync-drawer` | The autosync drawer | |
| `global-switch` | Global autosync switch | |
| `stack-item` | A row in the "All stacks" list | `data-stack` |
| `stack-switch` | Per-stack switch in "All stacks" | `data-stack`; only in the all-stacks list |
| `queue-item` | A row in the queued list | `data-stack` |
| `wait-cell` | Wait-time text on a queue item | Masked in snapshots (dynamic) |
| `stack-filter` | Stack filter input | |
| `stack-filter-clear` | Filter clear button | |
| `update-banner` | PWA "new version available" banner | Hidden until a newer service worker is waiting (UE1/UE2) |
| `update-banner-reload` | Banner's Reload action | Activates the waiting worker → one reload |
| `update-banner-close` | Banner's dismiss button | |

---

## Responsive (≤ 700 px)

**Header — compact single row.** The header is already glyph-only on every viewport (see [Header — right](#layout)), so little changes ≤ 700 px beyond tightening the row to 48 px, which **must never scroll horizontally**. The brand is the ship logo plus, **in portrait**, the version label (it clips with an ellipsis and can shrink so the row still never scrolls; the full string stays in its `title` tooltip). The `skipper-cd` wordmark is hidden; the version label is also dropped in the tighter landscape orientation. Control-specific changes:

- **Deploy indicator** — the deploying stack name and the look-ahead trail are dropped (both kept in `title`/`aria-label`); the anchor/ship glyph shows, and the trail collapses to a compact peach **`+N` count chip** (`deploy-count`) when stacks are still to come this run. Tapping the glyph still opens the run panel (which lists the names).
- **Autosync control** — unchanged (already icon-only); keeps its glyph and pending-count pill.
- **View toggle / theme toggle / connection** — unchanged; already glyph-only.
- **View-options popover** — unchanged; still opened from the active view button.
- **Theme picker** — hidden entirely; overriding the palette is rarely needed on a phone. The configured theme (and any override already saved from a previous desktop visit) still applies.

On touch there are no hover tooltips, so each control's label is reachable via the **tap-reveal bubble** (a tap flashes the `title`; the action still fires). The `.status-area` gap tightens to 10 px and the header padding to 12 px so the row fits a 360 px viewport without overflow.

**Deploy table.** Column header hidden. Rows collapse to a 2×2 grid (stack + badge on row 1, time + duration on row 2). Files column hidden. Since there is no keyboard for type-to-search, the [Deploys view-options popover](#view-options-popover) gains a **"Search stacks"** entry that reveals the [filter](#deploys-filter) — the type-to-reveal path is desktop-only.

Since the Files pill is not visible on mobile, tapping anywhere on a row (that has `changed_files`) triggers the files/diff panel instead. Rows with changed files get `cursor: pointer` on mobile. The toggle behaviour (tap again to close) is identical to the desktop pill behaviour. The [health pill](#stack-health) keeps its own tap target — a tap on it stops propagation and opens the `health-panel`, not the row's files/diff panel.

The Autosync drawer spans the full width below the header.
