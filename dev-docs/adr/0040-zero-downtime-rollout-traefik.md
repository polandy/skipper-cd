# ADR-0040: Zero-downtime rollout (Traefik), opt-in per service

Status: accepted (backend implemented; UI deploying sub-label pending)
Date: 2026-07-19

## Context

Every deploy today recreates a stack's containers in place:
`docker compose up -d --remove-orphans` (optionally `--wait` + an HTTP probe,
ADR-0022). For a single-replica service that means the old container stops
before the new one serves — a few seconds of 502 on every image bump. For a
homelab web app behind Traefik that is the one visible rough edge of an
otherwise hands-off CD flow.

True zero-downtime needs three things, independent of the tool: overlap (old and
new run briefly together), a proxy that health-checks and drains, and an app
that tolerates two versions running for a moment. Kubernetes and Docker Swarm
both provide this, but adopting an orchestrator contradicts skipper-cd's premise
(a thin, single-host, compose-driven CD tool). The lightweight prior art is the
`docker-rollout` CLI plugin: scale a service up, wait for the new container's
healthcheck, let the reverse proxy shift traffic, then remove the old one — all
with plain `docker compose` and a proxy that load-balances across healthy
containers. skipper can implement the same dance itself.

The hard constraint is physical, not a design choice: two containers of one
service cannot share a published host port, and a service with an exclusive
volume lock (Postgres) cannot run twice. So zero-downtime applies only to
stateless services reachable purely through the proxy — never the whole stack.
This is why it must be **opt-in per service**, not a stack-wide mode.

Alternatives considered and rejected: a stack-wide `strategy: rollout` switch
with auto-detection of rollable services (magic; a wrong guess double-runs a
stateful service or fails cryptically); adopting Swarm (`docker stack deploy`
with `order: start-first` gives real rolling updates for free, but pulls in a
second orchestrator and `deploy:` semantics skipper otherwise ignores); a
Traefik API integration (skipper would have to model routers/labels — far more
coupling than relying on Traefik's stock docker-provider behaviour).

## Decision

An optional per-stack `rollout` section names the services that deploy with a
zero-downtime cutover instead of in-place recreate:

```yaml
stacks:
  - name: dashboard
    rollout:
      services: [web]
      health_timeout_seconds: 60   # optional
```

Rollout replaces exactly the `up` step of the per-stack sequence; pull, build,
`pre_deploy`/`post_deploy` hooks, the stack-level HTTP probe, on-demand stop, and
state save are all unchanged. `pre_deploy` still runs before anything is touched
(old version up → valid backup point); `post_deploy` runs after a successful
rollout against the new serving version. Non-listed services recreate in place as
today (correct for databases); listed services cut over one at a time:

```
up -d --remove-orphans <non-rolled services>          (in place, as today)
for each rolled service (sequentially):
  snapshot old container IDs (compose ps --format json)
  up -d --no-deps --no-recreate --scale <svc>=<n+1> <svc>  (start canary alongside old)
  wait canary healthy (poll ps, health_timeout_seconds)    (Traefik shifts traffic)
  docker stop <old> && docker rm <old>                     (Traefik deregisters)
```

Key decisions:

- **Per-service allowlist, validated at deploy time.** Eligibility is checked
  against the compose file before any container is touched: the service must
  exist, publish **no host `ports:`** (replicas would collide), set **no
  `container_name:`** (compose cannot scale a named container), and define a
  **`healthcheck:`** (the readiness signal). A violation fails the deploy with a
  clear message and a `failed` event — no cryptic docker error. `composeService`
  parsing is extended to read `ports:`/`container_name:`/`healthcheck:`.
- **skipper stays proxy-agnostic in code.** It implements only scale-up →
  wait-healthy → drain and relies on the proxy to discover the healthy canary,
  balance onto it, and deregister the drained container on its `die` event.
  Traefik does all three via its docker provider out of the box; no
  Traefik-specific code enters skipper. Traefik is the documented, tested
  prerequisite, not an integration.
- **Failure is zero-downtime by construction.** If the canary never turns
  healthy within the timeout, skipper removes the canary and leaves the old
  container serving — the stack never went down. This reuses `ErrRolledBack` →
  the `rolled_back` event/alert (identical *outcome*: stack on the old version),
  with **no new status**. The "rollback" is canary cleanup, not a git restore,
  so it needs no `--wait` restore leg. Non-health docker errors mid-cutover fall
  through to the existing git-restore `rollBackFailedDeploy` for a defined
  recovery.
- **First deploy of a service** (no running container) skips the canary dance —
  a plain `up -d --no-deps <svc>`, since nothing is serving to keep alive.
- **`rollout` is excluded from change detection** (Invariant 2): not a hashed
  input, and excluded from `Stack.ConfigHash` in discovery mode — joining
  `icon`/`self_heal`/`depends_on`/`hooks`. Switching a service to/from rollout
  shapes *how* a deploy applies, not *what* deploys, so it must not by itself
  redeploy an unchanged stack. Invariant 2's never-hash list gains `rollout`.
- **Sequential across rolled services; partial-new on multi-service failure.**
  Rolled services cut over one at a time; if a later one fails, earlier
  successes stay on the new version. Atomic multi-service cutover is out of scope
  (ArgoCD sync-wave territory); the typical `rollout.services` is a single
  frontend service.

Reading container IDs needs command output, which the deploy `Runner` interface
lacks (`Run` is fire-and-forget). `command.ShellRunner` already has an `Output`
method (used by `internal/health`/`orphans`/`applink`); rollout wires the same
runner through a small consumer-side `Outputter` interface in `internal/deploy`
and parses `compose ps --format json` with a small local parser — no real docker
in tests.

## Consequences

- Zero-downtime for the services that need it, with one config block, no
  orchestrator, and no change to how deploys are triggered (still git-driven).
  Failure never drops the service — a genuine improvement over the recreate
  path's git-restore rollback, which briefly does.
- **Verified on real Traefik (v3.7.7, host-b, 2026-07-19).** A real cutover
  confirmed the mechanics end-to-end, and surfaced a drain-edge race: "canary
  healthy" (docker healthcheck) precedes "proxy is routing to the new container".
  An optional **`drain_seconds`** (docker-rollout's `--wait-after-healthy`) was
  added — skipper holds the old container that long after the canary is healthy
  before removing it. With `drain_seconds` + a Traefik retry middleware a cutover
  drops from ~1–2 s of `502`s (in-place recreate) to at most a single sub-second
  blip; the residual is a Traefik/Docker network-reconfiguration transient on
  backend removal, at the noise floor and not eliminable from skipper's
  proxy-agnostic side. Effectively-zero, not mathematically-zero, downtime.
- **Traefik (or an equivalent health-aware, drain-on-stop docker-provider
  proxy) is a hard prerequisite** for rolled services, documented next to the
  config reference. A rolled service with a published host port or no
  healthcheck is refused at deploy time, not silently degraded.
- Rollout runs under the deploy mutex (Invariant 7) like everything else; the
  canary wait extends a rolled stack's deploy by up to `health_timeout_seconds`.
  That is the accepted cost of overlap, bounded by the timeout.
- `compose ps` parsing and container-ID tracking add a small amount of
  runtime-state inspection to a package that has so far only *applied* compose
  files — kept isolated in `rollout.go` and validated against the same
  `--format json` output the health poller already consumes.
- The stack-level `health_check.url` probe still runs once after all cutovers.
  Rolled services should rely on their compose `healthcheck:`; `health_check.url`
  is documented as the whole-stack smoke test to avoid confusing double-gating.
- Multi-service atomic cutover, connection-draining tuning (`drain_seconds`),
  and multi-replica scale management are explicitly deferred (see spec Open
  Questions) — the homelab single-frontend case is served first.

## References

- Spec: `dev-docs/zero-downtime-rollout-spec.md`
- Prior art: the `docker-rollout` docker CLI plugin (scale-up → wait → drain).
- Builds on: ADR-0004 (rollback via old compose from git), ADR-0022
  (health-check-gated rollback), ADR-0032 (deploy ordering / `depends_on`),
  ADR-0034 (stack discovery / `ConfigHash`), ADR-0038 (hooks — the
  excluded-from-hashing precedent).
