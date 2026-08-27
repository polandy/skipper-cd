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

Two image maps are kept, and they answer different questions:

- `images` — the **image references the compose file asks for**, per service. This alone decides whether a deploy needs a `docker compose pull`.
- `running_images` — the **image each service actually ran** after the last successful deploy: the reference the container runs, plus a short image id when that reference carries no digest (`nextcloud:34-ghostscript@40c2d6f1d8f0`; a digest-pinned `traefik:v3.7.9@sha256:…` is kept as-is). It is the baseline the next deploy's reported version change is measured against, so a floating tag that moved shows up as the version change it is. It is never a change-detection input: editing nothing but this cannot trigger a deploy.

A `project_dirs` map records each stack's compose project directory (its `project_directory`, or the compose file's own directory) from its last successful deploy. It lets skipper recognise a stack's running compose project by the `com.docker.compose.project.working_dir` label even after the stack is removed from the repo — the basis for [orphan detection](#orphaned-stacks).

When a stack leaves the deploy set — its directory is gone from the repo, or its entry from the `stacks:` list — the first run that sees it gone records a `removed` deploy event for it and drops its `stacks`, `images` and `running_images` entries; its `project_dirs` entry is kept, so its still-running containers stay recognisable as [orphaned](#orphaned-stacks) rather than unmanaged. Nothing is torn down. A stack added back later therefore deploys from scratch. If a run discovers **no** stacks at all — usually a broken or empty clone, not a deleted fleet — nothing is reported removed and no state is dropped.

If the state file is absent or cannot be parsed (e.g. after a fresh install or corruption), all stacks are redeployed on the next run — but that run deliberately does **not** refresh images already on the host, so no floating tag moves unattended (see [First run and state loss](configuration.md#first-run-and-state-loss)). **Back this directory up** with the rest of your host state.

NixOS rebuild state (when [configured](nixos.md#nixos-rebuild)) is tracked under the reserved stack key `_nixos`. State is written atomically (temp file + rename).

A transient top-level key `nixos_rebuild_in_flight` may appear while a rebuild is running: it lists the changed nix files and is written just before a rebuild whose `switch-to-configuration` may restart skipper-cd. If the switch restarts skipper mid-rebuild, the marker survives and the next startup reconciles it into a `_nixos` success (the rebuild finished in its transient unit). It is cleared as soon as the rebuild's outcome is recorded, so it is normally absent.

## Health-watch state

When the [health watch](configuration.md#health-watch) is enabled, it keeps its own file `healthwatch.yaml` in the same directory (also written atomically): per stack, the last successful deploy (`commit`, `at`) and, per service, the last 10 accepted status phases — each with the status, when it began (`since`, second-granular UTC), and the newest commit that had touched the stack at that time. This is what lets a restart resume transition detection without re-alerting known failures. A missing or corrupt `healthwatch.yaml` is a clean slate: the next poll re-baselines silently, no alerts fire.

## Update-check state

When the [update check](configuration.md#update-check) is enabled, it keeps its own file `update-check.yaml` in the same directory (also written atomically): which advertised update each service was already notified about, so a restart does not re-send messages for updates that are still standing. A missing or corrupt file is harmless — at worst, each standing update notifies once more.

## Deploy audit log

When the [web UI](configuration.md) is enabled, skipper keeps a durable per-stack deploy audit log at `deploy-audit.jsonl` in the same directory — the "what happened to this stack, and when" trail behind the UI's [deploy-history panel](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md). It is separate from the bounded, in-memory live event feed: one JSON record per line, appended as outcomes land, so it survives restarts and is not evicted when older events roll off the live window.

Each record holds the metadata of one **terminal** deploy outcome — `stack`, `timestamp`, `status`, `duration_ms`, `commit_sha`, `changed_files` (a count), and `error` — with no diffs (the commit SHA identifies the change). Only `success`, `failed`, `rolled_back`, `rolled_back_unhealthy`, `healed`, `heal_exhausted`, and `removed` are recorded; in-progress, skipped and deferred (`queued`/`blocked`) statuses are not. The log keeps the most recent 200 records **per stack** (so a busy stack never evicts a quiet one's history) and is compacted in place — atomically, temp file + rename — to stay bounded; a torn trailing line from a crash mid-append is skipped on load rather than failing the whole file.

A stack that keeps failing the same way — the same status with the same error, on every reconcile — is recorded **once**, not once per attempt. That record then also carries `repeat_count` (how many occurrences it stands for) and `first_seen` (the oldest of them), while `timestamp` is the newest; the UI shows it as `×N` on the row. Without this a single standing failure would fill the whole per-stack window in hours and push the stack's real deploys out of it. Only the failure statuses collapse this way — two successes in a row are two deploys and stay two records. Collapsing rewrites the affected record in place, so the file is no longer strictly append-only; a log written before this behaviour existed is folded down the first time it is loaded.

## Orphaned stacks

With the [web UI](configuration.md) enabled, skipper shows compose projects running on the host that its current stack set no longer accounts for — for example a stack whose directory was removed from the deploy repo but that is still running. On the health-poll cadence it lists running compose projects and matches each to a stack by its `com.docker.compose.project.working_dir` label:

- **orphaned** — a project skipper once deployed (its directory is under `stacks_base_dir`, or is recorded in `project_dirs`) that no stack now covers. Safe to remove.
- **unmanaged** — a project skipper never deployed (a manually started stack, another tool's containers). Never touched.

Expand a row to see its containers (name, image, state, ports) and any named volumes it holds. Disabled stacks are hands-off and are not flagged. Detection is read-only; removing an orphan is a manual `docker compose down` for now.

The section opens by itself when an orphaned project first appears, so a stack that left the repo is not hidden behind a collapsed header, and its count is tinted while one is present. The removal is also recorded in the **Deploys** view as a `removed` row against the commit that did it, so *when* a stack stopped being managed stays answerable after the containers are gone.
