# ADR-0048: Multi-host federated UI (reverse-proxy switcher, not a control plane)

Status: proposed
Date: 2026-07-21

## Context

A homelab running skipper on more than one host (today: nuc + argoneon) has
no single place to look — each host's UI only ever shows that host's own
stacks. ArgoCD's answer to "many hosts, one view" is a central control
plane: one server holds the desired state for every cluster and pushes to
each. That model contradicts skipper's premise everywhere else in this
codebase — deploys are git-driven per host, there is no central authority
that could go down and take every host's deploy path with it
([scope][scope]).

The alternative is federation: keep every host's skipper fully independent
(it clones its own repo, hashes its own state, deploys on its own webhook)
and add a thin UI layer on top that lets one instance show another's. Two
shapes were considered for that layer:

- **Client-side aggregation.** The primary's browser JS fetches each peer's
  `/api/*` directly and renders a merged view. Rejected: peers would need
  CORS opened to the primary's origin (a new, host-specific attack surface
  per peer) and every new UI data shape doubles as a cross-host contract the
  instant a second consumer parses it — the opposite of the low-coupling
  design the rest of this codebase favors.
- **Server-side reverse proxy.** The primary forwards requests to
  `/peer/{name}/...` straight through to the peer's own server, byte for
  byte, including the SSE stream. Chosen: the primary's Go code never parses
  peer data, so there is no shape to keep in sync and no new cross-origin
  surface — from the browser's perspective it is just browsing to a
  different page on the same origin.

[scope]: ../../CLAUDE.md

## Decision

### Reverse proxy, not aggregation

An optional `peers:` list in `skipper.yaml` names other skipper instances by
URL. When set, the primary mounts `httputil.ReverseProxy` handlers at
`/peer/{name}/`, forwarding everything — UI assets, `/api/*`, and the SSE
stream (with response flushing so events still arrive live) — unmodified
except for stripping the path prefix. No `internal/deploy` change; this is
a UI/transport-layer feature, package-isolated in a new `internal/peers`.
Full field-level detail lives in the companion spec,
[`dev-docs/multi-host-spec.md`](../multi-host-spec.md).

### The switcher is the leftmost header control

Selecting a host is a more fundamental question than selecting a view
(Deploys/Stacks) — it changes *whose* data every other control on the page
refers to. It therefore sits ahead of the brand mark, not folded into the
existing view-switch group. Wide viewports get a chip per host (monogram,
name, reachability dot); narrow viewports collapse to a single trigger that
opens a popover list — reusing the app-link feature's existing
trigger-button-opens-popover component (`.link-btn` → `.link-pop`) rather
than building a second one, per the one-look-and-feel rule in
`ui-design-concept.md`. Both placements were validated against an
interactive mockup before writing this ADR, not guessed at.

### A peer's page is unmodified — themed, chromed, and all

Because the proxy forwards bytes rather than data, selecting a peer does
not "load its data into the local UI" — it navigates to the peer's own
`index.html`, which bakes in *that host's* configured theme
(ADR-0021/ADR-0035) and renders with the primary none the wiser about its
contents. This is a deliberate, visible proof that the mechanism really is
a proxy: a federated view that quietly re-skinned peers to match the
primary would be misrepresenting what is actually being shown.

### Unreachable peers show a notice, never stale data

`/api/peers` reports each peer's reachability from a cheap, ~10s-cached
probe. An unreachable peer's chip renders visibly offline; selecting it
shows a plain "unreachable" notice in place of a table. Nothing is cached
or replayed client-side to paper over the gap — a stale deploy table is a
worse failure mode than an honest "can't reach it right now."

### Read-only surface, existing front-auth

The browser only ever talks to the primary, so the primary's existing
front-auth (Authelia in the homelab) covers every proxied view for free;
peers can then firewall their own port to just the primary's IP. No new
auth mechanism, no token, no change to what a deploy can do — federation is
purely a UI convenience layer, in scope under the same
[viz-only constraint][scope] as everything else in this codebase: it adds
no way to trigger, edit, or redeploy a peer, only to look at it.

### Switch, not merge — phase 1 only

A single combined "all hosts" table (one event feed, one stack list with a
host column) is real UI work with an aggregation question this ADR does not
answer. It is deferred to a phase 2 decided after living with the switcher,
not designed speculatively now.

## Open questions (block implementation)

1. **Returning from a peer.** Every skipper ships the same self-contained
   UI (ADR-0035); a peer's page only renders its own switcher if *that*
   host also has `peers:` configured. In the intended one-primary setup
   (only the primary lists peers), a page loaded via `/peer/argoneon/` has
   no switcher of its own — only the browser back button. Needs a decision
   between requiring reciprocal `peers:` config on every federated host, or
   making the switcher detect a `/peer/<name>/` path prefix client-side
   (independent of that host's own config) and render a minimal "back to
   primary" affordance. The latter solves *back* but not *sideways* to a
   third host without the full peer list.
2. **Chip-row scale limit.** Unvalidated past a handful of peers; whether
   the popover collapse should trigger by host *count* as well as by
   viewport width is undecided.

## Consequences

- **No central failure point.** The primary is a convenience layer, not a
  dependency — if it is down, every host still deploys on its own webhook
  and still serves its own UI directly at its own address.
- **Version-skew tolerant by construction.** A peer on an older skipper
  build just serves its older UI through the proxy; the primary has no
  shape to keep compatible because it never parses what it forwards.
- **New `internal/peers` package**: config type, reachability cache, and
  the proxy handler, wired into `cmd/skipper/main.go` under the
  `ui_enabled` block — no change to the deploy path or its invariants.
- **PWA/service-worker care needed.** The SW registers at scope `/`; peer
  views under `/peer/...` must be excluded from SW caching (network-only
  route) so one host's cached shell can never shadow another's.
- **Two open questions block implementation** (see above) — this ADR
  records the shape of the decision, not a green light to build; the
  peer-side "getting back" gap in particular needs resolving first.

## References

- Spec: `dev-docs/multi-host-spec.md`
- Builds on: ADR-0021 (configurable UI themes), ADR-0035 (self-contained,
  same-origin UI assets — the property that makes "just proxy it" work),
  ADR-0041 (the app-link popover component this reuses).
- Scope constraint: [`CLAUDE.md`][scope] — viz-only, deploys stay
  git-driven; this ADR adds no trigger/edit surface for a peer.
