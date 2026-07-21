# Feature Spec: Zero-Downtime Rollout (Traefik)

Status: implemented (backend); UI deploying sub-label pending
Date: 2026-07-19

## Goal

An opt-in, per-service **zero-downtime deploy strategy** for stacks fronted by a
reverse proxy that load-balances across healthy containers (Traefik is the
supported one). Instead of recreating a service in place — the current
`up -d --remove-orphans`, which stops the old container before the new one
serves — skipper starts the new version *alongside* the old, waits for it to
become healthy, lets the proxy shift traffic to it, and only then removes the
old container. There is never a moment with zero serving containers.

The motivating case: a homelab web app (e.g. a dashboard, `*arr`, Nextcloud
frontend) behind Traefik that today blinks 502 for a few seconds on every image
bump. The user wants that gap gone for the services where it matters, without
adopting an orchestrator (Kubernetes/Swarm) and without changing how deploys are
triggered (still git-driven).

Non-goals:

- **Stateful / single-instance services.** A service with an exclusive volume
  lock (Postgres, SQLite) or a published host port cannot run two replicas.
  Rollout is opt-in per service precisely so the DB in the same stack keeps its
  normal in-place recreate.
- **A proxy abstraction.** skipper does not talk to Traefik's API, write labels,
  or model routers. It relies only on Traefik's own docker-provider behaviour
  (discover healthy containers, load-balance across them, deregister on the
  container `die` event). Any proxy with that behaviour works; Traefik is what we
  document, test against, and name.
- **All-or-nothing consistency across multiple rolled services.** Rolled
  services roll sequentially; a failure leaves earlier successes on the new
  version (see Decisions #3). Cross-service atomic cutover is ArgoCD sync-wave
  territory, out of scope.
- **Multi-replica scale management.** Rollout assumes the steady state is one
  container per rolled service (the homelab norm) and cuts over 1→canary→1. It
  is not a `deploy.replicas` autoscaler.
- **Manual/triggered rollout.** Like every other deploy, it runs only when the
  stack's tracked inputs change. No UI button (skipper stays git-driven).

## User model

```yaml
# skipper.yaml (per-stack)
stacks:
  - name: dashboard
    rollout:
      services: [web]              # only these roll; every other service recreates normally
      health_timeout_seconds: 60   # optional; per rolled service; default = deploy_health_check timeout or 60
      drain_seconds: 5             # optional; wait after canary healthy before draining the old container
```

- **`rollout.services`** is an explicit allowlist of compose service names. A
  service is eligible only if it is (a) reachable purely through the proxy —
  **no published host `ports:`** (two replicas would collide on the port), and
  (b) has a compose **`healthcheck:`** (the readiness signal skipper waits on).
  Both are checked at deploy time (see Semantics); a violation fails the deploy
  with a clear message rather than letting docker error cryptically.
- Services **not** listed deploy exactly as today (in-place recreate). This is
  correct for databases and anything else that cannot run two instances.
- `health_timeout_seconds` bounds how long skipper waits for a canary to turn
  healthy before treating the rollout as failed. It falls back to the stack's
  `deploy_health_check.timeout_seconds` if set, else the shared default (60s).

## Semantics

Rollout replaces exactly one step of the per-stack sequence — the `up` block in
`deployStackIfChanged` (`deploy.go`, the `up -d --remove-orphans` +
`--wait`/probe leg). Everything else is unchanged, **including hooks**:

```
pre_deploy hooks                     ── unchanged; runs while the OLD version is still up (backup point)
  → pull (if images changed) → build (if Dockerfiles)
  → APPLY:                              ── rollout replaces the plain `up` here
      up -d --remove-orphans <non-rolled services>   (in-place, as today)
      for each rolled service (sequentially):
        rollOne(service)
  → HTTP probe (if deploy_health_check.url)   ── stack-level gate 2, unchanged
  → post_deploy hooks                  ── unchanged; runs against the new serving version
  → stop on-demand containers → record hashes/images → save state
```

`pre_deploy` still runs first, before any container is touched — the old
container is still up, so a backup captures the running old version exactly as
with recreate (ADR-0038). Rollout keeps the old container alive *longer* (through
the canary phase), but the hook position does not move. On a canary failure
skipper returns before `post_deploy`, so post hooks correctly do not run — same
as a recreate rollback. Non-rolled services go up first (their changes apply —
brief downtime is inherent and unavoidable for a single-instance service), then
each rolled service cuts over with no gap.

### `rollOne(service)` — the cutover

1. **Snapshot** the service's current running container IDs via
   `docker compose ps --format json <service>`. Call the running set *old*.
   - If *old* is empty (first-ever deploy of this service), there is nothing to
     keep alive: run a plain `up -d --no-deps <service>` and return. No canary,
     no downtime concern — it was not serving.
2. **Start the canary** without touching the old container:
   `docker compose up -d --no-deps --no-recreate --scale <service>=<n+1> <service>`
   (n = number of old containers, normally 1). Compose creates another container
   from the *new* definition; `--no-recreate` leaves the old one running;
   `--no-deps` avoids re-touching dependencies already brought up above.
3. **Wait for the canary to become healthy**: poll `compose ps --format json`
   for the new container ID (the one not in *old*) until its `Health` is
   `healthy`, or `health_timeout_seconds` elapses. Traefik discovers the healthy
   canary and begins load-balancing onto it during this window.
4. **Drain the old container**: `docker stop <old-id>` then `docker rm <old-id>`.
   Traefik deregisters it on the `die` event; from here only the new version
   serves. (`stop` respects the container's stop-grace so in-flight requests
   finish.)

Compose scale bookkeeping is intentionally not reconciled with a trailing `up`:
the next full deploy resets container naming, and a reconcile risks re-creating a
second container from the freed name index. The service ends on one new
container.

### Failure = zero-downtime by construction

If the canary never turns healthy within the timeout (step 3), skipper
**removes the canary** (`docker stop`/`rm` the new container IDs, i.e. every
container *not* in *old*) and leaves the old container untouched — it never
stopped serving. The stack stays on its previous version with **no downtime even
on failure**, which is strictly better than the recreate path's git-restore
rollback (that one briefly drops the service).

This maps onto the existing terminal semantics with **no new event status**: a
failed rollout reuses `ErrRolledBack` → `rolled_back` event (the stack never left
the old version; the "rollback" here is canary cleanup, not a git restore, and
needs no `--wait` restore leg). A failure of the docker/compose commands
themselves mid-cutover (not a health timeout) that could leave the service in an
unknown state falls through to the existing git-restore `rollBackFailedDeploy`
path for that stack, so there is always a defined recovery.

### Compose-level validation

The rollout eligibility rules — for each name in `rollout.services`:

- it **exists** in the compose file → else fail (`service %q is not defined in docker-compose.yml`);
- it has **no published `ports:`** → else fail (`publishes host ports; cannot run two replicas — route it via the proxy instead`);
- it sets **no `container_name:`** → else fail (compose cannot scale a named container);
- it defines a **`healthcheck:`** → else fail (`no healthcheck; rollout needs a readiness signal`).

live in one place: `config.ValidateRolloutServices(services, *compose.File)`. It
is the single source of the rules, called from two spots:

- **deploy time** (`internal/deploy`, every mode): before any container is
  touched, so a violation fails fast with a `failed` event — same shape as a
  `pre_deploy` hook failure. In host-config mode this is the only check (the repo
  clone does not exist at config-load, so it cannot run at startup).
- **discovery time** (`config.LoadRepoStacks`, stack-discovery mode only): the
  clone is present there, so each rolled stack is validated on **every sync**. A
  violation becomes an entry-level `StackError` surfaced on the stack's row, not
  only when it next redeploys. This matters because `rollout` is excluded from
  change detection — editing it never triggers the redeploy the deploy-time check
  would ride on. Discovery also parses every stack's compose (a broken one is a
  `StackError`) and checks that relative `env_files`/`watch_dirs` exist.

Both callers parse the compose via the shared **`internal/compose`** package
(`compose.Parse` + `compose.Service` predicates), so there is one parser and one
set of eligibility rules — no duplication between `config` and `deploy`. The
deploy path wraps `compose.File` in its own `composeFile` for its image/build
helpers.

## Change detection

`rollout` is **excluded from hashing** (Invariant 2). It is not a hashed input,
and in stack-discovery mode it is **excluded from `Stack.ConfigHash`**, joining
`icon`/`self_heal`/`depends_on`/`hooks` on that list: it shapes *how* a deploy
applies, not *what* is deployed. Switching a service from recreate to rollout —
or back — must not by itself redeploy an otherwise-unchanged stack. The new
strategy takes effect on the stack's next real deploy. Invariant 2's never-hash
list gains `rollout`.

## Package layout

- `internal/config`: a `Rollout *Rollout` field on `Stack`
  (`Services []string`, `HealthTimeoutSeconds int`, `DrainSeconds int`). Load-time
  validation: non-empty `services`, no blank/whitespace names,
  `health_timeout_seconds >= 0`, `drain_seconds >= 0`.
  Mirrored in `repo.go`'s override struct for discovery mode, and **left out** of
  `stackConfigHash`'s hashed set.
- `internal/deploy/rollout.go`: `rollout(ctx, run, compose, state)` orchestrating
  "up non-rolled → rollService per rolled", and `rollService(ctx, run, service,
  timeout)` implementing the four-step cutover, plus `serviceContainers` (ps read)
  and `removeContainers` (stop+rm).
- **Output-capable runner.** The current `deploy.Runner` (alias of
  `command.Runner`) only has `Run` (fire-and-forget); reading container IDs needs
  `Output`. `command.ShellRunner` already has an `Output` method
  (`internal/command/runner.go`), used by `internal/health`/`orphans`/`applink`
  via their own `Outputter` interfaces. deploy adds the same consumer-side
  interface + a `Outputter` Config field, wired to a `command.ShellRunner` in
  main. Tests inject a fake. `docker compose ps --format json` is parsed by a
  small local parser (ID/Name/Service/State/Health) — deploy stays
  self-contained rather than reaching into `internal/health`'s unexported types.
- No new package: rollout is meaningless outside a deploy, like hooks.

## Testing

Table tests with the recording Runner + a fake `Output` returning canned
`compose ps --format json`, asserting exact argv sequences:

- **Happy path**: snapshot ps → `up --scale=2 --no-recreate --no-deps` →
  poll ps until healthy → `stop`+`rm` old id → done. Assert the old id is the one
  removed and the new id survives.
- **First deploy** (empty old set): plain `up -d --no-deps <svc>`, no scale, no
  stop/rm.
- **Canary never healthy** (ps keeps returning `starting`/`unhealthy` until the
  timeout): the canary id is `stop`+`rm`'d, the old id is **never** touched, the
  event is `rolled_back` via `ErrRolledBack`, no state save. (Safety-critical
  failure path — required per the repo's coverage principle.)
- **Non-rolled + rolled mix**: non-rolled services get the plain
  `up -d --remove-orphans <names>` first; the rolled service cuts over after;
  `--no-deps` prevents re-touching the DB.
- **Validation**: unknown service / published `ports:` / missing `healthcheck:`
  each fail before any container command runs, with the specified messages and a
  `failed` event.
- **ConfigHash exclusion**: flipping `rollout` on/off does not change a stack's
  `ConfigHash` (guards Invariant 2).
- **Hook interaction**: `pre_deploy` runs before the rollout (old still up);
  `post_deploy` runs after a successful rollout; a canary failure skips
  `post_deploy` and emits `rolled_back`.

## UI surface

Minimal, reusing existing surfaces (no new streaming machinery), read-only:

1. **Deploying sub-label.** While a stack rolls, the `deploying` badge shows the
   phase, e.g. `rolling web (1/2)`, driven by the same UI-sink-gated lightweight
   snapshot pattern as the hooks `hookrun` / `upcoming` run-plan snapshots
   (published only with a UI sink → costs nothing headless). Plain `deploying`
   during pull/build as today.
2. **Roster badge (optional, follow-up).** A small "rollout" marker on the
   stack cell for stacks with a `rollout` block, panel listing the rolled
   services — mirrors the hooks badge. Can ship in a later increment.

Full surface + `data-testid`s go in `internal/ui/UI_SPEC.md` when the increment
is built. E2E: a new mask asserting the rolling sub-label (behaviour) — no new
visual baseline needed if the idle header is unchanged.

## Decisions

1. **Per-service allowlist, not a stack-wide switch with auto-detection.** A
   stack mixes rollable (stateless, proxy-fronted) and non-rollable (DB,
   port-publishing) services. Auto-detecting rollability from "no ports + has
   volume?" is magic and dangerous — a wrong guess either fails cryptically or
   double-runs a stateful service. An explicit `services:` list makes the
   operator state intent; skipper then *validates* that intent against the
   compose file (ports/healthcheck) and refuses if it cannot hold.

2. **Traefik-only, and skipper stays proxy-agnostic in code.** True
   zero-downtime needs a proxy that (a) discovers healthy containers, (b)
   balances across them, (c) drains on stop. skipper implements only the
   scale-up → wait-healthy → drain dance and relies on the proxy for the rest.
   We document/test Traefik because it does all three out of the box via its
   docker provider; no Traefik-specific code enters skipper. Documented
   prerequisite, not an integration.

3. **Sequential rolled services; failure leaves earlier successes on new.**
   Rolling `[web, api]` and having `api` fail leaves `web` on the new version
   and `api` on the old. Atomic multi-service cutover would require holding all
   canaries up simultaneously and a two-phase commit — real complexity for a
   case the homelab rarely hits (rolled lists are usually one app service). We
   accept partial-new state on multi-service failure, report `rolled_back`
   (canary of the failed service cleaned up), and document that the typical
   `rollout.services` is a single frontend service.

4. **Reuse `rolled_back`, no new event status.** A failed rollout leaves the
   stack on the old version — identical *outcome* to a git-restore rollback, so
   it reuses `ErrRolledBack` and the `rolled_back` event/alert. The only
   difference is the mechanism (canary cleanup vs. git restore), which is a log
   detail, not a user-facing status. Keeps the event model and the existing
   alert (ADR-0020/0031) unchanged.

## Open questions

1. **Stop grace / connection draining. — ANSWERED (real Traefik test, host-b
   2026-07-19).** A real cutover behind Traefik v3.7.7 showed a brief race, so
   `drain_seconds` was added: after the canary is healthy skipper waits
   `rollout.drain_seconds` before removing the old container (the equivalent of
   `docker-rollout`'s `--wait-after-healthy`), so the proxy can route to the new
   container while the old is still up. Measured over repeated cutovers
   (~300 req/cutover at 10 req/s):
   - in-place recreate: ~1–2 s of `502`s (many requests);
   - rollout, no proxy tuning: a `502` at the drain edge (Traefik still routed to
     the stopping old container) plus a single sub-second `000`;
   - rollout + Traefik **retry** middleware: the `502`s vanish;
   - rollout + retry + `drain_seconds` + active LB healthcheck: a single
     sub-second `000` per cutover remains (~0.3 %), a Traefik/Docker
     network-reconfiguration transient when the old backend is removed — at the
     noise floor and not eliminable from skipper's proxy-agnostic side.

   Conclusion: rollout delivers **effectively-zero** downtime (one sub-second
   blip per deploy vs. seconds of `502`s). The documented recipe is
   `drain_seconds` + a Traefik retry middleware; a `pre_stop` hook (docker-rollout
   has one) could later drain in-container for apps that need it.

2. **`compose ps` scale race.** After `--scale=2`, compose may briefly report
   the old container as recreated depending on version. Need to confirm across
   the compose versions skipper targets that `--no-recreate` reliably preserves
   the old container ID. Validate on real Traefik in the homelab before marking
   accepted.

3. **Rollout + `deploy_health_check.url` overlap.** With both a rollout (per-container
   healthcheck gate) and a stack-level `deploy_health_check.url` (external probe), the
   probe runs once after all cutovers — correct, but double-gating. Document
   that rollout services should rely on their compose `healthcheck:` and reserve
   `deploy_health_check.url` for whole-stack smoke tests.
