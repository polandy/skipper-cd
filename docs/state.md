# skipper-cd State File — Reference

Deployment state is persisted at `/var/lib/skipper/state.yaml`. It stores the per-file hashes from the last successful deployment of each stack, as well as the Git commit SHA that future deploys diff against (`last_deployed_commit`). That commit advances to `HEAD` at the end of a run — but not while a change is still queued by [paused autosync](autosync.md), so a deferred change keeps its diff base until it actually deploys.

```yaml
last_deployed_commit: abc123def456...
stacks:
  traefik:
    /var/lib/skipper/repo/modules/traefik/docker-compose.yml: 9f86d081...
  gitea:
    /var/lib/skipper/repo/modules/gitea/docker-compose.yml: aabbccdd...
    /run/secrets/rendered/skipper/compose.env: 11223344...
```

If the state file is absent or cannot be parsed (e.g. after a fresh install or corruption), all stacks are redeployed on the next run.

NixOS rebuild state (when [configured](nixos.md#nixos-rebuild)) is tracked under the reserved stack key `_nixos`. State is written atomically (temp file + rename).

A transient top-level key `nixos_rebuild_in_flight` may appear while a rebuild is running: it lists the changed nix files and is written just before a rebuild whose `switch-to-configuration` may restart skipper-cd. If the switch restarts skipper mid-rebuild, the marker survives and the next startup reconciles it into a `_nixos` success (the rebuild finished in its transient unit). It is cleared as soon as the rebuild's outcome is recorded, so it is normally absent.

## Health-watch state

When the [health watch](configuration.md#health-watch) is enabled, it keeps its own file `healthwatch.yaml` in the same directory (also written atomically): per stack, the last successful deploy (`commit`, `at`) and, per service, the last 10 accepted status phases — each with the status, when it began (`since`, second-granular UTC), and the newest commit that had touched the stack at that time. This is what lets a restart resume transition detection without re-alerting known failures. A missing or corrupt `healthwatch.yaml` is a clean slate: the next poll re-baselines silently, no alerts fire.
