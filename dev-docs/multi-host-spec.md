# Feature Spec: Multi-Host Federated UI

Status: proposal (not accepted)
Date: 2026-07-18

## Goal

One pane of glass for a multi-host homelab (nuc + argoneon today): a single
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
# nuc's skipper.yml (the federating instance)
peers:
  - name: argoneon
    url: http://argoneon:8001
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
- The **UI** grows a host switcher in the header (chips: local + one per
  peer, from a new `/api/peers` endpoint). Selecting a peer loads that
  peer's UI in place via the proxy prefix. Phase 1 is switch-not-merge;
  a merged "all hosts" table is explicitly deferred (see open questions).
- Auth: peers sit behind the primary's existing front-auth (Authelia)
  because the browser only ever talks to the primary. Peers can then
  firewall their own port to the primary's IP (same pattern as
  signal-api's `allowedClientIP` on nuc).

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
   config? Today the `[nuc]`/`[argoneon]` prefix is manual per-target
   config — orthogonal, leave as is.
