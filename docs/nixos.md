# skipper-cd on NixOS — Reference

skipper-cd is built for NixOS: it can trigger `nixos-rebuild switch` as part of a deploy, and it ships a NixOS module so the service itself is declarative. This page covers both. For the Docker-container path instead, see [Docker](docker.md).

- [NixOS Rebuild](#nixos-rebuild) — trigger `nixos-rebuild switch` when `.nix` files change
- [NixOS Module](#nixos-module) — run skipper-cd itself as a declarative systemd service
- [Recommended Pattern: Self-Registering Stacks](#recommended-pattern-self-registering-stacks)
- [`project_directory` and Docker Compose Project Identity](#project_directory-and-docker-compose-project-identity)

---

## NixOS Rebuild

The optional `nixos_rebuild` section triggers `nixos-rebuild switch` when any `*.nix` file or `flake.lock` in the repository changes. This closes the GitOps loop for NixOS configurations: a merged PR or Renovate automerge triggers a webhook, skipper-cd pulls the change and runs the rebuild automatically.

```yaml
nixos_rebuild:
  flake: ".#myhost"
```

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | no | `true` | Set to `false` to temporarily disable without removing the section. When the section is present and `enabled` is omitted, it defaults to `true`. Omitting the entire `nixos_rebuild` section disables the feature. |
| `flake` | string | yes | — | Flake reference passed to `nixos-rebuild switch --flake <flake>` (e.g. `.#myhost`). |

**Important:** The skipper-cd systemd service must run as root for `nixos-rebuild` to work. The NixOS rebuild runs **before** any Docker stack deployments. If the rebuild fails, all Docker stack deploys are aborted to prevent deploying against a potentially broken system.

The rebuild runs in a transient systemd unit (`skipper-nixos-rebuild`) outside skipper's own cgroup, so a rebuild that restarts the skipper service itself keeps running. Nix hashes are saved to state *before* the rebuild because the rebuild may restart skipper; if the rebuild fails while skipper is still alive, the pre-saved hashes are reverted so the next sync retries.

NixOS rebuild state is tracked under the reserved key `_nixos` in the [state file](state.md) and appears in [Prometheus metrics](metrics.md) with the label `stack="_nixos"`. It also participates in [autosync](autosync.md) like a stack (independently pausable, queued rather than rebuilt while paused).

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
| `autoRecover` | bool | `true` | Install a timer that restarts the service whenever it is in a `failed` state, self-healing a self-update whose stop was force-killed. An intentional `systemctl stop` (unit goes `inactive`, not `failed`) is left alone. |
| `recoverInterval` | string | `2min` | How often the `autoRecover` timer checks for a failed service. Ignored when `autoRecover` is `false`. |

### Recommended Pattern: Self-Registering Stacks

To avoid maintaining a manual stack list that can diverge from the services actually enabled, define a `services.skipper-cd.stacks` option in the skipper-cd wrapper module and have each service module register itself:

**`modules/skipper-cd/default.nix`** — defines the option and generates the config:

```nix
{ config, lib, skipper-cd, ... }:
let
  stackType = lib.types.submodule {
    options = {
      name                 = lib.mkOption { type = lib.types.str; };
      project_directory    = lib.mkOption { type = lib.types.str; default = ""; };
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

**`modules/monica/default.nix`** — with `on_demand_containers`:

```nix
services.skipper-cd.stacks = [{
  name                 = "monica";
  on_demand_containers = [ "monica-app" "monica-db" ];
}];
```

Note there is no `project_directory` here even though monica is also managed by a NixOS systemd service at `/etc/nixos/modules/monica` — with the top-level `project_directory_base = "/etc/nixos/modules";` set once (see below), every stack module gets the matching `project_directory` for free and only needs to declare one when its directory breaks the `<project_directory_base>/<name>` pattern.

Because the list is only populated when a service's `enable = true`, disabled services are automatically absent from `skipper.yml`. There is no risk of skipper-cd deploying a stack for a service that has been turned off.

### `project_directory` and Docker Compose Project Identity

When a NixOS systemd service manages a Docker Compose stack (via `WorkingDirectory = /etc/nixos/modules/<name>`), Docker labels containers with `com.docker.compose.project.working_dir=/etc/nixos/modules/<name>` — Docker's own compose-project-identity label, unrelated to skipper's config field name. skipper-cd always reads the compose file from its repo clone at `<stacks_base_dir>/<name>/docker-compose.yml` for change detection and deployment. When `project_directory` is set, skipper-cd passes it as `--project-directory` so Docker Compose uses the same project identity as systemd.

Setting the top-level [`project_directory_base`](configuration.md#top-level-fields) to the NixOS modules directory (e.g. `/etc/nixos/modules`) derives every stack's `project_directory` as `<project_directory_base>/<name>` automatically — the common case with the self-registering pattern above, where every stack module lives at that same predictable path. A stack module only sets its own `project_directory` when its directory doesn't follow that pattern; the explicit value always wins over the derived one.

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
- **`.env` files** at `project_directory` are loaded automatically via `--project-directory`
- **Manual `docker compose` commands work.** Running `docker compose up -d` (or `logs`, `ps`, …) by hand from `/etc/nixos/modules/<name>` — for local testing or troubleshooting — resolves to the *same* project as skipper's own deploys, so it reuses the existing containers/network/volumes instead of standing up a second, conflicting project. Without `project_directory` set to that path, skipper's own deploys use the repo clone's directory as project identity instead, and a manual command run from `/etc/nixos/modules/<name>` would create a separate project.

#### Stacks that build their own image

Docker Compose resolves relative paths in the compose file against the project directory, and a `build:` context is one of them. With `project_directory` set, a `context: .` would otherwise point at the NixOS modules directory rather than at the repo clone — so the build would read a Dockerfile that skipper never checked for changes, and a base-image bump merged into the repo would produce no new image.

skipper-cd pins it: for the `docker compose build` call only, every relative build context is rewritten to its absolute path in the repo clone. A `dockerfile:` stays relative to that context and follows it. Everything else on the same call — project identity, `.env` loading, relative bind mounts — keeps resolving against `project_directory` unchanged.

There is nothing to configure. Two cases are left alone: a stack without `project_directory` (compose already resolves against the clone), and an absolute `context:`, which names its own tree. If a stack's build inputs genuinely live outside the repo — generated artifacts, for example — give it an absolute `context:`; note that skipper does not watch such a directory for changes unless it is listed in `watch_dirs`.
