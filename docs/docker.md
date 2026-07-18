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
    group_add:
      - "999"   # host's docker group GID — `getent group docker | cut -d: -f3`
    read_only: true
    cap_drop: [ALL]
    tmpfs:
      - /tmp    # rollback writes the restored compose file here
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

Container notes:

- **Mount referenced paths at identical paths.** skipper-cd drives the *host's* Docker daemon through the socket, so every path the config references — [`working_dir`](configuration.md#stack-fields), `vars_file`, `env_files` — must be mounted (read-only) at the same path inside the container as on the host.
- **`nixos_rebuild` cannot run in a container** — leave it out of the config.
- **Shutdown:** skipper-cd exits gracefully on SIGTERM and waits for an in-flight deploy; give it room with `stop_grace_period` (Docker's default is 10 s).
- **`localhost` URLs** ([notifications](configuration.md#notifications), [health-check](configuration.md#health-check-gated-rollback) probes) resolve inside the container. Use an address reachable from the container, or `network_mode: host` (then drop `ports:`).

### Running as non-root

The image runs as a fixed non-root user (`skipper`, UID/GID 1000):

- **Docker socket access** is granted via `group_add`, not the container's own user — add the host's docker group GID (`getent group docker | cut -d: -f3`, commonly `999` or `998`) as shown above. This works alongside `/var/run/docker.sock` being root:docker-owned on the host; the container process joins that GID as a supplementary group.
- **`./skipper.yml` must be readable by UID/GID 1000** (or world-readable) — it's bind-mounted read-only, so the *host* file's permissions apply inside the container. `chmod 640` plus `chown root:1000` on the host (matching the container's fixed GID) works without making it world-readable, since it holds `webhook_secret`.
- **Upgrading an existing deployment**: a `skipper-data` volume created by an image version that ran as root will have root-owned files. `docker run --rm -v skipper-data:/data alpine chown -R 1000:1000 /data` before the next start, or the container fails to write `state.yaml` and the deploy history.

## Migrating from the NixOS module

The switch itself never redeploys anything: bind-mount the host's `/var/lib/skipper` at the same path so `state.yaml` and the deploy history carry over, and the startup run skips every unchanged stack.

1. Copy the effective `skipper.yml` and remove the `nixos_rebuild` block.
2. `systemctl stop skipper-cd` (plus any timer that would restart it).
3. Start the container with the mounts above; `network_mode: host` keeps ports and `localhost` URLs unchanged.

Going back is the reverse: remove the container, start the service again. Nix changes pushed in between are applied on the module's first sync.

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
