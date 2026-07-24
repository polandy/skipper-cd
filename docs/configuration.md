# skipper-cd Configuration — Reference

The configuration file is a YAML file passed via the `-config` flag (default: `/etc/skipper/skipper.yml`).

For a guided walkthrough see the [Quickstart](index.md#quickstart). This page is the full reference for every field.

---

## Minimal Example

The smallest working config. Stack discovery is on by default, so every `<stacks_base_dir>/<name>/docker-compose.yml` in the deploy repo is a stack — no `stacks:` list needed:

```yaml
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: stacks                  # relative to repo_dir; omit for the repo root
webhook_secret: "your-secret-here"
```

skipper clones the repo and deploys every discovered stack; a git push then fires the signed webhook and it redeploys only what changed (a [reconcile loop](#periodic-reconcile) re-syncs on a timer as a safety net). `port` (8080), `metrics_port` (9120), `ui_enabled`, and `autosync` all take their defaults. Add a `stacks:` list only to override a discovered stack (hooks, `deploy_health_check`, …), or set `stack_discovery: false` to list the stacks manually instead.

---

## Full Example

```yaml
repo_url: ssh://git@gitea.example.com/user/nixos-config.git
repo_dir: /var/lib/skipper/repo        # optional, this is the default
branch: main                            # optional, default: main
vars_file: /etc/skipper/vars.env        # optional
command_timeout_seconds: 300            # optional, default: 300
log_format: pretty                      # optional, default: pretty (colored console); "text" or "json" for machine-readable logs
stacks_base_dir: modules                # relative to repo_dir (the clone); omit for the repo root
project_directory_base: /etc/nixos/modules    # optional; see project_directory_base below
webhook_secret: "your-secret-here"
port: 8080
metrics_port: 9120
ui_enabled: true                        # optional, default: true (live web UI on the webhook port)
autosync: true                          # optional, default: true (pause deploys globally)
stack_discovery: false                  # this example lists stacks manually; omit it to auto-discover them (the default)

stacks:
  - name: traefik
    env_files:
      - /run/secrets/rendered/skipper/compose.env

  - name: gitea
    # autosync: false                    # optional; pause just this stack (default: inherit global)
    env_files:
      - /run/secrets/rendered/skipper/compose.env

  # project_directory_base above already derives this stack's project_directory as
  # /etc/nixos/modules/nextcloud (matching the NixOS systemd service that
  # also manages it) — nothing to repeat here.
  - name: nextcloud
    env_files:
      - /run/secrets/rendered/skipper/compose.env

  # A stack whose NixOS-managed directory doesn't follow the pattern still
  # overrides project_directory explicitly; it wins over project_directory_base.
  - name: legacy-app
    project_directory: /etc/nixos/modules/legacy-app-v2
    env_files:
      - /run/secrets/rendered/skipper/compose.env

# Optional: trigger nixos-rebuild when .nix files or flake.lock change.
nixos_rebuild:
  flake: ".#myhost"
```

## Top-level Fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `repo_url` | string | yes | — | URL of the Git repository to clone and pull (supports SSH and HTTPS). |
| `repo_dir` | string | no | `/var/lib/skipper/repo` | Local directory where the repository is cloned. skipper-cd manages this directory independently of any live checkout. Must be absolute when set. |
| `branch` | string | no | `main` | Git branch to track. Used for `git clone --branch` and `git reset --hard origin/<branch>`. |
| `vars_file` | string | no | — | Path to a `KEY=VALUE` env file containing non-secret values available during every `docker compose` invocation (see [vars_file](#vars_file)). Changes to this file trigger redeployment of all stacks. When set, it must exist and be readable. |
| `command_timeout_seconds` | int | no | `300` | Maximum number of seconds a single shell command (`docker compose pull/up`, `git clone/fetch`, `nixos-rebuild`) is allowed to run before being killed. Applies per command; a deploy run has no overall deadline. Must be ≥ 0. |
| `log_format` | string | no | `pretty` | Log output format: `pretty` (colored, icon-led console narration — see [Pretty console output](#pretty-console-output)), `text` (logfmt), or `json` (structured logs, e.g. for Loki ingestion). |
| `stacks_base_dir` | string | no | repo root | Base directory holding one subdirectory per stack (`<stacks_base_dir>/<name>/docker-compose.yml`); always the source of the compose file and change detection. Relative to `repo_dir` (the clone) — e.g. `stacks`, not `/var/lib/skipper/repo/stacks`. Omit it for the repo root itself. An absolute value, or one escaping the clone via `../`, is rejected at startup. |
| `project_directory_base` | string | no | — | Base directory a stack's `project_directory` is derived from as `<project_directory_base>/<name>` when the stack does not set its own `project_directory` (see [project_directory and Docker Compose project identity](nixos.md#project_directory-and-docker-compose-project-identity)). Avoids repeating a common prefix (e.g. a NixOS modules directory) across stacks. Must be absolute — checked at startup. |
| `webhook_secret` | string | **yes** | — | HMAC-SHA256 secret validating incoming webhook payloads (Gitea and GitHub/Forgejo signatures). Required — the webhook is skipper's primary deploy trigger, so every request is signature-verified; an empty secret is rejected at startup. |
| `port` | int | no | `8080` | Port on which the webhook HTTP server listens. Exposes `/webhook` and `/healthz` (200 while the last repository sync succeeded or none ran yet, 503 with the error when it failed). Must be 1–65535 and differ from `metrics_port`. |
| `metrics_port` | int | no | `9120` | Port on which the Prometheus metrics HTTP server listens. Exposes `/metrics`. Must be 1–65535 and differ from `port`. |
| `ui_enabled` | bool | no | `true` | Serve the web UI (live deploy dashboard, event history, [autosync](autosync.md) controls) on the webhook `port`. Also required for [stack health](#stack-health), [service icons](#service-icons), the deploy audit API, and the [PWA](pwa.md). |
| `autosync` | bool | no | `true` | Global default for whether detected changes deploy automatically. Set to `false` to pause all stacks (a per-stack `autosync` still overrides it). See [Autosync](autosync.md). |
| `stacks` | list | no | — | List of Docker Compose stacks (see [Stack Fields](#stack-fields)). Under discovery (the default) it is optional and holds only per-stack overrides, matched to discovered directories by `name`. With `stack_discovery: false` this list *is* the stack set. |
| `stack_discovery` | bool | no | `true` | Discover the stack set from the deploy repo: every directory under `stacks_base_dir` with a `docker-compose.yml` is a stack; per-stack overrides come from the optional `stacks:` list above (see [Stack discovery](#stack-discovery)). Set `false` to list the stacks in this file yourself. |
| `nixos_rebuild` | object | no | — | NixOS rebuild configuration (see [NixOS](nixos.md)). Omit the section entirely to disable. |
| `icons` | object | no | — | Web-UI service-icon configuration (see [Service Icons](#service-icons)). Omit to use defaults. |
| `notifications` | list | no | — | Outbound notification targets messaged on terminal deploy outcomes (see [Notifications](#notifications)). Omit to disable. |
| `ui_theme` | string | no | `catppuccin` | Web UI colour palette: one of `catppuccin`, `nord`, `solarized`, `gruvbox`, `rose-pine` (see [Web UI Theme](#web-ui-theme)). |
| `ui_theme_switcher` | bool | no | `false` | Show the in-UI theme picker so a browser can try other palettes locally. Off by default — the deployed `ui_theme` is then fixed (see [Web UI Theme](#web-ui-theme)). |
| `runtime_health_poll_interval_seconds` | int | no | `30` | How often the web UI polls its stacks' runtime health (see [Stack health](#stack-health)). `0` disables the health view. Only used when `ui_enabled`; the poll also runs only while a browser is connected. |
| `reconcile_interval_seconds` | int | no | `300` | How often skipper re-runs its git sync + deploy on a timer, so a missed webhook cannot leave the host drifted (see [Periodic reconcile](#periodic-reconcile)). `0` disables it (pure webhook + startup). Runs headless — not tied to the UI. |
| `self_heal` | bool | no | `false` | Global default for whether a stack the health poller finds degraded is automatically restored to its running state by a corrective redeploy (see [Self-heal](#self-heal)). A per-stack `self_heal` overrides it. Requires `runtime_health_poll_interval_seconds` > 0. |
| `self_heal_min_unhealthy_polls` | int | no | `3` | Consecutive degraded health polls a stack must show before self-heal acts (debounce). Must be ≥ 1. |
| `self_heal_max_attempts` | int | no | `3` | Corrective redeploys per outage before self-heal gives up and reports `heal_exhausted`. Must be ≥ 1. |
| `self_heal_cooldown_seconds` | int | no | `60` | Minimum gap between corrective redeploys of the same stack. Must be ≥ 0; an explicit `0` disables the cooldown. |
| `health_watch` | object | no | — | Own-stack health watchdog: detects per-service health transitions on the health poller's feed and alerts on failures/recoveries (see [Health watch](#health-watch)). Omit the section to disable. Requires `runtime_health_poll_interval_seconds` > 0. |
| `host_name` | string | no | OS hostname | This instance's own label in the merged [multi-host](#multi-host) view. Only meaningful when other instances fan this one in, or when this one lists `peers`. |
| `peers` | list | no | — | Other skipper instances whose read data this one fans into a single merged UI (see [Multi-host](#multi-host)). Each entry is `{ name, url }`. Omit for a single-host instance. |

## Pretty console output

With the default `log_format: pretty`, startup logs the effective stack set (name, hook counts, watch dirs), and every sync-and-deploy run is narrated with icons and color, mirroring the web UI's Deploys view:

```
14:32:07  ▣ stacks  · 4 discovered
14:32:07  ◆ nextcloud  hooks pre_deploy·1 post_deploy·1   watch ./nextcloud
14:32:08  ⇢ run starting  · 4 stacks
14:32:08  ▸ nextcloud  changed · 2 files
14:32:08    ↳ pre_deploy [1]
14:32:20  ✓ nextcloud  deployed
14:32:41  ↺ arr-stack  rolled back  — health check failed: GET /ping: context deadline exceeded
14:32:41  ▪ monitoring  unchanged, skipped
14:32:41  ✗ run complete  1 deployed · 1 rolled back · 1 skipped
```

Color auto-disables when stdout is not a terminal (e.g. redirected to a file) or `NO_COLOR` is set; icons still render. Use `log_format: text` or `log_format: json` for a log shipper (Loki, journald) or any other machine consumer.

## Stack Fields

Each entry under `stacks` configures one Docker Compose stack.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `name` | string | yes | — | Name of the stack. The compose file is always read from `<stacks_base_dir>/<name>/docker-compose.yml`. Also used as the key in the deploy state file. |
| `project_directory` | string | no | `<project_directory_base>/<name>` if set, else unset | Absolute path passed as `--project-directory` to `docker compose`. Controls Docker Compose project identity (container labels) and `.env` file loading. Change detection and the compose file always come from `<stacks_base_dir>/<name>`. Must be absolute — checked at startup. See [project_directory and Docker Compose project identity](nixos.md#project_directory-and-docker-compose-project-identity). |
| `env_files` | list of strings | no | — | Paths to `KEY=VALUE` env files whose contents are injected into the `docker compose` environment. These files are also hash-tracked: a change to any declared env file triggers a redeploy of that stack. With `stack_discovery: false` every entry must be absolute. Under discovery, a relative entry resolves against `stacks_base_dir` (see [Stack discovery](#stack-discovery)). |
| `watch_dirs` | list of strings | no | — | Paths to directories whose contents are recursively hash-tracked. Any file change inside a watched directory triggers a redeploy of that stack. Useful for stacks with auxiliary configuration directories (e.g. Grafana provisioning). Same absolute-path rule as `env_files` above. |
| `on_demand_containers` | list of strings | no | — | Container names to stop after a successful deployment. Use this for containers managed by an on-demand scheduler (e.g. Sablier): skipper-cd starts them via `docker compose up`, then immediately stops them so the scheduler can control their lifecycle. The [Stack health](#stack-health) view and the [health watch](#health-watch) know about this: an exited on-demand container reads as `stopped` (its intended idle state) whatever its exit code — never as `unhealthy`. Docker Compose auto-generates container names (`<project>-<service>-1`) unless the service sets `container_name:`; a name here that matches no declared `container_name` logs a startup-adjacent warning at deploy time (not a hard error, since the auto-generated name may still happen to be right) — set `container_name:` on the corresponding service to make it deterministic and checked. |
| `icon` | string | no | — | Icon-set slug for this stack's web-UI icon (e.g. `jellyfin` for a stack named `media`). Overrides the auto-match on the stack name. See [Service Icons](#service-icons). Purely visual — never hash-tracked. |
| `autosync` | bool | no | *inherit* | Overrides the global `autosync` for this stack (in both directions). When unset, the stack follows the global setting. See [Autosync](autosync.md). |
| `deploy_health_check` | section or bool | no | automatic if the compose file has a `healthcheck:` (except on-demand stacks) | Post-deploy health gate: when the stack does not become healthy after a deploy, it is rolled back to the previous version. Applied automatically at the default timeout when the compose file declares a `healthcheck:` — set this as a section to change the timeout or add an HTTP probe. A stack with `on_demand_containers` is never auto-gated. The scalar `false` explicitly disables the gate (keep a compose `healthcheck:` without gating on it); `true` enables it at the defaults. See [Health-check-gated rollback](#health-check-gated-rollback). |
| `self_heal` | bool | no | *inherit* | Overrides the global `self_heal` for this stack (in both directions). When unset, the stack follows the global setting. See [Self-heal](#self-heal). |
| `depends_on` | list of strings | no | — | Names of other stacks that must deploy before this one. Entries must name defined stacks and the graph must be acyclic. See [Deploy ordering](#deploy-ordering). |
| `hooks` | section | no | — | Shell commands run before (`pre_deploy`) and after (`post_deploy`) this stack's deploy — e.g. a database backup before it updates. Never hash-tracked. See [Deploy hooks](#deploy-hooks). |
| `rollout` | section | no | — | Deploy the named services with a zero-downtime cutover (new container alongside the old, then drain) instead of an in-place recreate. Needs a reverse proxy in front of the service (only Traefik tested). Never hash-tracked. See [Zero-downtime rollout](#zero-downtime-rollout). |

## Stack discovery

Stack discovery is **on by default** (`stack_discovery: true`; set `false` to list the stacks yourself). The **stack set** is discovered from the deploy repo — every directory under `stacks_base_dir` with a `docker-compose.yml` is a stack — so adding or removing a stack is a single git push. Per-stack **overrides** live in this one config's optional `stacks:` list, matched to discovered directories by `name`:

```
deploy-repo/
└── stacks/               # = stacks_base_dir
    ├── gitea/docker-compose.yml
    ├── traefik/docker-compose.yml
    └── wip/docker-compose.yml
```

```yaml
# host config (skipper.yaml)
stack_discovery: true
stacks_base_dir: stacks     # relative to repo_dir; omit to discover from the repo root
stacks:                     # optional — only the exceptions
  - name: traefik
    depends_on: [gitea]
    deploy_health_check: { url: http://localhost:8080/ping }
  - name: wip
    disabled: true          # discovered, deliberately not deployed
```

- Every directory under `stacks_base_dir` with a `docker-compose.yml` is a stack; name = directory name, deploy order alphabetical (plus `depends_on`). Defaults apply — a bare directory with no `stacks:` entry is a fully functional stack.
- **Fields** — the [Stack Fields](#stack-fields) above, plus `disabled`. An entry's `name` must match a discovered directory (a typo fails that entry). Relative `env_files`/`watch_dirs` paths resolve against `stacks_base_dir`; a relative path that escapes it via `../`, or that does not exist, fails that stack entry (absolute paths are unrestricted and not existence-checked — the host-secret escape hatch).
- **`disabled: true`** — parked: not deployed, not health-checked; a running stack keeps running. The web UI lists parked names in a `disabled` line below the deploy table.
- **One config file** — there is no separate in-repo override file. A leftover `<stacks_base_dir>/skipper.yaml` in the clone is **not read** and fails the whole stack phase loudly (a `_config` row), so un-migrated overrides never silently revert stacks to defaults.
- **Config edits redeploy** — changing a stack's effective config (e.g. its `watch_dirs`) redeploys exactly that stack. Because the config is host-side, the change is not a tracked repo file, so the UI shows the redeploy without a config diff (the diff is in the host's own git, e.g. `/etc/nixos`).
- **Validated every sync** — because discovery runs against the repo clone, each stack is checked up front, not only when it next deploys: its `docker-compose.yml` must parse, any relative `env_files`/`watch_dirs` must exist, and any `rollout` service must be present and eligible. A failure shows on the stack's row and excludes only that stack.
- **Self-heal** — activation is global-only here: the poller runs only when the host config sets `self_heal: true`. A per-stack `self_heal: true` cannot turn it on by itself (it never activates); once global is on, per-stack `self_heal: false` opts a stack out. A `stacks:` override that sets `self_heal: true` while the global flag stays off logs a startup warning, since it looks like a working opt-in but silently never activates.

## Health-check-gated rollback

Without any gate, a rollback only happens when `docker compose up` itself fails. A deploy whose containers *start* but stay broken (crash-loop, 500s) would be marked successful. A `healthcheck:` in the compose file closes that gap automatically (see Stage 1 below); a `deploy_health_check` section in this config adds an HTTP probe on top, or tunes the timeout:

```yaml
stacks:
  - name: whoami
    deploy_health_check:
      timeout_seconds: 60                # optional, default: 60
      url: http://localhost:8080/health  # optional HTTP probe
```

The gate has two stages.

### Stage 1 — the compose healthcheck (automatic)

The `healthy` state comes from a [`healthcheck:`](https://docs.docker.com/reference/compose-file/services/#healthcheck) defined on a service in your compose file. **This is the exposure-free path: the healthcheck command runs *inside* the container, so nothing needs a published port.** For a container whose endpoint is not reachable from the host, this is the mechanism to use:

```yaml
# docker-compose.yml
services:
  web:
    image: myapp:latest
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]  # localhost = inside the container
      interval: 5s
      retries: 5
```

As soon as any service in the stack's compose file declares a `healthcheck:`, skipper-cd applies this gate **automatically** — no `deploy_health_check` section needed in this config at all. `docker compose up` runs with `--wait --wait-timeout 60` (the default), so the `up` fails and the deploy rolls back unless every service reaches `running` (services without a healthcheck) or `healthy` (services with one). Requires Docker Compose v2.17+.

Add an explicit `deploy_health_check` section only to change the default 60s timeout, or to add the stage 2 HTTP probe below — it always wins over the automatic gate. A stack with no compose `healthcheck:` anywhere stays ungated unless it sets `deploy_health_check` itself.

**On-demand stacks are never auto-gated.** A stack with [`on_demand_containers`](#stack-fields) is stopped right after `up`, so skipper skips the automatic gate for it even when its compose file declares a `healthcheck:` — `--wait` would cold-start the on-demand container only for skipper to stop it again, and a slow warm-up would time out into a spurious rollback. No config is needed; set an explicit `deploy_health_check` only if you deliberately want a gate on such a stack.

**Opting out on any other stack.** To keep a compose `healthcheck:` (for external monitoring, `docker ps` status, or an orchestrator) *without* letting skipper `--wait` on it and roll back, set the scalar `deploy_health_check: false`. It overrides the automatic gate so the stack deploys with a plain `up`. The scalar `deploy_health_check: true` is the inverse — gate on at the defaults, equivalent to an empty `deploy_health_check: {}` mapping.

```yaml
stacks:
  - name: metrics-exporter
    deploy_health_check: false   # compose healthcheck is for monitoring; don't gate the deploy on it
```

### Stage 2 — the HTTP probe (optional)

Only when `url` is set: after a successful `up`, skipper-cd GETs the URL every 2 seconds until it answers with a 2xx status; anything else for `timeout_seconds` fails the deploy. The probe runs **from the skipper-cd host**, so the URL must be reachable from there (a published port on `localhost`, or a routable address via a reverse proxy). Use it when the stack has no internal `healthcheck:` but does expose a reachable endpoint, or as an extra end-to-end check on top of stage 1.

A failure in either stage triggers the regular rollback: the previous compose file is restored from the last deployed Git commit, the deploy is marked `rolled_back` (events, metrics and [notifications](#notifications) all see that status), and the change stays pending so the next push retries.

The rollback itself is verified through the same gate: its `up` also runs with `--wait`, and the HTTP probe (if configured) must pass again. So `rolled_back` guarantees the old version is actually healthy again. If the restored version *also* fails the gate — typically an environment problem such as a dead database or a broken secret — the deploy is marked **`rolled_back_unhealthy`** instead: the stack sits on the old compose file but needs attention *now*, because no version of it is verified healthy.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `timeout_seconds` | int | no | `60` | Wait budget, used both as `--wait-timeout` for compose and as the HTTP probe deadline. |
| `url` | string | no | — | HTTP(S) URL probed **from the host** after a successful `up`; must answer 2xx within `timeout_seconds`. Omit to rely on the container's compose `healthcheck:` alone (the exposure-free path). |

> **Note:** with `--wait`, a service that exits — even successfully — counts as a failure. Stacks with `on_demand_containers` are excluded from the automatic gate for this reason; for a deliberate one-shot container on a non-on-demand stack, set `deploy_health_check: false` to opt out (even if the compose file has a `healthcheck:`), or model the one-shot as a [`service_completed_successfully`](https://docs.docker.com/compose/how-tos/startup-order/) dependency.

## Deploy hooks

Run a shell command around a stack's deploy — most often a **backup before it updates**:

```yaml
stacks:
  - name: paperless
    hooks:
      pre_deploy:
        - "docker exec paperless-db pg_dump -U paperless | zstd > /backup/paperless-$(date +%F-%H%M).sql.zst"
      post_deploy:
        - "curl -fsS http://localhost:8000/api/health/"
      timeout_seconds: 120   # optional; per-hook, capped by command_timeout_seconds
```

- Each entry is one `sh -c` command line, run in order. Env = the stack's deploy environment plus `SKIPPER_STACK` and `SKIPPER_HOOK` (`pre_deploy`/`post_deploy`); working directory = the stack's project directory.
- `pre_deploy` runs **before any container is touched** — the old version is still up, so a `docker exec … pg_dump` backs up the running old version. A failing `pre_deploy` hook aborts the deploy before pull/up, with no rollback (nothing changed); the next sync retries. On a stack's first-ever deploy there is nothing to back up yet — guard the command if that matters.
- `post_deploy` runs after a successful `up` (and health gate). A failing `post_deploy` hook **rolls the deploy back** to the previous version, exactly like a `deploy_health_check` failure — even without a `deploy_health_check` set.
- Hooks are **not** hash-tracked: editing a hook does not trigger a redeploy; it takes effect on the stack's next deploy. (A hook that runs a script under a `watch_dirs` entry still redeploys when that script changes.)
- Hooks run only when the stack actually deploys — never on a skip, a self-heal, or a rollback.
- `timeout_seconds` bounds each hook; `command_timeout_seconds` is the hard ceiling (a larger value has no effect — and logs a startup warning). For a backup slower than that, raise `command_timeout_seconds`.

In the web UI, a stack with hooks shows a **hook badge** on its row (in both the Deploys and Stacks views) with a `pre+post` count — click it to see the configured commands. While a deploy runs a hook, the row shows which one is running (`pre_deploy hook 1/2`); the logs icon there opens the hook's live output in a log panel, inline on the same page.

## Zero-downtime rollout

Recreating a service in place stops the old container before the new one serves — a brief gap. `rollout` closes that gap: skipper starts the new container alongside the old, waits for it to become healthy, then drains the old one, so a healthy container is serving the whole time.

```yaml
stacks:
  - name: dashboard
    rollout:
      services: [web]              # only these roll; every other service recreates in place
      health_timeout_seconds: 60   # optional; default = deploy_health_check timeout, else 60
      drain_seconds: 5             # optional; hold the old container this long after the new one is healthy
```

**How it works.** For each listed service, skipper starts a second container, waits for its `healthcheck` to pass, waits `drain_seconds`, then stops and removes the old container. The reverse proxy in front of the service is what shifts traffic onto the new container and stops using the old one — skipper does no proxy configuration.

**Requirements.**

- A **reverse proxy** in front of the rolled services that load-balances across a service's containers by health and stops sending traffic to one once it is removed. **Only Traefik (v3) has been tested**; any proxy with these properties should work, but that is untested. Route the service through the proxy the way you already do (e.g. Traefik Docker labels) — there is nothing rollout-specific to configure on the proxy.
- Each rolled service must **name a service defined in the stack's `docker-compose.yml`**, **publish no host `ports:`** (two replicas would collide on the port — route it through the proxy), set no fixed **`container_name:`** (compose cannot scale a named container), and define a compose **`healthcheck:`** (the readiness signal). These are checked at deploy time, and in **stack-discovery mode** also at discovery — every sync, before any deploy — so a mistake surfaces in the Stacks view immediately (since `rollout` is not hash-tracked, an edit alone would otherwise not redeploy to reveal it).
- **`drain_seconds`** waits that many seconds after the new container is healthy before removing the old one, so the proxy can start routing to the new container while the old is still up (the equivalent of `docker-rollout`'s `--wait-after-healthy`). With Traefik, adding a **retry** middleware to the service as well makes a cutover drop from seconds of `502`s (in-place recreate) to at most a single sub-second blip.
- Only listed services roll. Everything else — databases, anything with a volume lock or a published port — recreates in place as usual.
- **Failure is zero-downtime too:** if the new container never turns healthy, skipper removes it and leaves the old one serving. The deploy is reported as `rolled_back`.
- `rollout` is **not** hash-tracked: switching a service to/from rollout does not itself redeploy.
- On a service's first-ever deploy (nothing running yet) there is no gap to avoid, so it comes up with a plain `up`.

## `vars_file`

The `vars_file` is a `KEY=VALUE` env file containing non-secret configuration values (e.g. a domain name) that should be available as environment variables during `docker compose` execution. This is useful for `${VAR}` substitutions in Docker Compose files without storing the values in a secrets manager.

**Example `vars.env`:**
```env
DOMAIN=example.com
INTERNAL_IP=192.168.1.10
```

**Environment variable precedence** (highest wins):
1. `env_files` entries (stack-level, typically secrets)
2. `vars_file` values (global non-secret values)
3. Process environment (`os.Environ()`)

The `vars_file` is included in hash tracking — any change to it triggers a redeploy of all stacks.

## Service Icons

When the web UI is enabled, each stack shows an icon in the deploy table for at-a-glance recognition. Icons are resolved per stack in priority order:

1. **Repo override** — an `icon.svg` (preferred) or `icon.png` in the stack's directory (`<stacks_base_dir>/<name>/`). Served directly from the clone, works offline.
2. **Configured slug** — the stack's `icon:` field, looked up in the icon set.
3. **Auto-match** — the stack name (slugified) looked up in the icon set, trying SVG, then PNG, then WebP.
4. **Fallback** — a monogram (the stack's first letter) rendered in the UI when nothing matches or the source is unreachable.

Browse available slugs at the [dashboard-icons repo](https://github.com/homarr-labs/dashboard-icons) before setting `icon:`.

Icons from the set are fetched once and cached on disk; the UI serves them same-origin from `/api/icons/<stack>`. `POST /api/icons/refresh` clears the cache so renamed stacks and newly published icons are picked up on the next load (e.g. `curl -X POST …/api/icons/refresh`).

The reserved NixOS pseudo-stack `_nixos` (see [NixOS](nixos.md)) is not a configured stack, so it resolves its icon by auto-matching the `nixos` slug — giving the rebuild a recognizable logo rather than a monogram.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `cache_dir` | string | no | `/var/lib/skipper/icons` | Directory where fetched icons are cached on disk. Must be absolute. |
| `source_url` | string | no | dashboard-icons CDN | Icon-set **root** URL; icons are fetched from `<source_url>/<format>/<slug>.<format>` for `format` in `svg`, `png`, `webp` (first hit wins). |

```yaml
icons:
  cache_dir: /var/lib/skipper/icons        # optional, this is the default
  source_url: https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons
```

> **Note:** A repo `icon.svg`/`icon.png` is **not** hash-tracked, so adding or changing an icon never triggers a redeploy. The one exception: if you list a stack's own directory under `watch_dirs`, its icon files would be hashed along with everything else — keep icons out of watched directories.

## Traefik app links

skipper-cd has no Traefik configuration of its own — there is nothing to set in `skipper.yml`. Instead, when the web UI is enabled, it reads the [Traefik](https://doc.traefik.io/traefik/) routing labels your stack's `docker-compose.yml` *already* declares and, when it finds one, shows a link icon in the Stacks view that opens the app directly. This section documents how skipper-cd interprets **your** Traefik setup, not a feature you configure here.

```yaml
services:
  app:
    labels:
      traefik.enable: "true"
      traefik.http.routers.app.rule: "Host(`app.example.com`)"
```

- Detection requires `traefik.enable: "true"` on the container — Traefik's own opt-in convention (skipper-cd honours the same gate Traefik itself does; a container Traefik wouldn't route to gets no icon either).
- The hostname is read from the container's **live** labels (`docker inspect`), not the compose file text, so `${DOMAIN}`-style variables resolve to their real, running value.
- A stack with several routers/hosts shows a popover to pick from; one with no matching label shows no icon — there is nothing to turn on or off.
- Links always open over `https://`.

> **Docker labels only.** This only sees routing declared via Traefik's **Docker provider** (the `labels:` shown above). A stack whose Traefik routes are instead defined through the file/dynamic provider, a Kubernetes Ingress, or any non-Docker provider carries no container labels for skipper-cd to read, so it gets no icon — even though Traefik is actively routing to it. That's not a bug to work around; it's just outside what this reads.

## Notifications

skipper-cd can POST a message to one or more targets whenever a deploy reaches a **terminal** outcome. This works independently of the web UI — notifications need neither `ui_enabled` nor an open browser.

Only terminal statuses are ever delivered: `failed`, `success`, `rolled_back`, `rolled_back_unhealthy`, `heal_exhausted` (the transient `deploying`, `skipped` and `queued` are never sent, and neither is the routine `healed`). Each target chooses which of the five it wants via `on:`; when omitted, all five are delivered.

Delivery is **fire-and-forget**: sending runs in the background with its own 10-second per-request timeout, never blocking or delaying a deploy. A failed or slow target is logged and dropped — there is no retry queue, so a notification can be lost if the target is down. For guaranteed history, read the persisted events or scrape metrics instead.

```yaml
notifications:
  # Signal via the signal-cli-rest-api stack.
  - format: signal
    url: http://localhost:8020        # service base; /v2/send is appended
    number: "+491234567890"           # sender (required for signal)
    recipients: ["+491234567890"]     # recipients (required for signal)
    prefix: host-a                    # optional; message becomes "[host-a] …"
    # on:  defaults to [failed, success, rolled_back, rolled_back_unhealthy, heal_exhausted]

  # A second, independent target for failures only.
  - format: generic
    url: https://ntfy.example.com/skipper
    headers:
      Authorization: "Bearer <token>"
    on: [failed, rolled_back, rolled_back_unhealthy]
```

### Target Fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `format` | string | no | `generic` | Provider shape of the request: `signal` or `generic`. |
| `url` | string | yes | — | Endpoint the notification is POSTed to. For `signal` this is the `signal-cli-rest-api` **base** (e.g. `http://localhost:8020`); `/v2/send` is appended automatically. |
| `on` | list | no | all five | Subset of `failed`, `success`, `rolled_back`, `rolled_back_unhealthy`, `heal_exhausted` that triggers this target. |
| `prefix` | string | no | — | Prepended as `[<prefix>] ` to the `signal` message, e.g. to label which host/instance sent it. Ignored by `generic` (its structured payload already carries the event). |
| `headers` | map | no | — | Static HTTP headers added to the request. Only meaningful for `generic` (e.g. an auth token). |
| `number` | string | for `signal` | — | Signal sender number. Required for `format: signal`, rejected on any other format. |
| `recipients` | list | for `signal` | — | Signal recipient numbers (non-empty). Required for `format: signal`, rejected on any other format. |

### Formats

| Format | Body sent | Use for |
|---|---|---|
| `signal` | `{"message": …, "number": …, "recipients": […]}` to `<url>/v2/send` | The `bbernhard/signal-cli-rest-api` service. |
| `generic` | The full deploy event as JSON (diffs and commit metadata stripped), plus any `headers` | ntfy, Gotify, or your own endpoint. |

> **Reachability.** `url` must be reachable from wherever skipper-cd runs. As a **host service** (e.g. the [NixOS module](nixos.md)), a container's host-published port is `http://localhost:8020` — reaching it directly, bypassing any reverse proxy/auth. When skipper-cd itself runs **in a container**, `localhost` is the container, not the host (see [Docker](docker.md)).

> **No email.** Native SMTP is deliberately unsupported. Point a `generic` target at an email-capable relay (ntfy, Apprise, Shoutrrr) if you need email.

> **Self-notify caveat.** If Signal is delivered through a `signal-api` stack that skipper itself deploys, a failed `signal-api` deploy cannot notify you about itself. Configure a second, independent target (e.g. `generic` → ntfy) so at least one path never depends on the stack being reported on.

## Web UI Theme

`ui_theme` picks the web UI's colour palette. This is a per-instance, config-time choice — handy for telling several skipper-cd instances apart at a glance (e.g. one per host) — and is independent of the header's dark/light toggle, which stays a per-browser preference within whichever palette is configured.

```yaml
ui_theme: nord   # optional, default: catppuccin
```

Five palettes ship, each with a dark and light variant (the header's dark/light toggle flips between them per browser). The previews below are scaled-down mockups of the header and stack list in each palette.

**`catppuccin`** — the default. Soft pastels on a muted indigo base (Mocha dark / Latte light).

<div class="theme-grid">
<figure class="tp" data-theme="catppuccin"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>Mocha · dark</figcaption></figure>
<figure class="tp light" data-theme="catppuccin"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>Latte · light</figcaption></figure>
</div>

**`nord`** — arctic blue-greys, a single frost-blue accent.

<div class="theme-grid">
<figure class="tp" data-theme="nord"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>dark</figcaption></figure>
<figure class="tp light" data-theme="nord"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>light</figcaption></figure>
</div>

**`solarized`** — the low-contrast terminal classic.

<div class="theme-grid">
<figure class="tp" data-theme="solarized"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>dark</figcaption></figure>
<figure class="tp light" data-theme="solarized"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>light</figcaption></figure>
</div>

**`gruvbox`** — warm retro-groove browns, orange accent.

<div class="theme-grid">
<figure class="tp" data-theme="gruvbox"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>dark</figcaption></figure>
<figure class="tp light" data-theme="gruvbox"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>light</figcaption></figure>
</div>

**`rose-pine`** — muted rose and iris-purple on a plum-black base.

<div class="theme-grid">
<figure class="tp" data-theme="rose-pine"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>dark</figcaption></figure>
<figure class="tp light" data-theme="rose-pine"><div class="tp-bar"><span class="tp-logo">skipper<i>-cd</i></span><span class="tp-dot tp-a"></span><span class="tp-dot tp-s"></span><span class="tp-dot tp-d"></span></div><div class="tp-body"><div class="tp-card"><span class="tp-dot tp-s"></span><span class="tp-line"></span><em class="tp-badge tp-s">up</em></div><div class="tp-card"><span class="tp-dot tp-d"></span><span class="tp-line short"></span><em class="tp-badge tp-d">fail</em></div></div><figcaption>light</figcaption></figure>
</div>

Every palette drives the whole UI, including the PWA install identity (favicon, browser theme colour, app splash screen) — see [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md#design) for the full token design and [PWA](pwa.md) for the installed-app behaviour.

### Theme switcher

By default the deployed `ui_theme` is fixed: there is no way to change it from the browser, which keeps the per-instance colour a reliable at-a-glance marker. Set `ui_theme_switcher: true` to add a small palette picker to the header — handy for trying the built-in themes out before committing one to the config.

```yaml
ui_theme_switcher: true   # optional, default: false
```

The picker is a **local, per-browser** override only: it never changes the deployment's `ui_theme` (there is no endpoint to do so) and only affects the browser that set it, applied instantly with no reload. While an override differs from the configured theme, a dismissible notice under the header points it out. Turning the flag back off hides the picker and re-pins every browser to the configured theme (a stale override left in a browser's `localStorage` simply lies dormant). See [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md#theme-override) for the exact behaviour.

## Multi-host

Run skipper on several hosts and view them all in **one merged UI**. Every host keeps running its own independent skipper (own clone, own state, own webhook); one instance is additionally the **primary** that reads the others' data and renders it together. Deploys stay per-host and git-driven — the primary only ever *reads* from its peers, never deploys or controls them.

```yaml
# on the primary only
host_name: host-a
peers:
  - name: host-b
    url: http://host-b:8001
  - name: host-c
    url: http://host-c:8001
```

- **Only the primary needs `peers`.** A peer needs no extra config — just `ui_enabled: true` (it already serves the read API) and to be reachable from the primary.
- `name` is the host's label and its identity colour in the merged view; `url` is its skipper base URL on the LAN.
- `host_name` is the primary's own label (defaults to the OS hostname); set it to match how the hosts name each other.
- **What you get:** the Deploys and Stacks views merge every host's rows, each tagged with a colour **host dot**. A **Hosts** control (top right) filters by host and links out to each host's own UI. Click a host dot to isolate the view to that host; click again to clear. The filter is remembered per browser.
- **Read-only peers.** Peer rows mirror state only — clicking one shows that deploy's commit, its **containers/health**, its **container logs**, its app link and diff (all loaded from the peer), but no local actions. To watch a peer deploy live or see its full history, open its own UI from the Hosts control.
- **Reachability.** The merged view refreshes on the [health-poll](#stack-health) cadence, so it can lag a few seconds behind a peer's live deploy. An unreachable peer's last-known rows stay, dimmed, with a banner — never silently dropped.
- **Access.** Peers must be reachable from the primary (server-to-server), not from the browser. Keep each peer's port on the LAN and firewall it to the primary's address.

If the primary goes down, every host still deploys on its own webhook and serves its own UI at its own address — the primary is a convenience layer, not a dependency.

## Stack health

When the web UI is enabled, skipper-cd shows the **live runtime health** of the stacks it deploys, next to each stack in the deploy view: `healthy`, `unhealthy`, `starting`, `stopped`, or `unknown`. This is a current, at-a-glance view — distinct from the deploy outcome — so a stack that deployed fine but whose container has since started crash-looping reads as `unhealthy` without waiting for the next push.

The health is read by polling `docker compose ps` for each stack, using the same compose file and `--project-directory` identity used to deploy it. Nothing is written and nothing is restarted — the view is read-only. It covers only skipper-cd's own stacks (not other containers on the host). Click a stack's health to expand a per-service breakdown (each service's container state and health).

Containers listed in a stack's [`on_demand_containers`](#stack-fields) get special treatment: skipper stops them itself after every deploy (often via SIGKILL, so they exit non-zero) and the on-demand scheduler starts them on request — that idle is their intended state. An exited on-demand container therefore always reads as `stopped`, never `unhealthy`, whatever its exit code; the per-service panel labels it `on-demand`. A crash-looping or unhealthy on-demand container still counts as a real failure.

`runtime_health_poll_interval_seconds` controls the cadence:

- **default `30`** — poll every 30 seconds.
- **`0`** — disable the health view entirely (no polling, no pill).
- The poll only runs while `ui_enabled` **and** at least one browser is connected to the dashboard, so an unattended instance does no health work.

On a resource-constrained host, raise the interval rather than disabling it. See [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md#stack-health) for the UI surface.

## Health watch

The [Stack health](#stack-health) view only *shows* health, and only while a browser is watching. The **health watch** goes one step further: it tracks each **service's** health over time and reports **transitions**: a service turning `unhealthy` fires an alert, and its recovery fires a matching all-clear. Transitions between the other statuses (`starting`, `stopped`) are recorded and logged but never alert, so an intentional `docker compose down` does not page.

The watchdog has no poll loop of its own: it consumes the health poller's feed (like [self-heal](#self-heal)), so its cadence **is** `runtime_health_poll_interval_seconds` — which must therefore be > 0 — and enabling it makes that poll run headless, independent of the UI and of connected browsers. There is no extra `docker compose ps` work: one poll serves the UI view, self-heal, and the watchdog.

```yaml
health_watch:                      # cadence = runtime_health_poll_interval_seconds
  debounce_polls: 2                # consecutive confirmations before a change is accepted
  attribution_window_seconds: 300  # transitions this soon after a deploy are marked deploy-correlated
  alert_cooldown_seconds: 1800     # rate limit for repeat alerts of one service; 0 = off
  targets:                         # optional; same fields as notifications targets, but no `on:`
    - format: signal
      url: http://localhost:8020
      number: "+491234567890"
      recipients: ["+491234567890"]
      prefix: host-a
```

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `debounce_polls` | int | no | `2` | How many consecutive health polls a new status must persist before it is accepted. Absorbs transient blips; a service flapping every poll never alerts. |
| `attribution_window_seconds` | int | no | `300` | A transition beginning within this window after a stack's deploy is reported as *deploy-correlated* (with the deploy's newest commit). Later transitions still carry the commit as context, without the correlation. |
| `alert_cooldown_seconds` | int | no | `1800` | Minimum gap between repeat alerts of the same service and direction (failure / recovery) — rate-limits a flapping service. A first failure and its recovery are never delayed, and a suppressed alert is delivered late if the state still holds when the cooldown expires (delayed, never missing). `0` disables the cooldown. |
| `targets` | list | no | — | Alert sinks in the same shape as [notification targets](#target-fields), except `on:` is not valid here (a health target receives all alert-worthy transitions). With no targets, transitions are still logged and recorded. |

A new failure reads `🚨 stack health: <stack>/<service> healthy → unhealthy (was healthy 2h13m) — after deploy of a1b2c3d`; the recovery reads `✅ stack health recovered: <stack>/<service> after 4m12s`. The `generic` format posts the structured alert as JSON with a `"type": "health"` marker so a receiver shared with deploy notifications can tell the payloads apart.

Transitions are persisted across restarts (no re-alerting of known failures; a failure that happened while skipper was down is alerted after startup), and with the web UI enabled each service's [Stack health](#stack-health) panel shows a timeline of its recent status phases. This watches **only skipper's own stacks** — it is not a host-wide container watchdog.

## Deploy ordering

By default stacks deploy in the order they appear under `stacks`. When one stack must come up before another — a database before the app that migrates against it — declare it with `depends_on`:

```yaml
stacks:
  - name: postgres
  - name: app
    depends_on: [postgres]
  - name: monitoring        # unrelated, deploys in config order
```

Stacks then deploy in dependency order (a stable topological sort: a stack never deploys before one it depends on, and stacks not otherwise constrained keep their config order). A config with no `depends_on` anywhere behaves exactly as before. Deploys stay sequential — this orders the sequence, it does not run stacks in parallel. Names must refer to defined stacks and the graph must be acyclic — a typo or cycle fails fast, before any stack deploys.

Within a run, a dependency's outcome gates its dependents:

- **Dependency failed or rolled back** → the dependent does not deploy. It emits a `blocked` event, stays dirty (its hashes are not recorded), and retries automatically on the next sync — once the dependency deploys cleanly, the dependent follows in the same run. `blocked` is deliberately not a notification (the dependency's own failure already alerts, and a blocked stack re-reports every reconcile tick).
- **Dependency queued** (its `autosync` is paused) → the dependent queues too, rather than overtaking a change that is being deliberately held back.
- **Dependency unchanged** (skipped) → it is already at its desired state, so the dependent proceeds.

`depends_on` guarantees the dependency's deploy *completed*; by default `docker compose up` returns once containers start, not once they are ready. For true readiness (the database accepting connections before the app starts), add a [`deploy_health_check`](#health-check-gated-rollback) to the **dependency** — because deploys are sequential, the dependent only starts once the dependency proved healthy.

## Periodic reconcile

Deploys are normally driven by push webhooks. The webhook is a single point of delivery, though: if one is lost — skipper was down or restarting when the push landed, a network blip, a misconfigured hook, or a change that reached the branch some other way — the running stacks stay drifted from the deploy repo until the *next* push or a restart.

To close that gap, skipper also re-runs its git sync + deploy on a timer. Each tick fetches the branch tip and deploys only what actually changed (via the same hash-based change detection as a webhook), so a reconcile against an unchanged repo is a cheap no-op: no compose command runs and no event is emitted. It reconciles against **git desired state only** — it does not inspect or restart containers.

`reconcile_interval_seconds` controls the cadence:

- **default `300`** — reconcile every 5 minutes. On by default, so a missed webhook self-corrects within one interval.
- **`0`** — disable the loop entirely, restoring pure webhook + startup behaviour.
- Unlike the health poll, it is **not** tied to the UI — it runs headless, since it is a correctness feature rather than a display feed.

A tick is skipped while a deploy is already in flight (a reconcile carries no unique information, so it is dropped rather than queued behind the running deploy), and it flows through the same per-stack deploy gate as a webhook — so a stack with `autosync` off is queued, not force-deployed.

## Self-heal

Periodic reconcile closes drift on the **git axis** (git changed, live didn't). Self-heal closes it on the **runtime axis**: a stack the [health poller](#stack-health) finds degraded — a container that was stopped or removed out of band, exhausted its restart policy, or turned `unhealthy` — is automatically restored to its **currently deployed** running state by a corrective `docker compose up -d`. It is **not** a git deploy: the desired version is unchanged, so there is no change detection and no rollback — it only restarts what should already be running.

Self-heal is **opt-in and off by default**. Enable it globally with `self_heal: true`, or per stack, and leave it off for stacks you deliberately stop (a one-shot job). It is a backstop for the cases Docker's own `restart:` policy cannot handle — recreating a removed container, recovering an exhausted policy, reacting to a failing `HEALTHCHECK` — not a replacement for it; set a restart policy too.

An idle [`on_demand_containers`](#stack-fields) container is exempt: skipper stops it on purpose after each deploy, so its `stopped` state is not drift and self-heal never wakes it — even under a global `self_heal: true`. An on-demand container in any other bad state (crash-looping, `unhealthy`) is still a real failure and is healed as usual.

Guardrails keep it from fighting a genuinely broken stack:

- **Debounce** — a stack must read degraded for `self_heal_min_unhealthy_polls` consecutive polls (default 3) before the first redeploy, so a transient blip or Docker's own restart wins the race first.
- **Cooldown** — at least `self_heal_cooldown_seconds` (default 60, explicit `0` disables) between redeploys of the same stack.
- **Circuit breaker** — after `self_heal_max_attempts` (default 3) redeploys that don't restore the stack, skipper gives up, leaves it reported `unhealthy`, and emits a single `heal_exhausted` event (a `heal_exhausted` [notification](#notifications) fires by default — the "a stack is down and I couldn't fix it" alarm). The counter resets when the stack recovers or a real git deploy of it runs.

Self-heal rides the health-poll cadence and, like periodic reconcile, runs **headless** — so it needs `runtime_health_poll_interval_seconds` > 0 even with the UI off. A successful redeploy shows as a `healed` event in the deploy log.

## Keeping images up to date

skipper deploys the images your compose files declare; it does **not** watch registries for new versions on its own. That is a deliberate scope choice: skipper acts on git, so an image update should reach it *as a change in git*. The supported way to automate updates is to let [Renovate](https://docs.renovatebot.com/) keep the `image:` references in your **deploy repo** current: Renovate opens (or auto-merges) a change that bumps the reference and pins it to a digest, and that merge is an ordinary push, so skipper's webhook and [periodic reconcile](#periodic-reconcile) path pick it up and redeploy — no skipper configuration required.

Digest pinning is what makes this work. With a plain mutable tag like `image: caddy:latest`, the text in git never changes when the registry publishes a new image behind the same tag, so skipper never sees a change. Renovate's digest pinning rewrites the reference to `image: caddy:2.8.4@sha256:…` and keeps that digest current — so **every** update lands in git as a changed reference and deploys through the normal path.

A minimal `renovate.json` for the deploy repo:

```json
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": ["config:recommended", "docker:pinDigests"],
  "packageRules": [
    {
      "matchManagers": ["docker-compose"],
      "automerge": true,
      "automergeType": "pr"
    }
  ]
}
```

- `docker:pinDigests` makes Renovate pin (and keep current) a `@sha256:…` digest on every image reference — the key that closes the mutable-tag gap.
- The `docker-compose` manager covers `docker-compose.yml` / `compose.yaml` `image:` fields out of the box.
- `automerge: true` lets low-risk updates merge without review, so the deploy is hands-off end to end. Drop it to keep every bump behind a reviewed PR — the merge still triggers the deploy either way.

On a Gitea/Gogs forge, run **self-hosted** Renovate (the hosted Mend app is GitHub-only) — for example a scheduled `renovatebot/renovate` container with `RENOVATE_PLATFORM=gitea`, a bot token, and an explicit repo list (or `RENOVATE_AUTODISCOVER=true`) pointed at the deploy repo. A missed merge webhook is not fatal: the [periodic reconcile](#periodic-reconcile) loop re-fetches the branch tip within its interval and converges.
