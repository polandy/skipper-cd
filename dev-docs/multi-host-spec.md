# Feature Spec: Multi-Host Federated UI

Status: implemented on the feature branch (see [ADR-0048](adr/0048-multi-host-federated-ui.md)).
**Reworked 2026-07-21**: the earlier reverse-proxy switcher is dropped in
favour of read-data fan-in — see the "Why not a reverse proxy" section.
The host-indicator treatment (a per-row **monogram chip**, not a Host column) and the
dot's click-to-filter interaction were chosen against an interactive mockup
and iterated live in `make ui-preview`.
Date: 2026-07-18 (reworked 2026-07-21, built 2026-07-22)

## Goal

One pane of glass for a multi-host homelab (host-a + host-b today): the
primary skipper renders **every host's deploys, stacks and health in its own
single UI**, merged, with the host as one more dimension on each row. A
glance answers "is anything wrong across all my hosts right now" without
clicking through hosts one at a time.

skipper stays decentralized: **every host keeps running its own full,
independent skipper** (own clone, own state, own webhook). One instance is
additionally the *primary* that fans the others' read data in. If the
primary dies, every host still deploys and still serves its own UI at its
own address — the primary is a convenience layer, never a dependency.

Non-goals: central deploy orchestration, cross-host `depends_on`, moving
stacks between hosts, an agent protocol, shared/persisted cross-host state,
sending any command to a peer. Deploy logic remains strictly per-host and
git-driven; the primary only ever *reads* from peers.

## Why not a reverse proxy (the discarded first design)

The first design proxied each peer's whole page at `/peer/{name}/`: you
switched to one host at a time, looking at that host's own served UI. It was
dropped because:

- **It never merged.** "Switch, not merge" means you still inspect hosts one
  by one — a bookmark folder, not a pane of glass. The actual goal (one
  merged overview) was deferred to a hard phase 2 that the proxy made
  *harder*, not easier.
- **Its two open questions were symptoms of the wrong decomposition.**
  Forwarding a peer's whole self-contained UI (ADR-0035) meant the peer's
  page knew nothing of the federation → the unsolved "how do you get back"
  problem, and the per-host theme-switch that reskinned the UI on every hop.
- **It forwarded peer *write* endpoints too** (autosync toggle, icon
  refresh), quietly widening scope.

Fanning read data in dissolves all three: you never leave the primary's UI,
so there is no "getting back" and no theme-switch; and consuming a *read*
API can't expose peer writes.

## User model

```yaml
# the primary's skipper.yaml
host_name: host-a        # this instance's own label (defaults to the OS hostname)
peers:
  - name: host-b
    url: http://host-b:8001
  - name: host-c
    url: http://host-c:8001
```

- `peers` is optional; without it nothing changes. The intended homelab
  setup is one primary; only it needs a `peers:` list (no reciprocal config
  — see Consequences, this is a direct win of the pivot).
- `name` is the display label and identity key (drives the per-host colour);
  `url` is the peer's skipper base URL on the LAN.
- `host_name` (`config.HostName`) is the primary's own label, the identity
  key its local rows are tagged and coloured by. It defaults to the OS
  hostname; set it explicitly to match how the peers name each other.

## Mechanism: read-data fan-in over the versioned API

Each skipper already serves the read model its own UI consumes. Under this
design that read model is the **versioned, read-only `/api/v1`** of
[ADR-0039](adr/0039-read-only-http-json-api.md) — which this feature is the
first concrete consumer of. Federation is then:

```
peer /api/v1 (snapshot + events)  ──▶  primary fans in, tags host  ──▶  one merged model  ──▶  primary's own UI
```

- **The primary fans in, server-side.** For each configured peer it reads
  that peer's `/api/v1` snapshot and updates, tags every stack/event/health
  record with the originating host name, and merges them with its own local
  data into one model. The browser opens **one** connection, to the primary,
  exactly as today; every row simply gains a `host` field.
- **Parsing peer data is safe here because `/api/v1` is a versioned,
  additive-only contract** (ADR-0039). "Don't parse peer data" was only wise
  in the *absence* of such a contract (it was the reverse-proxy design's
  rationale); the versioned API is exactly what makes fan-in robust against
  version skew — a peer field the primary doesn't know is ignored, never a
  parse break.
- **Poll first, stream later.** v1 polls each peer's snapshot on the
  existing reconcile/health ticker cadence (no new persistent connection,
  reuses infrastructure skipper already has). The merged overview may lag a
  few seconds behind a peer's live deploy; to watch a peer deploy *live*,
  open that peer's own full UI via the Hosts panel link (a plain external
  link, no proxy). SSE fan-in (the primary subscribing to each peer's
  `/api/v1` event stream for true live merge) is a documented later
  enhancement, not a prerequisite.
- **Peers need only be reachable from the primary** (server-to-server), not
  from the browser — the same firewall posture the homelab already wants.

Nothing here persists cross-host state: the merged model is ephemeral and
rebuilt from peers on demand. Correctness invariants around deploys,
rollback and state are untouched because nothing in this path writes deploy
state — on the primary or on any peer.

## UI

The single UI is always the primary's own — one theme throughout (ADR-0021),
no per-host reskin.

- **Logo stays anchored top-left.** The brand mark keeps the corner.
- **A compact "Hosts" control joins the right-hand controls** (with the view
  toggle, autosync, theme). It is a button — server-rack glyph + a
  `selected/total` count badge + a warning dot when any peer is unreachable +
  chevron — that opens **one drawer doing two jobs**: (1) a **multi-select
  filter** (a checkbox per host, plus a "Select all" shortcut) over both
  merged views; (2) per host, its colour chip, a reachability dot
  (up / stale / down) and a link to **open that host's own full UI** (the
  live deep-dive; `self` has no link). The reachability dots sit in a
  fixed-width slot so they line up down the column whether or not a row
  carries the link. Filtering to a subset lights the button (glyph + badge in
  the accent colour) so an active filter is visible without a permanent chip
  row. The drawer reuses the autosync drawer's `.drawer` shell and is a
  registered dismiss surface (Escape / click-outside close it, mutually
  exclusive with the other pop-outs). The control and drawer are hidden
  entirely on a single-host instance.
- **Per-host identity colour.** Each host gets a colour from a categorical
  palette of `HOST_COLOR_COUNT` (6) slots, defined **per theme** (`--host-0`
  … `--host-5`, dark and light) and kept **clear of the status hues**
  (success/failed/queued/rolled_back), applied to a small **host chip**
  (`.host-mono`) — a colour-tinted **monogram** — at the head of each
  merged-feed row's stack cell, and to the host's pill in the filter drawer. A
  **labelled chip, deliberately not a dot: a dot already means deploy status
  here**, so the identity channel takes a different shape (`hostMonogram`:
  initials of a separated name, e.g. `host-a`→`HA`, else first three letters,
  `argoneon`→`ARG`). The chip rides inside the existing stack cell — no dedicated
  Host column, which was rejected for inflating every row's width — so it costs
  no layout and flows inline at every width. Host colour is the *identity*
  channel; the status left-bar and status badge remain the *state* channel,
  deliberately separate.
  - **Assignment (`assignHostColors`, `app-helpers.js`).** A host's slot is
    chosen deterministically from a name hash (FNV-1a), so the same host
    keeps its colour across sessions, host-set order, host count, and which
    instance is the primary — independent of position. **No two hosts ever
    share a colour while the palette has a free slot**: when two names hash
    to the same slot and slots remain, the later host (in a fixed name order)
    is bumped forward by linear probing. The palette's **slot→hue order is
    interleaved** (not the monotonic cool ramp) because probing hands out
    numerically adjacent slots first, and on a monotonic ramp two adjacent
    slots are the two closest hues and read as the same colour; interleaving
    keeps every adjacent-slot pair ≥~50° apart. Beyond 6 hosts the palette is
    exhausted and colours necessarily repeat — `collectWarnings` flags the
    config once `len(peers)+1 > HOST_COLOR_COUNT`. `ui.HostColorCount` is the
    single Go source of truth, mirrored by the JS constant and the per-theme
    CSS slots (generated by `scripts/gen-host-colors.mjs`).
  - **The chip is also the quick filter.** Clicking a host chip isolates both
    merged views to that host; clicking any chip again clears back to all
    hosts (`toggleHostFilterTo` — a toggle, complementing the drawer's
    multi-select). Because a chip must stay clickable to clear the filter, the
    chips stay visible in multi-host even when a single host is in view (an
    earlier "hide the redundant indicator at one host" rule was dropped for
    exactly this reason). Its full hostname appears on hover (native `title`)
    and on tap (the `data-taptip` bubble).
  - **Active-filter cue.** While a filter is narrowing the view
    (`hostFilterActive`), the visible chips wear a steady selected-state halo
    (a ring in the host colour) so it is obvious the filter is on and any
    chip invites a re-click to clear it. Steady, not pulsing — one halo per
    row pulsing at once read as an alert rather than a filter state.
- **Merged feed.** Deploys/Stacks rows from all selected hosts interleave;
  each row carries its host chip. Peer deploy rows are read-only mirrors: the
  usual *local* fetches (diff/history/health/logs/hooks) are omitted, since the
  primary has none of a peer's detail — but a click never dead-ends, it opens a
  compact **peer-detail** panel (`createPeerDetailPanel`): the commit,
  changed-file count and status the fan-in carries, the **peer's diff loaded
  inline** (fetched through the primary's proxy `GET
  /api/peers/{name}/events/{id}/diffs`, since the browser can't reach the peer
  cross-origin; the audit record now carries the deploy's event `id` for this).
  Opening the peer's own UI is the Hosts drawer's job, not repeated per row; an
  evicted or unreachable diff simply leaves the read-only facts. Local deploy rows
  are inserted at the top as they arrive; peer rows are then re-slotted by
  timestamp (`schedulePeerReflow`) so the merge stays chronological. An active
  host filter is signalled by the lit Hosts control + the count badge + the
  chip halos (there is no separate summary line).
- **Merged roster (Stacks view).** The Stacks view is merged the same way:
  local stacks first, then each peer's stacks appended per host, every row
  carrying the host chip and obeying the same host filter. Peer roster rows
  are read-only (host chip + icon + name, status, last deploy, commit) — no
  jump / logs / hooks / app-link affordance and no expand panel. Only the
  peer rows are rebuilt on a peers refresh (`refreshPeerRosterRows`), so an
  open local audit/health panel is never collapsed by a background poll.
- **Unreachable peer → flagged, not blanked.** In the merged view an
  unreachable peer's last-known rows stay but are dimmed (`.peer-stale`) and
  a stale banner names them ("host-c is unreachable — showing its last-known
  deploys."); other hosts stay live. (This differs from the discarded proxy
  design, where you looked at one host and blanking was right — in a merged
  view, silently dropping a host's rows is worse than an honest stale
  marker.)
- **Reuse over new components** per `ui-design-concept.md`: the drawer
  mirrors the app-link/autosync popover patterns; badges, status tokens and
  the row/table language are the existing ones.

## Console logging (pretty mode)

The fan-in narrates itself in the pretty console log (ADR-0042), styled to
match the deploy-lifecycle lines:

- A startup line — `⇢ multi-host fan-in · N peers · poll Ns` — when the fan-in
  is enabled.
- **Edge-triggered peer reachability**, logged once per up/down transition
  (never every poll, so a healthy fan-in stays silent): `▲ peer host-c
  unreachable — <reason>` (Warn) when a peer stops responding — including a
  peer down from the first poll — and `✓ peer host-c reachable again` (Info)
  on recovery. A peer reachable on first contact is the normal case and logs
  nothing. The transition detection lives in `peers.Poll`
  (`MsgPeerUnreachable` / `MsgPeerReachable`); the pretty rendering is an
  anchor table entry in `internal/prettylog`. In any other log mode the same
  messages print plainly.

## Package layout

- New `internal/peers` package: the fan-in `Registry` (`Poll` / `State` /
  `Hosts`), the `Client` seam + `NewHTTPClient` (reads each peer's
  `/api/v1/snapshot` curated to stacks/health/app_links + `/api/audit`
  best-effort, version-skew tolerant via `json.RawMessage`), the per-peer
  reachability cache (keeps last-known + marks `Stale` on failure), and the
  edge-triggered reachability logging. Wired in `cmd/skipper/main.go` under
  the `ui_enabled` block (a `peers-fanin` poll loop republishing the `peers`
  state on the health-poll cadence, `GET /api/peers`). No change inside
  `internal/deploy`.
- `internal/config`: the `Peer{Name,URL}` type + `peers:` slice with
  `validatePeers`, `host_name` (`config.HostName`, defaults to the OS
  hostname), and a `collectWarnings` entry when `len(peers)+1 > HostColorCount`.
- `internal/events`: `StatePeers` (`"peers"`) added to `AllStateNames`, so the
  merged state ships in the `/api/v1/snapshot` and over SSE like every other
  state.
- `internal/prettylog`: anchor-table entries for the fan-in startup + peer
  reachability lines.
- `internal/ui`: `HostColorCount` (single Go source of truth), `PeersHandler`
  (`GET /api/peers`, takes a `func() any` to avoid a ui→peers→config→ui import
  cycle). The UI itself — Hosts control + drawer, the per-row host chip and its
  click-filter, the merged deploy feed + roster — lives in the embedded
  `index.html` / `app.css`, with the pure host helpers (`assignHostColors`,
  `hostColorIndex`, `hostFilterActive`, `HOST_COLOR_COUNT`) in `app-helpers.js`.
- Depends on ADR-0039's `/api/v1` existing on peers (and, for the merged
  local+peer model, on the primary reading its own read model through the
  same builders — the snapshot+stream single-read-model property ADR-0039
  establishes).

## Testing

- `internal/peers`: fan-in client against `httptest`/fake peers — host
  tagging, merge ordering, a peer returning an error/timeout →
  last-known-kept + marked stale, recovery clears stale, an unknown future
  field ignored (version-skew tolerance), and reachability edges logged once
  per transition (down-from-first-poll once, reachable-first-contact silent).
- `internal/config`: `host_name` default + override; peers validation; the
  palette-overflow warning derived from `ui.HostColorCount`.
- `internal/prettylog`: the fan-in startup + peer up/down anchors render with
  their glyphs and consume their attrs (not raw `key=value`).
- UI unit (`node --test`): `assignHostColors` is stable per name, independent
  of order/count, hands distinct hosts distinct slots while the palette has
  room (repeating only past 6); `hostColorIndex` range/determinism;
  `hostFilterActive`.
- UI e2e (one new mask, next free letter): the merged feed interleaves hosts
  with the per-row chip; clicking a chip isolates to that host and re-clicking
  clears; the Hosts drawer multi-select filters both views; an offline peer
  shows the stale banner and dimmed last-known rows, not a blank. A stub
  `/api/v1` peer (or a second instance) is started by the harness.

## Open questions

1. **Primary→peer auth.** *Resolved for v1:* network-layer — bind each peer's
   `/api/v1` to the LAN and firewall it to the primary's IP (no app change).
   ADR-0039's deferred bearer token stays the follow-up if a stricter posture
   is ever needed.
2. **Poll vs. SSE fan-in for v1.** *Resolved:* poll each peer's snapshot on the
   existing ticker (ships sooner, reuses infrastructure). SSE fan-in for a
   truly live merged feed is a documented later enhancement, not a
   prerequisite.
3. **Per-host colour palette across themes.** *Resolved:* six per-theme
   slots (`--host-0…5`, dark + light) kept clear of the status hues, with a
   deterministic collision-avoiding assignment (see Per-host identity colour)
   so no two hosts share a colour until the palette is exhausted. Per-theme
   contrast tuning of the concrete hues stays open for review; the mechanism
   is fixed.
4. **Filter persistence.** *Resolved:* the selected host subset is **not**
   persisted — it resets to "all hosts" on each load. The filter is a
   transient view control, not a stored preference.
5. **Notifications host prefix.** Could derive from `peers` config instead
   of the current manual per-target prefix — orthogonal, leave as is for now.
