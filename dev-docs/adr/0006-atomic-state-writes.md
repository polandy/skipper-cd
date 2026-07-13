# ADR-0006: Atomic writes for persisted state

Status: accepted
Date: 2026-07-09

## Context

`state.yaml` and `deploy-history.yaml` are rewritten on every run. The
process can die mid-write — most plausibly when `nixos-rebuild` restarts the
skipper-cd service (ADR-0005). A truncated `state.yaml` is treated as
corrupt and forces a full redeploy of all stacks.

## Decision

All persisted files are written via temp file in the target directory +
`os.Rename` (`writeFileAtomic`). The rename is atomic on POSIX filesystems,
so readers see either the old or the new complete file. The small helper is
duplicated in `internal/deploy` and `internal/events` rather than introducing
a shared package for 15 lines ("a little copying is better than a little
dependency").

## Consequences

- A crash mid-write can no longer corrupt state or history.
- Writes must stay on the same filesystem as the target (temp file is
  created next to it).
- Durability (fsync) is deliberately not guaranteed — a power loss may lose
  the newest state version, which only causes a redundant redeploy.
