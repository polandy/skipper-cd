# ADR-0049: Explicit opt-out for the automatic deploy_health_check gate

Status: accepted
Date: 2026-07-21

## Context

ADR-0046 made the deploy gate automatic: a stack with no `deploy_health_check`
whose compose file declares a `healthcheck:` on any service gets the `up
--wait` + rollback gate for free, at the default timeout, no URL probe. That
removed the `deploy_health_check: {}` boilerplate for the common case.

But the automatic gate keys off the compose `healthcheck:` *presence alone*,
and before this ADR there was no way to say "no gate here" once a compose
healthcheck existed — absence of `deploy_health_check` was the trigger, not an
opt-out. That collides with `on_demand_containers` stacks (Sablier-managed:
skipper runs `up` then immediately stops the container so the scheduler owns
its lifecycle). Such a stack must *not* be `--wait`-gated: `--wait` would
cold-start the on-demand container and block until it reports healthy only for
skipper to stop it again — wasteful, and a slow warm-up would time out into a
spurious rollback. That is exactly why the NixOS module deliberately omitted
`deploy_health_check` for on-demand stacks. ADR-0046 turned that omission into
the *trigger* for the gate, and several on-demand stacks in the homelab
(mediatracker, vscode-server, monica, karakeep, cyberchef, bentopdf) do
declare a compose `healthcheck:` — so on the first upgrade past ADR-0046 they
would silently start being `--wait`-gated with no config-level way to stop it.

A compose `healthcheck:` is also legitimately present for reasons unrelated to
deploy gating — external monitoring, `docker ps` STATUS, an orchestrator's own
probes. The operator must be able to keep it without skipper turning it into a
deploy gate. ADR-0046 already named this as a deliberately-preserved
capability ("the operator must still be able to have a compose healthcheck
without skipper rolling back on it") but shipped no mechanism for it.

## Decision

`deploy_health_check` accepts a **boolean scalar** in addition to a mapping:

- `deploy_health_check: false` — explicitly disable the gate, overriding the
  ADR-0046 automatic compose-`healthcheck:` gate. The stack deploys with a
  plain `up` (no `--wait`), whatever its compose file declares.
- `deploy_health_check: true` — explicitly enable the gate at the defaults;
  equivalent to the empty mapping `{}`. Included for symmetry so the field
  reads as a clean on/off switch, and to gate a stack whose compose file has
  no `healthcheck:` of its own (`up --wait` still waits for container start).
- `deploy_health_check: {…}` (a mapping) — unchanged: explicit gate config
  (`timeout_seconds`, `url`).
- Omitted — unchanged: the automatic gate applies iff the compose file has a
  `healthcheck:` (ADR-0046).

Implemented as an optional `Enabled *bool` on the `HealthCheck` struct, set by
a custom `UnmarshalYAML` that accepts the scalar form; a non-boolean scalar is
a load error. `HealthCheck.IsDisabled()` (nil-safe: a nil/absent gate is *not*
disabled) is consulted in one place, `resolveHealthCheck` (ADR-0046), which
returns no gate for an explicitly-disabled stack before it would otherwise
auto-detect one. Every downstream consumer already reads the resolved
`run.stack.DeployHealthCheck`, so rollback.go/rollout.go/deploy.go inherit the
opt-out with no change.

The alternative — an internal-only bool with no config surface, inferring "off"
from `on_demand_containers` inside skipper — was rejected: it would re-entangle
two independent concepts (on-demand lifecycle vs. deploy gating) that the
config keeps separate, and it would give a non-on-demand stack no way to keep a
monitoring-only compose healthcheck ungated. A scalar on/off switch is the
minimal surface that covers both cases and reads plainly.

## Consequences

- An on-demand stack (or any stack) can keep a compose `healthcheck:` for
  monitoring while opting out of the deploy gate with one line:
  `deploy_health_check: false`. The NixOS module renders exactly this for
  stacks whose gate is turned off (its `deploy_health_check.enable = false`
  case), instead of omitting the field and silently falling into the automatic
  gate.
- `deploy_health_check: false` is part of a stack's `ConfigHash`
  (`stackDeployInputs`, via the `Enabled` field), so flipping the gate on/off
  redeploys exactly that stack once under the new policy — the same treatment
  the timeout/url fields already get, and distinct from absence.
- `deploy_health_check: {}` and a bare compose-`healthcheck:` gate keep behaving
  exactly as before; existing configs need no change. The new capability is
  purely additive.
- No new invariant beyond Invariant 3's existing gate description (updated to
  note the explicit off-switch).
