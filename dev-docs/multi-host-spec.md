# Feature Spec: Multi-Host Federated UI

Status: proposal (not accepted). **Reworked 2026-07-21**: the earlier
reverse-proxy switcher is dropped in favour of read-data fan-in — see the
"Why not a reverse proxy" section and [ADR-0048](adr/0048-multi-host-federated-ui.md).
UI (logo placement, compact Hosts control, multi-select filter, per-host
identity colours) validated against an interactive mockup.
Date: 2026-07-18 (reworked 2026-07-21)

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
  `selected/total` count badge + chevron — that opens **one popover doing
  two jobs**: (1) a **multi-select filter** (a checkbox per host, plus a
  "Select all" shortcut) over the merged feed; (2) per host, its
  reachability and a link to **open that host's own full UI** (the live
  deep-dive). Filtering to a subset lights the button (glyph + badge in the
  accent colour) so an active filter is visible without the chips being
  permanently on screen. A compact dropdown (not a permanent chip row) is
  chosen because the real header's right side is already busy; it mirrors
  the autosync button's open-a-panel pattern.
- **Per-host identity colour.** Each host gets a fixed colour from a
  categorical palette kept **clear of the status hues**
  (success/failed/queued/rolled_back), applied to the host badge (monogram
  chip + tinted pill) and to the host's monogram in the filter popover. In
  the merged feed the leftmost **Host column** becomes a scannable colour
  stripe — you pick out one host's rows at a glance. Host colour is the
  *identity* channel; the status left-bar and status badge remain the
  *state* channel, deliberately separate. The palette must be derived per
  theme (or chosen to read on both light and dark grounds) so "host-a is
  blue" holds across themes — see Open Questions.
- **Merged feed.** Deploys/Stacks rows from all selected hosts interleave;
  each row carries its host badge. When exactly **one** host is selected the
  Host column hides (redundant). A one-line filter summary above the table
  states what is shown ("N deploys · all 3 hosts" / "…filtered to host-a,
  host-c").
- **Unreachable peer → flagged, not blanked.** In the merged view an
  unreachable peer's last-known rows stay but are dimmed and a stale banner
  names them ("host-c unreachable — showing last known state, synced Nm
  ago"); other hosts stay live. (This differs from the discarded proxy
  design, where you looked at one host and blanking was right — in a merged
  view, silently dropping a host's rows is worse than an honest stale
  marker.)
- **Reuse over new components** per `ui-design-concept.md`: the popover
  mirrors the app-link/autosync popover patterns; badges, status tokens and
  the row/table language are the existing ones.

## Package layout

- New `internal/peers` package: the `peers` config type, a per-peer
  reachability + snapshot **fan-in client** over `/api/v1` (behind a
  consumer-side interface so tests inject a fake peer), and the merge that
  tags records with their host. Wired in `cmd/skipper/main.go` under the
  `ui_enabled` block. No change inside `internal/deploy`.
- Depends on ADR-0039's `/api/v1` existing on peers (and, for the merged
  local+peer model, on the primary reading its own read model through the
  same builders — the snapshot+stream single-read-model property ADR-0039
  establishes).

## Testing

- `internal/peers`: fan-in client against `httptest` peer servers serving
  canned `/api/v1` JSON — host tagging, merge ordering, a peer returning
  an error/timeout → last-known-kept + marked stale, reachability cache
  expiry, an unknown future field ignored (version-skew tolerance).
- UI unit (`node --test`): host-colour assignment is stable per name;
  merged-feed filtering by the selected host set; single-host → host column
  hidden.
- UI e2e (one new mask): merged feed shows host badges in host colours; the
  Hosts control filters (multi-select) and the summary/host-column react;
  an offline peer shows the stale banner not a blank. A second skipper
  instance (or a stub `/api/v1`) is the peer, started by the harness.

## Open questions

1. **Primary→peer auth.** Homelab-simplest: bind each peer's `/api/v1` to
   the LAN and firewall it to the primary's IP (network-layer auth, no app
   change). The alternative is ADR-0039's deferred bearer-token — multi-host
   is the concrete consumer that would justify building it. Decide before
   implementation.
2. **Poll vs. SSE fan-in for v1.** Polling reuses the existing ticker and
   ships sooner; SSE fan-in gives a truly live merged feed but adds a
   persistent peer subscription with reconnect/backoff. Proposed: poll for
   v1, SSE as a follow-up.
3. **Per-host colour palette across themes.** A fixed categorical palette is
   simplest and keeps "host-a is blue" stable, but must be validated for
   contrast on every theme's light and dark grounds; deriving from theme
   tokens keeps it in-system but risks two hosts landing on near hues.
   Needs a concrete palette + a contrast check.
4. **Filter persistence.** Persist the selected host subset per browser
   (like the theme override), or reset to "all hosts" on each load? Small
   call, left open.
5. **Notifications host prefix.** Could derive from `peers` config instead
   of the current manual per-target prefix — orthogonal, leave as is for now.
