# ADR-0071: A switch that activated with failed units is applied, not a failed rebuild

Status: accepted
Date: 2026-11-20

## Context

`switch-to-configuration` exits with status **4** when the new configuration
activated (profile switched, bootloader installed, units restarted) but some
units failed to start or reload; it prints `warning: the following units
failed: …`. `nixos-rebuild` propagates that status. skipper treated it like
any other failed rebuild: it reverted the pre-saved `_nixos` hashes
(ADR-0015) and aborted the stack phase.

A host unit that `Requires=` a compose stack (e.g. a oneshot that polls the
app) fails while that stack is down. Aborting the stack phase then keeps the
stack down, the next sync rebuilds again, the unit fails again: a deadlock
that only a human can break.

## Decision

After the transient unit ends failed, read `ExecMainStatus` and
`InvocationID` (`systemctl show`). Status 4 becomes
`nixos.ErrActivatedUnitsFailed`, carrying the failed units parsed from that
invocation's journal. The deploy side then keeps the pre-saved hashes,
clears the in-flight marker, emits a `_nixos` `failed` event naming the units,
counts the error and **continues to the stack phase**. Every other status
(and an unreadable one) keeps the ADR-0015 behaviour: revert and abort.

Rejected: keep aborting on status 4 — re-running a switch that already
applied cannot start the units and starves the stacks they depend on.

## Consequences

- A host unit depending on a stack recovers once the stack phase brings the
  stack up; the failed event stays visible in history and notifications.
- A transient user-unit reload failure (the ADR-0015 incident class, if it
  exits 4) is no longer retried by the next sync. The system configuration
  itself applied, so nothing is silently left unapplied; the event reports it.
- Operators should still keep host units that wait on a compose stack from
  blocking activation (e.g. not `wantedBy` the activation target).
