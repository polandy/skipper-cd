# skipper-cd on NixOS — Reference

skipper-cd is built for NixOS: it can trigger `nixos-rebuild switch` as part of a deploy, and it ships a NixOS module so the service itself is declarative. This page covers both. For the Docker-container path instead, see [Docker](docker.md).

- [NixOS Rebuild](#nixos-rebuild) — trigger `nixos-rebuild switch` when `.nix` files change
- [NixOS Module](#nixos-module) — run skipper-cd itself as a declarative systemd service
- [Recommended Pattern: Self-Registering Stacks](#recommended-pattern-self-registering-stacks)
- [`working_dir` and Docker Compose Project Identity](#working_dir-and-docker-compose-project-identity)

---

## NixOS Rebuild

The optional `nixos_rebuild` section triggers `nixos-rebuild switch` when any `*.nix` file or `flake.lock` in the repository changes. This closes the GitOps loop for NixOS configurations: a merged PR or Renovate automerge triggers a webhook, skipper-cd pulls the change and runs the rebuild automatically.

```yaml
nixos_rebuild:
  flake: ".#nuc"
```

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | no | `true` | Set to `false` to temporarily disable without removing the section. When the section is present and `enabled` is omitted, it defaults to `true`. Omitting the entire `nixos_rebuild` section disables the feature. |
| `flake` | string | yes | — | Flake reference passed to `nixos-rebuild switch --flake <flake>` (e.g. `.#nuc`). |

**Important:** The skipper-cd systemd service must run as root for `nixos-rebuild` to work. The NixOS rebuild runs **before** any Docker stack deployments. If the rebuild fails, all Docker stack deploys are aborted to prevent deploying against a potentially broken system.

The rebuild runs in a transient systemd unit (`skipper-nixos-rebuild`) outside skipper's own cgroup, so a rebuild that restarts the skipper service itself keeps running ([ADR-0014](adr/0014-nixos-rebuild-in-transient-systemd-unit.md)). Nix hashes are saved to state *before* the rebuild because the rebuild may restart skipper; if the rebuild fails while skipper is still alive, the pre-saved hashes are reverted so the next sync retries ([ADR-0015](adr/0015-revert-nix-hashes-on-surviving-rebuild-failure.md)).

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
| `autoRecover` | bool | `true` | Install a timer that restarts the service whenever it is in a `failed` state, self-healing a self-update whose stop was force-killed ([ADR-0017](adr/0017-self-heal-failed-self-update.md)). An intentional `systemctl stop` (unit goes `inactive`, not `failed`) is left alone. |
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

### `working_dir` and Docker Compose Project Identity

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
