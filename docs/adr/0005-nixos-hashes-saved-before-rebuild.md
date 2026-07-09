# ADR-0005: NixOS hashes are persisted before the rebuild runs

Status: accepted
Date: 2026-07-09 (documents an existing decision)

## Context

skipper-cd can run `nixos-rebuild switch` when nix files change. The rebuild
may restart the skipper-cd service itself — killing the process in the
middle of its own deploy run.

## Decision

The new nix file hashes are written to `state.yaml` (reserved stack key
`_nixos`) *before* `nixos-rebuild` starts. NixOS rebuild runs before any
stack deploys; if it fails, all stack deploys abort.

## Consequences

- After a rebuild-induced restart, the startup deploy run sees unchanged nix
  hashes and does not rebuild again (no rebuild loop).
- If the rebuild fails, the already-persisted hashes mean it is *not*
  retried on the next unchanged push; a fixing commit (or state removal)
  triggers the retry. This trade-off is accepted to avoid rebuild loops.
- Stack deploys interrupted by the restart are caught up on startup, because
  their hashes were not yet persisted.
