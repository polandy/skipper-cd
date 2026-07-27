# HTTP JSON API spec

Status: implemented (ADR-0039), living reference — kept in sync with the API.

A read-only, versioned HTTP surface for scripts, status pages and second
dashboards. Design rationale and the scope constraints are in
[ADR-0039](adr/0039-read-only-http-json-api.md). This file is the endpoint and
schema reference.

**Scope reminder:** read-only. skipper is a visualization tool; deploys are
git-driven. There is no trigger/redeploy/force-sync/config-write endpoint — the
only `POST`s are the two pre-existing runtime toggles below, neither of which
bypasses git. The push webhook remains the sole deploy trigger.

## Principles

- **Versioned.** Everything is under `/api/v1/`. Within v1 only additive changes
  (new fields, new endpoints); a breaking shape change goes to `/api/v2/`.
- **One read model, two transports.** Each JSON body is the *same serialized
  value* the SSE stream publishes under its state-event name. REST handlers and
  the SSE snapshot (`stateSnapshot.collect`) call the same builders, so the two
  can never drift (ADR-0039).
- **Snapshot + stream.** `GET /api/v1/…` answers "what is true now"; the SSE
  stream `GET /api/events` carries "what changed." Live push stays SSE.
- **JSON only.** `Content-Type: application/json`, UTF-8. Errors use standard
  HTTP status codes with a `{"error": "..."}` body.
- **Writes are same-origin only.** The two `POST`s carry no token of their own,
  so a request a browser makes from *another site* is refused with `403`
  (`Sec-Fetch-Site`, falling back to `Origin`). Without this, any page a viewer
  opens could pause autosync globally — a change whose effect (nothing deploys
  any more) stays invisible until someone notices. Non-browser clients send
  neither header and are unaffected, so the documented `curl` calls keep
  working. Reads are never gated.

## Endpoint summary

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/v1/snapshot` | All state blocks in one document |
| GET | `/api/v1/stacks` | Roster inventory of every managed stack |
| GET | `/api/v1/stacks/{name}` | One stack, aggregated across models |
| GET | `/api/v1/deploys` | Deploy-event history (paginated) |
| GET | `/api/v1/deploys/{id}` | One deploy event, including diffs |
| GET | `/api/v1/health` | Container-health snapshot |
| GET | `/api/v1/orphans` | Orphan-project snapshot |
| GET | `/api/v1/autosync` | Autosync + queue snapshot |
| POST | `/api/v1/autosync` | Set a non-persistent autosync override |
| POST | `/api/v1/icons/refresh` | Clear the icon cache |
| GET | `/api/v1/audit?stack={name}` | Per-stack audit records |
| GET | `/api/v1/version` | Build info |

Unchanged and *not* part of the v1 contract (own consumers, own stability):
`GET /api/events` (SSE), `GET /healthz`, `GET /metrics`, `GET /api/icons/{stack}`.

## Endpoints

### `GET /api/v1/snapshot`

The whole current picture in one call — the REST equivalent of the baseline the
SSE stream sends on connect, built from the same collector. Optional subsystems
are present only when enabled.

```json
{
  "stacks":     [ /* Entry, see /stacks */ ],
  "deploys":    [ /* recent DeployEvent, newest first, capped */ ],
  "health":     { /* see /health */ },
  "orphans":    { /* see /orphans */ },
  "autosync":   { /* see /autosync */ },
  "queue":      { "count": 0, "pending": [] },
  "upcoming":   { /* run plan, ADR-0024 */ },
  "healthwatch":{ /* see below */ },
  "version":    { /* see /version */ }
}
```

`health`, `orphans` and `healthwatch` are omitted when their subsystem is
disabled — a consumer must treat missing keys as "feature off," not "empty."

### `GET /api/v1/stacks`

The roster inventory: every stack skipper owns (discovered set in
stack-discovery mode, else the host `stacks:` list), including never-deployed and
`disabled: true` stacks. Same model as the Stacks view
([`stack-roster-spec.md`](stack-roster-spec.md)); array element is `roster.Entry`:

```json
[
  {
    "name": "traefik",
    "disabled": false,
    "last_status": "deployed",
    "last_at": "2026-07-19T09:12:44Z",
    "last_commit": "9878db3c"
  },
  { "name": "old-stack", "disabled": true }
]
```

- `last_status` empty / absent ⇒ the stack has never deployed.
- Order: enabled first (alphabetical), then disabled (alphabetical).
- No `icon` field — resolve it from `name` via `GET /api/icons/{name}`.

### `GET /api/v1/stacks/{name}`

One stack aggregated across the read models — the "detail" a `stacks/{name}`
status check wants without four calls:

```json
{
  "name": "traefik",
  "disabled": false,
  "config": {
    "watch_dirs": ["traefik"],
    "env_files": ["traefik/.env"],
    "deploy_health_check": { "url": "http://…/ping", "timeout_seconds": 30 },
    "depends_on": ["authelia", "crowdsec"]
  },
  "autosync": { "effective": true, "source": "global" },
  "last_deploy": { /* newest DeployEvent for this stack, or null */ },
  "health": { /* this stack's slice of the health snapshot, or null */ }
}
```

`config` reflects the stack's deploy-shaping fields only; it never exposes
secrets (env-file *contents* are never returned, only paths). `404` if the name
is not in the current stack set.

### `GET /api/v1/deploys`

Deploy-event history as paginated JSON. Query params:

| Param | Meaning | Default |
|-------|---------|---------|
| `stack` | filter to one stack | all |
| `limit` | max events, 1–500 | 100 |
| `before` | return events with `id <` this (page backwards) | newest |

Body is a `DeployEvent` array, newest first. `diffs` and `commits` are stripped
here (as in the SSE payload); fetch them per-event.

```json
[
  {
    "id": 812,
    "timestamp": "2026-07-19T09:12:44Z",
    "stack": "traefik",
    "status": "deployed",
    "duration_ms": 4213,
    "changed_files": ["traefik/docker-compose.yml"],
    "has_diffs": true
  }
]
```

### `GET /api/v1/deploys/{id}`

One deploy event **including** `diffs` and `commits` (the on-demand detail the UI
loads via `/api/events/{id}/diffs`). `404` if the id is unknown or aged out of
history.

### `GET /api/v1/health`

The container-health snapshot (`health.Snapshot`, ADR-0027) — per-stack container
states as polled while a UI/consumer is connected. `404`/omitted-from-snapshot
semantics: returns `503` with `{"error":"health polling disabled"}` when the
feature is off, so a script can distinguish "off" from "all healthy."

### `GET /api/v1/orphans`

The orphan-project snapshot (`orphans.Snapshot`, ADR-0036): compose projects
running on the host that the current stack set does not account for, with their
containers. `503` when orphan detection is disabled.

### `GET /api/v1/autosync` and `POST /api/v1/autosync`

Unchanged behaviour from today's `/api/autosync`, re-homed under `/v1/`.

- **GET** → the autosync controller snapshot (global + per-stack effective
  state) plus the queue view.
- **POST** `{ "scope": "global"|"stack", "stack": "name", "enabled": true }` →
  sets a **non-persistent** runtime override (reset on restart), publishes the
  new state, and drains the queue if the change enables sync. This gates whether
  a *git-driven* deploy may run; it never triggers one outside git (ADR-0016).

### `POST /api/v1/icons/refresh`

Clears the server-side icon cache. Re-homed from `/api/icons/refresh`.

### `GET /api/v1/audit?stack={name}`

Per-stack durable audit records (append-only NDJSON, ADR-0033), newest first.
`stack` is required. Re-homed from `/api/audit`.

### `GET /api/v1/version`

```json
{ "version": "0.15.0", "commit": "9878db3c", "built": "2026-07-18T…" }
```

## Auth

v1 ships behind the same front door as the UI (Authelia in the homelab); the SSE
stream already depends on that cookie SSO. A bearer-token mode for scriptable
`curl` access from another host is the expected follow-up (ADR-0039) and is
**not** part of v1. Do not expose the API to the internet without it.

## Errors

| Status | When |
|--------|------|
| `400` | malformed query/body |
| `404` | unknown stack name or deploy id |
| `503` | a queried optional subsystem (health/orphans) is disabled |

Body on error: `{"error": "human-readable reason"}`.

## Should the UI use these endpoints?

Partly, and deliberately so.

- **Live deltas stay on SSE.** The UI's value is push — deploys stream in. A
  poll-based UI would be a regression, so `GET /api/events` remains the delta
  transport and does not move to polling.
- **The initial paint stays on the stream.** Splitting it out (`GET
  /api/v1/snapshot` for the initial state, SSE for changes only) was tried and
  reverted: subscribing and reading the baseline have to be one ordered
  operation, or a change published between them reaches nobody (ADR-0039
  amendment). The shapes still cannot rot, because the stream's baseline and
  this endpoint are built by the same collector.

That migration is a **follow-up**, not a prerequisite: the endpoints ship and are
useful (scripts, status pages) before the UI's initial paint is moved onto them.

## Implementation notes

- New `internal/httpapi` package owns the handlers and the shared JSON builders;
  `stateSnapshot.collect` (SSE burst) and the REST handlers call the *same*
  builders so REST and SSE never diverge.
- Register routes in a `registerAPIRoutes(mux, …)` registrar in
  `cmd/skipper/main.go`, alongside the existing `registerEventRoutes` etc., gated
  on the same UI-enabled (`broadcaster != nil`) condition — the read API and the
  UI share a data surface.
- Reuse the existing `writeJSON` helper and `events.DeployEvent` /
  `roster.Entry` / `health.Snapshot` / `orphans.Snapshot` types verbatim; do not
  define parallel DTOs.
- Tests: table-style, assert the exact JSON body for each endpoint and the
  `503`-when-disabled paths for the optional subsystems.
- Document the contract for users in a new `docs/api.md` and add it to the
  MkDocs nav.
