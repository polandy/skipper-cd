# Stage 1: Build
FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY cmd/ cmd/
COPY internal/ internal/

RUN go build -o /skipper ./cmd/skipper

# Stage 2: Runtime
FROM alpine:3.21

RUN apk add --no-cache git docker-cli docker-cli-compose

COPY --from=build /skipper /usr/local/bin/skipper

RUN mkdir -p /var/lib/skipper /etc/skipper

EXPOSE 8080 9120

ENTRYPOINT ["skipper", "-config", "/etc/skipper/skipper.yml"]
