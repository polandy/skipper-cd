# Stage 1: Build
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY cmd/ cmd/
COPY internal/ internal/

RUN go build -o /skipper ./cmd/skipper

# Stage 2: Runtime
FROM alpine:3.24

RUN apk add --no-cache git docker-cli docker-cli-compose

COPY --from=build /skipper /usr/local/bin/skipper

RUN mkdir -p /var/lib/skipper /etc/skipper

EXPOSE 8080 9120

# /healthz returns 503 when the last repository sync failed. Assumes the
# default webhook port 8080; override the check when configuring another port.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["skipper", "-config", "/etc/skipper/skipper.yml"]
