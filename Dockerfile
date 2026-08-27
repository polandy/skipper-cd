# Stage 1: Build
FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY cmd/ cmd/
COPY internal/ internal/
COPY .release-please-manifest.json ./

# Branch/commit come from CI (docker.yml passes --build-arg). The build strips
# .git, so the commit cannot be recovered at runtime — it must be injected here.
ARG COMMIT=""
ARG BRANCH=""

# Inject the release-please-tracked version plus the CI branch/commit into
# main.* so the UI header and /api/version report the deployed build.
RUN VERSION=$(sed -n 's/.*"\.":[[:space:]]*"\([^"]*\)".*/\1/p' .release-please-manifest.json) \
    && go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}" -o /skipper ./cmd/skipper

# Stage 2: Runtime
#
# Alpine, not distroless: skipper shells out to real git/docker-cli binaries
# (internal/command, deliberately not a Go git library — it catches real argv
# mistakes fakes can't). Distroless has no package manager to add those back,
# and hand-copying them in would hit a musl/glibc ABI mismatch against
# distroless/base plus git's runtime helper files under /usr/share/git-core.
FROM alpine:3.24

# UID/GID 1000: fixed so a volume from a previous (root-only) image version can
# be re-chowned to a known target during upgrade (see docs/docker.md). The
# docker-cli talks to the socket over HTTP, not Linux capabilities, so running
# as this unprivileged user doesn't affect docker.sock access — that's gated
# entirely by group membership (docs/docker.md's `group_add`).
# apk upgrade first: pick up security patches the base image tag lags behind
# (the CI vulnerability scan fails the build on known-fixed CVEs otherwise).
RUN apk upgrade --no-cache \
    && apk add --no-cache git docker-cli docker-cli-compose \
    && addgroup -g 1000 skipper \
    && adduser -D -u 1000 -G skipper -h /var/lib/skipper skipper \
    && mkdir -p /var/lib/skipper /etc/skipper \
    && chown -R skipper:skipper /var/lib/skipper /etc/skipper

COPY --from=build /skipper /usr/local/bin/skipper

EXPOSE 8080 9120

# /healthz returns 503 when the last repository sync failed. Assumes the
# default webhook port 8080; override the check when configuring another port.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

USER skipper
ENTRYPOINT ["skipper", "-config", "/etc/skipper/skipper.yml"]
