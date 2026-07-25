# Getting Started

From nothing to a working deploy in four steps. skipper-cd keeps a clone of your **deploy repo**; on every push it hashes each stack's files and runs `docker compose up -d` **only for the stacks that changed** — everything else keeps running untouched.

```
   git push                webhook (HTTP POST)              docker compose up -d
you ─────────▶ Git host ───────────────────────▶ skipper-cd ────────────────▶ only changed stacks
                (Gitea / GitHub / Forgejo)
```

## Step 1 — Lay out the deploy repo

Each stack is a directory holding an ordinary compose file at `<stacks_base_dir>/<name>/docker-compose.yml`:

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

The directory name is the stack name — skipper-cd discovers it automatically, no config entry needed. Push the repo to your Git host.

!!! tip
    `modules/` is just a convention — `stacks_base_dir` can be any path in the repo. On NixOS, [self-registering stacks](nixos.md#recommended-pattern-self-registering-stacks) keep the stack list in sync automatically.

## Step 2 — Run skipper-cd

```yaml
# skipper.yml
repo_url: https://gitea.example.com/user/deploy-repo.git
stacks_base_dir: modules                         # relative to the repo clone; omit for the repo root
webhook_secret: "a-long-random-string"           # you'll paste this into the webhook too
ui_enabled: true                                 # live web UI on the webhook port
```

Stack discovery is on by default, so `traefik`, `gitea`, and `monitoring` are picked up from the repo layout above with no `stacks:` list needed — add one only to override a discovered stack (hooks, `deploy_health_check`, …) or to list stacks manually instead (`stack_discovery: false`).

Start it **[with Docker](docker.md)** (published image + Docker socket + this file — use an `http(s)` `repo_url`, the image ships no ssh client) or **[on NixOS](nixos.md)** (declarative module, can also run `nixos-rebuild switch`).

The first start clones the repo and deploys every stack — there's no prior state, so everything counts as changed. Watch the logs, or open `http://<host>:8080`. Full field reference: **[Configuration](configuration.md)**.

## Step 3 — Add the webhook (optional)

The webhook is optional — skipper already converges each host on its [reconcile](configuration.md#periodic-reconcile) timer (default every 5 minutes). Adding one just makes a push land in seconds instead of at the next tick. To run reconcile-only, leave `webhook_secret` empty and skip to [Step 4](#step-4-push-and-verify).

Target: `http://<skipper-host>:8080/webhook` — must be reachable from where your Git host runs (LAN, reverse proxy, or a tunnel for cloud forges).

=== "Gitea / Forgejo"

    **Settings → Webhooks → Add Webhook → Gitea**

    | Field | Value |
    |---|---|
    | **Target URL** | `http://<skipper-host>:8080/webhook` |
    | **HTTP Method** | `POST` |
    | **Content Type** | `application/json` |
    | **Secret** | same string as `webhook_secret` |
    | **Trigger** | *Push events* |

=== "GitHub"

    **Settings → Webhooks → Add webhook**

    | Field | Value |
    |---|---|
    | **Payload URL** | `http://<skipper-host>:8080/webhook` |
    | **Content type** | `application/json` |
    | **Secret** | same string as `webhook_secret` |
    | **Events** | *Just the push event* |

Content type must be `application/json` (form-encoded payloads are not understood), and the secret must match `webhook_secret` — payloads with a bad HMAC signature are rejected.

## Step 4 — Push and verify

Change one stack's `docker-compose.yml`, commit, push. The UI/logs show exactly that stack redeploying, and your forge's webhook page shows skipper-cd's `202 Accepted`. Use the forge's **redeliver** button to retest without a new commit.

No deploy? The usual suspects:

- secret mismatch — the delivery log shows a `401`
- content type isn't `application/json`
- push landed on a branch other than the configured `branch` (default `main`)
- skipper-cd not reachable from the forge — `curl http://<skipper-host>:8080/healthz` (200 once the first sync succeeded)

## What's next

- **[Automatic rollback](configuration.md#health-check-gated-rollback)** — gate deploys on a health check, restore the previous version on failure.
- **[Notifications](configuration.md#notifications)** — Signal / ntfy / Gotify messages on deploy outcomes.
- **[Periodic reconcile](configuration.md#periodic-reconcile)** and **[self-heal](configuration.md#self-heal)** — self-correct missed webhooks and stacks that fell over out of band.
- **[Hands-off image updates](configuration.md#keeping-images-up-to-date)** — let Renovate bump `image:` tags; skipper-cd deploys the merge.
- **[NixOS rebuilds](nixos.md#nixos-rebuild)** — the same push can also run `nixos-rebuild switch` when `.nix` files change.
