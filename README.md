<p align="center">
  <img src="skipper-cd.png" alt="skipper-cd logo" width="200">
</p>

<h1 align="center">skipper-cd</h1>

<p align="center"><i>Simple, fast Docker Compose CD</i></p>
<br>

A lightweight CD tool that listens for push webhooks, maintains a local clone of a Git repository, and deploys changed Docker Compose stacks. It can also trigger `nixos-rebuild switch` when `.nix` files or `flake.lock` change. Unchanged stacks are skipped automatically.

Supported webhook signatures: **Gitea** (`X-Gitea-Signature`) and **GitHub/Forgejo** (`X-Hub-Signature-256`).

> **Note:** Only Gitea webhooks have been tested so far. GitHub and Forgejo support should work but is untested — feedback welcome via [GitHub Issues](https://github.com/polandy/skipper-cd/issues).

## How It Works

On each incoming webhook (or on startup), skipper-cd:

1. Checks the push payload's `ref`: only pushes to the configured `branch` trigger a deploy (payloads without a `ref`, e.g. a manual `curl`, always trigger one).
2. Pulls the latest commits from the configured repository.
3. **NixOS rebuild** (optional): If `nixos_rebuild` is configured, hashes all `*.nix` files and `flake.lock`. When any file has changed, runs `nixos-rebuild switch --flake <flake>`. If the rebuild fails, all subsequent Docker stack deploys are aborted.
4. Computes SHA-256 hashes for each stack's `docker-compose.yml`, any declared `env_files`, the global `vars_file`, and any `Dockerfile`s referenced by services with a `build:` section.
5. Compares the current hashes against the hashes from the previous deployment.
6. Skips stacks whose files have not changed.
7. For changed stacks, runs `docker compose pull` for services with remote images only (services with `build:` and services sharing a locally-built image name are excluded), then `docker compose build --pull` (only when `build:` services are present), followed by `docker compose up -d --remove-orphans`.
8. **Automatic rollback:** If `docker compose up` fails, skipper-cd retrieves the previous `docker-compose.yml` from the last deployed Git commit and runs `docker compose up -d` with it to restore containers. The deploy is marked as `rolled_back` in the UI and metrics. If no previous commit is available or the rollback itself fails, the deploy is marked as `failed`.
9. Stops any containers listed in `on_demand_containers` (see below) so that an on-demand scheduler can take over lifecycle management.
10. Logs the git diff of each changed file (relative to the last deployed commit).

Concurrent webhook requests and the startup deploy are serialized by a deployment lock. If a deploy is already in progress, subsequent requests wait for it to finish before starting their own sync+deploy cycle.

## Locally Built Images

Services with a `build:` section are automatically detected. skipper-cd runs `docker compose build --pull` for them and excludes them from `docker compose pull`. If a build service also has an `image:` field (tagging the built image), any other service referencing that same image name is also excluded from pull — since the image is produced locally, not available on a registry.

**Example:**

```yaml
services:
  app:
    build: "."
    image: nextcloud:34-ghostscript  # locally built, tagged with this name
  cron:
    image: nextcloud:34-ghostscript  # uses the same locally-built image
  db:
    image: postgres:16-alpine        # remote image, pulled normally
```

In this example, `docker compose pull` runs only for `db`. The `app` service is excluded because it has `build:`, and `cron` is excluded because its image name matches the locally-built `nextcloud:34-ghostscript`.

## Configuration

The configuration file is a YAML file passed via the `-config` flag (default: `/etc/skipper/skipper.yml`).

### Full Example

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

stacks:
  - name: traefik
    env_files:
      - /run/secrets/rendered/skipper/compose.env

  - name: gitea
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
  flake: ".#nuc"
```

### Top-level Fields

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
| `stacks` | list | yes | — | List of Docker Compose stacks to manage (see [Stack Fields](#stack-fields)). |
| `nixos_rebuild` | object | no | — | NixOS rebuild configuration (see [NixOS Rebuild](#nixos-rebuild)). Omit the section entirely to disable. |

### Stack Fields

Each entry under `stacks` configures one Docker Compose stack.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `name` | string | yes | — | Name of the stack. The compose file is always read from `<stacks_base_dir>/<name>/docker-compose.yml`. Also used as the key in the deploy state file. |
| `working_dir` | string | no | — | Absolute path passed as `--project-directory` to `docker compose`. Controls Docker Compose project identity (container labels) and `.env` file loading. Change detection and the compose file always come from `<stacks_base_dir>/<name>`. |
| `env_files` | list of strings | no | — | Absolute paths to `KEY=VALUE` env files whose contents are injected into the `docker compose` environment. These files are also hash-tracked: a change to any declared env file triggers a redeploy of that stack. |
| `watch_dirs` | list of strings | no | — | Absolute paths to directories whose contents are recursively hash-tracked. Any file change inside a watched directory triggers a redeploy of that stack. Useful for stacks with auxiliary configuration directories (e.g. Grafana provisioning). |
| `on_demand_containers` | list of strings | no | — | Container names to stop after a successful deployment. Use this for containers managed by an on-demand scheduler (e.g. Sablier): skipper-cd starts them via `docker compose up`, then immediately stops them so the scheduler can control their lifecycle. |

### `vars_file`

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

### NixOS Rebuild

The optional `nixos_rebuild` section triggers `nixos-rebuild switch` when any `*.nix` file or `flake.lock` in the repository changes. This closes the GitOps loop for NixOS configurations: a merged PR or Renovate automerge triggers a webhook, skipper-cd pulls the change and runs the rebuild automatically.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | no | `true` | Set to `false` to temporarily disable without removing the section. When the section is present and `enabled` is omitted, it defaults to `true`. Omitting the entire `nixos_rebuild` section disables the feature. |
| `flake` | string | yes | — | Flake reference passed to `nixos-rebuild switch --flake <flake>` (e.g. `.#nuc`). |

**Important:** The skipper-cd systemd service must run as root for `nixos-rebuild` to work. The NixOS rebuild runs **before** any Docker stack deployments. If the rebuild fails, all Docker stack deploys are aborted to prevent deploying against a potentially broken system.

NixOS rebuild state is tracked under the reserved key `_nixos` in the [state file](#state-file) and appears in [Prometheus metrics](#prometheus-metrics) with the label `stack="_nixos"`.

## Prometheus Metrics

skipper-cd exposes the following metrics on the `/metrics` endpoint:

| Metric | Type | Description |
|---|---|---|
| `skipper_webhooks_received_total` | counter | Total number of webhook requests received, labelled by `status`. |
| `skipper_deploys_triggered_total` | counter | Total number of stack deploys triggered, labelled by `stack`. |
| `skipper_deploys_skipped_total` | counter | Total number of stack deploys skipped (no changes), labelled by `stack`. |
| `skipper_deploy_errors_total` | counter | Total number of failed deploys, labelled by `stack`. |
| `skipper_deploy_rollbacks_total` | counter | Total number of successful rollbacks after failed deploys, labelled by `stack`. |
| `skipper_last_deploy_timestamp` | gauge | Unix timestamp of the last successful deploy, labelled by `stack`. |
| `skipper_deploy_lock_waits_total` | counter | Deploy runs that had to wait for a running deploy to finish (queueing indicator, see ADR-0010). |

## Docker

> **Note:** Running skipper-cd as a Docker container has not been tested yet. If you try it, feedback and bug reports are welcome via [GitHub Issues](https://github.com/polandy/skipper-cd/issues).

```yaml
services:
  skipper:
    image: ghcr.io/polandy/skipper-cd:latest
    restart: unless-stopped
    ports:
      - "8080:8080"   # webhook
      - "9120:9120"   # metrics
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./skipper.yml:/etc/skipper/skipper.yml:ro
      - skipper-data:/var/lib/skipper

volumes:
  skipper-data:
```

The Docker socket mount is required — skipper-cd uses it to manage compose stacks. The `skipper-data` volume persists the cloned repository and deploy state across restarts.

## NixOS Module

skipper-cd ships with a NixOS module at `nixosModules.default`.

### Flake Input

```nix
# flake.nix
inputs = {
  skipper-cd.url = "path:/path/to/skipper-cd";   # or a git+https URL
  skipper-cd.inputs.nixpkgs.follows = "nixpkgs";
};
```

Pass it through `specialArgs` so the module can access the flake:

```nix
nixosConfigurations.myhost = lib.nixosSystem {
  specialArgs = { inherit skipper-cd; };
  modules = [ ./configuration.nix ];
};
```

### Importing the Module

```nix
# configuration.nix (or any imported module)
{ skipper-cd, ... }:
{
  imports = [ skipper-cd.nixosModules.default ];

  services.skipper-cd = {
    enable    = true;
    package   = skipper-cd.packages.x86_64-linux.default;
    configFile = "/run/secrets/skipper.yml";  # path to the rendered skipper.yml
  };
}
```

The module creates the state directory at `/var/lib/skipper`, adds `git`, `docker`, and `docker-compose` to the service `PATH`, and runs skipper-cd as a hardened systemd service under the `root` user (Docker socket access via `SupplementaryGroups = [ "docker" ]`).

### Service Options

| Option | Type | Default | Description |
|---|---|---|---|
| `enable` | bool | `false` | Enable the skipper-cd systemd service. |
| `package` | package | — | The skipper-cd derivation to use. |
| `configFile` | path | — | Absolute path to `skipper.yml`. |
| `stateDir` | string | `/var/lib/skipper` | Directory for deploy state and the repository clone. |
| `nixosRebuild` | bool | `false` | Set when `nixos_rebuild` is configured in `skipper.yml`. Relaxes the systemd sandboxing so `nixos-rebuild` can run. |
| `stopTimeout` | string | `15min` | systemd `TimeoutStopSec`. On shutdown skipper-cd waits for an in-flight deploy to finish; keep this longer than a typical deploy run or systemd will kill the service mid-deploy. |

### Recommended Pattern: Self-Registering Stacks

To avoid maintaining a manual stack list that can diverge from the services actually enabled, define a `services.skipper-cd.stacks` option in the skipper-cd wrapper module and have each service module register itself:

**`modules/skipper-cd/default.nix`** — defines the option and generates the config:

```nix
{ config, lib, skipper-cd, ... }:
let
  stackType = lib.types.submodule {
    options = {
      name                 = lib.mkOption { type = lib.types.str; };
      working_dir          = lib.mkOption { type = lib.types.str; default = ""; };
      watch_dirs           = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; };
      on_demand_containers = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; };
    };
  };
in {
  imports = [ skipper-cd.nixosModules.default ];

  options.services.skipper-cd.stacks = lib.mkOption {
    type        = lib.types.listOf stackType;
    default     = [];
    description = "Stacks managed by skipper-cd. Each service module appends itself here when enabled.";
  };

  config = lib.mkIf config.services.skipper-cd.enable {
    services.skipper-cd = {
      package    = skipper-cd.packages.x86_64-linux.default;
      configFile = config.sops.templates."skipper/skipper.yml".path;
    };
  };
}
```

**`modules/gitea/default.nix`** — self-registration inside the existing `mkIf` block:

```nix
config = lib.mkIf config.services.gitea-docker.enable {
  # ... existing gitea config ...

  services.skipper-cd.stacks = [{ name = "gitea"; }];
};
```

**`modules/monitoring/default.nix`** — with `watch_dirs`:

```nix
services.skipper-cd.stacks = [{
  name       = "monitoring";
  watch_dirs = [ "/var/lib/skipper/repo/modules/monitoring/grafana-data/provisioning" ];
}];
```

**`modules/monica/default.nix`** — with `on_demand_containers` and `working_dir`:

```nix
services.skipper-cd.stacks = [{
  name                 = "monica";
  working_dir          = "/etc/nixos/modules/monica";
  on_demand_containers = [ "monica-app" "monica-db" ];
}];
```

Because the list is only populated when a service's `enable = true`, disabled services are automatically absent from `skipper.yml`. There is no risk of skipper-cd deploying a stack for a service that has been turned off.

#### `working_dir` and Docker Compose Project Identity

When a NixOS systemd service manages a Docker Compose stack (via `WorkingDirectory = /etc/nixos/modules/<name>`), Docker labels containers with `com.docker.compose.project.working_dir=/etc/nixos/modules/<name>`. skipper-cd always reads the compose file from its repo clone at `<stacks_base_dir>/<name>/docker-compose.yml` for change detection and deployment. When `working_dir` is set, skipper-cd passes it as `--project-directory` so Docker Compose uses the same project identity as systemd:

```
docker compose \
  -f /var/lib/skipper/repo/modules/<name>/docker-compose.yml \
  --project-directory /etc/nixos/modules/<name> \
  up -d
```

This ensures:
- **Change detection** uses the repo clone (merged PRs are detected immediately)
- **Compose file** is always read from the repo clone (latest version)
- **Project identity** matches the NixOS systemd path (no container name conflicts)
- **`.env` files** at `working_dir` are loaded automatically via `--project-directory`

## State File

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
