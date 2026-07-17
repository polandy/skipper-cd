# Getting Started

This walkthrough takes you from nothing to a working deploy: structure a repo, point skipper-cd at it, wire up the webhook, and watch a push redeploy only what changed. It is host-agnostic — the same four steps apply whether you run skipper-cd [on NixOS](nixos.md) or [in Docker](docker.md).

## The mental model

skipper-cd keeps a local clone of one **deploy repo** and watches it for pushes. Your Docker Compose stacks live in that repo. When you push, your Git host fires a webhook; skipper-cd pulls, hashes each stack's tracked files, and runs `docker compose up -d` **only for the stacks whose files actually changed**. Everything else keeps running untouched.

```
   git push                webhook (HTTP POST)              docker compose up -d
you ─────────▶ Git host ───────────────────────▶ skipper-cd ────────────────▶ only changed stacks
                (Gitea / GitHub / Forgejo)
```

So there are exactly three moving parts to set up: the **repo layout**, the **skipper-cd service**, and the **webhook** that connects them.

## Step 1 — Lay out your deploy repo

skipper-cd finds each stack's compose file at `<stacks_base_dir>/<name>/docker-compose.yml`. Give every stack its own directory named exactly as you'll list it in the config:

```
deploy-repo/
└── modules/                     # this is your stacks_base_dir
    ├── traefik/
    │   └── docker-compose.yml
    ├── gitea/
    │   └── docker-compose.yml
    └── monitoring/
        ├── docker-compose.yml
        └── grafana/             # optional: watched via watch_dirs
            └── provisioning/
```

The directory name (`traefik`, `gitea`, …) is the stack `name`. There's nothing skipper-specific *inside* a `docker-compose.yml` — they're ordinary Compose files. Push this repo to your Git host and you're ready to point skipper-cd at it.

!!! tip
    `modules/` is just a convention — `stacks_base_dir` can be any path in the repo (or the repo root itself). The [NixOS module](nixos.md#recommended-pattern-self-registering-stacks) lets each service module register its own stack so the list can never drift from what's actually enabled.

## Step 2 — Run skipper-cd

Write a minimal `skipper.yml` pointing at your repo and listing the stacks:

```yaml
# skipper.yml
repo_url: ssh://git@gitea.example.com/user/deploy-repo.git
stacks_base_dir: /var/lib/skipper/repo/modules
webhook_secret: "a-long-random-string"    # you'll paste this into the webhook too
ui_enabled: true                           # optional: live web UI on the webhook port

stacks:
  - name: traefik
  - name: gitea
  - name: monitoring
```

Then start the service. Pick your platform:

- **[On NixOS](nixos.md)** — the shipped module runs skipper-cd as a declarative, hardened systemd service, and can additionally run `nixos-rebuild switch` when your `.nix` files change.
- **[With Docker](docker.md)** — run the published container image with the Docker socket and your `skipper.yml` mounted in.

On startup skipper-cd clones the repo and does a first deploy of every stack (there's no prior state, so everything counts as changed). Watch the logs — or, with `ui_enabled: true`, open `http://<host>:8080` — to confirm each stack came up. The full field reference is in **[Configuration](configuration.md)**.

## Step 3 — Point your Git host at skipper-cd

This is the link that turns a `git push` into a deploy. skipper-cd listens for a **POST** to `/webhook` on its main `port` (default `8080`), so the webhook target is:

```
http://<skipper-host>:8080/webhook
```

The host must be reachable from wherever your Git host runs (same LAN, a reverse proxy, or a tunnel for a cloud-hosted forge like GitHub). Add the webhook in your repo's settings:

=== "Gitea / Forgejo"

    In the repo: **Settings → Webhooks → Add Webhook → Gitea**.

    | Field | Value |
    |---|---|
    | **Target URL** | `http://<skipper-host>:8080/webhook` |
    | **HTTP Method** | `POST` |
    | **Content Type** | `application/json` |
    | **Secret** | the same string as `webhook_secret` in `skipper.yml` |
    | **Trigger** | *Push events* |

    skipper-cd verifies the `X-Gitea-Signature` HMAC that Gitea sends.

=== "GitHub"

    In the repo: **Settings → Webhooks → Add webhook**.

    | Field | Value |
    |---|---|
    | **Payload URL** | `http://<skipper-host>:8080/webhook` |
    | **Content type** | `application/json` |
    | **Secret** | the same string as `webhook_secret` in `skipper.yml` |
    | **Events** | *Just the push event* |

    skipper-cd verifies the `X-Hub-Signature-256` HMAC that GitHub sends.

!!! warning "Content type must be `application/json`"
    skipper-cd parses the raw JSON push payload. The form-encoded option some forges offer (`application/x-www-form-urlencoded`) won't be understood — pick JSON.

The `Secret` and `webhook_secret` **must match**: skipper-cd rejects any payload whose HMAC signature doesn't verify. (Leaving `webhook_secret` empty disables verification entirely — fine for a quick local test, not for anything reachable.)

## Step 4 — Verify the loop

Make a trivial change to one stack's `docker-compose.yml`, commit, and push. Then check that it deployed:

- **Web UI** (`ui_enabled: true`) — the changed stack shows a fresh deploy; unchanged stacks stay idle.
- **Logs** — skipper-cd logs the pull, which stacks it hashed, and the `docker compose` commands for the one that changed.
- **Delivery** — your Git host's webhook page shows the delivery and skipper-cd's `202 Accepted` response. A `202` means the payload was accepted and the deploy runs in the background.

Your forge can also **redeliver** the last webhook from that page — handy for testing without another commit. To sanity-check reachability without a signature, `curl` the health endpoint (this is a plain readiness probe, not the webhook):

```bash
curl http://<skipper-host>:8080/healthz     # 200 once the first sync has succeeded
```

If a push doesn't deploy, the usual suspects are: a secret mismatch (check the forge's delivery response for a 401), the wrong content type, the push landing on a branch other than the configured `branch` (default `main`), or skipper-cd not being reachable from the forge.

## What's next

The loop now runs itself on every push. From here you can add:

- **[Automatic rollback](configuration.md#health-check-gated-rollback)** — gate a deploy on a health check and restore the previous version if it fails.
- **[Notifications](configuration.md#notifications)** — get a Signal / ntfy / Gotify message on deploy outcomes.
- **[Periodic reconcile](configuration.md#periodic-reconcile)** and **[self-heal](configuration.md#self-heal)** — self-correct a missed webhook or a stack that fell over out of band.
- **[Hands-off image updates](configuration.md#keeping-images-up-to-date)** — let Renovate bump your `image:` tags in git, and skipper-cd deploys the merge.
- **[NixOS rebuilds](nixos.md#nixos-rebuild)** — have the same push that redeploys containers also run `nixos-rebuild switch` when your `.nix` files change.
