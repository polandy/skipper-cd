# skipper-cd with Docker — Reference

skipper-cd can run as a Docker container instead of a [NixOS service](nixos.md). This page covers running it in Docker and how it handles locally-built images.

---

## Running as a Container

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

> **Note:** [Notification](configuration.md#notifications) `url`s are resolved from inside skipper-cd's container, so `localhost` is the container itself — point them at an address reachable from the container.

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
