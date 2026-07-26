<p align="center">
  <img src="assets/skipper-cd-logo.png" alt="skipper-cd logo" width="160">
</p>

# skipper-cd

*Simple, fast GitOps CD for Docker Compose — push to Git, redeploy only what changed*

![skipper-cd's Deploys view — a live, newest-first feed of every deploy with the service versions it changed, its status and health, and the commit's diff](assets/screenshots/deploys.png)

skipper-cd is a lightweight CD tool that maintains a local clone of a Git repository and reconciles your Docker Compose stacks to it on a timer, redeploying only the stacks whose files actually changed — with an optional push webhook to make a change land in seconds instead of at the next tick. On NixOS it can also run `nixos-rebuild switch` when your `.nix` files change — closing the GitOps loop for the whole host. Pair it with automated dependency updates (e.g. [Renovate](https://docs.renovatebot.com/)) — which can even automerge routine minor and patch bumps — and the loop runs itself: each merge rebuilds the NixOS host and redeploys only the stacks it changed, with no manual steps. Unchanged stacks are skipped automatically.

Supported webhook signatures: **Gitea** (`X-Gitea-Signature`) and **GitHub/Forgejo** (`X-Hub-Signature-256`).

!!! note
    Only Gitea webhooks have been tested so far. GitHub and Forgejo support should work but is untested — feedback welcome via [GitHub Issues](https://github.com/polandy/skipper-cd/issues).

## Quickstart

Point skipper-cd at your deploy repo — that's the whole config:

```yaml
# skipper.yml
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: modules                 # relative to the repo clone; omit for the repo root
# webhook_secret: "your-secret-here"     # optional — enables the push webhook; reconcile runs without it
```

Every `<stacks_base_dir>/<name>/docker-compose.yml` in the repo is a stack — skipper-cd discovers them automatically. A [reconcile loop](configuration.md#periodic-reconcile) pulls, diffs, and redeploys only what changed on a timer, keeping every host converged to the repo. Wire up the (HMAC-signed) push webhook and a merge lands in seconds instead of at the next tick.

New here? The **[Getting Started walkthrough](getting-started.md)** covers the whole loop end to end — repo layout, running the service, and wiring up the webhook. Run it **[on NixOS](nixos.md)** as a declarative systemd service, or **[with Docker](docker.md)** as a container; the full configuration reference is in **[Configuration](configuration.md)**.

## Features

- **Deploys only what changed** — SHA-256 hashes of each stack's compose file, env files, watched dirs, and `build:` Dockerfiles are persisted; unchanged stacks are skipped.
- **Automatic rollback** — if `docker compose up` fails, or an optional post-deploy health check (compose `--wait` and/or an HTTP probe) doesn't pass, skipper-cd restores the previous compose file from the last deployed Git commit ([details](configuration.md#health-check-gated-rollback)).
- **NixOS rebuilds** — optionally run `nixos-rebuild switch` before stack deploys when `.nix` files change, so one webhook updates both the host and its containers.
- **Autosync & queue** — pause deploys globally or per stack; changes that arrive while paused are queued and applied when you resume ([details](autosync.md)).
- **Periodic reconcile** — the convergence baseline: re-syncs and redeploys on a timer (default every 5 minutes) so each host catches up to the repo with or without a webhook ([details](configuration.md#periodic-reconcile)).
- **Orphan detection** — surfaces compose projects still running on the host that your current stack set no longer covers, e.g. a stack directory removed from the repo ([details](state.md#orphaned-stacks)).
- **Web UI** — a single embedded page with live deploy status, a **Stacks inventory** (every stack skipper owns, its last outcome, the version each service runs, and its containers, one click away from any deploy row and back), **live container logs** (per stack, filterable to any subset of its services, streamed from a logs icon), service icons, an event log, and installable as a PWA.
- **Observability** — Prometheus metrics and a `/healthz` endpoint out of the box.
- **Secure webhooks** — HMAC-SHA256 signature verification for Gitea and GitHub/Forgejo.

![skipper-cd's Stacks view — every stack skipper owns, with its last outcome, health, running service version, and deployed commit](assets/screenshots/stacks.png)

## Why skipper-cd?

**One static binary, one YAML file — no cluster, no database, no control plane.** Push to Git and only the stacks whose files changed redeploy, with automatic rollback and drift self-heal. On NixOS it rebuilds the host, too. That minimal footprint and narrow scope are the point:

- **Docker Compose, not Kubernetes** — built for individual hosts and homelabs, not a cluster. A `.nix` change triggers `nixos-rebuild switch`, so one push updates the host and its containers together.
- **One host or many** — run an instance per host, each fully autonomous (its own repo clone, state, deploys, and webhook); a primary can fan the others in for a single merged, read-only view.
- **Near-zero config** — one YAML file, and stack discovery means most stacks need no per-stack entry at all.
- **Git-only, no drift** — no imperative "redeploy" button and no in-UI editing; the repo is the only way to change what runs. SHA-256 hashing redeploys just the stacks that changed, a failed deploy rolls back, and drift is self-healed on a reconcile loop.

skipper-cd reconciles each host to Git on a timer, with a webhook to make a push land in seconds rather than minutes.

**Scope is deliberate.** Aimed at self-hosted homelab hosts, skipper-cd leaves several concerns to purpose-built tools: **secrets** (e.g. `sops` or `agenix`) and **multi-user access, RBAC, or SSO** (put skipper behind a reverse proxy or auth gateway). It's no central control plane either — several hosts link into a read-only merged view, each still deploying itself from Git. It does one thing: keep each host's Compose stacks — and, on NixOS, the host itself — in sync with Git.

## The Bigger Picture

skipper-cd is the last automation link in a **self-driving, declarative NixOS homelab**. Keep your host configuration and your Docker Compose stacks in one Git repo, and let the machine maintain itself:

1. **[Renovate](https://docs.renovatebot.com/)** (or Dependabot) opens PRs for image tags, flake inputs, and other dependency bumps — and **automerges** the routine minor and patch ones.
2. skipper-cd picks the merge up on its next **reconcile** tick — or instantly, if a **push webhook** is wired up.
3. It pulls, runs `nixos-rebuild switch` if any `.nix` file changed, then redeploys **only** the stacks whose files changed.
4. Everything unchanged keeps running, untouched.

The payoff: your homelab stays current and fully declarative with **Git as the single source of truth** — and you only step in for the changes that genuinely need a human (major bumps, breaking notes).

!!! warning "Automerge minor and patch, not major"
    skipper-cd deploys whatever lands on the branch, unattended — so scope Renovate's (or Dependabot's) automerge to **minor and patch** bumps, which respect semver's backward-compatibility promise. Leave **major** bumps for a human to merge: they can carry breaking config changes or one-way data migrations that an [automatic rollback cannot undo](configuration.md#health-check-gated-rollback).

## How It Works

On each reconcile tick — or on an incoming webhook or at startup — skipper-cd:

1. Checks the push payload's `ref`: only pushes to the configured `branch` trigger a deploy (payloads without a `ref`, e.g. a manual `curl`, always trigger one).
2. Pulls the latest commits from the configured repository.
3. **NixOS rebuild** (optional): hashes all `*.nix` files and `flake.lock`; if any changed, runs `nixos-rebuild switch` in a transient systemd unit. If the rebuild fails, all stack deploys are aborted. See [NixOS](nixos.md#nixos-rebuild).
4. Computes SHA-256 hashes for each stack's `docker-compose.yml`, any declared `env_files`, the global `vars_file`, watched directories, and any `Dockerfile`s of `build:` services — then compares them against the previous deployment and **skips unchanged stacks**.
5. For changed stacks, runs `docker compose pull` for remote images only, `docker compose build --pull` when `build:` services are present, then `docker compose up -d --remove-orphans`.
6. **Automatic rollback:** if `docker compose up` fails — or a configured [health check](configuration.md#health-check-gated-rollback) does not pass afterwards — retrieves the previous `docker-compose.yml` from the last deployed commit and brings containers back up with it (marked `rolled_back`; `rolled_back_unhealthy` when even the restored version fails the health gate), otherwise `failed`.
7. Stops any `on_demand_containers` so an on-demand scheduler can take over their lifecycle, and logs the git diff of each changed file.

Concurrent webhook requests and the startup deploy are serialized by a deployment lock; if a deploy is already running, later requests wait for it to finish.

**Autosync** (steps 3 and 5) can be paused globally or per stack. While a stack is paused, a detected change is not deployed: it is marked `queued`, logged, and left pending, then deployed automatically when sync is re-enabled. Autosync is on everywhere by default. See [Autosync](autosync.md).
