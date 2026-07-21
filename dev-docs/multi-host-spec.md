# Feature Spec: Multi-Host Federated UI

Status: proposal (not accepted). Header placement and mobile behavior
validated 2026-07-21 against an interactive mockup; two gaps surfaced by
that mockup are recorded in Open Questions below.
Date: 2026-07-18

## Goal

One pane of glass for a multi-host homelab (host-a + host-b today): a single
skipper UI that shows every host's stacks, deploy history, health, and
events. ArgoCD's answer is a central control plane managing many clusters;
skipper's answer stays decentralized — **every host keeps running its own
full, independent skipper**, and one instance additionally *federates* the
others' UIs.

Non-goals: central deploy orchestration, cross-host `depends_on`, moving
stacks between hosts, an agent protocol, shared state. Deploy logic remains
strictly per-host; if the federating instance dies, every host still
deploys and still serves its own UI.

## User model

```yaml
# host-a's skipper.yml (the federating instance)
peers:
  - name: host-b
    url: http://host-b:8001
```

- `peers` is optional; without it nothing changes. Any instance may list
  peers (including mutually), but the intended homelab setup is one
  "primary" UI host.
- `name` is the display label (host chip in the UI); `url` is the peer's
  skipper base URL on the LAN.

## Mechanism: reverse proxy, not aggregation

The federating instance mounts a path-prefixed reverse proxy per peer:

```
/peer/{name}/            → peer's UI + API + SSE, proxied 1:1
```

- **No cross-host merge logic on the server.** The primary does not parse,
  cache, or re-emit peer data — it forwards bytes (including the SSE
  stream, with flushing). This keeps the server change tiny and version-
  skew-tolerant: a peer on an older skipper just serves its older UI.
- The **UI** grows a host switcher: the **leftmost** header control, ahead
  of even the brand mark (separated by a hairline). Which host's data is on
  screen is a more basic question than which view (Deploys/Stacks) is
  active, so it leads. Wide: a chip per host (local + peers, from a new
  `/api/peers` endpoint), monogram + name + a reachability dot, current
  host highlighted. Narrow: collapses to a single trigger button (active
  host's monogram + chevron) opening a popover list — **the same
  trigger-button-opens-popover pattern the app-link feature already built**
  for its own multi-host case (`internal/ui/static/index.html`:
  `.link-wrap > .link-btn` → `.link-pop`); reuse that component's CSS
  rather than adding a second one, per `ui-design-concept.md`. Selecting a
  peer loads that peer's UI in place via the proxy prefix. Phase 1 is
  switch-not-merge; a merged "all hosts" table is explicitly deferred (see
  open questions).
- Auth: peers sit behind the primary's existing front-auth (Authelia)
  because the browser only ever talks to the primary. Peers can then
  firewall their own port to the primary's IP (same pattern as
  signal-api's `allowedClientIP` on host-a).

## Behavior details

- Peer health: `/api/peers` reports reachability (one cheap probe of the
  peer's existing status endpoint, cached ~10s). Unreachable peer → its
  chip renders greyed "offline"; selecting it shows an offline notice, no
  stale data.
- The proxy strips/rewrites nothing except the path prefix; the UI must
  therefore keep using **relative** URLs (it already does — single embedded
  `index.html`, no absolute paths). A regression test locks this in.
- PWA/service worker: the SW registers with scope `/`; peer views under
  `/peer/...` are excluded from SW caching (network-only route) so one
  host's cached UI never shadows another's.
- Timeouts: proxy requests bound to the existing command-timeout budget;
  SSE excepted (long-lived, closed when the client disconnects).

## Package layout

- New `internal/peers` package: config type, reachability cache, and the
  `httputil.ReverseProxy` handler; wired in `cmd/skipper/main.go` under the
  `ui_enabled` block. No changes inside `internal/deploy`.

## Testing

- `internal/peers`: handler tests with `httptest` peer servers — path
  rewrite, SSE passthrough (flush behavior), offline peer → 502 with a
  JSON body the UI understands, reachability cache expiry.
- UI e2e (one mask): header shows peer chips, switching loads the peer
  view, offline chip state. Peer is a second skipper instance started by
  the harness.

## Open questions

1. Merged all-hosts table (host column, combined event feed) as phase 2?
   Requires client-side aggregation over `/peer/*/api/...` — doable with
   no server change, but real UI work. Proposed: ship switcher first,
   decide after living with it.
2. Should notifications gain the host prefix automatically from `peers`
   config? Today the `[host-a]`/`[host-b]` prefix is manual per-target
   config — orthogonal, leave as is.
3. **Getting back from a peer isn't solved yet.** Every skipper instance
   ships the same self-contained UI (ADR-0035), so a peer's page — served
   fresh by the peer itself, only forwarded through the primary's proxy —
   only shows its own switcher if *that* host also has `peers:` configured.
   In the intended one-primary setup (only host-a lists peers), a page loaded
   via `/peer/host-b/` has no switcher at all once you're on it, only
   the browser's back button. Two ways out: (a) require reciprocal
   `peers:` on every federated host, so each one's own page always renders
   a switcher; or (b) make the switcher **path-aware, not config-aware**
   on the served side — the shared JS bundle detects a `/peer/<name>/`
   path prefix client-side (independent of that host's own `peers:` config)
   and renders a minimal "back to primary" control. (b) needs no config
   changes on peers, but only gets you *back*, not *sideways* to a third
   host, without the full chip list. Needs a decision before implementation.
4. **Chip-row scale limit.** Three hosts still reads fine as a chip row at
   full width. Unclear where that stops working — four or five peers may
   want the same trigger+popover collapse even on desktop. No threshold
   picked; revisit once a real deployment has more than a couple of peers.
