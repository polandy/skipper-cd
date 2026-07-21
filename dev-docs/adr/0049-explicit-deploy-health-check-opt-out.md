# ADR-0049: Suppress the automatic deploy_health_check gate for on-demand stacks, plus an explicit opt-out

Status: accepted
Date: 2026-07-21

## Context

ADR-0046 made the deploy gate automatic: a stack with no `deploy_health_check`
whose compose file declares a `healthcheck:` on any service gets the `up
--wait` + rollback gate for free, at the default timeout, no URL probe. That
removed the `deploy_health_check: {}` boilerplate for the common case.

But the automatic gate keys off the compose `healthcheck:` *presence alone*,
with no way to say "no gate here" once a compose healthcheck exists — absence
of `deploy_health_check` was the trigger, not an opt-out. Two situations need
the gate off despite a compose healthcheck being present:

1. **On-demand stacks.** A stack with `on_demand_containers` (Sablier-managed:
   skipper runs `up`, then immediately stops the container so the scheduler
   owns its lifecycle) must not be `--wait`-gated. `--wait` would cold-start the
   on-demand container and block until it reports healthy only for skipper to
   stop it again — wasteful, and a slow warm-up would time out into a spurious
   rollback. Before ADR-0046 these stacks were ungated precisely by *omitting*
   `deploy_health_check`; ADR-0046 turned that omission into the trigger.
   Several such stacks (mediatracker, vscode-server, monica, karakeep,
   cyberchef, bentopdf on the homelab) declare a compose `healthcheck:`, so on
   the first upgrade past ADR-0046 they would silently start being gated.

2. **Monitoring-only healthchecks.** A compose `healthcheck:` is also
   legitimately present for reasons unrelated to deploy gating — external
   monitoring, `docker ps` STATUS, an orchestrator's own probes. The operator
   must be able to keep it without skipper turning it into a deploy gate.

skipper already knows which stacks are on-demand — the health poller treats an
exited on-demand container as `stopped`, never `unhealthy` (ADR-0027). Gating a
deploy on a container skipper is about to stop is the same category of mistake
in a different place, so skipper can and should refuse it itself rather than
relying on the operator to remember an opt-out on every on-demand stack.

## Decision

**Two changes, resolved together in `resolveHealthCheck` (the ADR-0046
resolution point in `deployStackIfChanged`):**

1. **A stack with `on_demand_containers` is never auto-gated.** When it sets no
   explicit `deploy_health_check`, no gate is inferred even if its compose file
   declares a `healthcheck:`. This needs zero per-stack config and cannot be
   forgotten. An explicit `deploy_health_check` still wins — an operator who
   deliberately wants a gate on an on-demand stack can set one.

2. **`deploy_health_check` accepts a boolean scalar** as an explicit on/off
   switch, for the monitoring-only case (and any non-on-demand stack that wants
   a compose healthcheck without a gate):
   - `deploy_health_check: false` — explicitly disable the gate, overriding the
     automatic one. Plain `up`, no `--wait`.
   - `deploy_health_check: true` — enable at the defaults; equivalent to the
     empty mapping `{}`. Included for symmetry and to gate a stack whose compose
     file has no `healthcheck:` (`up --wait` still waits for container start).
   - A mapping (`{…}`) — unchanged: explicit gate config.
   - Omitted — the automatic behavior above (on-demand suppression, else
     ADR-0046 compose-healthcheck gate).

The scalar is implemented as an optional `Enabled *bool` on the `HealthCheck`
struct, set by a custom `UnmarshalYAML` (a non-boolean scalar is a load error);
`HealthCheck.IsDisabled()` (nil-safe) is what `resolveHealthCheck` consults.
Every downstream consumer already reads the resolved `run.stack.DeployHealthCheck`,
so rollback.go/rollout.go/deploy.go inherit both behaviors unchanged.

The automatic on-demand suppression was preferred over an internal-only
inference with no config surface because it *is* the config surface skipper
already has (`on_demand_containers`); the explicit scalar covers the residual
monitoring-only case the automatic rule cannot see. Together they cover both
situations while keeping the on-demand case zero-config.

## Consequences

- On-demand stacks need **no** `deploy_health_check` line to stay ungated —
  skipper suppresses the gate from `on_demand_containers` alone. The NixOS
  homelab module therefore renders nothing extra for them; the earlier plan to
  emit `deploy_health_check: false` per on-demand stack is unnecessary.
- Any stack can still keep a compose `healthcheck:` for monitoring while opting
  out of the deploy gate with one line: `deploy_health_check: false`.
- `deploy_health_check: false` is part of a stack's `ConfigHash`
  (`stackDeployInputs`, via the `Enabled` field), so flipping the gate on/off
  redeploys exactly that stack once. On-demand suppression is resolved at
  deploy time from `on_demand_containers` (already hashed), so it needs no hash
  change of its own.
- `deploy_health_check: {}` and a bare compose-`healthcheck:` gate on a
  non-on-demand stack keep behaving exactly as before; existing configs need no
  change. Both changes are additive.
- No new invariant beyond Invariant 3's existing gate description (updated to
  note on-demand suppression and the explicit off-switch).
