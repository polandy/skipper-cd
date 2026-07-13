# skipper-cd

*Simple, fast Docker Compose CD — with first-class NixOS support*

skipper-cd is a lightweight CD tool that listens for push webhooks, maintains a local clone of a Git repository, and redeploys only the Docker Compose stacks whose files actually changed. On NixOS it can also run `nixos-rebuild switch` when your `.nix` files change — closing the GitOps loop for the whole host. Unchanged stacks are skipped automatically.

Supported webhook signatures: **Gitea** (`X-Gitea-Signature`) and **GitHub/Forgejo** (`X-Hub-Signature-256`).

!!! note
    Only Gitea webhooks have been tested so far. GitHub and Forgejo support should work but is untested — feedback welcome via [GitHub Issues](https://github.com/polandy/skipper-cd/issues).

## Quickstart

Point skipper-cd at your deploy repo and list the stacks you want managed — that's the whole config:

```yaml
# skipper.yml
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: /var/lib/skipper/repo/modules
webhook_secret: "your-secret-here"

stacks:
  - name: traefik
  - name: gitea
```

Each stack's compose file lives at `<stacks_base_dir>/<name>/docker-compose.yml`. Push to the repo, your Git host fires a webhook, and skipper-cd pulls, diffs, and redeploys only what changed.

Run it **[on NixOS](nixos.md)** as a declarative systemd service, or **[with Docker](docker.md)** as a container. The full configuration reference is in **[Configuration](configuration.md)**.

## Features

- **Deploys only what changed** — SHA-256 hashes of each stack's compose file, env files, watched dirs, and `build:` Dockerfiles are persisted; unchanged stacks are skipped.
- **Automatic rollback** — if `docker compose up` fails, skipper-cd restores the previous compose file from the last deployed Git commit.
- **NixOS rebuilds** — optionally run `nixos-rebuild switch` before stack deploys when `.nix` files change, so one webhook updates both the host and its containers.
- **Autosync & queue** — pause deploys globally or per stack; changes that arrive while paused are queued and applied when you resume ([details](autosync.md)).
- **Web UI** — a single embedded page with live deploy status, service icons, an event log, and installable as a PWA.
- **Observability** — Prometheus metrics and a `/healthz` endpoint out of the box.
- **Secure webhooks** — HMAC-SHA256 signature verification for Gitea and GitHub/Forgejo.

## How It Works

On each incoming webhook (or on startup), skipper-cd:

1. Checks the push payload's `ref`: only pushes to the configured `branch` trigger a deploy (payloads without a `ref`, e.g. a manual `curl`, always trigger one).
2. Pulls the latest commits from the configured repository.
3. **NixOS rebuild** (optional): hashes all `*.nix` files and `flake.lock`; if any changed, runs `nixos-rebuild switch` in a transient systemd unit. If the rebuild fails, all stack deploys are aborted. See [NixOS](nixos.md#nixos-rebuild).
4. Computes SHA-256 hashes for each stack's `docker-compose.yml`, any declared `env_files`, the global `vars_file`, watched directories, and any `Dockerfile`s of `build:` services — then compares them against the previous deployment and **skips unchanged stacks**.
5. For changed stacks, runs `docker compose pull` for remote images only, `docker compose build --pull` when `build:` services are present, then `docker compose up -d --remove-orphans`.
6. **Automatic rollback:** if `docker compose up` fails, retrieves the previous `docker-compose.yml` from the last deployed commit and brings containers back up with it (marked `rolled_back`), otherwise `failed`.
7. Stops any `on_demand_containers` so an on-demand scheduler can take over their lifecycle, and logs the git diff of each changed file.

Concurrent webhook requests and the startup deploy are serialized by a deployment lock; if a deploy is already running, later requests wait for it to finish.

**Autosync** (steps 3 and 5) can be paused globally or per stack. While a stack is paused, a detected change is not deployed: it is marked `queued`, logged, and left pending, then deployed automatically when sync is re-enabled. Autosync is on everywhere by default. See [Autosync](autosync.md).
