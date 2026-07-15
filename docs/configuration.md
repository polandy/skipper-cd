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
| `ui_theme_switcher` | bool | no | `false` | Show the in-UI theme picker so a browser can try other palettes locally. Off by default — the deployed `ui_theme` is then fixed (see [Web UI Theme](#web-ui-theme)). |
| `health_poll_interval_seconds` | int | no | `30` | How often the web UI polls its stacks' runtime health (see [Stack health](#stack-health)). `0` disables the health view. Only used when `ui_enabled`; the poll also runs only while a browser is connected. |
| `auth` | object | no | — | Access control for the web UI's data API: a trusted reverse-proxy header and/or a shared token entered in the PWA (see [Access control](#access-control)). Omit to leave the UI open. |

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
| `health_check` | section | no | — | Post-deploy health gate: when the stack does not become healthy after a deploy, it is rolled back to the previous version. See [Health-check-gated rollback](#health-check-gated-rollback). |

## Health-check-gated rollback

Without a `health_check`, a rollback only happens when `docker compose up` itself fails. A deploy whose containers *start* but stay broken (crash-loop, 500s) would be marked successful. Adding a `health_check` section closes that gap:

```yaml
stacks:
  - name: whoami
    health_check:
      timeout_seconds: 60                # optional, default: 60
      url: http://localhost:8080/health  # optional HTTP probe
```

The gate has two stages.

### Stage 1 — the compose healthcheck (recommended)

When the section is present, `docker compose up` runs with `--wait --wait-timeout <timeout_seconds>`, so the `up` fails unless every service reaches `running` (services without a healthcheck) or `healthy` (services with one). Requires Docker Compose v2.17+.

The `healthy` state comes from a [`healthcheck:`](https://docs.docker.com/reference/compose-file/services/#healthcheck) defined on the service in your compose file. **This is the exposure-free path: the healthcheck command runs *inside* the container, so nothing needs a published port.** For a container whose endpoint is not reachable from the host, this is the mechanism to use:

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

With that in place, a stack-level `health_check: {}` (no `url`) is enough — `--wait` gates the deploy on the internal healthcheck and rolls back if it never turns `healthy`.

### Stage 2 — the HTTP probe (optional)

Only when `url` is set: after a successful `up`, skipper-cd GETs the URL every 2 seconds until it answers with a 2xx status; anything else for `timeout_seconds` fails the deploy. The probe runs **from the skipper-cd host**, so the URL must be reachable from there (a published port on `localhost`, or a routable address via a reverse proxy). Use it when the stack has no internal `healthcheck:` but does expose a reachable endpoint, or as an extra end-to-end check on top of stage 1.

A failure in either stage triggers the regular rollback: the previous compose file is restored from the last deployed Git commit, the deploy is marked `rolled_back` (events, metrics and [notifications](#notifications) all see that status), and the change stays pending so the next push retries.

The rollback itself is verified through the same gate: its `up` also runs with `--wait`, and the HTTP probe (if configured) must pass again. So `rolled_back` guarantees the old version is actually healthy again. If the restored version *also* fails the gate — typically an environment problem such as a dead database or a broken secret — the deploy is marked **`rolled_back_unhealthy`** instead: the stack sits on the old compose file but needs attention *now*, because no version of it is verified healthy.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `timeout_seconds` | int | no | `60` | Wait budget, used both as `--wait-timeout` for compose and as the HTTP probe deadline. |
| `url` | string | no | — | HTTP(S) URL probed **from the host** after a successful `up`; must answer 2xx within `timeout_seconds`. Omit to rely on the container's compose `healthcheck:` alone (the exposure-free path). |

> **Note:** with `--wait`, a service that exits — even successfully — counts as a failure. Don't enable `health_check` on stacks with deliberate one-shot containers, or model those as [`service_completed_successfully`](https://docs.docker.com/compose/how-tos/startup-order/) dependencies.

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

Icons from the set are fetched once and cached on disk; the UI serves them same-origin from `/api/icons/<stack>`. `POST /api/icons/refresh` clears the cache so renamed stacks and newly published icons are picked up on the next load (e.g. `curl -X POST …/api/icons/refresh`).

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

Only terminal statuses are ever delivered: `failed`, `success`, `rolled_back`, `rolled_back_unhealthy` (the transient `deploying`, `skipped` and `queued` are never sent). Each target chooses which of the four it wants via `on:`; when omitted, all four are delivered.

Delivery is **fire-and-forget**: sending runs in the background with its own 10-second per-request timeout, never blocking or delaying a deploy. A failed or slow target is logged and dropped — there is no retry queue, so a notification can be lost if the target is down. For guaranteed history, read the persisted events or scrape metrics instead.

```yaml
notifications:
  # Signal via the signal-cli-rest-api stack.
  - format: signal
    url: http://localhost:8020        # service base; /v2/send is appended
    number: "+491234567890"           # sender (required for signal)
    recipients: ["+491234567890"]     # recipients (required for signal)
    prefix: nuc                       # optional; message becomes "[nuc] …"
    # on:  defaults to [failed, success, rolled_back, rolled_back_unhealthy]

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
| `on` | list | no | all four | Subset of `failed`, `success`, `rolled_back`, `rolled_back_unhealthy` that triggers this target. |
| `prefix` | string | no | — | Prepended as `[<prefix>] ` to the `signal` message, e.g. to label which host/instance sent it. Ignored by `generic` (its structured payload already carries the event). |
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

## Access control

By default the web UI is open — skipper-cd is designed to sit behind a reverse
proxy that authenticates users (e.g. Authelia forward-auth). The optional `auth`
section lets skipper-cd authorize clients itself instead, gating the **data API**
(`/api/*` — deploy events, logs, autosync, health). Two independent paths are
supported; configure either or both. When neither is set the UI stays open.

The gate **fails closed**: with `auth` configured, a request satisfying no path
gets `401`. The app **shell** (`/`, the manifest, the service worker, PWA icons)
and the operational endpoints (`POST /webhook`, which has its own HMAC, and
`GET /healthz`) always stay open, so the page can load and prompt for a token and
so liveness probes keep working.

```yaml
auth:
  # Proxy path — trust a header your reverse proxy sets after it authenticated
  # the user. Only requests coming *from* a trusted proxy are believed.
  trusted_header: Remote-User
  trusted_proxies:
    - 10.0.0.0/8
    - 127.0.0.1

  # Token path — a shared secret entered once in the PWA login screen.
  token: "a-long-random-cookie-safe-secret"
```

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `trusted_header` | string | no | — | HTTP header your reverse proxy sets once it has authenticated the user (e.g. `Remote-User`). A request carrying a non-empty value **and** arriving from a trusted proxy is authorized. Empty disables the proxy path. |
| `trusted_proxies` | list of strings | when `trusted_header` is set | — | Upstreams allowed to assert `trusted_header`, as CIDRs (`10.0.0.0/8`) or bare IPs (`127.0.0.1`). The check is anchored on the request's real network peer, not a forwardable header, so a direct client cannot spoof the header. |
| `token` | string | no | — | Shared secret for the PWA/direct path. Presented via the `skipper_auth` cookie (set by the login screen) or an `Authorization: Bearer <token>` header (handy for scripts). Compared in constant time. Empty disables the token path. Use a **cookie-safe** value — hex or base64/base64url — as it is stored in a cookie verbatim. |

### Proxy path

Point `trusted_header` at whatever your proxy injects after authenticating the
user, and list the proxy's address in `trusted_proxies`. Because authorization
requires the request to *originate* from a trusted proxy, ensure your proxy
**strips** the header from inbound client requests before setting it — otherwise
a client that can reach skipper-cd directly (not through the proxy) is still
rejected, but a client routed *through* a misconfigured proxy that forwards a
client-supplied header would not be.

### Token path (PWA login)

With `token` set, opening the UI shows a login screen. The entered token is
validated and then stored in the `skipper_auth` cookie (`SameSite=Lax`, one
year; `Secure` on HTTPS). The browser then sends it automatically on every
request — including the SSE event stream and PWA navigations, which cannot carry
a custom header, which is why a cookie (not a header) is used. Sign out from the
view-options popover, which clears the cookie.

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

Every palette drives the whole UI, including the PWA install identity (favicon, browser theme colour, app splash screen) — see [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md#design) for the full token design and [PWA](pwa.md#33-theme) for the installed-app behaviour.

### Theme switcher

By default the deployed `ui_theme` is fixed: there is no way to change it from the browser, which keeps the per-instance colour a reliable at-a-glance marker. Set `ui_theme_switcher: true` to add a small palette picker to the header — handy for trying the built-in themes out before committing one to the config.

```yaml
ui_theme_switcher: true   # optional, default: false
```

The picker is a **local, per-browser** override only: it never changes the deployment's `ui_theme` (there is no endpoint to do so) and only affects the browser that set it, applied instantly with no reload. While an override differs from the configured theme, a dismissible notice under the header points it out. Turning the flag back off hides the picker and re-pins every browser to the configured theme (a stale override left in a browser's `localStorage` simply lies dormant). See [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md#theme-override) for the exact behaviour.

## Stack health

When the web UI is enabled, skipper-cd shows the **live runtime health** of the stacks it deploys, next to each stack in the deploy view: `healthy`, `unhealthy`, `starting`, `stopped`, or `unknown`. This is a current, at-a-glance view — distinct from the deploy outcome — so a stack that deployed fine but whose container has since started crash-looping reads as `unhealthy` without waiting for the next push.

The health is read by polling `docker compose ps` for each stack, using the same compose file and `--project-directory` identity used to deploy it. Nothing is written and nothing is restarted — the view is read-only. It covers only skipper-cd's own stacks (not other containers on the host).

`health_poll_interval_seconds` controls the cadence:

- **default `30`** — poll every 30 seconds.
- **`0`** — disable the health view entirely (no polling, no pill).
- The poll only runs while `ui_enabled` **and** at least one browser is connected to the dashboard, so an unattended instance does no health work.

On a resource-constrained host, raise the interval rather than disabling it. See [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md#stack-health) for the UI surface.
