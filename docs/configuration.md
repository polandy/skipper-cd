# skipper-cd Configuration — Reference

The configuration file is a YAML file passed via the `-config` flag (default: `/etc/skipper/skipper.yml`).

For a minimal starting point see the [Quickstart](index.md#quickstart). This page is the full reference for every field.

---

## Full Example

```yaml
repo_url: ssh://git@gitea.example.com/user/nixos-config.git
repo_dir: /var/lib/skipper/repo        # optional, this is the default
branch: main                            # optional, default: main
vars_file: /etc/skipper/vars.env        # optional
command_timeout_seconds: 300            # optional, default: 300
log_format: text                        # optional, default: text ("json" for structured logs)
stacks_base_dir: /var/lib/skipper/repo/modules
webhook_secret: "your-secret-here"
port: 8080
metrics_port: 9120
autosync: true                          # optional, default: true (pause deploys globally)

stacks:
  - name: traefik
    env_files:
      - /run/secrets/rendered/skipper/compose.env

  - name: gitea
    # autosync: false                    # optional; pause just this stack (default: inherit global)
    env_files:
      - /run/secrets/rendered/skipper/compose.env

  # Set working_dir when a NixOS systemd service also manages this stack,
  # so Docker Compose uses the same project identity for both.
  - name: nextcloud
    working_dir: /etc/nixos/modules/nextcloud
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
| `repo_dir` | string | no | `/var/lib/skipper/repo` | Local directory where the repository is cloned. skipper-cd manages this directory independently of any live checkout. |
| `branch` | string | no | `main` | Git branch to track. Used for `git clone --branch` and `git reset --hard origin/<branch>`. |
| `vars_file` | string | no | — | Path to a `KEY=VALUE` env file containing non-secret values available during every `docker compose` invocation (see [vars_file](#vars_file)). Changes to this file trigger redeployment of all stacks. |
| `command_timeout_seconds` | int | no | `300` | Maximum number of seconds a single shell command (`docker compose pull/up`, `git clone/fetch`, `nixos-rebuild`) is allowed to run before being killed. Applies per command; a deploy run has no overall deadline. |
| `log_format` | string | no | `text` | Log output format: `text` (logfmt) or `json` (structured logs, e.g. for Loki ingestion). |
| `stacks_base_dir` | string | no | — | Base directory prepended to a stack's `name` to derive its working directory when `working_dir` is not set. Avoids repeating long paths across stacks. |
| `webhook_secret` | string | no | — | HMAC-SHA256 secret used to validate incoming webhook payloads (supports Gitea and GitHub/Forgejo signatures). When empty, signature validation is skipped (not recommended for production). |
| `port` | int | no | `8080` | Port on which the webhook HTTP server listens. Exposes `/webhook` and `/healthz` (200 while the last repository sync succeeded or none ran yet, 503 with the error when it failed). |
| `metrics_port` | int | no | `9120` | Port on which the Prometheus metrics HTTP server listens. Exposes `/metrics`. |
| `autosync` | bool | no | `true` | Global default for whether detected changes deploy automatically. Set to `false` to pause all stacks (a per-stack `autosync` still overrides it). See [Autosync](autosync.md). |
| `stacks` | list | yes | — | List of Docker Compose stacks to manage (see [Stack Fields](#stack-fields)). |
| `nixos_rebuild` | object | no | — | NixOS rebuild configuration (see [NixOS](nixos.md)). Omit the section entirely to disable. |
| `icons` | object | no | — | Web-UI service-icon configuration (see [Service Icons](#service-icons)). Omit to use defaults. |
| `notifications` | list | no | — | Outbound notification targets messaged on terminal deploy outcomes (see [Notifications](#notifications)). Omit to disable. |
| `ui_theme` | string | no | `catppuccin` | Web UI colour palette: one of `catppuccin`, `nord`, `solarized`, `gruvbox`, `rose-pine` (see [Web UI Theme](#web-ui-theme)). |

## Stack Fields

Each entry under `stacks` configures one Docker Compose stack.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `name` | string | yes | — | Name of the stack. The compose file is always read from `<stacks_base_dir>/<name>/docker-compose.yml`. Also used as the key in the deploy state file. |
| `working_dir` | string | no | — | Absolute path passed as `--project-directory` to `docker compose`. Controls Docker Compose project identity (container labels) and `.env` file loading. Change detection and the compose file always come from `<stacks_base_dir>/<name>`. See [working_dir and Docker Compose project identity](nixos.md#working_dir-and-docker-compose-project-identity). |
| `env_files` | list of strings | no | — | Absolute paths to `KEY=VALUE` env files whose contents are injected into the `docker compose` environment. These files are also hash-tracked: a change to any declared env file triggers a redeploy of that stack. |
| `watch_dirs` | list of strings | no | — | Absolute paths to directories whose contents are recursively hash-tracked. Any file change inside a watched directory triggers a redeploy of that stack. Useful for stacks with auxiliary configuration directories (e.g. Grafana provisioning). |
| `on_demand_containers` | list of strings | no | — | Container names to stop after a successful deployment. Use this for containers managed by an on-demand scheduler (e.g. Sablier): skipper-cd starts them via `docker compose up`, then immediately stops them so the scheduler can control their lifecycle. |
| `icon` | string | no | — | Icon-set slug for this stack's web-UI icon (e.g. `jellyfin` for a stack named `media`). Overrides the auto-match on the stack name. See [Service Icons](#service-icons). Purely visual — never hash-tracked. |
| `autosync` | bool | no | *inherit* | Overrides the global `autosync` for this stack (in both directions). When unset, the stack follows the global setting. See [Autosync](autosync.md). |

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

Icons from the set are fetched once and cached on disk; the UI serves them same-origin from `/api/icons/<stack>`. The header **Icon refresh** control (or the `i` hotkey) clears the cache so renamed stacks and newly published icons are picked up.

The reserved NixOS pseudo-stack `_nixos` (see [NixOS](nixos.md)) is not a configured stack, so it resolves its icon by auto-matching the `nixos` slug — giving the rebuild a recognizable logo rather than a monogram.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `cache_dir` | string | no | `/var/lib/skipper/icons` | Directory where fetched icons are cached on disk. |
| `source_url` | string | no | dashboard-icons CDN | Icon-set **root** URL; icons are fetched from `<source_url>/<format>/<slug>.<format>` for `format` in `svg`, `png`, `webp` (first hit wins). |

```yaml
icons:
  cache_dir: /var/lib/skipper/icons        # optional, this is the default
  source_url: https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons
```

> **Note:** A repo `icon.svg`/`icon.png` is **not** hash-tracked, so adding or changing an icon never triggers a redeploy. The one exception: if you list a stack's own directory under `watch_dirs`, its icon files would be hashed along with everything else — keep icons out of watched directories.

## Notifications

skipper-cd can POST a message to one or more targets whenever a deploy reaches a **terminal** outcome. This works independently of the web UI — notifications need neither `ui_enabled` nor an open browser.

Only terminal statuses are ever delivered: `failed`, `success`, `rolled_back` (the transient `deploying`, `skipped` and `queued` are never sent). Each target chooses which of the three it wants via `on:`; when omitted, all three are delivered.

Delivery is **fire-and-forget**: sending runs in the background with its own 10-second per-request timeout, never blocking or delaying a deploy. A failed or slow target is logged and dropped — there is no retry queue, so a notification can be lost if the target is down. For guaranteed history, read the persisted events or scrape metrics instead.

```yaml
notifications:
  # Signal via the signal-cli-rest-api stack.
  - format: signal
    url: http://localhost:8020        # service base; /v2/send is appended
    number: "+491234567890"           # sender (required for signal)
    recipients: ["+491234567890"]     # recipients (required for signal)
    # on:  defaults to [failed, success, rolled_back]

  # A second, independent target for failures only.
  - format: generic
    url: https://ntfy.example.com/skipper
    headers:
      Authorization: "Bearer <token>"
    on: [failed, rolled_back]
```

### Target Fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `format` | string | no | `generic` | Provider shape of the request: `signal` or `generic`. |
| `url` | string | yes | — | Endpoint the notification is POSTed to. For `signal` this is the `signal-cli-rest-api` **base** (e.g. `http://localhost:8020`); `/v2/send` is appended automatically. |
| `on` | list | no | all three | Subset of `failed`, `success`, `rolled_back` that triggers this target. |
| `headers` | map | no | — | Static HTTP headers added to the request. Only meaningful for `generic` (e.g. an auth token). |
| `number` | string | for `signal` | — | Signal sender number. Required for `format: signal`, rejected on any other format. |
| `recipients` | list | for `signal` | — | Signal recipient numbers (non-empty). Required for `format: signal`, rejected on any other format. |

### Formats

| Format | Body sent | Use for |
|---|---|---|
| `signal` | `{"message": …, "number": …, "recipients": […]}` to `<url>/v2/send` | The `bbernhard/signal-cli-rest-api` service. |
| `generic` | The full deploy event as JSON (diffs stripped), plus any `headers` | ntfy, Gotify, or your own endpoint. |

> **Reachability.** `url` must be reachable from wherever skipper-cd runs. As a **host service** (e.g. the [NixOS module](nixos.md)), a container's host-published port is `http://localhost:8020` — reaching it directly, bypassing any reverse proxy/auth. When skipper-cd itself runs **in a container**, `localhost` is the container, not the host (see [Docker](docker.md)).

> **No email.** Native SMTP is deliberately unsupported (see [ADR-0020](https://github.com/polandy/skipper-cd/blob/main/dev-docs/adr/0020-outbound-deploy-notifications.md)). Point a `generic` target at an email-capable relay (ntfy, Apprise, Shoutrrr) if you need email.

> **Self-notify caveat.** If Signal is delivered through a `signal-api` stack that skipper itself deploys, a failed `signal-api` deploy cannot notify you about itself. Configure a second, independent target (e.g. `generic` → ntfy) so at least one path never depends on the stack being reported on.

## Web UI Theme

`ui_theme` picks the web UI's colour palette. This is a per-instance, config-time choice — handy for telling several skipper-cd instances apart at a glance (e.g. one per host) — and is independent of the header's dark/light toggle, which stays a per-browser preference within whichever palette is configured.

```yaml
ui_theme: nord   # optional, default: catppuccin
```

| Value | Look |
|---|---|
| `catppuccin` (default) | Soft pastels on a muted indigo base (Mocha dark / Latte light). |
| `nord` | Arctic blue-greys, a single frost-blue accent. |
| `solarized` | The low-contrast terminal classic. |
| `gruvbox` | Warm retro-groove browns, orange accent. |
| `rose-pine` | Muted rose and iris-purple on a plum-black base. |

Every palette drives the whole UI, including the PWA install identity (favicon, browser theme colour, app splash screen) — see [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md#design) for the full token design and [PWA](pwa.md#33-theme) for the installed-app behaviour.
