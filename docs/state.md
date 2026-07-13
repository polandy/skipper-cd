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
