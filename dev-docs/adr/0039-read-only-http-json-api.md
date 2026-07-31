# ADR-0039: Read-only HTTP JSON API (snapshot + stream)

Status: accepted. Parked for want of a concrete consumer when written;
**[ADR-0048](0048-multi-host-federated-ui.md) (multi-host federated UI) is
that consumer** — its read-data fan-in polls this API, so it ships first.
**Core implemented 2026-07-21**: `GET /api/v1/snapshot` (the whole state as
one JSON document, built from the same collector as the SSE stream so the two
cannot drift). The remaining per-topic endpoints in this ADR
(`/api/v1/stacks`, `/api/v1/deploys`, …) are additive within this design and
deferred until a consumer needs them.
**Amended 2026-07-27 — the UI's initial paint moved back onto the stream.**
Fetching the baseline separately, as the UI did from 2026-07-21, split
"read the current state" and "start listening" into two operations on two
connections, leaving a window between them in which a published change reached
nobody and was never re-sent (a queued deploy could stay invisible until the
next run). The stream now subscribes first and *then* writes the baseline from
the same `collect`, so the two are one ordered operation and a change racing a
connect is delivered right after the baseline. `GET /api/v1/snapshot` is
unchanged and remains the read surface for external consumers — notably the
multi-host fan-in — it is simply no longer the UI's connect path, so the
no-drift property is kept by the shared collector rather than by shared use.
**Amended 2026-07-31 — the subscribe-first rule covers deploy events too.**
The handler had kept the original order for the deploy history (replay, then
subscribe), leaving the same gap for a deploy event published mid-connect. It
now subscribes to the deploy broadcaster before reading the history snapshot
and drops live events whose monotonic ID the replay (or a Last-Event-ID
resume) already covered, so a racing deploy event is delivered right after the
replay instead of staying invisible until the next reconnect.
Date: 2026-07-19

## Context

Everything skipper knows about the world it manages — the stack roster, deploy
history, container health, orphans, autosync and queue state — is today
reachable over HTTP in exactly **one** shape: the SSE stream at
`GET /api/events`. Deploy events and per-topic *state snapshots* (`stacks`,
`health`, `orphans`, `autosync`, `queue`, `upcoming`, `healthwatch`) are
multiplexed onto that one stream, each under its own SSE event name. A few
plain-JSON GETs exist alongside it (`/api/version`, `/api/audit`, `/api/queue`,
`/api/autosync`, `/api/events/{id}/diffs`, `/api/icons`), but they grew
per-feature, are unversioned, and don't cover the headline question a script or
a second dashboard asks first: *"which stacks does this skipper manage, and what
is each one's last outcome?"* Answering that today means opening an SSE stream
and fishing the `stacks` event out of it — a poor fit for `curl`, a cron check,
or a status page.

At the same time the SSE stream carries the initial paint by *replaying* it: a
freshly connected UI client gets the whole event history plus a burst of the
current state snapshots (`stateSnapshot.collect`), then live deltas. That
couples "give me the current state" to "subscribe to changes" — there is no way
to ask for just the former.

We want a real, documented, scriptable read surface without turning skipper into
something it isn't. The [viz-only scope][scope] is a hard constraint: skipper is
an overview tool, deploys are git-driven. The API adds **no** way to trigger,
force, redeploy, or edit — the only deploy trigger remains the push webhook.

[scope]: ../../CLAUDE.md

## Decision

### A versioned, read-only REST surface under `/api/v1/`

Add plain-JSON GET endpoints for the current state of each read model, plus the
existing runtime toggles that were already within scope (autosync override, icon
refresh). Full endpoint list, JSON schemas and status codes live in the
companion spec, [`dev-docs/http-api-spec.md`](../http-api-spec.md). The headline
additions:

- `GET /api/v1/stacks` — the roster inventory (every managed stack, `disabled`
  flag, last deploy outcome), the same data the Stacks view renders.
- `GET /api/v1/stacks/{name}` — one stack aggregated across roster, health,
  autosync-effective and its newest audit record.
- `GET /api/v1/deploys` — deploy-event history as paginated JSON (today only
  replayable via SSE).
- `GET /api/v1/health`, `GET /api/v1/orphans` — the health and orphan snapshots
  as one-shot GETs.
- `GET /api/v1/snapshot` — all state blocks in one document, for a dashboard or
  status check that wants the whole picture in a single call.

`/metrics` (Prometheus) and `/healthz` (liveness) stay where they are, unversioned
— they are not part of this contract and have their own established consumers.

### One read model, two transports — REST and SSE never diverge

The value is not the endpoints, it is that the REST body and the SSE state event
for a given topic are **the same serialized value**. A new `internal/httpapi`
package (or an extension of the existing `stateSnapshot` collector) owns each
read model and its JSON shape. `stateSnapshot.collect` already assembles every
snapshot for the SSE burst; the REST handlers call the *same* builders. There is
no second, hand-maintained JSON shape to drift out of sync. When the `stacks`
model gains a field, both the SSE `stacks` event and `GET /api/v1/stacks` gain
it at once, by construction.

### Snapshot + stream: the API is the initial paint, SSE carries deltas

The clean division of labour is the standard **snapshot + stream** pattern:

- **`GET /api/v1/…` answers "what is true now."** One-shot, cacheable, scriptable.
- **`GET /api/events` (SSE) carries "what changed."** Live push stays SSE — a
  polling UI would be a regression; deploys must stream in.

This is what lets the UI dogfood the API instead of leaving it as dead weight
beside the UI's real data source (see Consequences). The SSE stream keeps its
history/state replay for now (backward compatible, and Last-Event-ID resume
depends on it); moving the UI's initial paint onto REST is an *option* the spec
describes, not a prerequisite for shipping the endpoints.

### Read-only, plus the two in-scope runtime toggles — nothing else

`POST` exists only where it already did and stays within the viz-only scope:

- `POST /api/v1/autosync` — a non-persistent autosync override (does **not**
  bypass git; it gates whether a *git-driven* deploy is allowed to run, per
  [ADR-0016][adr16]).
- `POST /api/v1/icons/refresh` — clears the icon cache.

There is deliberately no `POST /redeploy`, no force-sync, no config write. A
consumer that wants a deploy pushes to git; the webhook does the rest.

[adr16]: 0016-autosync-and-queue-via-leave-dirty.md

### Auth is deferred, not designed in

In the homelab, skipper sits behind Authelia and the SSE stream already relies
on that cookie SSO. The scriptable surface would benefit from a bearer token
(cookie SSO is awkward for `curl` from another host), but that is a separate
decision. v1 ships with the same front-door auth as everything else and the spec
notes token auth as the expected follow-up. This keeps the ADR about *shape*,
not about a homelab auth choice that may never be needed.

### Versioning makes the JSON a contract

The `/api/v1/` prefix marks these bodies as a stability promise: additive
changes only within v1, breaking shape changes go to `/api/v2/`. The pre-existing
unversioned routes (`/api/events`, `/api/audit`, …) keep working; new consumers
are pointed at `/api/v1/`. A short `docs/api.md` documents the contract for
users.

## Consequences

- **New `internal/httpapi` package** (small, one job): owns the read-model JSON
  shapes and the REST handlers, fed by the same builders that seed the SSE
  snapshot. `stateSnapshot.collect` and the REST handlers share those builders,
  so the two transports cannot drift.
- **The UI can dogfood the API.** Migrating the initial paint to
  `GET /api/v1/snapshot` (then SSE for deltas only) is a natural follow-up: it
  removes the initial-state burst from the SSE handler and makes external
  consumers eat exactly the JSON the UI paints. Left as a follow-up so shipping
  the read surface doesn't require touching the live path.
- **A stable JSON contract to maintain.** Once `/api/v1/stacks` is scripted
  against, its shape is owned. The `/v1/` prefix and additive-only rule are the
  guard; a `dev-docs/http-api-spec.md` review gate keeps shapes deliberate.
- **No new persistence, no new poll, no new deploy path.** Every endpoint reads
  models that already exist; correctness invariants around deploys, rollback and
  state are untouched because nothing here writes deploy state.
- **Auth gap is explicit.** Until token auth lands, the API is exactly as
  exposed as the UI — fine behind Authelia, to be revisited before any
  internet-facing use.
