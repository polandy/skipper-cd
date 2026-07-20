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
| [0021](0021-configurable-ui-themes.md) | Configurable UI themes, with a per-browser override |
| [0022](0022-health-check-gated-rollback.md) | Health-check-gated rollback |
| [0023](0023-pwa-update-prompt.md) | Prompt to reload when a new PWA version is waiting |
| [0024](0024-upcoming-deploys-look-ahead.md) | Upcoming-deploys look-ahead (run plan) in the header |
| [0025](0025-reconcile-self-restart-interrupted-nixos-rebuild.md) | Reconcile a self-restart-interrupted NixOS rebuild into a success |
| [0026](0026-yaml-state-persistence-not-sqlite.md) | Persist state as YAML files, not an embedded database |
| [0027](0027-live-stack-health-in-ui.md) | Live stack health in the UI (own stacks, display-only) |
| [0028](0028-periodic-reconcile-loop.md) | Periodic reconcile loop against git |
| [0029](0029-runtime-drift-self-heal.md) | Runtime drift self-heal via corrective redeploy (opt-in) |
| [0030](0030-image-update-automation.md) | Image-update automation (delegate to Renovate; git write-back if in-repo) |
| [0031](0031-notify-on-own-stack-health-change.md) | Notify on own-stack health change (background watchdog, persisted phase history) |
| [0032](0032-stack-deploy-ordering-via-depends-on.md) | Stack deploy ordering via depends_on (block on failed dependency, queue-aware) |
| [0033](0033-durable-per-stack-deploy-audit-log.md) | Durable per-stack deploy audit log (append-only NDJSON, terminal outcomes) |
| [0034](0034-stack-discovery-from-repo.md) | Stack definitions come from the deploy repo (auto-discovery + central overrides) |
| [0035](0035-ui-assets-same-origin-from-embed.md) | UI assets served same-origin from embed.FS (self-contained UI) |
| [0036](0036-orphan-detection-and-prune.md) | Orphan detection and optional prune (working_dir identity, health-poll cadence) |
| [0037](0037-container-logs-in-ui.md) | Container logs in the UI (live-streamed via SSE, one follow-child per viewer) |
| [0038](0038-pre-post-deploy-hooks.md) | Pre-/post-deploy hooks (backup-before-update) |
| [0039](0039-read-only-http-json-api.md) | Read-only HTTP JSON API (snapshot + stream; proposed) |
| [0040](0040-zero-downtime-rollout-traefik.md) | Zero-downtime rollout (Traefik), opt-in per service |
| [0041](0041-traefik-app-link-detection.md) | Traefik app-link detection (live labels, health-poll cadence) |
| [0042](0042-pretty-console-log.md) | Pretty console output (default `log_format`) |
| [0043](0043-single-config-file.md) | Single configuration file (fold per-stack overrides into the host config) |
