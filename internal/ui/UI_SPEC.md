# skipper-cd Web UI — Specification

Single-page application served at `/` when `ui_enabled: true`. No external JS dependencies. The app script lives in `index.html`; its pure, DOM-free helpers (time/duration formatting, health/level classification, diff-line classing, …) are extracted into `static/app-helpers.js`, loaded first so the app calls them as globals — and exercised in isolation by a `node --test` unit layer (`app-helpers.test.js`, `make ui-unit`) with no build step (ADR-0035). The stylesheet lives in `static/app.css`, linked from `index.html`'s `<head>` and served same-origin (`AppCSSHandler`), so `index.html` stays the app-shell markup and script only (ADR-0035 amendment). Real-time deployment events via SSE (`/api/events`); real-time log lines via SSE (`/api/logs`). The `/api/events` stream also carries `autosync` and `queue` events that drive the autosync controls and the queue drawer live (see [Autosync](#autosync)), and a `health` snapshot that drives the per-stack [Stack health](#stack-health) pill.

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
| `--queued` | Queued/deferred deploy, pending count, autosync paused, and `blocked` (dependency failed) — all pending states (shares its colour with `--hunk`, distinct semantic) |
| `--diff-add` | Diff additions |
| `--hunk` | Diff hunk headers, WARN log level |

Each theme maps these tokens to its own raw colours (`--raw-accent`, `--raw-success`, …) under `:root[data-theme="<name>"]` / `:root[data-theme="<name>"].light`; adding a theme is a self-contained CSS block, nothing else in the page references a theme name. Background depth: `crust` (sunken — log pane, diff/files panels) → `mantle` (page) → `base` (header glass, cards) → `surface0` (raised — tags, toggle tracks). Text: `text` / `subtext0` / `overlay1` (primary / secondary / muted).

Fonts: **DM Sans** (UI) + **JetBrains Mono** (timestamps, stack names, badges). Both are **self-hosted and embedded** (latin subset, weights 400/500/600): the `woff2` files live in `static/fonts/` and are served same-origin from the embedded FS under `/fonts/` (scoped `FontsHandler`, immutable cache), referenced by `@font-face … url(/fonts/…)`, `<link rel="preload">`ed in the head, and cached in the service-worker app shell. There is no external Google Fonts request, so the page stays fully self-contained, works offline, and renders deterministically for visual snapshots (ADR-0035; see [`docs/e2e-tests.md`](../../docs/e2e-tests.md) §5). Background: mantle with subtle grid overlay and peach radial glow at top centre.

---

## Layout

Sticky frosted-glass header (56 px) + centred main (max 1040 px).

**Header — left:** skipper-cd container-ship logo (inline SVG, 32 px — a hull with wave carrying three container boxes: one in `--accent`, one in `--success`, one outlined; hull, outline and wave follow `--text-primary` via `currentColor`, so the logo tracks the theme toggle) and, stacked beside it, a text column: the `skipper-cd` wordmark (accent `-cd`) over a small muted **version label** showing the deployed build identity (`v<semver> · <commit>`, or the branch name for feature builds; local builds without ldflags show `dev`). The label is fetched once on load from `GET /api/version`, left empty until it resolves, and carries a `title` tooltip with the full string. It is a shrinkable flex item (`min-width: 0`) that shows in full whenever the header has room and only clips with an ellipsis when space genuinely runs out — no fixed character cap. The favicon is the same ship as an SVG data URI, its colours drawn from the configured theme, with a `prefers-color-scheme` media query switching between its light and dark variant (favicons cannot follow the in-page toggle).

**Header — right.** The controls are **icon/glyph-only** on every viewport (no text labels or switch tracks in the header row). Each glyph follows the theme via `currentColor`, is muted by default, and only takes on colour to signal a real state. Pointer users get the native `title` tooltip on hover; since touch never shows `title` tooltips, any glyph-only control marked `data-taptip` (or nested under one — the whole header opts in as a group) flashes its label in a small **tap-reveal bubble** on tap (the control's action still fires). It's a deliberate opt-in, not every titled element — see [`dev-docs/ui-design-concept.md`](../../dev-docs/ui-design-concept.md). A one-time [first-run tour](#first-run-header-tour) also labels the controls on a fresh browser. In order:

- **Deploy indicator** — an **anchor** glyph (muted) at rest, swapping to a **ship** (accent, pulsing) while deploying. The glyph leads; to its right comes the active stack name(s) and then a dimmed **look-ahead trail** — `→ a · b · +N` — naming the stacks that will deploy *next in the same run*, capped at three names plus a `+N` overflow. The trail's source is the [`upcoming`](#event-lifecycle-sse) SSE event (distinct from the autosync pending queue: this is the *active run's* remaining work). `idle` / `deploying <stacks> · next <stacks>` is mirrored into `title` + `aria-label`. While a run is active the indicator is a **button** (`role="button"`, `Enter`/`Space`): it toggles the [Run panel](#run-panel). Visible in all views.
- **View toggle** — segmented control of three icons: a rows/table glyph (`deploys`), a layers glyph (`stacks`, the [Stacks view](#stacks-view)) and a pulse/waveform glyph (`logs`, the [Log view](#log-view) — a live tail, not a generic terminal). Default: `deploys`. State persisted in `localStorage` key `activeView`. The **active** button always carries a top bar marking it as the current view (every view, Logs included). On Deploys/Stacks it additionally carries a small `▾` (the `.vt-caret` child, flipping up while its popover is open) and opens the [View-options popover](#view-options-popover); Logs has no popover — no caret, and its controls live inline in its own panel header. The other buttons switch views.
- **Autosync control** — a single sync-arrows glyph showing global autosync state; **muted by default**, turning `--queued` (amber) when paused and `--accent` while its drawer is open. When deploys are queued it also shows an amber **pending count** pill (hidden at zero). Not a `localStorage` preference — it reflects server state from the `autosync`/`queue` SSE events. Click (or `Enter`/`Space`) toggles the [Autosync drawer](#autosync). Visible in all views.
- **Theme picker** — a palette glyph over a transparent native `<select>` of the five built-in palettes (see [Theme override](#theme-override)); clicking opens the native option list. **Opt-in**: present only when `ui_theme_switcher: true` (see [`docs/configuration.md`](../../docs/configuration.md#web-ui-theme)); off by default, so the deployed theme is fixed. Desktop only (hidden ≤ 700 px). Visible in all views.
- **Theme toggle** — a moon (dark, default) / sun (light) glyph switching between the configured theme's dark and light variant. State persisted in `localStorage` key `colorScheme` (`dark` / `light`). Visible in all views.
- **Connection indicator** — a **chain-link** glyph: a closed link in `--success` when `connected`, a closed link pulsing `--accent` while `connecting`, and a **broken link** pulsing `--danger` while `reconnecting`. State is on `data-state`. Bound to `/api/events`; the log stream has no own indicator. `reconnecting` recovers on its own from *both* a transient drop (the browser's built-in retry) and a fatal stream error — a non-2xx response or bad content-type, which closes `EventSource` for good — via a capped-backoff retry the page runs itself.

### First-run header tour

Because the header is glyph-only, a first-time operator would otherwise learn it only by hovering for a `title` tooltip (which touch never shows). On a **fresh browser only**, a one-time tour teaches the mapping (T3.15): a small uppercase **caption** sits under each control (`Search`, `Deploys`/`Stacks`/`Logs`, `Hosts`, `Autosync`, `Theme`, `Light / Dark`), and a slim accent **banner** (`data-testid="header-tour"`) appears under the header explaining it, with a **Got it** button (`data-testid="header-tour-dismiss"`). Activating it — or pressing `Esc` — dismisses the tour for good and returns focus to the active view button.

The state is a single `localStorage` key `headerTourSeen` (`'1'` once dismissed), applied as a `.header-tour-seen` class on `<html>` by the **pre-paint** inline script (alongside the theme classes), so a returning browser never flashes the tour and its header is byte-identical to the no-tour case — each caption wrapper is `display:contents` when seen, only becoming a column during the tour, and the header grows to fit the captions just for that one visit (the `--app-header-h` var kept in sync so the Logs fullscreen offset stays correct). It is purely storage-gated (no timers), so the shown/dismissed states are deterministic. The tour is a **desktop/tablet affordance**: on the compact ≤ 700 px header the captions don't fit, and a banner naming captions nobody can see is pointless, so the whole tour is suppressed there — a phone-only visitor is never marked seen, so it still greets them the first time they open a wider viewport. A caption whose control isn't currently rendered (the search glyph on Logs, the Hosts control on a single-host instance) is dropped so no label floats without a glyph.

### View-options popover

The view-specific toggles live in a small popover anchored under the view toggle (styled like the [Autosync drawer](#autosync)), **not** in the header row — so switching views never makes a header control appear or disappear. Only Deploys and Stacks carry one (Logs' controls live inline in its own panel header instead — see [Log view](#log-view)). It is opened by clicking the already-active view button (the `▾` hint), and dismisses on outside-click or `Esc`; it and the Autosync drawer are mutually exclusive. Inside, each option is a full row (glyph + label + switch track). Contents by active view:

- **Deploys** → **Time mode** — switches the Time column between relative (`Xs ago`) and absolute (`toLocaleString()`). Default: inactive (relative). `localStorage` key `timeMode`. Plus **Version changes** (`data-testid="image-delta-toggle"`) — shows/hides the [Version column](#image-delta). Default: **active** (shown); a per-browser choice, so only the *off* state is stored (`localStorage` key `imageDelta` = `off`; absent means on). Flipping it collapses or restores the whole column (header and row separators included) on every rendered row live, no reload. On **mobile only**, the group is preceded by a **"Search stacks"** action row that reveals and focuses the [Deploys filter](#deploys-filter) (the touch entry point for type-to-search).
- **Stacks** → **Time mode** — the same shared toggle as Deploys (one `timeMode`; flipping it re-renders both views). Plus, on **mobile only**, a **"Search stacks"** action row that reveals and focuses the [Stacks filter](#stacks-view) (the touch entry point for its type-to-search).

### Theme override

The theme picker is **opt-in**: it renders only when `ui_theme_switcher: true`. By default the flag is off — the picker is absent, the pre-paint script ignores any saved `themeOverride`, and the deployed `ui_theme` is fixed and cannot be switched from the browser. The switcher exists mainly to try palettes out. When enabled, it lets a **browser** show a different palette than the one `ui_theme` configures for the environment, without touching the deployment. This is purely a local, client-side preference:

- **Gating** — the flag is baked into `<html data-theme-switcher="on|off">` at serve time (`ui.IndexHandler`). The pre-paint script and the picker JS both read it: with the switcher off the `<select>` is hidden (CSS) and no override or mismatch notice is applied, so a `themeOverride` left in `localStorage` from a time the flag was on lies dormant until it is re-enabled.

- **Applying** — selecting a theme sets `document.documentElement`'s `data-theme` attribute immediately (every theme's CSS is always present in the stylesheet, so there is no reload and no flash). `<html>` carries a second, immutable attribute, `data-server-theme`, holding the value the environment is actually configured with — set once at serve time by `ui.IndexHandler` and never touched by JS. It is the reference the mismatch notice compares against.
- **Persisting** — a non-default choice is saved to `localStorage` key `themeOverride`. Picking the theme that matches `data-server-theme` again removes the override, so the page goes back to following whatever the environment is configured for (including across a `ui_theme` change on a later deploy). A pre-paint inline script in `<head>` re-applies a saved override before first render, the same no-flash approach the colour-scheme toggle uses.
- **Mismatch notice** — whenever `themeOverride` is set and differs from `data-server-theme` (which, by the rule above, is whenever an override exists at all), a dismissible notice appears under the header: *"Showing `<override theme>` in this browser — this environment is configured for `<server theme>`."* It auto-hides after 6 seconds, or immediately on clicking its close button; the check re-runs (and the notice reappears if still applicable) on every page load and every theme-picker change, but never nags mid-session beyond that.
- **Scope** — the override never reaches the server: there is no endpoint to change `ui_theme` from the UI. It only ever affects the browser that set it.

---

## Deploy table

6-column grid (`150px minmax(0,1.15fr) minmax(0,1fr) 110px 76px 96px`): **Time · Stack · Version · Status · Duration · Files**. The columns live in a `--deploy-cols` variable on `#deploy-table` so the header and the rows stay in lockstep; `.no-version` swaps in the 5-column set when the [Version column](#image-delta) is toggled off.

Rows are prepended (newest first) with a slide-in animation and are **top-aligned** (`align-items: start`) with a hairline separator between them: the [Version column](#image-delta) is routinely taller than one line, so centring would drag Time/Stack/Status to its middle and cost the row its common starting line, and the spacing alone would no longer show where one row ends. The separators are tied to that column — with it toggled off every row is a single line again, so they disappear with it. Time cells show relative or absolute time depending on the header toggle. Relative times refresh every 30 s; tooltip always shows the other format. A row that changed one or more service images names them in its **Version** column — the [image delta](#image-delta).

### Stack icons

The Stack cell carries a small icon chip (18 px, fixed box, `object-fit: contain`) left of the name for recognition. The image is served same-origin from `GET /api/icons/<stack>` (no CSP concern); on any load error the chip swaps to a **monogram** — the stack's first letter on an accent-tinted chip — via the `<img>` `error` handler, so a broken image never shows. Icons are resolved server-side (repo `icon.svg`/`icon.png` override → configured `icon:` slug → auto-match on the stack name → 404 → monogram) and cached on the host; see the README "Service Icons" section.

The reserved pseudo-stacks resolve to fixed slugs instead of the monogram: `_nixos` (the NixOS rebuild) auto-matches the `nixos` icon and `_config` (file-level stack-config failures in stack-discovery mode, ADR-0034/ADR-0043) the `git` icon.

**Refreshing icons.** The server-side icon cache is cleared by `POST /api/icons/refresh` (e.g. `curl -X POST …/api/icons/refresh`); the next load of each icon then picks up renamed stacks and newly published icons. Like `POST /api/autosync` it is same-origin-only — a browser on another site gets `403` — which leaves the `curl` call unaffected (see [`dev-docs/http-api-spec.md`](../../dev-docs/http-api-spec.md)). It is an **ops endpoint** — there is deliberately no header control and no keyboard trigger (the single-key `i` hotkey was removed so type-to-search in the deploys view can own printable keys — see [Deploys filter](#deploys-filter)).

### Image delta

A deploy row surfaces **which service(s) it updated, and to what**, without opening the diff. When the event carries `image_changes`, the row's **Version column** (`.col-version`) holds a chip per changed service (`.tag-delta`, wrapped in `data-testid="svc-delta"`), **stacked one per line** — a column, rather than a note beside the stack name, so the versions line up down the table and can be scanned vertically. Each chip shows the **service name** (always — it names which service moved, so a `caddy` service in a `web` stack, or several services, each stays identifiable), then the change. The change reduces the image reference to the shortest tokens that actually differ:

- a **tag bump** shows the tags (`ghcr.io/acme/api:1.5.0` → `ghcr.io/acme/api:1.6.0` renders `1.5.0 → 1.6.0`);
- a **same-tag rebuild** (only the pinned digest moved) shows the shared tag plus a `↻` **rebuilt** marker (`nginx:1.25@sha256:aa…` → `…bb…` renders `1.25 ↻`), not two unreadable hex digests — the full digests are on the chip's tooltip/label.

The registry and repository are dropped from the visible chip — the service name already identifies the image. Each chip carries `role="img"` + an `aria-label` announcing the change as one phrase (`web updated from 1.25 to 1.26`) and a `title` with the **full** `old → new` reference (registry + repo + digest — progressive disclosure). The chip itself is low-chroma (a neutral fill, only the new tag in the diff-add accent) so it does not compete with the status colour, the log's primary scan signal. An empty `old` reads as the service's first image (no left side); an empty `new` reads as **removed**. **Every** changed service is listed — the column is the answer to "what moved", so nothing is folded behind a count. Only services whose image reference actually changed are listed, so a config-only deploy shows no delta.

The data rides the `image_changes` field on the event's SSE payload (small, like `heal_drift`) — no extra fetch, so the column fills the moment the row renders, and also when a live `deploying` row settles (its `deploying` event carries no `image_changes`). A **healed** row leaves the cell empty (self-heal re-applies the same version), as does a **peer row** (the fan-in's audit records carry no image changes) — both still emit the cell so the grid stays aligned.

The chip itself (`versionChipHTML`) is the UI's **one** version component: the same frame and colours render a deploy's change here, a stack's running version in the [Stacks view](#service-versions), and each line of the [containers panel](#stack-health). Only the tokens inside differ — old→new here, a single current token there.

**Width and responsive.** Stack keeps the larger share of the free width; a chip that outgrows the column **wraps within itself** rather than truncating, so a narrow column costs height, never the service name. On **mobile** there are no columns (the row is a 2×2 block), so the Version column becomes a **full-width row** beneath the name/time, keeping its stacked chips and staying left of the row-tap centre. The column is toggleable per browser via the Deploys [**Version changes** view-option](#view-options-popover) (default on); switching it off collapses the column entirely — chips, cell width and the `Version` header — rather than leaving an empty column behind.

### Stack health

The Status cell also carries a **health pill** (`data-testid="health-pill"`) stacked below the deploy status badge — a small dot + label showing the stack's *current* runtime health, ArgoCD-style. It reflects the live [`health`](#event-lifecycle-sse) SSE snapshot, not the deploy outcome, and is a distinct axis from the [Status badge](#status-badges): a `success` deploy can still be `unhealthy` now (a container crash-looped afterwards), and a `deploying` stack can be `starting`. This is skipper-cd's own-stack health only (see [ADR-0027](../../dev-docs/adr/0027-live-stack-health-in-ui.md)); it is read-only and never restarts or redeploys anything.

| Health | Colour | Meaning |
|---|---|---|
| `healthy` | `--h-healthy` (green) | Every service running; every service with a healthcheck healthy |
| `unhealthy` | `--h-unhealthy` (red) | Any service `unhealthy`, `restarting`, or unexpectedly `exited`; dot pulses |
| `starting` | `--h-starting` (amber) | Any service still `starting` and none unhealthy; dot pulses |
| `stopped` | `--h-stopped` (grey) | No running containers for the project. Also the per-service status of an exited `on_demand_containers` container, whatever its exit code — skipper stops those on purpose after the deploy (ADR-0027 amendment) |
| `unknown` | `--h-unknown` (grey, dashed border) | Health could not be read (`ps` failed) — never a false `unhealthy` |

The health colours are their own semantic tier (`--h-*`), kept a distinct hue from `--accent` and the teal `--success` so the pill never reads as a deploy status.

**Placement — newest row per stack only.** The deploy table is an event log (many rows per stack over time) while health is a single current per-stack value, so the pill renders **only on the topmost (most recent) row for each stack**; older rows of the same stack carry no pill. This keeps the pill reading as "the current state of this stack" rather than implying it belonged to a past deploy, and keeps the 5-column grid intact (no dedicated Health column).

**Surfacing unhealthy stacks — beacon + attention band.** Because the pill is row-bound, a stack that last deployed long ago sits far down the log — or its row ages out of the bounded event list entirely, dropping the pill altogether. Two snapshot-driven affordances lift a currently-**unhealthy** stack to the top, both reading the same `health` snapshot via the pure `attentionStacks()` helper (only `unhealthy` qualifies — `starting`/`stopped`/`unknown` are reporting-only and stay quiet):

- **Health beacon** (`data-testid="health-beacon"`) — a red, `--danger`-tier chip in the header next to the [deploy indicator](#header--right), **present in every view**. Hidden when nothing is unhealthy. It shows a pulsing dot + the unhealthy stack **count** (`data-testid="health-beacon-count"`), with a pluralised `title`/`aria-label` (`"2 stacks unhealthy"`). It is a `<button>` opening a popover (`data-testid="health-beacon-pop"`) that lists each unhealthy stack (`data-testid="health-beacon-item"`); activating an item jumps to that stack **in the current list view** — the roster row when the Stacks view is active, else the newest deploy row — so a jump never yanks you out of the view you are triaging in. The popover joins the shared surface-exclusivity (Escape / outside-click close, like the run/view popovers).
- **Attention band** (`data-testid="attention-band"`) — a `--danger`-tinted card pinned **above** the deploy log (inside `#deploy-table`, so it hides with the Deploys view), listing one row per unhealthy stack (`data-testid="attention-row"`: icon + name + the [health pill](#stack-health)). Hidden when empty, so it costs no vertical space in the healthy case. Clicking a row jumps to that stack's newest deploy row.
- **Stacks view** needs no band: the [roster](#stacks-view) already carries a per-row health pill, unhealthy rows **sort to the top** there (see below), and an unhealthy row wears a **severity bar + tint** — the same `--danger` treatment a `failed`/`rolled_back` deploy row wears — so the row reads as bad at a glance, not just via its pill. Reordering is legitimate in an inventory (unlike the deploy log), so the Stacks view floats the signal in place rather than lifting a copy out.

Both jumps reuse `jumpToStack(…)` and so degrade to a plain view switch when the stack has no row (same as the [cross-view jump button](#cross-view-stack-jump)). Deliberately **not** done for the Deploys log: re-sorting it to float unhealthy rows up — that would break the event-log's chronological reading; the band lifts a *copy* of the signal out instead of reordering the log.

**Per-service breakdown.** The pill is a real `<button>` — keyboard users reach it with `Tab` and toggle the panel with `Enter`/`Space`, like the files pill and the history button. Clicking (or activating) the pill inserts a sibling panel directly below the row (`data-testid="health-panel"`, same expand pattern as the files/diff/error panels), listing each service (`data-testid="health-service"`) with its running [version](#service-versions) (`data-testid="health-version"`, when the snapshot carries images), its raw container state (`running`/`exited`/…) and its **classified status** as the coloured element. The raw compose `health:` value is deliberately not spelled out — with a healthcheck it always equals the classified status, so showing both would say `healthy` twice on every line. A service from the stack's `on_demand_containers` carries an `· on-demand` label in its state cell (plus an explanatory `title`), so its `exited · stopped` reads as the scheduler-managed idle it is — the backend also classifies it that way (`stopped`, never `unhealthy`, whatever the exit code). Clicking again removes it. On mobile the pill stays tappable in the collapsed 2×2 row. **One panel per row:** the health panel and the files/diff panel are mutually exclusive — opening one closes the other, so the open panel is always the row's direct sibling and the row's tint/bar is never bound to two panels at once (a click order must not change the layout).

**Status history (health watch).** When the [health watch](../../docs/configuration.md#health-watch) is configured (ADR-0031), the panel additionally shows *how long* each status has held and *what came before*: the service line gains the current phase's age (`unhealthy · for 6h12m`), and — once a service has **more than one** accepted phase (a lone baseline would only repeat the inline age) — a compact timeline (`data-testid="health-history"`) below it lists the service's last accepted status phases (≤ 10, newest first) — one line per phase (`data-testid="health-phase"`) with a status-coloured dot, the status, when it began (local time), how long it held, and — when the phase began within the attribution window after a deploy — the deploy's short commit as a chip (`data-testid="health-phase-commit"`, a [commit link](#commit-links)). Only debounce-accepted phases appear, so the timeline shows real transitions, not probe blips. The data comes from the `healthwatch` SSE snapshot; the panel renders whatever snapshot is current at open time (like the service list itself). **Graceful degradation:** with the watchdog off there is no `healthwatch` snapshot and the panel renders exactly as before — no age, no timeline.

**Row binding.** So the open panel always reads as tied to its row (whatever its position, and with several panels open), the pill click also **tints the parent row and the panel** in the stack's health colour and draws a shared 3px left bar down both, and the panel leads with a **header echoing the stack name + status pill**. The tint/bar colour is the stack's rolled-up status — the worst (least-healthy) service, the same colour as the pill: `unhealthy` (red) dominates, else `starting` (amber), else `healthy` (green), else `stopped` (grey). The parent row carries `health-open` + `data-health` while its panel is open (both cleared on close); the panel carries `data-health` for the same colour (ADR-0027, variant A).

### Self-heal

Where the [Stack health](#stack-health) pill only *reports* a degraded stack, **self-heal** (opt-in, per stack) automatically restores it with a corrective `docker compose up -d` — the runtime-drift counterpart to the [periodic reconcile](configuration.md#periodic-reconcile) loop's git-drift correction. It surfaces in the deploy log as two statuses (see [Status badges](#status-badges)): a `healed` row each time a corrective redeploy runs, and a single `heal_exhausted` row when skipper gives up after repeated redeploys fail to restore the stack (also the default `heal_exhausted` notification). The action is backend-only — there is no UI control to trigger or configure it; it is driven entirely by `self_heal` config. See [ADR-0029](../../dev-docs/adr/0029-runtime-drift-self-heal.md).

**Self-heal badge + detail panel.** A heal is not a git deploy — nothing changed, so there is no diff and no files pill. Instead the healed row's files cell carries a teal **self-heal badge** (`data-testid="heal-pill"`) standing in for the files pill. Clicking it (or anywhere on the row) expands a bound detail panel (`data-testid="heal-panel"`, variant A, teal left bar) that explains the corrective redeploy and — when the event recorded them — lists the **services that had drifted** when the heal ran (each service name + its degraded `unhealthy`/`stopped` status), so the row answers "why did this heal" rather than being a dead end. The drift rides the `healed` event on the SSE payload (it is small, unlike diffs), so no extra fetch is needed; older healed events without recorded drift show the explanation alone.

### Row overflow menu

To keep the newest row per stack calm — identity + status + one navigation action rather than a cluster of look-alike glyphs (T3.13) — its **secondary actions collapse behind a single `⋯` button** (`data-testid="more-btn"`) in the stack cell, opening a small popover menu (`data-testid="more-pop"`) of labelled rows: [deploy history](#deploy-history), [container logs](#container-logs) and, when the stack declares any, [deploy hooks](#deploy-hooks). The relocated buttons keep their own testids and click behaviour, so opening a panel is unchanged — they are only moved. What stays **directly on the row** is the identity (icon + name), the [cross-view jump](#cross-view-stack-jump) button, the [status badge](#status-badges) + [health pill](#stack-health) and the [files/diff pill](#expandable-panels) — state plus the one primary navigation, not the deeper actions. The menu is a registered dismiss surface (outside-click / `Esc`, `aria-expanded` on the trigger); picking an action closes it. It opens left-aligned but **flips right-aligned near the viewport edge** so it never spills off-screen in portrait. The `⋯` menu is a **Deploys-view** affordance (newest row per stack). The [Stacks roster](#stacks-view) does **not** collapse this way: its row-body click already opens the health + history panel, so a `⋯` there usually wrapped a single action (logs) for no density gain. Instead its secondary actions sit **inline** in the stack cell beside the [jump](#cross-view-stack-jump) and app-link — [container logs](#container-logs) always, [deploy hooks](#deploy-hooks) when the stack declares any — with no `more-btn` on the roster; the running-hook pulse rides the inline hooks badge directly. A `_nixos` row's menu (Deploys view) holds only its deploy history (it has no container logs and no hooks).

### Deploy history

The **newest row per stack** carries a small **history button** (`data-testid="history-btn"`) — a clock glyph — inside its [⋯ overflow menu](#row-overflow-menu). Clicking it inserts a per-stack **deploy-history panel** (`data-testid="audit-panel"`) below the row, listing that stack's *durable* record of past **terminal deploy outcomes** (skipper-cd [ADR-0033](../../dev-docs/adr/0033-durable-per-stack-deploy-audit-log.md)). Unlike the live deploy table — a bounded, global, cross-stack event log — this is the answer to "what is the full deploy history of *this* stack", kept per stack and surviving both ring eviction and restarts.

- **Source.** Fetched on open from [`GET /api/audit?stack=<name>`](#audit-api) (not from the SSE feed), so it always reflects the latest — no client cache to go stale.
- **Rows** (`data-testid="audit-row"`, newest first) — each past deploy as time · **status** (coloured dot + label, keyed off `data-status`) · duration · short [commit SHA](#commit-links) (full SHA in `title`, linked to the forge) · changed-file count. A `failed` / `rolled_back*` / `heal_exhausted` record also shows its error message (truncated, full text in `title`). Only terminal outcomes appear — `success`, `failed`, `rolled_back`, `rolled_back_unhealthy`, `healed`, `heal_exhausted`; in-progress (`deploying`), no-op (`skipped`) and deferral (`queued`, `blocked`) statuses are never recorded.
- **No diffs.** Records are metadata only; the short SHA identifies the commit, and the live [diff panel](#expandable-panels) still serves diffs for events still in the ring. The history answers *when / result / how long / which commit / how many files*, not "show me the code change".
- **One panel per row.** The history panel shares the [health panel](#stack-health)'s binding: opening it closes an open health or files/diff panel on the row (and vice versa), and it tints the row with a neutral **accent** bar (not a status colour — the panel is many statuses, not one).

### Cross-view stack jump

Every stack name — in a deploy row and in a [Stacks view](#stacks-view) roster row alike — carries a small compass **jump button** (`data-testid="jump-btn"`) that switches to the other view and lands on that same stack there, so the two views (event log vs. inventory) read as one connected surface instead of two disconnected tabs. The `_nixos` rebuild pseudo-stack has **no** jump button (it is not in the Stacks roster, so there is nowhere to land) and **no** [container-logs button](#container-logs) (it is not a compose project); its rows carry only the affordances that apply to it — its git diff and its [deploy history](#deploy-history). It sits beside the name, before the [⋯ overflow menu](#row-overflow-menu) — a distinct compass glyph, so a tap can't be mistaken for the menu or for opening a panel; it stops the click from reaching the row's own click handler (which would otherwise also open that row's diff/history panel).

Landing behaviour is direction-dependent: **Deploys → Stacks** always lands on *the* roster row (the Stacks view has exactly one row per stack — inventory, not a log); **Stacks → Deploys** lands on the stack's *newest* row (first in DOM order — the deploy table is a log with one row per deploy). The landing row scrolls into view (`scrollIntoView({block:'center'})`) and flashes an accent tint + left bar for ~1.8s (`.jump-target`, `@media (prefers-reduced-motion: reduce)` skips the animation but keeps the tint) — a temporary highlight, not a persisted state like `diff-open`/`health-open`/`audit-open`. The button always renders; when the target view has no row for that stack (e.g. jumping to a stack that has never deployed), the jump degrades to a plain view switch with nothing to land on.

The jump also **clears the target view's own [search filter](#deploys-filter)** before landing: a leftover query from before the jump could otherwise leave the landing row `.filtered-out` (hidden), switched to but invisible with no indication why.

### Disabled stacks

In [stack-discovery mode](../../docs/configuration.md#stack-discovery) a stack can be parked with `disabled: true` — present in the repo, deliberately not deployed. Those names render as a quiet **chip line below the deploy table** (`data-testid="disabled-stacks"`): a muted `disabled` label followed by one dashed-border chip per name, with an explanatory `title` on the line. Driven by the [`stacks`](#event-lifecycle-sse) SSE snapshot; the line is hidden entirely when the set is empty (always, when stacks are listed explicitly) and in the logs view. Deliberately **not** table rows: the table is an event log and a disabled stack has no events — the line is inventory, not history.

A file-level config failure in discovery mode (an unreadable stacks base dir, or a leftover in-repo `skipper.yaml` that ADR-0043 no longer reads) emits an ordinary `failed` event under the reserved `_config` pseudo-stack, so it renders as a regular failed row — no dedicated surface; the error panel shows the message, and its [icon](#stack-icons) is the git logo.

### Orphans

Compose projects running on the host that the discovered stack set no longer accounts for (ADR-0036) render as a **collapsed section below the [disabled line](#disabled-stacks)** (`data-testid="orphans"`). A muted header button (`#orphans-head`, `aria-expanded`) shows an `Orphans` label and a count pill; clicking it toggles the `open` class to reveal one row per orphan. Each row carries a class tag — amber `orphaned` (a removed stack, prunable) or dashed-muted `unmanaged` (never deployed by skipper, never pruned) — the project name, its working_dir, and a right-hand note: the container count (via `orphanMeta`) or `state only` for an orphan surfaced from a stale `state.yaml` entry with nothing running.

A row with containers is itself an **expandable button** (`aria-expanded`, a leading caret): clicking it toggles `.orphan-item.open` to reveal the detail panel. It opens with an optional project-level facts block (`config` — the compose file the project came from; `volumes` — the named volumes compose created, tagged `kept on prune` since prune never passes `--volumes`), then a per-container list — a state dot (`orphanStateClass` maps docker `State` to the health `healthy`/`unhealthy`/`stopped` dot colours), the container name, its image, its published ports (when any), and its status text (e.g. `Up 5 days`, `Exited (0) 2 days ago`). A state-only orphan has no containers, so its row is a plain `div` with a hidden caret. Driven by the [`orphans`](#event-lifecycle-sse) SSE snapshot on the health-poll cadence; the section is hidden entirely when the set is empty and in the logs view. Read-only: pruning is config-driven, not a UI action.

The [deploy search](#deploy-table) also scans orphans: an active query matches an orphan on its project name, working_dir, config file, volumes, or any container field (`orphanMatchesQuery`). A matching orphan is shown and **auto-expanded** (and the section itself opened, `applyOrphansSectionOpen`) so the hit is visible; when the match is on a container the non-matching containers are hidden (`containerMatchesQuery` → `.orphan-cont.filtered-out`) so the hit stands out. An orphan that does **not** match is hidden while the search is active (`.orphan-item.filtered-out`), like the deploy table filters its rows. Search-expansion is additive to `orphansOpen` (a manual open is never overridden) and everything reverts when the query clears. Orphans are counted among the searchable elements, so the filter's `hits/total` includes them, and the section header's count pill shows the number of matching orphans while a search is active (the total otherwise).

### Status badges

Every badge **leads with an icon** in a shared fixed slot (`svg.badge-ico`, 24×24 stroke geometry matching the header icons; `currentColor`, so it inherits the badge's text colour) so the glyphs read at one optical size regardless of label width (T3.14). `deploying` keeps its animated spinner in that slot; unknown/label-only statuses render text alone. The icon markup comes from the pure `statusIcon` helper (`app-helpers.js`), so `badgeHTML` stays a string builder. The two worst terminal states — `rolled_back_unhealthy` and `heal_exhausted` — render as a **solid alert chip** (opaque `--danger` fill, `--crust` text + warning icon, a soft danger glow), the loudest chip in the status column rather than the smallest, correcting the earlier inverted hierarchy where the most attention-critical states were 9px two-line dim text (T3.14).

| Status | Icon | Colour | Notes |
|---|---|---|---|
| `deploying` | spinner | `--accent` (peach) | Animated spinner dot |
| `success` | check | `--success` (teal) | |
| `failed` | cross | `--danger` (red) | Error panel expanded below row |
| `rolled_back` | revert arrow | `--rollback` (maroon) | Deploy failed but old containers restored and verified healthy; error panel shows details |
| `rolled_back_unhealthy` | warning | `--danger` (red, **solid**) | Rollback ran but the restored version also failed the health gate — a solid danger chip with a warning icon; the label still stacks "rolled back" / "unhealthy" on two lines (one line would overflow the status column); error panel shows details |
| `queued` | clock | `--queued` (yellow) | Deploy deferred — autosync paused, change waiting; tinted row with amber left bar and a `paused: <global\|stack>` tag on the stack cell. See [Autosync](#autosync). |
| `blocked` | no-entry | `--queued` (yellow) | Deploy held back — a `depends_on` dependency failed this run; shares the amber pending treatment of `queued` (tinted row, amber left bar) with a `blocked by <dep>` tag on the stack cell. Like `queued` it is a pending row keyed by stack, not a notification. See [Deploy ordering](configuration.md#deploy-ordering). |
| `healed` | heal cross | `--success` (teal) | Self-heal restored a degraded stack with a corrective redeploy (not a git deploy — no diffs); tinted row with a teal left bar. Its files cell carries a **self-heal badge** that expands the [heal detail panel](#expandable-panels) (what drifted). See [Self-heal](#self-heal). |
| `heal_exhausted` | warning | `--danger` (red, **solid**) | Self-heal gave up after repeated redeploys did not restore the stack — a solid danger chip with a warning icon; the label stacks "self-heal" / "failed" on two lines (like `rolled_back_unhealthy`); tinted row with a red left bar; error panel shows details. See [Self-heal](#self-heal). |

### Commit links

Every commit SHA the UI prints — the roster's [Commit column](#stacks-view), the [deploy-history](#deploy-history) rows, the [diff panel](#expandable-panels)'s commit header and per-commit list, the [health timeline](#stack-health)'s phase chip, the [change-detection](#change-detection) lead's `Unchanged since <sha>`, a [peer's detail](#multi-host-federated-ui) panel — is an `<a class="sha-link">` to that commit on the forge, `<repo_web_url>/commit/<sha>` (the path shape Gitea and GitHub share). The href always carries the **full** SHA even where the cell prints the 7-char short form. It opens in a new tab (`target="_blank" rel="noopener noreferrer"`): the UI is a live SSE stream, so navigating away from it would cost the session. A click on the link is swallowed by the row handlers, so following a commit never also toggles that row's panel.

The base comes from the `stacks` state (`repo_web_url`), set explicitly via the `repo_web_url` config key or derived server-side from `repo_url` with any credentials stripped. A **peer's** SHAs link through that peer's own `repo_web_url` — each host tracks its own deploy repo — and a peer that reports none leaves them plain.

**Graceful degradation:** with no forge URL (a clone from a local path, or a peer running an older skipper) each SHA renders as the plain `<span>` it was before links existed — same class, same colour, no dead link. The link styling is affordance-only (`color: inherit`, no underline at rest; accent + underline on hover/focus), so a linked SHA does not shout over the status badges in the same dense row.

### Expandable panels

- **Files pill** — shown when `changed_files` is non-empty. Click inserts a full-width panel as a sibling element directly below the row (same pattern as the error detail panel). Clicking again removes the panel. The plain file list binds to its row (variant A, `data-status` + `bound`) exactly like the diff panel, so its status left bar stays continuous with the row above and the error detail below.
- **A row click never dead-ends.** Clicking a row body opens its diff/files panel when it has changed files, otherwise its [deploy-history](#deploy-history) panel — a row with nothing hashed (rare) still surfaces relevant info rather than doing nothing. A [peer row](#multi-host-federated-ui) (read-only mirror) instead opens a compact **peer-detail** panel (`data-testid="peer-detail"`) — the commit + changed-file count + status the fan-in carries, the peer's **containers (health) panel** rendered inline from the fanned-in `health`/`healthwatch` (`data-testid="health-panel"`, its per-service log button streaming the peer's container logs through the primary's proxy), **plus the peer's diff loaded inline** (`data-testid="peer-diff"`), fetched through the primary's proxy since the browser can't reach the peer cross-origin. (Opening the peer's own UI is the Hosts drawer's job, not repeated per row.)
- **Diff panel** — when `has_diffs` is `true` on the event, clicking the files pill fetches diffs from `GET /api/events/{id}/diffs` and renders a syntax-colored diff panel instead of the plain file list. Each file is a collapsible section (single-file diffs default to expanded). Diff lines are colored: additions (green), deletions (red), hunk headers (yellow), metadata (muted). Diffs (and commits, below) are cached client-side after the first fetch.
  - **Row binding (variant A)** — a diff panel opened from a deploy row binds to it exactly like the [health panel](#stack-health): the open row gains `diff-open` and the panel gains `bound` + `data-status`, so both share a **status-coloured left bar** (`inset 3px`) and tint (`--dc` from the deploy status: success→green, failed / rolled_back_unhealthy→red, rolled_back→rosé, queued→amber, deploying→accent). Closing the panel clears `diff-open`; an empty-diff fallback swaps in a **bound file list** that keeps the row open and shares the same bar. Opening the files/diff panel closes an open [health panel](#stack-health) on the same row and vice versa (one panel per row).
  - **Commit header** (`data-testid="diff-head"`) — above the file sections. When bound, an **echo line** repeats the stack name + `deploy diff` label + a status pill, so the panel names its own row even when scrolled away from it. Below that, when the diffs API returns `commits`, the **newest commit**'s subject (message first line), then a meta line of `author · relative time (full timestamp in title) · short SHA` (a [commit link](#commit-links)). For a **multi-commit range** the meta line instead shows `oldestSHA → newestSHA`, an `N commits` pill (`data-testid="commits-pill"`) toggling a collapsible per-commit list (`data-testid="diff-commit-list"`, collapsed by default), and the newest commit's author/time. A diff panel opened from a **log line** is unbound (no row to tie to) and shows the commit header without the echo line.
- **Heal detail** (`data-testid="heal-panel"`) — the healed-row counterpart of the files/diff panel, opened by the [self-heal badge](#self-heal) (`data-testid="heal-pill"`) or a tap anywhere on the row. A heal has no changed files, so instead of a file list the panel shows a one-line explanation (corrective `docker compose up -d`, no git change → no diff) and, when the `healed` event carries `heal_drift`, the **drifted services** it reacted to — each `name` with its degraded status chip. Bound to its row (variant A, teal `--dc`) so it shares the row's left bar; one panel per row (opening it closes an open health panel and vice versa). The drift is carried on the event's SSE payload, so there is no on-demand fetch.
- **Error detail** — shown for `failed`, `rolled_back`, `rolled_back_unhealthy`, and `heal_exhausted` events with an `error` field. Monospace, `pre-wrap`. **Row binding (variant A)** — like the diff/health panels, the error box binds to the row above it: it carries `data-status` and both share a **status-coloured left bar** (`inset 3px`) and tint (`--ec`: failed / rolled_back_unhealthy / heal_exhausted→red, rolled_back→rosé). Its top margin is negative and its top corners square off, and the row squares its bottom corners while the error box trails it (`.event-row:has(+ .error-detail)`), so the message reads as attached to its deploy row rather than a card floating between rows. When a files/diff panel is open on an errored row, that panel sits between the row and the error and squares its own bottom (`.bound:has(+ .error-detail)`), keeping the left bar unbroken across `row → panel → error`.

### Failed detail fetches

The lazy detail fetches — [deploy history](#deploy-history) (`/api/audit`), a row's or log line's [diff](#expandable-panels) (`/api/events/{id}/diffs`) and a [peer's diff](#multi-host-federated-ui) (`/api/peers/{name}/events/{id}/diffs`) — used to render their genuine-empty (or silent-fallback) result when the fetch itself failed, so a network drop looked identical to "nothing here" (T4.16). Instead a failed fetch (network error or `5xx`) now shows a shared **load-error line** (`data-testid="load-error"`): an amber-caution glyph, a plain message (`Couldn't load deploy history.` / `Couldn't load the diff.` / `Couldn't reach <host> for the diff.`), and a **Retry** (`data-testid="load-retry"`) that re-runs just that one fetch in place. It is deliberately **amber, not the red** of the deploy [error panel](#status-badges) (`error-detail`): the *deploy* is fine — only the panel fetch failed — so it reads as transient, not as a failed deployment. A genuine 404 (an evicted event / a peer that dropped the event) is **not** an error: it keeps its old behaviour (fall back to the file list, or drop the peer diff slot for the "open the peer" link). In the audit and peer contexts the host panel's own bar is flattened so the amber line carries the whole treatment; in the deploy-row diff context it stands in for the bound diff panel.

### Deploys filter

A **type-to-search** filter over the deploy rows by stack name, reusing the [Autosync drawer](#autosync)'s filter styling (magnifier + input + `×` clear). The bar is hidden until revealed, then **folds down** above the table (`data-testid="deploy-filter"`). It is revealed two ways: the **search trigger** — an always-visible quiet magnifier in the header, left of the [view switch](#layout) (`data-testid="stack-search-btn"`, T3.11) — opens/closes it with a click, and **type-to-search**: any printable key while the deploys view is active (with at least one row rendered) reveals it and seeds the first character (this is why the old single-key `i` icon-refresh hotkey was removed — see [Stack icons](#stack-icons)). The trigger carries `.active` + `aria-expanded` while the bar is open, is **view-aware** (opens the Deploys or Stacks filter for whichever view is active), and is hidden on the Logs view (which has its own in-panel search) and on mobile (where the popover entry below covers it).

- **Matching** — case-insensitive substring on the stack name. Non-matching rows are hidden (`filtered-out`), and any files/diff/error panel trailing a hidden row is hidden with it. A small `shown/total` count sits in the field; an all-hidden result shows a **"No stack matches …"** note (`data-testid="deploy-filter-empty"`).
- **Live rows** — the filter re-applies as new deploy events arrive, so a freshly deploying stack obeys the active query.
- **Dismissing** — `Esc` clears a non-empty field (first press), then folds the bar away (second press); clearing to empty and blurring also folds it away. The query is purely client-side — no request, no persistence.
- **Mobile** — touch has no keyboard to trigger type-to-search, so on mobile (≤ 700 px) the [Deploys view-options popover](#view-options-popover) carries a **"Search stacks"** entry (`data-testid="deploy-search"`, hidden on desktop): tapping it reveals the bar and focuses the field, raising the on-screen keyboard. The type-to-reveal behaviour is desktop-only.

---

## Stacks view

A third top-level view (`data-testid="stacks-view"`, [View toggle](#layout) `stacks`) that lists the **full set of stacks skipper owns** — the discovered set in [stack-discovery mode](../../docs/configuration.md#stack-discovery) (ADR-0034), the host `stacks:` list when discovery is off — each with its **last deploy outcome**. Where the [deploy table](#deploy-table) is an event log (only stacks with recent events, disabled stacks deliberately absent), this is **inventory**: every declared stack appears, including ones that have never deployed and ones parked with `disabled: true`. It is read-only; deploys are still git-driven. It reuses the deploy table's row/column/expand design wholesale ([`dev-docs/ui-design-concept.md`](../../dev-docs/ui-design-concept.md)); see also [`dev-docs/stack-roster-spec.md`](../../dev-docs/stack-roster-spec.md). Switching to it hides the deploy table, its disabled line, and the log pane (and vice versa).

- **Aligned table** — a fixed-grid column header (`.roster-list-header`, columns `Stack · Version · Status · Last deploy · Commit`) over the rows, exactly like the deploy table's header. **No** count or mode line above it. Empty state (`data-testid="roster-empty"`) when the set is empty.
- **Rows** (`data-testid="roster-row"`, one per stack, `data-stack` = name) — the deploy table's row frame **without** the status left bar: Stack (icon `/api/icons/<name>` + name) · **[version](#service-versions)** · **status** · last-deploy time · short [commit SHA](#commit-links), linked to that commit on the forge. Enabled stacks sort first (alphabetical), then disabled (alphabetical), so the list reads live-then-parked — except that a currently-**unhealthy** enabled stack floats to the very top (a stable sort keeps the alphabetical order within each group) and carries a `data-health` marker driving a `--danger` **severity bar + tint** (the same treatment a `failed` deploy row wears), so the Stacks view surfaces what needs attention first, the roster counterpart of the Deploys [attention band](#stack-health). The float is applied at full render (view switch / roster snapshot), not on every live health poll, so rows never jump out from under an open panel; the marker itself is kept in sync in place on every poll. **Status** reuses the deploy [status badge](#status-badges) (`data-testid="status-badge"`) for the last terminal outcome, with two synthetic flags (`.roster-flag`): **never deployed** (no audit record) and **disabled** (parked; muted `.disabled` row, no badge). Time/commit appear only for a real past deploy. A live `deploying` event refreshes just that row.
- **Expand → containers + history + change detection** — clicking a row stacks three bound panels below it as one accent card (see [design concept](../../dev-docs/ui-design-concept.md#expand--bound-panels)): the **containers** panel (`data-testid="health-panel"`, the stack's live [service health](#stack-health), shown only when health data exists), the per-stack **deploy-history** panel (`data-testid="audit-panel"`, from `GET /api/audit`), and finally the [**change-detection**](#change-detection) panel. The first two are the same panels the deploy table opens via its health pill / history button; here they carry the neutral accent bar. One stack open at a time.
- **Time mode** — the Stacks options popover carries the shared **Absolute time** toggle (`data-testid="roster-time-mode"`); default relative, hover reveals the absolute time (mouse).
- **Filter** — the same behaviour as the [Deploys filter](#deploys-filter): a bar (`data-testid="roster-filter-wrap"`) hidden until revealed by the header [search trigger](#deploys-filter) (`stack-search-btn`, view-aware), type-to-search (a printable key while on the Stacks view with rows), or, on mobile, the popover's **"Search stacks"** entry (`data-testid="roster-search"`). Case-insensitive substring on the name; `shown/total` count (`data-testid="roster-filter-count"`); non-matching rows and their trailing panels hidden (`filtered-out`); an all-hidden result shows a **"No stack matches …"** note (`data-testid="roster-filter-empty"`); `Esc` clears then folds.
- **Jump to Deploys** — every row's name carries the [cross-view jump button](#cross-view-stack-jump), landing on that stack's newest row back in the Deploys view.
- **App-link icon** ([ADR-0041](../../dev-docs/adr/0041-traefik-app-link-detection.md)) — when a stack's running containers carry a Traefik `Host()` rule (`traefik.enable=true` + a `traefik.http.routers.*.rule` label), a small link icon (`data-testid="app-link-btn"`) joins the row's button row after the [jump](#cross-view-stack-jump)/[logs](#container-logs) buttons. Exactly one discovered hostname renders it as a plain `<a target="_blank" rel="noopener">` — click opens the app directly. More than one renders it as a `<button>` that toggles a small popover (`data-testid="app-link-pop"`) listing each hostname as its own link; `Esc` or a click outside closes it. No hostname (no Traefik labels, never deployed, or `disabled: true`) renders nothing — never a disabled/ghost icon. Driven by the `app_links` SSE snapshot (below), patched into existing rows in place rather than a full re-render, so it never drops an open containers/history panel.

Driven by the [`stacks`](#event-lifecycle-sse) snapshot (initial value in `/api/v1/snapshot`, republished over SSE after every run), which carries the `roster` alongside the existing `disabled` list.

### Service versions

Inventory answers "what is running", so a roster row also shows **which version** — the image each container actually runs, from `compose ps` (`health.ServiceHealth.Image`, on the [`health`](#event-lifecycle-sse) snapshot). This is the *running* version, not the one the compose file declares: after a successful deploy the two agree, and while a change is pending or a deploy failed they do not — the row shows what is live.

- **On the row** — the **Version** cell (`data-testid="roster-version"`, the deploy table's own `.col-version`) holds **one** chip: the stack's lead service and its running version, plus a muted `+N` (`.ver-count`) for the services it stands for. One chip, not the Deploys column's full stack of them, so the roster keeps its one-line-per-stack rhythm: a deploy touches a service or two, while a stack *has* all of them — listing every service on every row would treble the height of an inventory. It is the same [version chip](#image-delta) in its **current-value** mode: one token, no arrow, and deliberately neutral (`.td-cur`) — the diff-add green there means "this is the new version", which a standing fact must not claim.
- **Lead service** — a stack is normally named after its main image, so the lead is the service whose own name or **image repository** mentions the stack name, shortest name first (`immich-server` beats `immich-machine-learning`); a stack of exactly one service needs no guess. When the name identifies none of them — a role-named stack like `monitoring` over prometheus/grafana/loki — naming one would be arbitrary, so the cell shows only the count (`3 services`) and defers to the panel. Resolved by the pure `rosterVersion()` helper.
- **In the panel** — each line of the [containers panel](#stack-health) carries its service's version between the name and its state (`data-testid="health-version"`), the panel's `.has-versions` class adding that column. The chip drops its service label there (the line already names the service), and the free width moves into the version column so every chip starts at the same x, right beside its service. On **mobile** the version moves to a second line under the service, since five cells do not fit a phone.
- **Degrading** — a snapshot without images (a stopped stack, or a peer running an older skipper) renders **no** version at all: an empty cell on the row and no version column in the panel, never an empty chip or an empty track. Parked (`disabled`) stacks are not polled and so never show one.
- **Freshness** — versions arrive with the `health` snapshot, not the roster one, so the cell is patched **in place** on every poll (`updateRosterHealth`) — it fills in on the first poll after the roster renders and updates after a deploy without dropping an open panel. Peer rows get versions the same way, since the [fan-in](#multi-host-federated-ui) forwards each peer's health verbatim.

There is **no** view-option to hide the column (unlike the Deploys [Version changes](#view-options-popover) toggle): a single-line cell costs the roster no height, so there is nothing to reclaim.

### Change detection

The last panel of a roster row's expand card (`data-testid="watched-panel"`) answers the question the rest of the UI leaves to `state.yaml` on the host: **"I pushed and this stack did nothing — why?"** A stack redeploys only when one of its hashed inputs changes (Invariant 2), so the panel names those inputs and, after a clean deploy, the commit they have not changed since.

- **Lead line** (`data-testid="watched-lead"`, from the pure `watchedSummary` helper) — `Unchanged since <short sha>. A deploy runs when any of these change:` after a `success`/`healed` outcome — falling back to *since the last deploy* when the audit record carries no commit, which is the case for a stack's very first deploy (there was no prior commit to diff against). A stack whose last outcome was `failed`/`rolled_back*`/`queued`/`blocked`/`heal_exhausted` has a change **pending**, so the "unchanged" claim is dropped and only the second sentence remains — saying nothing changed would be exactly backwards there. A stack that has never deployed has nothing recorded, so every input counts as changed; a stack parked with `disabled: true` reports that it is neither watched nor deployed, whatever its last deploy recorded.
- **File list** (`data-testid="watched-file"`) — the recorded input paths, sorted: the compose file, `env_files`, the global `vars_file`, `watch_dirs` contents and `build:` Dockerfiles. A path inside the repo clone renders **repo-relative** (the form the operator edits and commits); a host path — an env file, a vars file — stays absolute, since that is where it lives.
- **Config entry** (`data-testid="watched-config"`) — the stack's deploy-shaping config is hashed too, so editing it redeploys the stack. It is **not** a path: that hash is recorded under a synthetic `<stacks_base_dir>/skipper.yaml` key, and the file itself no longer exists (the config is host-side). It therefore renders as a **plain muted line** below the file list — *plus this stack's settings in the host `skipper.yml`* — rather than in the boxed, monospace frame a path gets, so nobody goes looking for a file that isn't there. (It started as a box distinguished only by a dashed border; rendered side by side that read as a second file, and the border style all but vanished in the light variant.) The backend keeps the two apart (`roster.Entry.WatchedConfig` vs `Watched`) rather than leaving the UI to recognise a path.
- **No fetch.** Unlike the deploy-history panel beside it, the data rides the `stacks` snapshot (`roster[].watched`), so there is no loading state and nothing to fail.

---

## Multi-host (federated UI)

When the config lists `peers:` ([ADR-0048](../../dev-docs/adr/0048-multi-host-federated-ui.md), [`dev-docs/multi-host-spec.md`](../../dev-docs/multi-host-spec.md)), the primary fans in each peer's read data and renders **one merged UI** — every host is a dimension on each row, never a page you navigate to. On a single-host instance (no `peers:`) none of the below appears and the UI is unchanged.

- **Hosts control** (`data-testid="hosts-btn"`) — a header button (server-rack glyph + `selected/total` badge + a warning dot when any peer is unreachable + chevron), joining the right-hand controls; lights the accent when a filter is narrowing the view. Shown only when peers are configured. Opens the **Hosts drawer** (`data-testid="hosts-drawer"`), which reuses the [autosync drawer](#autosync) `.drawer` shell and is a registered dismiss surface (Escape / click-outside, mutually exclusive with the other pop-outs). The drawer lists every host (`data-testid="host-row"`, `data-host` = name): a multi-select check, the host's colour chip + name (a `self` badge on the primary), a reachability dot (up / stale / down), and — for a peer — a link to open its own UI (`data-testid="host-link"`; `self` has none, and the link sits in a fixed-width slot so the dots align). A **"Select all"** entry (`data-testid="hosts-all-btn"`) clears the filter. The selection is persisted per browser (`localStorage` key `hostFilter`) and restores the first time the peers snapshot arrives, reconciled against the current host set (a saved host no longer present is dropped; an empty or now-complete intersection falls back to all hosts).
- **Per-host identity chip** (`.host-mono`) — a small colour-tinted **monogram** chip (`hostMonogram`: initials of a separated name, e.g. `host-a`→`HA`, else first three letters, `argoneon`→`ARG`) leading each merged-feed and roster row's stack cell, coloured by the host's palette slot (`assignHostColors`, six per-theme `--host-0…5` slots kept clear of the status hues; deterministic name-hash with collision-avoidance + an interleaved slot order so no two hosts share or near-share a colour until the six slots are exhausted). A **labelled chip, not a dot** — a dot already means deploy status, so the identity channel takes a different shape, separate from the status left-bar/badge. The chip doubles as a **quick filter**: clicking it isolates both views to that host, clicking any chip again clears back to all (a toggle complementing the drawer's multi-select). Chips stay visible in multi-host even at one host in view (so one is always there to clear the filter); while a filter is active they wear a steady host-colour halo. The chip shows its full hostname on hover (`title`) and tap (`data-taptip`).
- **Merged deploy feed** — local + peer deploy rows interleaved by timestamp. Peer rows are **read-only mirrors** (`.peer-row`): host chip, time, stack, status badge, duration, changed-file **count** — no *local* history/hooks fetch, but a click opens a compact **peer-detail** panel (`data-testid="peer-detail"`: commit + file count + status, the peer's **containers/health panel** rendered from its fanned-in `health`/`healthwatch` — its per-service log button proxied — and the **peer's diff loaded inline** via the primary's proxy — `GET /api/peers/{name}/events/{id}/diffs`), so a peer row is a glance that never dead-ends. Local rows are prepended live; peer rows are re-slotted by timestamp after they land.
- **Merged roster** (Stacks view) — local stacks first, then each peer's stacks appended per host, all host-tagged and obeying the same filter. Peer roster rows are read-only (host chip + icon + name + **app-link** from the peer's fanned-in `app_links`, status, last deploy, commit) and expand to the same read-only peer-detail with the peer's **containers/health** inline; only the peer rows (and their panels) rebuild on a peers refresh, so an open local panel is never collapsed.
- **Unreachable peer → flagged, not blanked** — a stale peer's last-known rows stay but dimmed (`.peer-stale`), and a banner (`data-testid="host-stale-banner"`) names them; other hosts stay live.

Driven by the [`peers`](#event-lifecycle-sse) snapshot; the effective host set is also served by `GET /api/peers`.

---

## Event lifecycle (SSE)

**Initial paint comes from `GET /api/v1/snapshot`** (ADR-0039), fetched on
every SSE (re)open — one JSON object of the current state snapshots keyed by
name (`stacks`, `health`, `autosync`, …), built from the same collector the
stream uses, so the two cannot drift. The `/api/events` stream itself no longer
replays an initial state burst: on connect it replays the deploy-event history
as `deploy` events, then emits a **`synced` marker** and streams live `deploy`
events and live state-snapshot changes. Fetching the snapshot on every open (not
just first load) is also how a reconnect resyncs state.

**Loading vs. empty (T4.17).** The deploy events are a *separate* channel from
the `/api/v1/snapshot` baseline, so until the history has replayed the UI cannot
tell "still connecting" from "no deploys". Rather than show the empty placeholder
in both cases, the table starts as a **loading skeleton** (`data-testid="loading-state"`
— a spinner + `Connecting to deployment stream…` over shimmer rows). It retires
the instant the picture is known: the first replayed `deploy` event shows the
table; if the history is empty, the `synced` marker reveals the genuine-empty
state (`data-testid="empty-state"`, `No deployments yet`). A failed snapshot does
**not** settle it — the skeleton stays until a live event or the next reconnect
resolves, so a transient outage never reads as "no deployments". The `synced`
marker is the deterministic seam this hangs on (no timers).

| Transition | Behaviour |
|---|---|
| `deploying` | New row prepended, tracked in memory. |
| `success` / `failed` / `rolled_back` / `rolled_back_unhealthy` (deploying row exists) | Existing row mutated in-place; error panel appended if needed. |
| `success` / `failed` / `rolled_back` / `rolled_back_unhealthy` (no existing row) | New row created directly. |
| `skipped` | Dropped — never rendered (an unchanged stack carries no signal). |
| `queued` / `blocked` | Pending row **keyed by stack**, sharing one lifecycle: a further `queued`/`blocked` for the same stack (another push while paused, or another reconcile tick while blocked) replaces it rather than stacking a duplicate. `queued` shows a `paused:` tag; `blocked` shows a `blocked by <dep>` tag. Removed when the stack next deploys (a `deploying` event supersedes it) or when it leaves the pending set in a `queue` snapshot — together with any panel open below it (files/diff, health, deploy history), so a drained row never strands a panel. Both carry `has_diffs`, so the row expands the held-back diff. The tag's reason comes from the `queue` snapshot, which is not ordered against the deploy event — the tag re-renders when a snapshot lands, so the reason always appears once known. It embeds a stack name (repo-controlled in stack-discovery mode), so it — like every stack name in the page — renders as literal text, never as markup. **The tag yields before the stack name does**: it ellipsises as the row narrows and is dropped entirely below 1000 px, where it had nothing useful left to say. The name is the row's identity and the badge already carries the state, so the reason is the cheaper thing to lose — it stays in full in the [autosync drawer](#autosync). |
| `healed` / `heal_exhausted` | New row created directly (self-heal is not preceded by a `deploying` event); `heal_exhausted` carries an error and expands an error panel. See [Self-heal](#self-heal). |

Besides `deploy` events, the stream carries named **state snapshots** — `autosync`, `queue`, `upcoming`, `hookrun`, `health`, `healthwatch`, `stacks`, `app_links`, and (when peers are configured) `peers` — each replacing the prior snapshot of that name. The initial value of each is served by `GET /api/v1/snapshot` (above); the stream then carries live changes to it.

- **`upcoming`** `{ "upcoming": ["grafana", "loki"] }` — the stacks that will deploy *after* the one currently deploying, in deploy order. The backend hashes every stack once upfront (after the git sync) to know which will actually deploy this run, then publishes the shrinking list as each stack starts; an empty list is published when the run ends. Drives the [Deploy indicator](#header--right) look-ahead trail and the [Run panel](#run-panel). Distinct from the autosync pending `queue` (deferred, paused stacks). `_nixos` is excluded — the rebuild has no per-stack deploying state.
- **`health`** `{ "stacks": { "gitea": { "status": "healthy", "services": [{ "name": "gitea", "state": "running", "health": "healthy" }] }, … } }` — the current runtime health of skipper-cd's own stacks, driving the [Stack health](#stack-health) pill. The backend polls `docker compose ps` for each stack (only while the UI is on **and** a client is subscribed), rolls each stack up to `healthy`/`unhealthy`/`starting`/`stopped`/`unknown`, and republishes the snapshot over SSE on its interval and after each deploy run (its initial value comes from `/api/v1/snapshot`). `_nixos` carries no health (it is not a compose project). See [ADR-0027](../../dev-docs/adr/0027-live-stack-health-in-ui.md).
- **`stacks`** `{ "disabled": ["experiments"], "repo_web_url": "https://forge.example.com/owner/repo", "roster": [ { "name": "traefik", "disabled": false, "last_status": "success", "last_at": "…", "last_commit": "a1b2c3d", "hooks": { "pre_deploy": ["docker exec traefik-restic backup"], "post_deploy": [] } }, … ] }` — stack-set facts that are not deploy events. `disabled` lists the names parked via `disabled: true` in stack-discovery mode (ADR-0034), driving the [Disabled stacks](#disabled-stacks) line (empty/null when stacks are listed explicitly). `roster` is the full inventory driving the [Stacks view](#stacks-view) — every declared stack with its last outcome (`last_status` empty = never deployed; disabled entries carry no outcome). A roster entry carries a `hooks` object with the stack's configured `pre_deploy` / `post_deploy` command lines (omitted when the stack declares none), driving the [hooks badge + panel](#deploy-hooks) with no extra fetch. `repo_web_url` is the deploy repo's forge browse URL every [commit link](#commit-links) is built from — omitted when the host could derive none. Initial value in `/api/v1/snapshot`, republished over SSE after every deploy run.
- **`hookrun`** `{ "stack": "paperless", "phase": "pre_deploy", "index": 1, "total": 2 }` — the hook a deploy is currently executing (ADR-0038), driving the [running-hook](#deploy-hooks) phase sub-label on the deploying row. Published by `runHooks` as each hook starts and cleared (empty payload) when the phase's hooks finish; only emitted with a UI sink, so headless deploys never compute it. Distinct from `upcoming` (which stacks come next) — this is the sub-step *within* the deploying stack.
- **`orphans`** `{ "orphans": [{ "project": "old", "class": "orphaned", "working_dir": "…", "config_file": "…/docker-compose.yml", "volumes": ["old_data"], "containers": [{ "name": "old-app-1", "service": "app", "image": "nginx:1.25", "state": "running", "status": "Up 3 days", "ports": "0.0.0.0:80->80/tcp" }], "prunable": true }] }` — compose projects the discovered stack set no longer accounts for (ADR-0036), driving the [Orphans](#orphans) section. Initial value in `/api/v1/snapshot`, republished over SSE on each health-poll tick while a UI client is subscribed; empty when none are found.
- **`app_links`** `{ "stacks": { "media": ["media.example.com"], "auth": ["auth.example.com", "sso.example.com"] } }` — each stack's Traefik-routed hostname(s), discovered live from running containers' labels (ADR-0041), driving the [App-link icon](#stacks-view). A stack with none discovered is simply absent from the map. Initial value in `/api/v1/snapshot`, republished over SSE on each health-poll tick while a UI client is subscribed, riding the same cadence as `health`/`orphans`.
- **`peers`** `{ "self": "host-a", "peers": [ { "name": "host-b", "url": "http://host-b:8001", "reachable": true, "stale": false, "last_seen": "…", "state": { "stacks": {…}, "health": {…}, "healthwatch": {…}, "app_links": {…} }, "deploys": [ /* audit records */ ] }, … ] }` — the merged multi-host read model (ADR-0048), present only when `peers:` is configured. `self` is the primary's own label; each peer carries its reachability + last-known curated state and recent deploys, tagged by host. Drives the [Multi-host](#multi-host-federated-ui) merged views. Republished on the health-poll cadence by the `peers-fanin` loop; the lean effective host set is also at `GET /api/peers`.
- **`healthwatch`** `{ "stacks": { "vaultwarden": { "vaultwarden": [{ "status": "unhealthy", "since": "2026-07-16T15:47:05Z", "commit": "a1b2c3d…", "deploy_correlated": true }, …] } } }` — the health watchdog's per-service status history (≤ 10 accepted phases per service, newest first), driving the [Status history](#stack-health) in the per-service panel. Only present when `health_watch` is configured; initial value in `/api/v1/snapshot`, republished over SSE on every accepted change. `deploy_correlated` is derived by the backend from the attribution window — the UI never computes it. See [ADR-0031](../../dev-docs/adr/0031-notify-on-own-stack-health-change.md).

---

## Autosync

Controls whether detected changes deploy automatically, per stack and globally. Paused stacks queue their changes and deploy them when sync resumes. Behaviour, semantics and the API contract are specified in [`docs/autosync.md`](../../dev-docs/autosync-spec.md); this section covers only the UI surface.

**Header control** — the [Autosync control](#header--right) shows global state and, when deploys are waiting, an amber **pending count** pill (hidden at zero). It is the drawer opener and the "how many are queued" indicator. It reflects server state (the `autosync`/`queue` SSE events), never `localStorage`.

**Autosync drawer** — an on-demand panel anchored under the header, opened by the control. Default hidden; closes on outside-click or `Esc`. Updated in real time from the `autosync` and `queue` SSE events, and painted from `GET /api/autosync` + `GET /api/queue` when opened. Contents, top to bottom:

- **Global autosync** — a switch (the header control mirrors its state). Toggling posts `POST /api/autosync {scope:"global", enabled}`.
- **Queued (N) · drains in this order** — the pending stacks in **deploy order** (`_nixos` first, then `skipper.yml` order), each row: position number, stack name, a `reason` chip (`global` / `stack`), changed-file count, and how long it has waited. Empty/hidden when nothing is queued.
- **All stacks** — one switch per managed stack with its current state, preceded by a **filter field** (case-insensitive substring match on stack name; a clear button appears when non-empty; `Esc` clears the field first, then closes the drawer). A "No stack matches …" state shows when the filter excludes everything. Toggling posts `POST /api/autosync {scope:"stack", stack, enabled}`. The switch reflects `effective`, so it stays correct across a toggle **while a filter is applied** (the toggle re-renders the list but preserves the query and the matched subset).

**Per-stack switch is an exception, not a pin.** A per-stack UI override is held only while it differs from what the stack would inherit; toggling a stack back to its inherited value clears the override (the "return to global" gesture) and toggling the global switch collapses any per-stack override that now matches the baseline. So the global switch behaves as a true master and a UI pause does not survive a global off→on cycle. Full semantics: [`docs/autosync.md`](../../dev-docs/autosync-spec.md#override-collapse) / [ADR-0019](../../dev-docs/adr/0019-autosync-ui-overrides-collapse-to-inherit.md).

**Enable triggers a drain; disable does not.** Enabling sync (global or a stack) triggers a deploy run that drains the queue; disabling only updates state. Switches use the same track/thumb geometry as the header `.filter-toggle`.

---

## Run panel

A read-only panel listing the **current deploy run** in deploy order, opened by clicking the [Deploy indicator](#header--right) while a run is active (closed at rest, since there is nothing to show). Styled like the [Autosync drawer](#autosync) — same glass shell, anchored under the header, closes on outside-click or `Esc`, and mutually exclusive with the Autosync drawer and the view-options popover. Painted from the in-memory deploying rows plus the `upcoming` snapshot; no dedicated endpoint.

- **Header** — title `This deploy run`; sub-line `<b>stack</b> deploying · N more this run` (or `· last in this run`, or `Nothing deploying.`).
- **Rows** — the active deploy first, a pulsing **ship badge** and `deploying now` (accent-tinted row); then the upcoming stacks, each a **position number** badge and `next` / `then`. Accent-tinted (active run) rather than the queue's amber. No switches — unlike the autosync drawer, this panel does not act on anything.

---

## Autosync & queue API

`GET /api/autosync`, `POST /api/autosync`, `GET /api/queue`, and the `autosync` / `queue` SSE events on `/api/events` are specified in [`docs/autosync.md`](../../dev-docs/autosync-spec.md). The `POST` shares the trust level of the other endpoints (unauthenticated at the process; edge auth in front).

---

## Log view

Styled as one big [container-log panel](#container-logs) (`clog-panel`) — the same header/search/body/footer chrome as the `docker compose logs` popup in the Deploys/Stacks views, just page-sized — rather than a plain pane with its controls tucked behind the view buttons. Shows all skipper-cd log output, **newest-first** (newest line at the top; there is no sort toggle — auto-scroll keeps the newest line in view). Each line: muted timestamp, level badge, optional stack prefix, message, then dim `key=value` attrs.

The panel header (`clog-head`) carries a **live/pause** pill (`clog-live`; pause freezes the pane without dropping anything — lines keep landing in the client-side buffer below and a click on live re-renders the window to catch up in one go), then **search**, **wrap**, **auto-scroll** (`follow-logs`, on by default) and **fullscreen** tools. Unlike the container-log panel there is no backlog-size (`clog-tail`) selector — `/api/logs` always replays its own backlog, the same reason the hook-log skipper mode hides it (see [Container logs](#container-logs)). These controls are **not** in the view-options popover — the Logs view button carries no popover at all now (`view-options` has no `logs` `vo-group`), since every control already lives in the panel header. Search reveals the same filter bar the deploys/stacks views use (seeded by type-to-search on desktop) — non-matching lines are hidden, matches highlighted, and a hit count shown; clicking the search tool again closes it and clears the query. Wrap soft-wraps long lines. Fullscreen fills the viewport below the sticky header (so the header stays reachable to toggle it back off; Esc also exits).

Timestamps show the time of day; lines from another day get a date prefix, and the full `toLocaleString()` timestamp is always in the tooltip.

Level colours: `ERROR` → red, `WARN` → yellow, `DEBUG` → muted, `INFO` → secondary text. Lines with a `stack` attr (the deploy lifecycle: `deploying stack`, `deploy complete`, failures) render it as an accent-coloured `[gitea]`-style prefix and omit it from the trailing attrs, so what was deployed when is scannable. Child-process lines (attrs contain `cmd` and `stream`) render a muted `[docker]`-style command prefix instead of a level badge and the message in primary text; child output carries no stack attribution (known limitation — the runner does not know which stack it runs for). **Exception: deploy-hook output** — `runHooks` knows the stack, so hook child-lines additionally carry `stack` + `hook` attrs and render with the `[stack]` prefix + a hook marker, making a running hook's output filterable by stack (see [Deploy hooks](#deploy-hooks)).

`deploy complete` lines carry the deploy event's ID as an `event_id` attr (logged only when an event sink is configured, i.e. `ui_enabled`). The log view renders it as a **diff pill** instead of a plain attr: clicking fetches the deploy's diff from `GET /api/events/{id}/diffs` and inserts the same collapsible diff panel used by the deploy table directly below the line (click again to close; a notice appears when no diff was recorded, e.g. the event fell out of the bounded history).

Received lines are held in a bounded client-side buffer (2000 entries, oldest dropped on overflow) and the pane renders a sliding window of it. The window starts at **500 lines** (the newest 500) and grows by **500** each time the user scrolls to the older (bottom) edge; a scroll that reveals older lines preserves the reading position. Live lines are added incrementally at the newest edge, and the rendered window is trimmed back to its size from the older edge — trimmed entries stay in the buffer, so scrolling back reveals them again. An active in-log search re-applies to freshly rendered lines. The `EventSource` for `/api/logs` is created lazily on first activation of the view and kept open afterwards; while the view is hidden, lines are buffered and the pane is rebuilt from the buffer on re-activation. It recovers from a fatal stream error (a non-2xx response or bad content-type, which closes `EventSource` for good) via the same capped-backoff retry the connection indicator uses — necessary because the log stream has no indicator of its own, so a silent stop would otherwise leave the pane frozen with no on-screen cue.

---

## Container logs

A **logs icon** (`clog-btn`) opens a live `docker compose logs` panel for a stack, filterable to a subset of its services ([ADR-0037](../../dev-docs/adr/0037-container-logs-in-ui.md)). It appears in both views:

- **Deploys view** — in the newest row's [⋯ overflow menu](#row-overflow-menu) (per stack, services merged) and on each service line of the [health panel](#stack-health) (per container, directly on the line).
- **Stacks view** — inline in the roster row's stack cell (per stack) and, when the row is expanded, on each container line of its health panel (per container).

Clicking it opens a panel (`clog-panel`) that trails the row/line it was opened from and streams from `/api/container-logs`: an initial backlog then a live follow. **Only one log is open at a time** — opening another closes the previous one (and stops its stream), so a viewer holds at most one follow stream. The same panel component has a second **"skipper" mode** ([Deploy hooks](#deploy-hooks), ADR-0038): opened from a running hook's logs icon it streams `/api/logs` filtered to the stack instead of `docker compose logs`, reusing the same header controls and single-log rule.

The panel header carries: a **live/pause** pill (`clog-live`; pause freezes the stream), **auto-scroll** (follow the tail), **line-wrap**, **in-log search**, a **per-service filter** (see below), a **backlog selector** (`clog-tail`: 50/200/1000, default 200), and **fullscreen** (`clog-fullscreen`; the panel reparents to `<body>` to overlay above the sticky header, restoring its place on exit; Esc exits). The body (`clog-body`) renders each line with a dimmed timestamp; the whole-stack and multi-service views prefix each line with its compose service, tinting `warn`/`error` lines. While a log is open, **typing routes into its search** (overriding the deploys/stacks type-to-search): matching lines highlight, the rest hide, and a hit count shows.

**Stream status.** The panel footer (`clog-stat`) reads `live · streaming` while the stream is up and `paused` while frozen. On an error it distinguishes what `EventSource` will actually do next (`clogStreamStatus`): a dropped connection the browser retries by itself shows `reconnecting…`, while a stream the server *closed* — a non-2xx response, i.e. a `404` for a stack that went away or a `429` when the server already runs its maximum number of log follows — shows `stream closed — reopen the log to retry`. `EventSource` never retries a non-2xx, so reporting `reconnecting…` there would promise a retry that never comes.

A closed stream also **settles the live/pause pill** (`.clog-live.dead`, label `closed`, `--danger` dot): the pill is the panel's other status surface, and leaving it green would state two contradictory things at once. It goes inert with it — without that, toggling it would put `live · streaming` back on a stream that is gone.

**Per-service filter.** The filter tool (funnel glyph) toggles a collapsible chip row (`clog-svcs`) under the head: an **all** chip plus one per service. Chips are multi-select toggles — tap to add a service, tap again to drop it, **all** clears back to the merged stream. The header scope reflects the selection (`stack · all services` → `stack / api` → `stack / api + db` → `stack / N services`). Selecting exactly one service drops the compose prefix (the scope is unambiguous); zero or several keep it so each line stays labelled. The selection rides the stream as a comma-separated `?services=` list; changing it re-pulls the backlog. Suppressed entirely for a stack with fewer than two services (nothing to filter) and in hook (skipper) mode.

---

## Deploy hooks

A stack can declare `pre_deploy` / `post_deploy` shell hooks that run around its deploy ([ADR-0038](../../dev-docs/adr/0038-pre-post-deploy-hooks.md), full spec [`dev-docs/deploy-hooks-spec.md`](../../dev-docs/deploy-hooks-spec.md)). The UI makes them **visible** — that a stack has hooks, and that one is running now, with its output reachable. Read-only: hooks are config-driven, never triggered from the UI.

### Hooks badge + panel (defined)

A stack that declares any hook carries a small **hooks badge** (`data-testid="hooks-badge"`) — inside the [⋯ overflow menu](#row-overflow-menu) on the **newest deploy row** per stack (Deploys view), and **inline** in the stack cell on the **roster row** (Stacks view). Absent when the stack has no hooks. Its glyph is a **fishing hook**, deliberately distinct from the container-logs icon it sits beside. It shows the split **`pre+post` count** (e.g. `2+1`), not the sum, so the shape of the hooks reads at a glance.

Its **tooltip is two lines** — `pre-deploy hook: N` / `post-deploy hook: N` (a `\n` in the `title`; the tap-tip bubble renders it via `white-space: pre-line`). Because the UI is glyph-only, the badge also flashes the shared **tap-tip bubble** (`.tap-tip`) on a touch `pointerdown`, so a touch user (no native tooltip) still sees what it is.

The badge is a real `<button>` (keyboard-operable like the health pill). Activating it opens a bound **hooks panel** (`data-testid="hooks-panel"`) below the row, listing the configured commands: a `pre_deploy` group then a `post_deploy` group, each command a monospace line (`data-testid="hooks-cmd"`, prefixed with a `$`) shown verbatim (a repo-controlled string → rendered as literal text, never markup). It **binds to its row (variant A)** with a neutral **accent** left bar (the panel is config, not a status), and joins the **one-panel-per-row** exclusivity — opening it closes an open health / files-diff / audit / heal panel on the row and vice versa.

Both the badge and the command text come from the `hooks` field on the [`stacks`](#event-lifecycle-sse) snapshot's roster entry (`{pre_deploy: [...], post_deploy: [...]}`, absent when none) — so opening the panel needs **no fetch and no endpoint**. Hook command lines are tiny and static, unlike the lazy-fetched audit/container-logs, so inlining them is the simpler pick.

### Running hook (live)

While a deploy executes a hook, the stack's row shows the active phase **identically in the Deploys view and the Stacks roster**: the [status badge](#status-badges) gains a phase sub-label (`data-testid="hook-phase"`) reading `pre_deploy hook 1/2` / `post_deploy hook`, and the row's `hooks-badge` gains an **active/pulsing** state (`data-hook-active`) so the badge that says "this stack has hooks" also says "one is running now". On the Deploys view the badge is collapsed inside the [⋯ overflow menu](#row-overflow-menu), so the pulse rides the **`⋯` button** (`more-btn`) — the visible cue stays on screen while the menu is closed; on the roster the badge is inline, so the pulse rides the badge itself. The status cell stacks the badge over the phase (the roster status cell does too, so both views match). The phase carries the **logs icon** that opens the hook log (below).

Driven by the [`hookrun`](#event-lifecycle-sse) SSE snapshot `{stack, phase, index, total}` — published as each hook starts and cleared to the zero value when the phase's hooks finish. Painted on both the deploy table and the roster (the roster re-renders wipe the status cell, so the phase is re-applied after each roster render). The plain `deploying` badge covers pull/up/probe; this refines it **during hook phases only**. When no hook is running the sub-label is absent and the row reads exactly as a normal deploying row.

### Hook log

Hook stdout/stderr already streams to `/api/logs` via the log pipeline (ADR-0013). For hooks these child-process lines carry a **`stack`** attr (threaded through the command context — `runHooks` knows the stack), so the [log view](#log-view) prefixes them `[stack]` and the stack filter matches them, unlike unattributed `docker`/`git` child output.

The running-hook phase carries a **logs icon** — the *same* `clog-btn` glyph the [container logs](#container-logs) feature uses (`data-testid="clog-btn"`, class `hook-log-btn`). Clicking it opens the container-logs panel **in a second "skipper" mode**, inline on the same page trailing the row: instead of `docker compose logs` it streams `/api/logs`, keeping only this stack's attributed lines (the hook output + the stack's deploy lifecycle), rendered via the shared log-line renderer. It reuses the panel's live/pause/wrap/search/fullscreen and the **one-log-open-at-a-time** rule; the tail selector is hidden (`/api/logs` replays its own backlog). Because it is a `.clog-btn`, both the deploy-table and the roster click handlers intercept `.hook-log-btn` **before** their container-logs handler, so it never opens compose logs by mistake. This icon shows **only while a hook runs** (on the `hook-phase`), temporally distinct from the persistent per-row container-logs `clog-btn` in the stack cell.

---

## Log API

`GET /api/logs` — SSE stream of captured log lines, event name `log`, payload:

```json
{"id":1720012345001,"time":"2026-07-10T12:00:00Z","level":"INFO","msg":"deploying stack","attrs":{"stack":"gitea"}}
```

On connect the in-memory backlog (bounded ring, 1000 entries, no persistence across restarts) is replayed — filtered by `Last-Event-ID` on reconnect — then live entries stream in. Entry IDs are seeded from the process start time so they stay monotonic across restarts. Slow consumers have lines dropped rather than blocking the logger. Same trust level as `/api/events` (unauthenticated); child-process output (`docker compose`, `git`, `nixos-rebuild`) is included — see ADR-0013.

---

## Container logs API

`GET /api/container-logs/{stack}` — SSE stream where each `data:` frame is one `docker compose logs` line: the backlog (`?tail=N`, default 200, clamped `[1,1000]`) then a live follow. With no `?services=` it streams the whole stack (services merged); a comma-separated `?services=a,b` list narrows it to that subset (exactly one drops the compose prefix, several keep it). `?since=<RFC3339>` resumes after a reconnect (server passes `--since`, skipping the backlog). UI-only (present only with `ui_enabled: true`). `{stack}` must be in the current stack set and every selected service must appear in the stack's health snapshot, else `404`. At most 16 follows run at once across all clients (each holds a `docker compose logs --follow` child, and the endpoint is unauthenticated like the rest of the read surface); beyond that a request is refused with `429` + `Retry-After` rather than spawning another child, and the slot is released on disconnect. The compose invocation reuses the deploy path's project selection so a read targets exactly the deployed project ([ADR-0037](../../dev-docs/adr/0037-container-logs-in-ui.md)); a client disconnect cancels the request context and kills the `logs --follow` child. Same trust level as `/api/logs`.

`GET /api/peers/{name}/container-logs/{stack}` — the peer proxy for the above (ADR-0048): the primary forwards a configured peer's container-logs SSE stream to the browser, which can't reach the peer cross-origin. The `?services=`/`?tail`/`?since` query passes through to the peer, which validates the stack/services (a `404` there is forwarded); `{name}` not a configured peer is `404` from the primary. Frames are forwarded with a flush; a client disconnect cancels the request context, tearing down the upstream stream (and the peer's `logs --follow` child). Present only when `peers:` is configured.

---

## Diff API

`GET /api/events/{id}/diffs` — returns `{"diffs": {"filepath": "diff content", ...}, "commits": [{"sha","subject","author","date"}, ...]}` or `{"diffs": null, "commits": null}`. `commits` are the git commits in the range `LastDeployedCommit..HEAD` that touched the event's changed files, newest first (capped at 50). Returns 404 for unknown event IDs.

## Audit API

`GET /api/audit` — the durable per-stack deploy audit log ([ADR-0033](../../dev-docs/adr/0033-durable-per-stack-deploy-audit-log.md)) as a JSON array, newest first. Each record: `{"stack","timestamp","status","duration_ms","commit_sha","changed_files","error"}` (`changed_files` is a **count**, not a list; `commit_sha` and `error` may be absent). Query params: `stack=<name>` returns that one stack's history (omit for recent records across all stacks); `limit=<n>` caps the count. An empty history returns `[]`. Only terminal outcomes are recorded (`success`, `failed`, `rolled_back`, `rolled_back_unhealthy`, `healed`, `heal_exhausted`); records are retained per stack (default 200 each). Drives the [Deploy history](#deploy-history) panel.

## Version API

`GET /api/version` — returns the build identity `{"version": "<semver>|dev", "branch": "<name>", "commit": "<short-sha>"}`. Fields are injected at build time via `-ldflags "-X main.version=… -X main.commit=… -X main.branch=…"`:

- `version` — semver from `.release-please-manifest.json` (`dev` for local builds without ldflags).
- `commit` — short git SHA. Injected by the Nix flake (`self.shortRev`) and by Docker/CI; for a local `go build` it is recovered from the Go build info (`-dirty` suffix for an uncommitted tree). May be empty.
- `branch` — git branch name. Only CI/Docker builds know it; the Nix flake and plain local builds leave it empty.

The header label is painted once on load: a **feature-branch** build (branch set and ≠ `main`) shows `branch · commit`; otherwise it shows `v<version> · commit` (or `dev · commit`). The same `version-commit` string is baked into the service worker's cache name (`/sw.js`) so two feature-branch builds that share a release semver still bust the app-shell cache.

Diffs and commit metadata are stored in `deploy-history.yaml` alongside events but are **not** included in SSE payloads (only `has_diffs: true` is sent). This keeps the real-time stream lightweight. Large diffs are truncated at 10 KB per file and 50 KB total per event.

---

## Progressive Web App (PWA)

The UI is an **installable PWA** (full spec: [`docs/pwa.md`](../../dev-docs/pwa-spec.md); decisions: [ADR-0018](../../dev-docs/adr/0018-pwa-installable-ui.md), [ADR-0023](../../dev-docs/adr/0023-pwa-update-prompt.md)). It is an enhancement layer — the page behaves exactly as before in a normal browser tab.

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
| `view-toggle` | Deploys/Stacks/Logs segmented icon control | Active button (`.active`) opens the view-options popover on Deploys/Stacks; Logs has none — its controls live in its own panel header |
| `stack-search-btn` | Always-visible search trigger (magnifier), left of the view switch | Opens the active view's stack filter (T3.11); `.active` + `aria-expanded` while open; hidden on Logs + mobile |
| `deploy-indicator` | Deploy indicator (anchor/ship glyph) | `idle`/`deploying <stacks> · next <stacks>` in `title` + `aria-label`; `role="button"`, opens the run panel while active |
| `deploy-next` | Look-ahead trail beside the active stack | Empty when nothing follows; hidden ≤ 700 px |
| `deploy-count` | Mobile `+N` count chip (upcoming) | Shown only ≤ 700 px; empty when nothing follows |
| `run-drawer` | The run panel (this run's stacks) | `.open` when shown |
| `autosync-btn` | Header autosync control (drawer opener) | `data-global` = `true`/`false` (global autosync state) |
| `pending-pill` | Amber pending-count pill | Hidden at zero |
| `view-options` | View-options popover (opened from the active view button) | `.open` when shown; holds each view's controls (`time-mode`; the mobile `*-search` entries); no `logs` group — see [Log view](#log-view) |
| `time-mode` | Deploys/Stacks time-mode toggle | Inside `view-options`; hidden until the popover opens; `.active` when on |
| `theme-toggle` | Header theme (dark/light) toggle | Glyph-only; moon in dark, sun in light |
| `theme-select` | Header theme picker (`<select>`) | Present only when `ui_theme_switcher` is enabled; transparent over a palette glyph. See [Theme override](#theme-override) |
| `theme-notice` | Theme mismatch notice | Shown when `themeOverride` differs from `data-server-theme` |
| `theme-notice-close` | Theme mismatch notice's dismiss button | |
| `conn-indicator` | Connection indicator (chain-link glyph) | `data-state` = `connecting`/`connected`/`reconnecting` |
| `loading-state` | Loading skeleton for the deploy table | Spinner + shimmer rows shown until the first snapshot settles; retires on the first `deploy` event or the `synced` marker (T4.17) |
| `empty-state` | Genuine-empty placeholder (`No deployments yet`) | Shown only once the `synced` marker confirms an empty history (T4.17) |
| `load-error` | Amber "couldn't load" line for a failed detail fetch | Audit / diff / peer-diff; carries a `load-retry` button (T4.16) |
| `load-retry` | Retry button inside a `load-error` | Re-runs just that fetch in place |
| `a11y-announce` | Off-screen polite live region | Announces terminal deploy outcomes to screen readers (T2.8); `sr-only`, `role="status"` |
| `deploys-table` | The deploys view container (header + rows) | Snapshot anchor (UA1) |
| `deploy-row` | A deploy table row | `data-stack`, `data-status` |
| `status-badge` | Status badge inside a row | |
| `stack-icon` | Icon chip in the stack cell | |
| `health-pill` | Stack health pill (a `<button>`) in the status cell | Newest row per stack only; keyboard-operable; `data-health` = `healthy`/`unhealthy`/`starting`/`stopped`/`unknown`; opens `health-panel` |
| `health-beacon` | Header chip counting unhealthy stacks | Every view; `hidden` when none unhealthy; `<button>` with pulsing dot + `health-beacon-count`; pluralised `title`/`aria-label`; opens `health-beacon-pop` |
| `health-beacon-pop` | Beacon popover listing unhealthy stacks | Each entry is a `health-beacon-item` (`data-stack`); activating jumps to the stack in the current list view (roster row in Stacks, else newest deploy row); Escape / outside-click close |
| `attention-band` | `--danger` card above the deploy log | Inside `#deploy-table` (hides with the view); `hidden` when none unhealthy; one `attention-row` per unhealthy stack |
| `attention-row` | An unhealthy stack row inside `attention-band` | `data-stack`; icon + name + health pill; click jumps to the stack's newest deploy row |
| `health-panel` | Per-service health breakdown panel below the row | Sibling of the row, like `files-panel`; leads with a stack + status header; carries `data-health` (drives the shared left bar/tint); the open row gets `health-open` + `data-health` |
| `health-service` | A service row inside `health-panel` | `data-health` per service |
| `health-version` | The service's running version inside a `health-service` line | Only when the health snapshot carries container images; holds the current-mode version chip (no service label — the line names it) |
| `more-btn` | `⋯` overflow-menu trigger in the stack cell (T3.13) | Newest deploy row per stack only (**not** the roster — its secondary actions are inline); collapses the row's secondary actions (history + container logs + deploy hooks); `<button>` with `aria-expanded`; `data-hook-active` pulses it while a hook runs; opens `more-pop` |
| `more-pop` | The overflow menu popover | Holds the relocated history/clog/hooks buttons as labelled rows; outside-click / `Esc` dismiss; flips `.align-right` near the viewport edge; picking an action closes it |
| `history-btn` | Deploy-history button (inside the `⋯` menu) | Newest row per stack only; lives in `more-pop`; opens `audit-panel` |
| `jump-btn` | Cross-view jump button in the stack cell | On every row, both views (directly on the row, not in the `⋯` menu); `data-jump-view`/`data-jump-stack` name the target; switches view and flashes `.jump-target` on the landing row |
| `audit-panel` | Per-stack deploy-history panel below the row | Sibling of the row; fetched from `/api/audit`; opens exclusively with health/diff panels; the open row gets `audit-open` |
| `watched-panel` | [Change-detection](#change-detection) panel — last card of a roster row's expand stack | `data-watched-for` = stack; from the `stacks` snapshot, no fetch |
| `watched-lead` | Its lead line — what is watched, and since which commit | Phrasing from the pure `watchedSummary` helper |
| `watched-file` | One watched input path | Repo-relative inside the clone, absolute for a host path |
| `watched-config` | The hashed stack config entry | Not a path — the synthetic config-hash key, rendered as prose |
| `audit-row` | A past-deploy row inside `audit-panel` | `data-status` = the terminal deploy status |
| `time-cell`, `duration-cell` | Time / duration cells | Masked in snapshots (dynamic) |
| `files-pill` | Files pill on a row | |
| `files-panel` | Expanded plain file-list panel | |
| `diff-panel` | Expanded diff panel | |
| `error-panel` | Error detail panel under a failed row | |
| `deploy-search` | "Search stacks" row in the deploys view-options popover | Mobile-only entry point; reveals + focuses `deploy-filter` |
| `deploy-filter-wrap` | The filter bar container | Collapsed (height 0) until revealed; the reveal-state hook |
| `deploy-filter` | Deploys type-to-search input | Hidden until revealed by `stack-search-btn`, typing (desktop), or `deploy-search` (mobile); folds down above the table |
| `deploy-filter-clear` | Deploys filter clear (`×`) button | Shown only when the field is non-empty |
| `deploy-filter-empty` | "No stack matches …" note | Shown when the query hides every row |
| `disabled-stacks` | Disabled-stacks chip line below the table | `.shown` when non-empty; hidden when stacks are listed explicitly and outside the deploys view |
| `orphans` | Orphans section below the disabled line | `.shown` when non-empty; `.open` toggles the body; hidden when empty and outside the deploys view |
| `stacks-view` | Stacks roster view container | Shown only when `activeView === 'stacks'` |
| `roster-row` | One stack's inventory row | `data-stack` = name; `.disabled` when parked; `.audit-open` when expanded |
| `roster-version` | A stack's Version cell in the roster row | Holds the lead service's running-version chip + a `+N` count, or a bare service count when no lead resolves; empty when no health data carries images |
| `roster-empty` | "No stacks to show" note | Shown when the set is empty |
| `roster-filter-wrap` / `roster-filter` | Stacks filter bar / input | Same reveal + type-to-search behaviour as `deploy-filter` |
| `roster-filter-count` / `roster-filter-clear` / `roster-filter-empty` | Stacks filter `shown/total` / clear / no-match note | Mirror the deploys-filter counterparts |
| `roster-search` | "Search stacks" row in the stacks view-options popover | Mobile-only entry point; reveals + focuses `roster-filter` |
| `roster-time-mode` | "Absolute time" toggle in the stacks popover | Shared `timeMode` with the deploys toggle |
| `app-link-btn` | App-link icon in the roster row's stack cell (ADR-0041) | Present only when the stack has a discovered Traefik hostname; `<a>` for one host, `<button>` (opens `app-link-pop`) for several |
| `app-link-pop` | Popover listing every discovered hostname | Only rendered for a multi-host stack; each entry is its own `<a target="_blank">`; closes on `Esc` or an outside click |
| `log-line` | A log line | `data-level` = level (or `cmd` for child output) |
| `level-badge` | Log level badge | |
| `stack-prefix` | `[stack]` prefix on a deploy log line | |
| `cmd-prefix` | `[cmd]` prefix on child-process output | |
| `diff-pill` | Diff pill on a `deploy complete` log line | |
| `clog-btn` | Console icon that opens a log (ADR-0037) | Container logs inside the newest deploy row's `⋯` menu (not `_nixos` — no compose project), on each `health-service` line, and inline in each roster row's stack cell (`data-clog-stack`/`data-clog-service`); the `hook-log-btn` variant on a running `hook-phase` opens the same panel in skipper mode (ADR-0038) |
| `clog-panel` | The live container-log panel | Trails the row/line; only one open at a time; gets `clog-fullscreen` in fullscreen |
| `clog-body` | The streamed log lines | `.clog-ln` per line; `.clog-hit` / `.clog-out` under an active in-log search |
| `clog-live` | Live/pause pill in the panel header | `.paused` when paused |
| `clog-search` | In-log search row inside the panel | Revealed by the search tool or by typing; holds the input + `.clog-hits` count |
| `clog-tail` | Backlog-size selector (50/200/1000) | |
| `clog-svcs` | Per-service filter chip row | Collapsible under the head (funnel tool toggles it); `all` + one `.clog-chip` per service, multi-select toggles; absent for a <2-service stack or hook mode |
| `hooks-badge` | Fishing-hook badge (ADR-0038) | Inside the `⋯` menu on the newest deploy row, inline in the stack cell on the roster row; present only when the stack declares hooks; shows the `pre+post` count; `<button>`, opens `hooks-panel`; two-line `title`; `data-hook-active` while a hook runs (Deploys: the `more-btn` carries the visible pulse; roster: the badge itself) |
| `hooks-panel` | Configured-hooks panel below the row | Bound (variant A, accent bar); commands come inline on the `stacks` snapshot (no fetch); one-panel-per-row with health/diff/audit/heal |
| `hooks-cmd` | A configured hook command line inside `hooks-panel` | Verbatim (`$`-prefixed), rendered as literal text |
| `hook-phase` | Phase sub-label on a `deploying` row (both views) | Present only while a hook runs (`pre_deploy hook 1/2` / `post_deploy hook`); carries a `clog-btn.hook-log-btn` logs icon that opens the inline hook log (the container-logs panel in skipper mode) |
| `logs-panel` | The Log view, styled as a page-sized `clog-panel` | `#log-view`; gets `.clog-fullscreen` in fullscreen |
| `logs-live` | Live/pause pill in the Log view's own header | `role="button"`, keyboard-operable (`Enter`/`Space`); `.paused` when paused; pausing freezes the pane, buffering continues |
| `logs-stat` | Connection status text in the Log view's footer | "live · streaming" / "paused" / "reconnecting…" |
| `log-search`, `log-wrap`, `follow-logs`, `log-fs` | Search/wrap/auto-scroll/fullscreen tools in the Log view's own header | `.clog-tool`s; `.on` when engaged; clicking `log-search` again closes it and clears the query |
| `log-filter-wrap` / `log-filter` | Logs in-view search bar / input | Same reveal + type-to-search behaviour as `deploy-filter`; no separate clear button — closing the search tool clears it |
| `log-filter-count` | Logs search hit count | |
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
| `hosts-btn` | Header Hosts control (drawer opener) | Multi-host only; `.enabled` when peers exist, `.filtered` under an active filter, `.has-offline` when a peer is unreachable |
| `hosts-drawer` | The Hosts drawer | Multi-select host filter + per-host reachability + open-UI link |
| `host-row` | A host row in the drawer | `data-host` = name; `.selected` when in view |
| `host-link` | A peer's "open its own UI" link | Peers only (`self` has none) |
| `hosts-all-btn` | "Select all" (clear filter) in the drawer | |
| `host-stale-banner` | Unreachable-peer banner above the merged feed | Shown when a selected peer is unreachable |

---

## Accessibility

The UI targets WCAG 2.1 AA. Beyond the per-component semantics noted above (the health pill / files pill / hooks badge are real `<button>`s, `aria-expanded` on every pop-out trigger, `role="switch"` + `aria-checked` on the autosync toggles), the cross-cutting guarantees:

- **Contrast** — the muted text tier (`--overlay1`, carrying timestamps, version label, duration, diff meta) meets AA (≥4.5:1) in every theme + variant. The low-contrast themes (Solarized both, Catppuccin/Rosé-Pine light) are retuned so muted < secondary < primary each stay distinct *and* AA. The queued amber, which fails on a light ground, has a darkened `--queued-text` token for its tag/badge/count **labels** (the bar/dot/glyph uses keep the brighter amber).
- **Focus ring** — one theme-aware `:focus-visible` ring (2px `--accent`) on every interactive control, keyboard-only (pointer focus never shows it). Element/role/attribute selectors cover the glyph-only header controls and the custom `role="button"`/`role="switch"` controls; a handful of controls (deploy indicator, health beacon) keep their own tuned ring.
- **Drawers/popovers** — opening the autosync / hosts / run drawer or the view-options popover moves focus inside it; Escape (or a keyboard close) returns focus to the opener; an outside-click close leaves focus where the click put it. The three `role="dialog"` drawers are `aria-modal` and **trap Tab** so it can't wander behind the open panel.
- **Live regions** — the connection indicator is a `role="status"` `aria-live="polite"` region (its state word is `sr-only`, not `display:none`, so it announces), and terminal deploy outcomes are voiced through the off-screen `a11y-announce` region as they land **live** (never on the history replay).
- **Touch targets** — small glyph buttons and status pills carry a transparent `::after` hit-area overlay lifting them to ≥24px (WCAG 2.5.8 AA) without changing anything visible; header controls grow taller (the header has the room). A full 44px (AAA) in the dense row cluster awaits the row-density redesign.
- **Multi-host** — the drawer host rows are keyboard-operable `role="checkbox"`es (`aria-checked` = in-view, Space toggles, focus is restored across the toggle rebuild) and the per-row `host-mono` identity chip is a keyboard-operable `role="button"` quick-filter.

## Responsive (≤ 700 px)

**Header — compact single row.** The header is already glyph-only on every viewport (see [Header — right](#layout)), so little changes ≤ 700 px beyond tightening the row to 48 px, which **must never scroll horizontally**. The brand is the ship logo plus, **in portrait**, the version label (it clips with an ellipsis and can shrink so the row still never scrolls; the full string stays in its `title` tooltip). The `skipper-cd` wordmark is hidden; the version label is also dropped in the tighter landscape orientation. Control-specific changes:

- **Deploy indicator** — the deploying stack name and the look-ahead trail are dropped (both kept in `title`/`aria-label`); the anchor/ship glyph shows, and the trail collapses to a compact peach **`+N` count chip** (`deploy-count`) when stacks are still to come this run. Tapping the glyph still opens the run panel (which lists the names).
- **Autosync control** — unchanged (already icon-only); keeps its glyph and pending-count pill.
- **View toggle / theme toggle / connection** — unchanged; already glyph-only.
- **View-options popover** — unchanged; still opened from the active view button.
- **Theme picker** — hidden entirely; overriding the palette is rarely needed on a phone. The configured theme (and any override already saved from a previous desktop visit) still applies.

On touch there are no hover tooltips, so each control's label is reachable via the **tap-reveal bubble** (a tap flashes the `title`; the action still fires). The `.status-area` gap tightens to 10 px and the header padding to 12 px so the row fits a 360 px viewport without overflow.

**Deploy table.** Column header hidden. Rows collapse to a 2×2 grid (stack + badge on row 1, time + duration on row 2). Files column hidden. Since there is no keyboard for type-to-search, the [Deploys view-options popover](#view-options-popover) gains a **"Search stacks"** entry that reveals the [filter](#deploys-filter) — the type-to-reveal path is desktop-only.

**Stacks view.** The roster collapses to the same 2×2 (stack/status on row 1, time/commit on row 2), and the [Version](#service-versions) cell becomes a **full-width third row** beneath them, as it does on a deploy row — dropped entirely when empty, so a stack without versions stays a clean 2×2. In the containers panel the version moves to a second line under its service, since name + version + state + status + logs do not fit one phone line.

Since the Files pill is not visible on mobile, tapping anywhere on a row (that has `changed_files`) triggers the files/diff panel instead. Rows with changed files get `cursor: pointer` on mobile. The toggle behaviour (tap again to close) is identical to the desktop pill behaviour. The [health pill](#stack-health) keeps its own tap target — a tap on it stops propagation and opens the `health-panel`, not the row's files/diff panel.

The Autosync drawer spans the full width below the header.
