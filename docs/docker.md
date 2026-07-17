# skipper-cd with Docker — Reference

skipper-cd can run as a Docker container instead of a [NixOS service](nixos.md). This page covers running it in Docker and how it handles locally-built images.

---

## Running as a Container

```yaml
services:
  skipper:
    image: ghcr.io/polandy/skipper-cd:latest
    restart: unless-stopped
    stop_grace_period: 15m   # let an in-flight deploy finish on shutdown
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

The container path has been verified end to end: repository clone, startup deploy, HMAC-signed webhook → redeploy, state persistence across restarts, and graceful shutdown. `nixos_rebuild` is the one feature that cannot work inside a container — leave it out of the config, or a `.nix` change would abort every deploy run.

### Mount referenced paths at identical paths

skipper-cd drives the **host's** Docker daemon through the socket, but the compose CLI runs inside skipper-cd's container. Compose resolves relative paths (bind mounts, env files) against the project directory *client-side* and sends absolute paths to the daemon — which interprets them as **host** paths. The consequence: every path your config references must exist at the **same path** inside the skipper-cd container as on the host:

- any [`working_dir`](configuration.md#stack-fields) (mount read-only — compose loads the project `.env` from there and derives the project identity from it),
- `vars_file` and per-stack `env_files` (read-only),
- the state/clone directory, if stacks bind-mount data from inside the repo checkout — in that case bind-mount the host directory (e.g. `/var/lib/skipper:/var/lib/skipper`) instead of a named volume, so container paths and host paths are the same strings.

Identical paths also keep the compose project identity and labels byte-compatible with a natively-running skipper-cd, which matters when [migrating](#migrating-from-the-nixos-module).

### Graceful shutdown

skipper-cd handles `SIGTERM` as PID 1: it stops accepting webhooks, waits for an in-flight deploy to finish, then exits. Give it room via `stop_grace_period` (compose) or `--stop-timeout` (`docker run`) — with Docker's default 10 s, a running deploy would be killed mid-`docker compose up`.

### `localhost` URLs

[Notification](configuration.md#notifications) `url`s and [health-check](configuration.md#health-check-gated-rollback) probe URLs are resolved from inside skipper-cd's container, so `localhost` is the container itself. Either point them at an address reachable from the container, or run the container with `network_mode: host` — then `localhost` URLs, the webhook port, and the metrics port all behave exactly as they would for a native process (drop the `ports:` section in that case).

## Migrating from the NixOS module

The container can take over from the [NixOS module](nixos.md) without touching the running stacks — deploy state, history, and clone carry over:

1. Copy the effective `skipper.yml` and **remove the `nixos_rebuild` block** (a rebuild cannot run inside a container).
2. Stop the systemd side: `systemctl stop skipper-cd.service` (plus any watchdog timer that would restart it).
3. Start the container with the docker socket, the config copy, and `/var/lib/skipper:/var/lib/skipper`, `working_dir`s and env-file paths mounted read-only at identical paths (see above); `network_mode: host` keeps webhook/metrics ports and `localhost` URLs unchanged.
4. Verify: `/healthz` returns 200 and the startup deploy run skips every stack — the shared `state.yaml` hashes are unchanged, so nothing redeploys.

Going back is the reverse: stop and remove the container, start the systemd service again. The state remains shared throughout, so the direction of the switch never triggers redeploys by itself. Nix changes pushed while the container was in charge are picked up on the module's first sync — the container never updates the `_nixos` hashes, so the returning service detects them as changed and rebuilds.

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
