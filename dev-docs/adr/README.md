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
| [0012](0012-dependabot-auto-merge.md) | Dependabot patch/minor updates merge automatically |
| [0013](0013-child-process-output-through-log-pipeline.md) | Child process output is tee'd through the log pipeline |
| [0014](0014-nixos-rebuild-in-transient-systemd-unit.md) | NixOS rebuild runs in a transient systemd unit |
| [0015](0015-revert-nix-hashes-on-surviving-rebuild-failure.md) | Revert nix hashes when a rebuild fails and skipper survives |
| [0016](0016-autosync-and-queue-via-leave-dirty.md) | Autosync with a leave-dirty queue and non-persistent overrides |
| [0017](0017-self-heal-failed-self-update.md) | Self-heal skipper-cd after a failed self-update |
| [0018](0018-pwa-installable-ui.md) | Installable PWA web UI with app-shell caching |
| [0019](0019-autosync-ui-overrides-collapse-to-inherit.md) | Autosync UI overrides collapse to inherit when they match the baseline |
| [0020](0020-outbound-deploy-notifications.md) | Outbound deploy notifications via a generic HTTP sink |
