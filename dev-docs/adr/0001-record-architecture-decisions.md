# ADR-0001: Record architecture decisions

Status: accepted
Date: 2026-07-09

## Context

skipper-cd has accumulated non-obvious design decisions (change detection,
rollback semantics, locking, timeouts). They lived only in CLAUDE.md
invariants and commit messages, which explain *what* must hold but not *why*
the alternative was rejected.

## Decision

We record architecturally significant decisions as ADRs, one file per
decision, in `docs/adr/`, numbered sequentially. Format: Status, Date,
Context, Decision, Consequences. A superseded ADR is not edited; a new ADR
replaces it and the old one gets `Status: superseded by ADR-XXXX`.

## Consequences

- New significant decisions require a short ADR alongside the code change.
- CLAUDE.md invariants stay as the enforcement summary; ADRs hold the
  reasoning behind them.
