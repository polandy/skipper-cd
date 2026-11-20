# UI redesign plan — one design for the Stacks and Deploys views

Status: **approved** (2026-11-17), implementation in the PR sequence of §6.
Mockup: the private artifact "Stacks view mockup" (toggle *View → Deploys* for
the second view, *Layout → Side panel* for the side panel).

The two table views were meant to be "the same surface with different data"
([ui-design-concept.md](ui-design-concept.md)). They share tokens and some
panel builders, but the row, the actions, the expand behaviour and the panel
styling have drifted apart, and the Stacks expand card has grown into three
equal-weight panels that are hard to read. This plan proposes one row anatomy
and one detail panel for both views, and the sequence of small PRs that gets
there without a big-bang rewrite.

## 1. Current state

Captured from `make ui-preview` (seeded instance, 1280 px and 400 px) and a
code inventory of `internal/ui/static`.

### 1.1 What an operator sees

**Deploys** (event log, 6 columns: Time · Stack · Changes · Status · Duration · Files)

- Each row carries up to: host chip, icon, name, jump glyph, `⋯` menu (newest
  row per stack only), pending tag, change chips (one per service *and* one per
  change kind, so a row can be five chips tall), status badge, retry/repeat
  note, health pill, duration, files pill or self-heal pill.
- A failed / rolled-back row has a **permanent red error strip** under it,
  full width, raw stderr. Two failures in the list already dominate the page.
- Clicking opens one of five bound panels below the row (diff/files, health,
  history, heal, hooks); which one depends on *where* you click (row body,
  health pill, files pill, `⋯` → history). Only one panel at a time.
- Phone: two-line rows, error strip wraps to three or four lines of mono text.

**Stacks** (inventory, 5 columns: Stack · Version · Status · Last deploy · Commit)

- Status cell stacks badge + held chip + retry button + health pill + outcome
  strip + last-incident line — up to five elements in a 130 px column.
- Inline glyphs on the row (jump, app link, logs, hooks) without labels.
- Expanding stacks **three panels** (containers, history, change detection) as
  one accent card. "healthy" is repeated in the row, the panel head and on every
  container line; the containers panel puts name and state ~900 px apart.

### 1.2 Duplicated

| Building block | Where it exists | Note |
|---|---|---|
| Row template | `createRow` (app-deploys.js:343), `createPeerRow` (app-hosts.js:84), roster row + `renderPeerRosterRows` (app-roster.js:58/159) | 4 templates; peer rows already diverge (no files pill, no jump) |
| Status colour | `.badge-*`, `--sc` row bar, `--dc` diff bind, `--ec` error strip, `--ac` audit dot, `--oc` outcome dot, `--hc` health | 7 parallel status→colour maps in app.css |
| Status label | `badgeHTML` vs `auditStatusLabel` + `.adot` (app-render.js:1475) | history rows rebuild the badge |
| Row actions | `⋯` menu on Deploys (`ensureRowMore`) vs inline `.row-actions` on Stacks (`rosterRowActionsHTML`) | same actions, two idioms; history button exists only on Deploys |
| Open state | `diff-open` (also reused for heal and peer detail), `health-open`, `audit-open`, `hooks-open` | four classes for "this row is expanded" |
| Name/stack cell | `.cell-stack/.stack-name` (24 ch cap) vs `.roster-stack/.roster-name` | duplicate declarations (app.css:915 vs 3277) |
| Time cell | `.cell-time` (`data-ts`, live re-render) vs `.roster-when` (no `data-ts`; time-mode change forces a full roster render) | |
| Grid | `--deploy-cols` variable vs literal roster columns | three breakpoints each, maintained twice |

### 1.3 Contradictions

- **Health pill click**: opens only the health panel on Deploys, but falls
  through to the full three-panel card on Stacks.
- **Hooks panel** is a framed card (margin, border, radius) while every other
  bound panel is flush on the sheet — against the concept's "part of the sheet,
  not a card on it".
- **Open roster peer row** gets no bar or tint: the `diff-open` CSS is scoped to
  `.event-row`.
- **Error detail** is a permanent sibling, not a toggled panel, so a toggled
  panel lands *between* the row and its error.
- **Concept doc vs reality**: the concept says the row carries "a status badge,
  a health pill, a files pill, a jump button, a logs icon, an app link" and the
  card "already stacks three panels" — and asks every addition to say what comes
  off. Nothing has come off so far; this redesign is that budget pass.

### 1.4 Bug found on the way (fix independently)

`refreshRosterRow` (app-roster.js:460) rewrites `.roster-status` with
`rosterStatusHTML` only, so a deploy event drops that row's health pill,
outcome strip and last-incident line until the next `stacks` snapshot rebuilds
the list. Small `fix(ui)` PR, independent of the redesign.

### 1.5 Structural obstacles to a shared detail panel

1. Panels are found purely by DOM position (`row.after`, `nextElementSibling`)
   — no "selected stack" state exists.
2. Filters (`applyDeployFilter`, `refilterRoster`, `hostHideRows`) give a
   trailing panel its row's visibility.
3. The roster rebuilds completely on every `stacks` snapshot and re-opens the
   card by `{stack, host, kind}`.
4. Open panels are never refreshed by later health snapshots — invisible today
   because panels close often; a persistent panel would show stale data.
5. Deploy rows churn (queued rows collapse, deploying rows mutate, superseded
   rows are removed, the `⋯` menu migrates to the newest row).
6. The container-logs panel (app-clog.js) is a global sibling anchored to a row
   or a `.hp-svc` line.
7. No Escape/focus handling for row panels; the chrome surface registry
   (app-chrome.js:99) only covers header pop-outs.
8. No URL state beyond `localStorage.activeView`.

## 2. Goals

1. **One look**: one row anatomy, one detail panel, one action idiom, one status
   colour map across both views — the concept's promise, made true in code.
2. **Answer first**: opening a stack or a deploy says in one sentence whether
   it is fine and what (if anything) to do, before any table.
3. **Calmer rows**: a row is identity + state + version + time. Every glyph
   without a label moves into the detail panel header, with a label.
4. **Each fact once**: health in the row, deviations in the panel; status in
   the badge, not also in a strip, a tint and a panel head.
5. **Structure that keeps it that way**: one row renderer (pure, unit-tested),
   one selection model, so peers and future fields cannot drift again.

Non-goals: no new trigger or control (scope rule — the existing
`POST /api/stacks/{stack}/retry` is the only action that does something); no
change to the Logs view, the header, autosync, orphans; no backend change.

## 3. Shared design system

### 3.1 Row anatomy (both views)

Four tracks, same grid variable, same breakpoints:

| Track | Stacks | Deploys |
|---|---|---|
| **Identity** | host chip · icon · name | host chip · icon · name |
| **State** | health — dot + word (*now*) | result — status badge (+ `held`, `↻ ×N`) (*then*) |
| **Version** | running version: lead chip + `+N`, `⇡` when an update exists | change: lead `old → new` chip + `+N`, or `N files` / `no git change` |
| **Time** | last-deploy badge · relative time · short SHA | relative time · short SHA · duration |

- The row carries **no glyph controls**. Clicking anywhere on it selects it.
  Jump, logs, hooks, history, app link move to the panel header (§3.3).
- The 3 px gutter keeps its rule: exceptions only (Stacks: unhealthy / stopped
  / held; Deploys: failed, rolled back, queued, blocked, healed).
- **Cell spacing**: the row owns its vertical padding (`padding-block`, one
  token), and the State cell lays its badges out as a wrapping flex group
  (`flex-wrap: wrap` + `gap`, `align-content: center`, right-aligned on
  phones). No margins on badges or notes, so a cell with two or three badges
  (`rolled back` + `held`, `failed ↻ ×4` + `held`) keeps the same space above
  as below when it wraps, at every width and in both themes.
- Attention ordering stays per view (Stacks floats problems up; Deploys stays
  chronological).
- Removed from the row: health pill on Deploys (current health is the Stacks
  view's and the panel's job), files pill, self-heal pill, `⋯` menu, retry
  note, outcome strip, last-incident line, Commit column, Duration and Files
  columns (Duration/SHA join the Time cell as muted text).
- The **error strip** under failed rows is removed; the condensed cause
  (ADR-0063) is the panel's first line (see open question Q2).
- Tablet ≤ 1000 px: Time cell drops SHA/duration. Phone ≤ 700 px: two lines —
  identity + state, then version + time. No horizontal scroll.

### 3.2 Detail panel (one component, two contents)

```
┌ icon  name  HOST  [kind]             [Open app] [Logs] [Hooks] [Show in …] ✕ ┐
│ ● Verdict line(s): one sentence each, at most one primary action (Retry once)│
│ ┌ fact ┬ fact ┬ fact ┬ fact ┐                                               │
│ Tab · Tab · Tab                                                             │
│ pane                                                                        │
└──────────────────────────────────────────────────────────────────────────────┘
```

| Part | Stack | Deploy |
|---|---|---|
| Verdict | held / failed / unhealthy service / stopped / queued / updates / self-healed / all healthy | rolled back / failed (+ repeats) / healed / queued / blocked / deployed; plus "Now: unhealthy" when current health differs |
| Facts | Running · Last deploy (+ outcome dots) · Health `n of m` · Hooks | Commit · When · Duration · Files |
| Tabs | **Containers** · History · Triggers (change detection) | Changes (files, attribution, diff) · Error · **Containers** · History |
| Default tab | History when held/failed/healed, Triggers when parked/never, else Containers | Error when it has one, else Changes, else Containers |

- Containers and History are the **same pane builders** in both views
  (today's `createHealthPanel` / `createAuditPanel`, re-styled as compact
  tables, broken services first).
- Every count badge on a tab is red when it contains a problem.
- Peer stacks get the same panel, read-only (no Retry, logs through the proxy).
- Pseudo-stacks (`_nixos`, `_config`, `_project_dir`) get Changes/Error/History
  only — no Containers, no "Show in Stacks".

### 3.3 Actions

Labelled buttons in the panel header, always in this order, shown only when
they apply: **Open app** (Traefik link) · **Logs** · **Hooks** · **Show in
Stacks/Deploys** · close. In-context actions sit next to the fact they act on:
*Retry once* in the held verdict, *Logs of `<svc>`* in an unhealthy-service
verdict and on each container line. No `⋯` menu, no unlabelled glyphs.

### 3.4 Placement

One panel component, three hosts:

- **Desktop ≥ 1200 px — side panel**: list at ~470 px, panel fills the rest;
  selection persists while the list updates. Recommended default (Q1).
- **701–1199 px — inline**: the same panel bound under the selected row
  (variant A, accent bar), so the list keeps its columns.
- **Phone ≤ 700 px — sheet**: full-screen over the list, close returns to the
  list at the same scroll position.

### 3.5 Badges and tokens

- One `--st` status token map (`[data-status]` → colour, dim, text) replacing
  `--sc/--dc/--ec/--ac/--oc`; health keeps `--hc`.
- `badgeHTML` is the only status renderer (history rows and the outcome dots
  use it or its colour token).
- Kept as is: frame budget (only failed/rolled_back tinted, the two worst solid),
  radius scale, type roles, version chip.

### 3.6 State model

`App.detail = { view, host, stack, eventId?, tab }`, keyed by **stack** (plus
`eventId` for a deploy's Changes/Error), owned by a new `app-detail.js`:

- re-renders the open panel on `health`, `stacks`, `hookrun` and deploy events
  for that stack (fixes the stale-panel problem);
- re-applies the selected-row highlight after a roster rebuild or a row churn;
- registers with the chrome surface registry (Escape, focus return, exclusive
  with drawers);
- a filtered-out selection keeps the panel open with a "hidden by filter" note;
- `jumpToStack` opens the target's panel instead of only flashing the row.

## 4. Changes per file

| File | Change |
|---|---|
| `app-render.js` | New pure `tableRowHTML(kind, model)` (one row for local/peer, deploy/roster), `detailHeadHTML`, `verdictHTML`, `factsHTML`, `tabsHTML`, `changesPaneHTML`; history rows use `badgeHTML`. Remove `rowActionClusterHTML`, `rosterRowActionsHTML`, `filesHTML`, `outcomeStripHTML` once unused. Unit-tested in `app-render.test.js`. |
| `app-helpers.js` | Pure `stackVerdict(entry, health)` / `deployVerdict(evt, health)` → list of `{tone, text, sub, action}`; `defaultTab`; sort keys stay. Unit-tested. |
| `app-detail.js` *(new, loaded after app-panels.js)* | `App.detail`: selection state, panel host placement (side / inline / sheet), tab switching, live re-render subscriptions, surface registration. |
| `app-panels.js` | Health, audit, watched, hooks, diff, heal builders become **pane** builders (no `row.after`, no binding classes); `ensureRowMore` and the `⋯` menu go. |
| `app-deploys.js` | `createRow` → `tableRowHTML`; drop `updateStackAffordances`'s pill/menu work, `createErrorDetail`, the 13-branch tbody click handler (→ one "select row" handler); filter no longer walks trailing panels. |
| `app-roster.js` | Row via `tableRowHTML`; drop the reopen-card logic (the selection survives the rebuild via `App.detail`); fix `refreshRosterRow` (§1.4). |
| `app-hosts.js` | `createPeerRow` and peer roster rows use the shared renderer; peer detail becomes the read-only panel. |
| `app-clog.js` | Anchor inside the panel's Containers pane (or as a pane of its own), not as a row sibling. |
| `app-chrome.js` | Register the detail surface; `jumpToStack` opens the panel; optional URL hash (Q5). |
| `app-stream.js` | Route health/stacks/hookrun/deploy updates to `App.detail.refresh(stack)`. |
| `index.html` | Side-panel host element next to each sheet; script tag for `app-detail.js`. |
| `app.css` | One grid variable for both tables; one row `padding-block` token and a wrapping flex State cell with `gap` (§3.1, no badge margins); `--st` token map; panel/tab/verdict/facts styles; delete per-panel binding rules (`diff-open`, `health-open`, `audit-open`, `hooks-open`, `.bound`, `:has(+ .error-detail)` chains), error strip, `⋯` menu, row-actions. Expected net shrink of several hundred lines. |

## 5. Impact on UI_SPEC.md and e2e

**UI_SPEC.md** — rewritten sections: Deploy table, Stack health (pill on
Deploys gone), Row overflow menu (removed), Deploy history (now a tab),
Cross-view jump (opens panel), Rollback linkage (retry note → verdict line),
Repeated outcomes (badge suffix), Expandable panels (→ Detail panel), Stacks
view, Outcome strip (moves into facts), Change detection (Triggers tab),
Responsive (tablet/mobile), Test hooks. **ui-design-concept.md** — rows,
columns, "Expand → bound panels" and the panel table are replaced by §3 of
this plan.

**Test IDs** — keep every existing testid that still names a thing
(`deploy-row`, `roster-row`, `status-badge`, `health-panel`, `audit-panel`,
`audit-row`, `watched-panel`, `diff-panel`, `error-panel`, `clog-btn`,
`jump-btn`, …) on the new nodes, so most selectors survive. New:
`detail-panel`, `detail-tab`, `detail-verdict`, `detail-facts`,
`detail-action`. Removed: `more-btn`, `more-pop`, `history-btn`,
`files-pill`, `heal-pill`, `retry-note`, `outcome-strip`, `last-incident` (in
the row).

**Playwright** (inventory: ~120 tests in 20+ specs touch rows or panels):

- Structural assertions that break by design: `nextElementSibling` checks in
  ua-deploys (322/373/425), uj-rollback-error (44/70-71), um-audit (44),
  ul-panel-exclusive (52), ui-diff-commit (63), ud-chrome (82); ~21
  `toHaveClass(/(audit|diff|health)-open/)`; 11 `jump-target`.
- Specs to rewrite: uad-overflow-menu and uae-roster-inline-actions (actions
  move to the panel header — fold into one new mask), ul-panel-exclusive and
  uaw-surface-exclusive (one panel by construction), uas-roster-phone-align.
- Baselines to regenerate (LFS): `deploys-table.png`, `diff-panel.png`,
  `mobile-layout.png`, `theme-dark.png`, `theme-light.png`; docs screenshots
  regenerate on their own. Add the first Stacks baselines (list + panel).
- New mask(s) for the detail panel; the letter is picked against live state
  when that PR opens (race rule), and written after the manual eyeball pass.

## 6. PR sequence

Each PR is one feature with its tests, its UI_SPEC/concept update and
screenshots; UI-visible PRs go through the manual look before the mask is
finalised.

| # | Type | Content | Visible change |
|---|---|---|---|
| 0 | `fix(ui)` | `refreshRosterRow` keeps health pill/strip (§1.4) | bug fix only |
| 1 | `refactor(ui)` | One pure row renderer for deploy/roster/peer rows (`tableRowHTML`); one grid variable (`--row-cols`); `--st` token map replacing `--sc/--dc/--ac/--oc` | none (baselines unchanged — that is the test) |
| 2 | `refactor(ui)` | `App.detail` selection model + `app-detail.js`; panels become panes; still rendered inline as today's card | no pixel change; Deploys keeps one open detail at a time, as Stacks already did |
| 3 | `feat(ui)` | Detail panel for **Stacks**: header with labelled actions, verdict, facts, tabs; history rows use `badgeHTML` (a visible change, so not in PR 1); row loses glyphs, status-cell stack, Commit column | Stacks view |
| 4 | `feat(ui)` | Detail panel for **Deploys**: Changes/Error/Containers/History tabs; row → 4 tracks; error strip (and its `--ec` map), `⋯` menu, pills removed. Done, the retry note last (now the verdict's Open rollback) | Deploys view |
| 5 | `feat(ui)` | Side-panel placement ≥ 1200 px, phone sheet, Escape/focus, live refresh; ADR for "side panel instead of bound panels" (a real tradeoff). Done: the side panel with ADR-0073, the phone sheet (its amendment) and the live refresh | both views |
| 6 | `feat(ui)` *(optional)* | URL hash deep link to a selection. Done for the Stacks detail only (Q5) | address bar |

PR 1 and 2 are refactors with zero intended pixel change, which makes them
safe to land first and keeps 3–5 small. 3 and 4 can be reviewed side by side
against the mockup.

## 7. Risks

- **Test churn** is the biggest cost (~120 e2e tests, 5 baselines). Mitigated
  by keeping testids and by PR 1/2 being pixel-neutral.
- **Losing glanceability for failures**: the red strip is loud on purpose.
  The gutter + tinted badge + `held` must still make a failure obvious in the
  list; verify on the hand test with real data (Q2).
- **Live updates under a persistent panel**: row churn and roster rebuilds must
  never close or jump the panel. Needs deterministic tests (settled-state
  signal, no sleeps — testing rule).
- **Peer and pseudo-stack edge cases** (read-only, no containers, older peers
  without fields) — covered by the shared renderer, but every tab must degrade.
- **Muscle memory**: the `⋯` menu and inline glyphs go away. Labelled buttons
  are one click further but discoverable.
- **Width 1000–1200 px** with side panel would starve the list; hence inline
  there.

## 8. Decisions

Reviewed with andy on 2026-11-17; the plan was approved as written, so each
open question takes the option this plan and the mockup already assume:

1. **Placement**: side panel on desktop ≥ 1200 px, inline 701–1199 px, sheet
   on phones (§3.4).
2. **Error strip**: removed completely; the condensed cause is the panel's
   first line. The gutter, the tinted badge and `held` carry the failure in
   the list (re-checked on the hand test, §7).
3. **Health on Deploys rows**: dropped; current health lives in the Stacks
   view and the panel's Containers tab.
4. **Columns**: Duration, Files and Commit fold into the Time cell as muted
   text.
5. **Deep links**: decided with andy on 2026-11-20, after PR 5 had landed: the
   Stacks detail only (`#stacks/[<host>/]<stack>[?tab=]`). Stacks are stable
   names, while a deploy's event id ages out of the bounded history, so a
   Deploys link would go dead; `UI_SPEC.md` "Stack detail panel" has the rules.
6. **Naming**: "Containers" in both views.
7. **Hooks**: a header action opening a pane, as mocked — not a fifth tab.
8. **This plan file**: merges to `main` with PR 1, so the decision lands
   together with the first code behind it.
