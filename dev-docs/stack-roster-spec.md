# Stacks roster tab

The deploy table is an **event log** — it shows only stacks that emitted an
event this session (plus persisted history), and by design a disabled or
never-deployed stack has no row (see `internal/ui/UI_SPEC.md`). Stack discovery
(ADR-0034) made the *full set of stacks skipper owns* an authoritative, known
quantity (`Deployer.CurrentStacks()`), so the UI can now answer questions the
event log can't: **what stacks exist, which have never deployed, which are
parked, and what each one's last outcome was.**

The **Stacks** view is a third top-level view (header switcher:
`Deploys / Stacks / Logs`) that renders that inventory. It is pure
visualization — no trigger/edit (see the project's viz-only scope).

## Data source

Two facts per stack, merged:

- **Membership + config** — the effective stack set via `stacksNow()`
  (`CurrentStacks()` in discovery mode, the host `stacks:` list in host-list
  mode), so the tab works in both modes. `disabled: true` names come from
  `CurrentDisabledStacks()`.
- **Last outcome** — the newest `audit.Record` per stack
  (`auditLog.Stack(name, 1)`, ADR-0033): terminal status, timestamp, commit
  SHA. A stack with no audit record has **never deployed**.

No new persistence and no new poll: both sources already exist.

## Backend

`internal/roster` (small, one job): merges the three inputs into a stable,
sorted list.

```go
type Entry struct {
    Name       string        `json:"name"`
    Disabled   bool          `json:"disabled"`
    LastStatus events.Status `json:"last_status,omitempty"` // empty = never deployed
    LastAt     *time.Time    `json:"last_at,omitempty"`
    LastCommit string        `json:"last_commit,omitempty"`
    Hooks      *Hooks        `json:"hooks,omitempty"`    // declared deploy hooks
    Watched       []string   `json:"watched,omitempty"`        // hashed input paths
    WatchedConfig bool       `json:"watched_config,omitempty"` // config hash tracked
}

func Build(stacks []config.Stack, disabled []string,
    last func(name string) (audit.Record, bool),
    watched func(name string) []string) []Entry
```

- **`Watched`**: the input paths whose hashes decide whether the stack
  redeploys (Invariant 2), as recorded by its last successful deploy
  (`Deployer.CurrentTrackedFiles`, published after every run like the project
  dirs orphan detection reads). It is what lets the UI answer "why did nothing
  happen for this stack" without an operator opening `state.yaml` on the host.
  Empty for a stack that has never deployed, and deliberately omitted for a
  parked (`disabled: true`) one — skipper is not watching it, whatever its last
  deploy recorded. `cmd/skipper` renders a path inside the repo clone
  repo-relative before it ships (`splitTrackedPaths`); host paths stay absolute.
- **`WatchedConfig`**: the stack's deploy-shaping config is hashed as well
  (`Stack.ConfigHash`), but under a *synthetic* `<stacks_base_dir>/skipper.yaml`
  key — ADR-0043 moved that config host-side, so no such file exists. It travels
  as a flag rather than inside `Watched` so the UI can render it as prose; a
  path would send an operator looking for a file that is not there.
- No `icon` field: the frontend resolves each stack's icon from its **name**
  via the existing `/api/icons/<name>` endpoint (override + repo icon +
  monogram fallback all server-side), exactly like the deploy table.
- **Order**: enabled stacks first (alphabetical), then disabled
  (alphabetical) — inventory reads as "what's live" then "what's parked",
  stable across snapshots. (The UI floats a currently-*unhealthy* enabled
  stack to the top of this order at render time — a client-side concern, since
  runtime health is not part of the roster snapshot; see `internal/ui/UI_SPEC.md`.)

Wired into the existing `stacks` SSE snapshot (`stacksState`), which is
published on connect and after every deploy run — the roster's natural
cadence. The snapshot keeps its existing `disabled` array (drives the Deploys
view's disabled line, unchanged) and gains `roster`:

```json
{ "disabled": ["experiments"],
  "roster": [ { "name": "traefik", "disabled": false,
                "last_status": "success", "last_at": "…", "last_commit": "a1b2c3d" },
              { "name": "grafana", "disabled": false },
              { "name": "experiments", "disabled": true } ] }
```

## Frontend

The Stacks view is a **table of stacks** that reuses the deploy table's row,
column, and expand language wholesale — see
[`dev-docs/ui-design-concept.md`](ui-design-concept.md) for that shared design.
Only what is specific to the roster is listed here.

- Third `data-view="stacks"` button in the header view switcher; `activeView`
  gains the `stacks` value (persisted in `localStorage`). The roster container
  shows for the stacks view and hides the deploy table + logs pane (and back).
- **Aligned table**, same fixed-grid + header treatment as the deploy table
  (`.roster-list-header`): columns `Stack · Status · Last deploy · Commit`. The
  Stack cell is icon (`/api/icons/<name>`) + name, like the deploy table's Stack
  cell. Rows are the deploy table's frame **without** the status left bar
  (status is the badge). Mobile collapses to the same 2×2 shape.
- **Status** reuses `badgeHTML` for the last terminal outcome, plus two
  synthetic `.roster-flag`s: **never deployed** (no `last_status`) and
  **disabled** (`disabled: true`, muted row, no badge). Time/commit are shown
  only for a real past deploy (suppressed while deploying, parked, or never
  deployed) and reuse `formatTime` / `fullTime` / `shortSHA`.
- **No title line.** The roster starts straight at the column header — no count
  or mode hint, matching the deploy table (which has none either).
- Re-renders on each `stacks` snapshot. A live `deploying` event refreshes only
  the affected row (an open panel survives); the next snapshot settles it.

### Row interactions (parity with the deploys view)

- **Inline secondary actions.** An enabled row's secondary actions sit inline in
  the stack cell, beside the cross-view **jump** and the **app-link**: the
  **container-logs** icon (ADR-0037) always, the **deploy-hooks** badge
  (ADR-0038) only when the stack declares hooks. Each keeps its own handler — the
  logs icon opens the log panel, the hooks badge the bound hooks panel. Earlier
  these folded behind a `⋯` overflow menu (as the deploy row still does), but on
  the roster the row-body click already opens the health + history panel, so the
  menu usually wrapped a single action (logs) — an extra click for no density
  gain. No **Deploy history** item — the row-body click (below) owns it. Disabled
  rows have no secondary actions; peer rows are read-only (logs reachable only via
  the expanded containers panel). The running-hook pulse rides the inline badge.
- **Click a row → containers + deploy history.** Expanding a stack stacks two
  bound panels below the row as one accent card: the **containers** panel first
  (the stack's live health/services, `createHealthPanel`, from the `health`
  snapshot — shown only when health data exists for the stack), then the
  **deploy-history** panel (ADR-0033, `createAuditPanel`, `/api/audit`). One
  stack open at a time; a full re-render (new snapshot) drops the open panels.
  These are the same two panels the Deploys view opens separately (health pill /
  clock button); in the roster they carry the neutral accent bar.
- **Time mode.** The Stacks options popover carries the same **Absolute time**
  toggle as Deploys — one shared mode; flipping it re-renders the roster too.
  Hovering a relative time reveals the absolute value (mouse; touch uses the
  toggle).
- **Search.** The same filter as the deploys view: a hidden bar revealed by
  type-to-search (a printable key while on the Stacks view) or, on touch, the
  "Search stacks" row in the Stacks view-options popover. Case-insensitive
  substring on the name; `shown/total` count; "No stack matches" note; Esc
  clears then folds; trailing panels share their row's filtered visibility.

## Testing

- `internal/roster`: table tests — merge/sort, never-deployed (no record),
  disabled ordering + parking, empty set.
- Frontend: Playwright Maske R (`ur-roster.spec.ts`) — view switch + deployed/
  disabled rows + aligned column header (UR1), click-a-row → containers +
  history (UR2), search incl. the mobile popover entry (UR3/UR4), and the shared
  time-mode toggle (UR5). Behaviour-only; the "never deployed" state is covered
  by the roster unit test (in
  discovery mode the first sync deploys every stack, so it isn't
  deterministically seedable in e2e) and shares its `.roster-flag` rendering
  with the disabled row.
- The header switcher gained a third control, so the full-page `ud-chrome`
  baselines (`theme-dark`, `theme-light`, `mobile-layout`) were regenerated for
  it; the roster's own view is not snapshotted (behaviour-only) and the
  element-scoped baselines are unaffected.

Not an ADR: no new architectural decision — this is a presentation surface over
the set ADR-0034 already established. Recorded here, in
[`ui-design-concept.md`](ui-design-concept.md), and in `UI_SPEC.md`.
