# ADR-0048: Multi-host federated UI (read-data fan-in, not a reverse proxy)

Status: proposed
Date: 2026-07-21 (reworked same day — supersedes the reverse-proxy version)

## Context

A homelab running skipper on more than one host (today: host-a + host-b) has
no single place to look — each host's UI only shows that host's own stacks.
The goal is one merged overview: *is anything wrong across all my hosts right
now.* ArgoCD's answer is a central control plane, which contradicts
skipper's premise everywhere else — deploys are git-driven per host, and no
central authority should be able to fail and take every host's deploy path
with it ([scope][scope]).

So the constraint is federation, not centralization: every host keeps
running its own full, independent skipper, and one instance additionally
shows the others. The question this ADR settles is *how* that one instance
gets at the others' data.

An earlier version of this ADR chose a **reverse proxy**: mount each peer's
whole page at `/peer/{name}/` and forward bytes (UI + API + SSE) unmodified.
That was reworked away before any implementation, because it optimized for a
tiny server change at the cost of the actual goal:

- **It never merged.** "Switch, not merge" means inspecting hosts one at a
  time — not a pane of glass. The merged overview was deferred to a phase 2
  the proxy made harder, since aggregation would have to claw structured
  data back out of proxied pages.
- **Its hardest open questions were symptoms.** Forwarding a peer's whole
  self-contained UI (ADR-0035) meant the peer's page knew nothing of the
  federation → an unsolved "how do you navigate back from a peer," and a
  per-host theme reskin on every hop (rationalized as "honest," really a UX
  smell for an overview tool).
- **It forwarded peer *write* endpoints** (autosync toggle, icon refresh),
  quietly widening the read-only scope.

## Decision

**Fan the peers' read data into the primary and render one merged UI.** A
host becomes a dimension on the primary's own model, not a separate page you
navigate to.

### Read-data fan-in over the versioned API

An optional `peers:` list in the primary's `skipper.yaml` names other
instances by URL. For each, the primary reads that peer's **versioned
read-only `/api/v1`** (ADR-0039), tags every stack/event/health record with
its host, and merges it with its own local data into one ephemeral model
that its existing UI renders. The browser still opens one connection, to the
primary; rows simply gain a `host` field. New package `internal/peers`
(config + a fan-in client behind a consumer-side interface + the
host-tagging merge); no `internal/deploy` change. Field-level detail lives
in [`dev-docs/multi-host-spec.md`](../multi-host-spec.md).

This makes **ADR-0039 the substrate, and multi-host its first concrete
consumer** — the reason that read API was parked ("wait for a consumer")
is now met.

### Parsing peer data is correct here — because the contract is versioned

The reverse-proxy design's proudest property was "the primary never parses
peer data." That was only a virtue in the *absence* of a stable contract.
`/api/v1` is versioned and additive-only (ADR-0039): the primary can parse
it safely, and version skew is handled *by the contract* (an unknown future
field is ignored) rather than by refusing to parse. Choosing fan-in commits
us to shipping `/api/v1` on every federated host.

### One UI, a host dimension — never a peer's page

The rendered UI is always the primary's own: one theme (ADR-0021), one
chrome. Consequences for the two problems that sank the proxy design:

- **No "getting back."** You never navigate to a peer's page, so there is
  nothing to get back from — the switcher is a filter over one model. The
  peer-return-path question simply stops existing.
- **No per-host reskin.** The primary renders peer data in its own theme.

The host surfaces as: a **per-host identity colour** (a categorical palette
kept clear of the status hues) on the host badge, so the merged feed's Host
column is a scannable colour stripe; and a compact **Hosts control** in the
right-hand header group — the logo keeps the top-left corner — opening one
popover that both multi-selects (filter the feed) and links out to each
host's own full UI for a live deep-dive. Placement, the compact-dropdown
(vs. a permanent chip row that wouldn't fit the busy header), multi-select,
and the colours were all validated against an interactive mockup before this
rewrite, not guessed.

### Read-only, and live-watch is a plain link

The primary only ever consumes each peer's *read* API — it cannot expose a
peer's write endpoints, so the scope leak of the byte-proxy is gone. To
watch a peer deploy live (the one thing a merged, possibly-polled overview
gives up), the Hosts popover links to that peer's own UI directly — a
hyperlink, no proxy.

### Poll first, stream later; unreachable is flagged, not blanked

v1 polls each peer's `/api/v1` snapshot on the existing ticker cadence (no
new persistent connection); SSE fan-in for a truly live merge is a later
enhancement. An unreachable peer's last-known rows stay in the merged feed
but dimmed, with a stale banner naming them — other hosts stay live. (In a
merged view, silently dropping a host's rows is worse than an honest stale
marker; this is the opposite call from the proxy design, correctly, because
there you looked at one host at a time.)

## Open questions

1. **Primary→peer auth.** Network-layer (bind peer `/api/v1` to LAN,
   firewall to the primary's IP) is the homelab-simplest and needs no app
   change; ADR-0039's deferred bearer token is the alternative, and
   multi-host is the concrete consumer that would justify building it.
2. **Poll vs. SSE fan-in for v1** — polling ships sooner and reuses the
   ticker; SSE gives a live merged feed at the cost of a persistent peer
   subscription. Proposed: poll for v1.
3. **Per-host colour palette across themes** — a fixed categorical palette
   keeps "host-a is blue" stable but needs a contrast check on every theme's
   light and dark grounds.

## Consequences

- **The actual goal ships in phase 1.** The merged overview *is* the feature,
  not a deferred phase 2; "filter to one host" and "see everything" are the
  same model, differently grouped.
- **No reciprocal config.** Only the primary needs `peers:`. Because peers
  are plain data sources (not pages that must render their own switcher),
  the "every host needs a peer list to navigate back" problem never arises —
  a direct simplification over the proxy design.
- **No central failure point.** The primary is a convenience layer; if it is
  down, every host still deploys on its own webhook and serves its own UI at
  its own address.
- **Depends on ADR-0039.** `/api/v1` must exist on every federated host; the
  primary's own merged model is built through the same read-model builders,
  which is the snapshot+stream single-source property ADR-0039 establishes.
- **The primary now parses peer data** — accepted, and safe, precisely
  because `/api/v1` is a versioned, additive-only contract.
- **New `internal/peers` package**; no change to the deploy path or its
  invariants (the fan-in is strictly read, on the primary and on peers).
- **Live peer-deploy watching is a click away, not embedded** — the Hosts
  popover links to the peer's own UI. Embedding the full peer UI was the
  root of the proxy design's problems and is deliberately given up.

## References

- Spec: `dev-docs/multi-host-spec.md`
- Depends on: ADR-0039 (read-only `/api/v1` — the fan-in substrate; this is
  its first consumer).
- Builds on: ADR-0021 (configurable UI themes), ADR-0035 (self-contained,
  same-origin UI — one bundle renders the merged view), ADR-0041 (the
  app-link popover pattern the Hosts control mirrors).
- Scope constraint: [`CLAUDE.md`][scope] — viz-only, deploys stay
  git-driven; the primary reads from peers and never commands them.

[scope]: ../../CLAUDE.md
