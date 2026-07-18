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
  (`CurrentStacks()` in discovery mode, the host `stacks:` list in legacy
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
}

func Build(stacks []config.Stack, disabled []string,
    last func(name string) (audit.Record, bool)) []Entry
```

- No `icon` field: the frontend resolves each stack's icon from its **name**
  via the existing `/api/icons/<name>` endpoint (override + repo icon +
  monogram fallback all server-side), exactly like the deploy table.
- **Order**: enabled stacks first (alphabetical), then disabled
  (alphabetical) — inventory reads as "what's live" then "what's parked",
  stable across snapshots.

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

- Third `data-view="stacks"` button in the header view switcher; `activeView`
  gains the `stacks` value (persisted in `localStorage`).
- A roster container, shown for the stacks view and hidden otherwise (the
  deploy table and logs pane hide for it in turn).
- One row per `Entry`: icon (`/api/icons/<name>`) · name · **status** ·
  relative time · short commit SHA. Status reuses the deploy table's badge
  (`badgeHTML`), plus two synthetic states: **never deployed** (no
  `last_status`) and **disabled** (`disabled: true`, muted, no badge). Reuses
  `formatTime` / `shortSHA`.
- A tab header line: `N stacks`, and in discovery mode a quiet
  `discovery` hint. Empty state when the set is empty.
- Re-renders on each `stacks` snapshot. A live `deploying` event refreshes
  only the affected row (not the whole list, so an open panel survives); the
  next snapshot settles it to the terminal status.

### Row interactions (parity with the deploys view)

- **Click a row → deploy history.** Toggles the deploy table's audit panel
  (ADR-0033, `createAuditPanel`) below the row, loaded from
  `/api/audit?stack=<name>`. One open at a time; the panel trails its row as a
  sibling and connects to it visually. A full re-render (new snapshot) drops
  any open panel — acceptable at the snapshot's per-run cadence.
- **Search.** The same filter as the deploys view: a hidden bar revealed by
  type-to-search (a printable key while on the Stacks view) or, on touch, the
  "Search stacks" row in the Stacks view-options popover. Case-insensitive
  substring match on the stack name; `shown/total` count; "No stack matches"
  empty note; Esc clears then folds; a trailing history panel shares its
  row's filtered visibility. The Stacks view gains its own view-options group
  so the popover is consistent across all three views.

## Testing

- `internal/roster`: table tests — merge/sort, never-deployed (no record),
  disabled ordering + parking, empty set.
- Frontend: Playwright Maske Q (`uq-roster.spec.ts`) — tab switch + deployed/
  disabled rows + discovery hint (UQ1), click-a-row-for-history (UQ2), search
  incl. the mobile popover entry (UQ3/UQ4). Behaviour-only; the "never
  deployed" state is covered by the roster unit test (in discovery mode the
  first sync deploys every stack, so it isn't deterministically seedable in
  e2e) and shares its `.roster-flag` rendering with the disabled row.
- The header switcher gains a third control, so the full-page `ud-chrome`
  baselines (`theme-dark`, `theme-light`, `mobile-layout`) are regenerated;
  the element-scoped baselines are unaffected.

Not an ADR: no new architectural decision — this is a presentation surface
over the set ADR-0034 already established. Recorded here + in `UI_SPEC.md`.
