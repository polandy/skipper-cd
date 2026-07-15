# skipper-cd State File — Reference

Deployment state is persisted at `/var/lib/skipper/state.yaml`. It stores the per-file hashes from the last successful deployment of each stack, as well as the Git commit SHA at the time of that deployment.

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
