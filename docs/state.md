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

With the [web UI](configuration.md) enabled you don't need to read this file to answer *"why didn't my stack redeploy?"*: expanding a stack in the **Stacks** view lists exactly these tracked inputs, and — after a clean deploy — the commit none of them has changed since.

A `project_dirs` map records each stack's compose project directory (its `project_directory`, or the compose file's own directory) from its last successful deploy. It lets skipper recognise a stack's running compose project by the `com.docker.compose.project.working_dir` label even after the stack is removed from the repo — the basis for [orphan detection](#orphaned-stacks).

If the state file is absent or cannot be parsed (e.g. after a fresh install or corruption), all stacks are redeployed on the next run.

NixOS rebuild state (when [configured](nixos.md#nixos-rebuild)) is tracked under the reserved stack key `_nixos`. State is written atomically (temp file + rename).

A transient top-level key `nixos_rebuild_in_flight` may appear while a rebuild is running: it lists the changed nix files and is written just before a rebuild whose `switch-to-configuration` may restart skipper-cd. If the switch restarts skipper mid-rebuild, the marker survives and the next startup reconciles it into a `_nixos` success (the rebuild finished in its transient unit). It is cleared as soon as the rebuild's outcome is recorded, so it is normally absent.

## Health-watch state

When the [health watch](configuration.md#health-watch) is enabled, it keeps its own file `healthwatch.yaml` in the same directory (also written atomically): per stack, the last successful deploy (`commit`, `at`) and, per service, the last 10 accepted status phases — each with the status, when it began (`since`, second-granular UTC), and the newest commit that had touched the stack at that time. This is what lets a restart resume transition detection without re-alerting known failures. A missing or corrupt `healthwatch.yaml` is a clean slate: the next poll re-baselines silently, no alerts fire.

## Deploy audit log

When the [web UI](configuration.md) is enabled, skipper keeps a durable per-stack deploy audit log at `deploy-audit.jsonl` in the same directory — the "what happened to this stack, and when" trail behind the UI's [deploy-history panel](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md). It is separate from the bounded, in-memory live event feed: one **append-only** JSON record per line, so it survives restarts and is not evicted when older events roll off the live window.

Each record holds the metadata of one **terminal** deploy outcome — `stack`, `timestamp`, `status`, `duration_ms`, `commit_sha`, `changed_files` (a count), and `error` — with no diffs (the commit SHA identifies the change). Only `success`, `failed`, `rolled_back`, `rolled_back_unhealthy`, `healed`, and `heal_exhausted` are recorded; in-progress, skipped and deferred (`queued`/`blocked`) statuses are not. The log keeps the most recent 200 records **per stack** (so a busy stack never evicts a quiet one's history) and is compacted in place — atomically, temp file + rename — to stay bounded; a torn trailing line from a crash mid-append is skipped on load rather than failing the whole file.

## Orphaned stacks

With the [web UI](configuration.md) enabled, skipper shows compose projects running on the host that its current stack set no longer accounts for — for example a stack whose directory was removed from the deploy repo but that is still running. On the health-poll cadence it lists running compose projects and matches each to a stack by its `com.docker.compose.project.working_dir` label:

- **orphaned** — a project skipper once deployed (its directory is under `stacks_base_dir`, or is recorded in `project_dirs`) that no stack now covers. Safe to remove.
- **unmanaged** — a project skipper never deployed (a manually started stack, another tool's containers). Never touched.

Expand a row to see its containers (name, image, state, ports) and any named volumes it holds. Disabled stacks are hands-off and are not flagged. Detection is read-only; removing an orphan is a manual `docker compose down` for now.
