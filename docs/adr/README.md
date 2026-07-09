# Architecture Decision Records

One file per decision, numbered sequentially (see
[ADR-0001](0001-record-architecture-decisions.md) for the process).

| ADR | Title |
|-----|-------|
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions |
| [0002](0002-hash-based-change-detection.md) | Hash-based change detection with persisted state |
| [0003](0003-runner-abstraction-and-fake-based-tests.md) | Runner abstraction and fake-based tests |
| [0004](0004-rollback-via-old-compose-file-from-git.md) | Rollback via the old compose file from git |
| [0005](0005-nixos-hashes-saved-before-rebuild.md) | NixOS hashes are persisted before the rebuild runs |
| [0006](0006-atomic-state-writes.md) | Atomic writes for persisted state |
| [0007](0007-graceful-shutdown-waits-for-running-deploy.md) | Graceful shutdown waits for the running deploy |
| [0008](0008-per-command-timeouts.md) | Timeouts apply per command, not per deploy run |
| [0009](0009-webhook-branch-filter.md) | Webhook filters pushes by branch ref |
| [0010](0010-no-deploy-coalescing.md) | Concurrent deploy runs serialize and wait — no coalescing (yet) |
| [0011](0011-release-automation-via-conventional-commits.md) | Releases are automated from Conventional Commits |
