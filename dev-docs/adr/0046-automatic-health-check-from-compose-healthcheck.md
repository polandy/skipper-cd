# ADR-0046: Automatic health_check gate from the compose file's own healthcheck

Status: accepted
Date: 2026-07-21

## Context

`health_check` is opt-in per stack (ADR-0022). In practice, the common case —
gate the deploy on the stack's own compose `healthcheck:`, no HTTP probe, the
default timeout — was written the same way on every stack that wanted it:
`health_check: {}`. That line carries no information the compose file didn't
already have: a service with a `healthcheck:` block has, by definition, opted
into being health-checked; skipper's `--wait` gate is the natural deploy-time
consequence of that, not a second decision to make.

Making the gate key off compose `healthcheck:` presence *unconditionally* was
considered and rejected: a `healthcheck:` block is sometimes present for
reasons unrelated to deploy-time gating (external monitoring, `docker ps`
status), and `health_check` is deliberately opt-in (ADR-0022) — the operator
must still be able to have a compose healthcheck without skipper rolling back
on it. The chosen design keeps the opt-in *shape* (an explicit `health_check`
still always wins) while removing the redundant declaration for the common
case of wanting exactly what the compose file already describes.

## Decision

When a stack sets no `health_check` but its compose file declares a
`healthcheck:` on at least one service, skipper applies the same `--wait` +
rollback gate automatically, at the default timeout
(`config.DefaultHealthCheckTimeoutSeconds`, 60s) with no URL probe — exactly
what `health_check: {}` already produced. An explicit `health_check` — even an
empty `{}` — always wins over the automatic gate; a stack with no compose
`healthcheck:` anywhere stays ungated, same as today.

Resolved once, in `internal/deploy`, not `internal/config`:
`resolveHealthCheck(explicit *config.HealthCheck, cf *composeFile)
*config.HealthCheck` runs in `deployStackIfChanged` right after the compose
file is parsed, and the result is stored on `run.stack.HealthCheck`. Every
downstream consumer of the effective gate (`deploy.go`'s `up`/probe,
`rollback.go`, `rollout.go`) already reads `run.stack.HealthCheck`, so they
inherit the automatic gate for free without change.

The **unresolved** `stack.HealthCheck` (the function parameter, not
`run.stack`) still feeds `addStackConfigHash` — the automatic gate is
deliberately excluded from `Stack.ConfigHash`. It doesn't need to be there: a
compose file gaining or losing a `healthcheck:` block already changes the
compose-file hash (Invariant 2), which already redeploys the stack. Folding it
into `ConfigHash` too would just be a second signal for the same event.

## Consequences

- A stack whose compose file already declares a `healthcheck:` gets deploy-time
  verification and automatic rollback without any `health_check` line in the
  skipper config — the common case needs zero declaration.
- `health_check: {}` remains valid and behaves identically to today (it simply
  now equals what the automatic path already does); existing configs need no
  migration.
- A per-stack `health_check` is still required to add the stage-2 HTTP probe
  or change the timeout — the automatic gate only ever produces the bare
  default.
- No new config surface, no new invariant beyond Invariant 3's existing gate
  description (updated to note the automatic case).
