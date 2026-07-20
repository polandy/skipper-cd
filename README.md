<p align="center">
  <img src="skipper-cd-logo.png" alt="skipper-cd logo" width="200">
</p>

<h1 align="center">skipper-cd</h1>

<p align="center"><i>Simple, fast GitOps CD for Docker Compose — push to Git, redeploy only what changed</i></p>

<p align="center">📖 <b><a href="https://polandy.github.io/skipper-cd/">Documentation</a></b></p>
<br>

A lightweight CD tool that listens for push webhooks, maintains a local clone of a Git repository, and redeploys only the Docker Compose stacks whose files actually changed. On NixOS it can also run `nixos-rebuild switch` when your `.nix` files change — closing the GitOps loop for the whole host. Pair it with automated dependency updates (e.g. [Renovate](https://docs.renovatebot.com/)) — which can even automerge routine minor and patch bumps — and the loop runs itself: each merge rebuilds the NixOS host and redeploys only the stacks it changed, with no manual steps. Unchanged stacks are skipped automatically.

Supported webhook signatures: **Gitea** (`X-Gitea-Signature`) and **GitHub/Forgejo** (`X-Hub-Signature-256`).

> **Note:** Only Gitea webhooks have been tested so far. GitHub and Forgejo support should work but is untested — feedback welcome via [GitHub Issues](https://github.com/polandy/skipper-cd/issues).

## Quickstart

Point skipper-cd at your deploy repo — that's the whole config:

```yaml
# skipper.yml
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: /var/lib/skipper/repo/modules
webhook_secret: "your-secret-here"
```

Every `<stacks_base_dir>/<name>/docker-compose.yml` in the repo is a stack — skipper-cd discovers them automatically. Push to the repo, your Git host fires the (HMAC-signed) webhook, and skipper-cd pulls, diffs, and redeploys only what changed. A reconcile loop re-syncs on a timer as a safety net for any missed webhook.

New here? The **[Getting Started walkthrough](https://polandy.github.io/skipper-cd/getting-started/)** covers the whole loop end to end — repo layout, running the service, and wiring up the webhook. Run it **[on NixOS](docs/nixos.md)** as a declarative systemd service, or **[with Docker](docs/docker.md)** as a container; the full configuration reference is in **[docs/configuration.md](docs/configuration.md)**.

## Features

- **Deploys only what changed** — SHA-256 hashes of each stack's compose file, env files, watched dirs, and `build:` Dockerfiles are persisted; unchanged stacks are skipped.
- **Automatic rollback** — if `docker compose up` fails, or an optional post-deploy health check (compose `--wait` and/or an HTTP probe) doesn't pass, skipper-cd restores the previous compose file from the last deployed Git commit ([docs](docs/configuration.md#health-check-gated-rollback)).
- **NixOS rebuilds** — optionally run `nixos-rebuild switch` before stack deploys when `.nix` files change, so one webhook updates both the host and its containers.
- **Autosync & queue** — pause deploys globally or per stack; changes that arrive while paused are queued and applied when you resume ([docs](docs/autosync.md)).
- **Web UI** — a single embedded page with live deploy status, live container logs, service icons, an event log, and installable as a PWA.
- **Orphan detection** — surfaces compose projects still running on the host that your current stack set no longer covers, e.g. a stack directory removed from the repo ([docs](docs/state.md#orphaned-stacks)).
- **Deploy notifications** — POST a message to Signal (via `signal-cli-rest-api`) or any HTTP endpoint (ntfy, Gotify, custom) on terminal deploy outcomes ([docs](docs/configuration.md#notifications)).
- **Observability** — Prometheus metrics and a `/healthz` endpoint out of the box.
- **Secure webhooks** — HMAC-SHA256 signature verification for Gitea and GitHub/Forgejo.

## The Bigger Picture

skipper-cd is the last automation link in a **self-driving, declarative NixOS homelab**. Keep your host configuration and your Docker Compose stacks in one Git repo, and let the machine maintain itself:

1. **[Renovate](https://docs.renovatebot.com/)** (or Dependabot) opens PRs for image tags, flake inputs, and other dependency bumps — and **automerges** the routine minor and patch ones.
2. The merge fires a **push webhook**.
3. skipper-cd pulls, runs `nixos-rebuild switch` if any `.nix` file changed, then redeploys **only** the stacks whose files changed.
4. Everything unchanged keeps running, untouched.

The payoff: your homelab stays current and fully declarative with **Git as the single source of truth** — and you only step in for the changes that genuinely need a human (major bumps, breaking notes).

## How It Works

On each incoming webhook (or on startup), skipper-cd:

1. Checks the push payload's `ref`: only pushes to the configured `branch` trigger a deploy (payloads without a `ref`, e.g. a manual `curl`, always trigger one).
2. Pulls the latest commits from the configured repository.
3. **NixOS rebuild** (optional): hashes all `*.nix` files and `flake.lock`; if any changed, runs `nixos-rebuild switch` in a transient systemd unit. If the rebuild fails, all stack deploys are aborted. See [NixOS](docs/nixos.md#nixos-rebuild).
4. Computes SHA-256 hashes for each stack's `docker-compose.yml`, any declared `env_files`, the global `vars_file`, watched directories, and any `Dockerfile`s of `build:` services — then compares them against the previous deployment and **skips unchanged stacks**.
5. For changed stacks, runs `docker compose pull` for remote images only, `docker compose build --pull` when `build:` services are present, then `docker compose up -d --remove-orphans`.
6. **Automatic rollback:** if `docker compose up` fails — or a configured [health check](docs/configuration.md#health-check-gated-rollback) does not pass afterwards — retrieves the previous `docker-compose.yml` from the last deployed commit and brings containers back up with it (marked `rolled_back`; `rolled_back_unhealthy` when even the restored version fails the health gate), otherwise `failed`.
7. Stops any `on_demand_containers` so an on-demand scheduler can take over their lifecycle, and logs the git diff of each changed file.

Concurrent webhook requests and the startup deploy are serialized by a deployment lock; if a deploy is already running, later requests wait for it to finish.

**Autosync** (steps 3 and 5) can be paused globally or per stack. While a stack is paused, a detected change is not deployed: it is marked `queued`, logged, and left pending, then deployed automatically when sync is re-enabled. Autosync is on everywhere by default. See [Autosync](docs/autosync.md).

## Documentation

| Topic | Description |
|---|---|
| **[Getting Started](https://polandy.github.io/skipper-cd/getting-started/)** | End-to-end walkthrough — deploy-repo layout, running the service, and wiring up the webhook. |
| **[Configuration](docs/configuration.md)** | Full config reference — top-level & stack fields, `vars_file`, service icons, notifications. |
| **[NixOS](docs/nixos.md)** | Running skipper-cd on NixOS: the `nixos_rebuild` feature, the NixOS module, and the self-registering-stacks pattern. |
| **[Docker](docs/docker.md)** | Running skipper-cd as a container, and how locally-built images are handled. |
| **[Autosync](docs/autosync.md)** | Pausing and queueing deploys, globally and per stack. |
| **[Metrics](docs/metrics.md)** | Prometheus metrics exposed on `/metrics`. |
| **[State File](docs/state.md)** | Format and semantics of `state.yaml`. |
| **[Install as App (PWA)](docs/pwa.md)** | Installing the web UI as a Progressive Web App. |
| **[Contributor docs](dev-docs/)** | ADRs, the E2E test spec, and feature specs — internal design records, not part of the user manual. |

## Releases

Releases are automated with [release-please](https://github.com/googleapis/release-please) based on the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) history (see [ADR-0011](dev-docs/adr/0011-release-automation-via-conventional-commits.md)). Every push to `main` updates a release PR that collects the pending changes; merging it creates the GitHub release, the `v*` tag, the CHANGELOG entry, and triggers the Docker image build.

## AI-Assisted Development

Much of this codebase was written with AI assistance ([Claude Code](https://claude.com/claude-code)), reviewed and maintained by the author. Every change goes through the same gate as any other: tests first, `go vet`/`gofmt`/`go test -race` in CI, and a human review before it lands on `main`.
