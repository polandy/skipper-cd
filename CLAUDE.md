# skipper-cd

Lightweight Docker Compose CD tool in Go. Receives Git push webhooks (Gitea/GitHub HMAC), keeps a local clone of the deploy repo, and redeploys only stacks whose tracked files changed (SHA-256 hashes persisted in `state.yaml`). Optionally runs `nixos-rebuild` first when nix files change.

## Commands

- Build: `go build ./...` (binary: `go build -o skipper ./cmd/skipper`)
- Test: `go test ./...` — fast, needs no docker/git; tests inject fake runners
- Verify before finishing any change:
  `go build ./... && go vet ./... && go test ./... && test -z "$(gofmt -l cmd internal)"`
  (the `test -z` wrapper is required — `gofmt -l` exits 0 even when files need formatting; the paths `cmd internal` keep `vendor/` out)
- Nix: `nix build` / `nix develop`. After dependency changes run `go mod tidy && go mod vendor` — the flake uses `vendorHash = null`, so `vendor/` must stay committed and in sync.

## Packages

- `cmd/skipper` — entrypoint; single `-config` flag (default `/etc/skipper/skipper.yml`)
- `internal/config` — YAML config load + validation
- `internal/deploy` — CORE: orchestration (deploy.go), change hashing (hash.go), image/pull logic (images.go), state persistence (state.go), env files (envfile.go)
- `internal/command` — `Runner` interface over os/exec; tests inject fakes through it
- `internal/git` — local clone management (clone, reset --hard, diff, file-at-commit)
- `internal/webhook` — HMAC-verified push handler; responds 202 immediately, deploy runs in a goroutine
- `internal/nixos` — nixos-rebuild; must stay free of docker/state/metrics/events knowledge
- `internal/events` — SSE broadcaster + bounded persisted event history
- `internal/metrics` — Prometheus metrics
- `internal/ui` — the web UI is ONE embedded file `static/index.html` (no JS deps); read `internal/ui/UI_SPEC.md` before UI changes

## Invariants — do not break these

1. Change detection and the compose file always come from the repo clone (`<stacks_base_dir>/<name>/docker-compose.yml`). `working_dir` only sets `--project-directory` (compose project identity + `.env` loading) — never conflate the two.
2. Hashed inputs per stack: compose file, `env_files`, global `vars_file`, `watch_dirs` contents (recursive), and Dockerfiles of `build:` services. A missing/corrupt `state.yaml` means all stacks redeploy — by design, not a bug.
3. Rollback fetches the old compose file from `state.LastDeployedCommit` into a temp file; `--project-directory` must then point at the original compose dir (the temp file lives in /tmp). A successful rollback still returns an error containing "rolled back" — that substring drives the `rolled_back` event status in deploy.go.
4. NixOS rebuild runs before stack deploys; if it fails, all stack deploys abort. Nix hashes are saved to state *before* the rebuild because the rebuild may restart the skipper service. Nix state uses the reserved stack key `_nixos`.
5. `compose pull` is skipped entirely when no `image:` reference changed; otherwise it excludes `build:` services and services whose `image:` matches a locally-built image name.
6. Env precedence (highest wins): `env_files` > `vars_file` > `os.Environ()`.
7. All deploys serialize on one mutex (`SyncAndDeployAll`, TryLock-then-Lock); concurrent webhooks wait — they don't queue duplicates.

## Testing

Table-style tests with `t.TempDir()` and real files on disk; inject `recordingRunner` fakes (implementing `command.Runner`) and assert the exact docker/git argv — never shell out for real. All patterns live in `internal/deploy/deploy_test.go`.

## Don'ts & pointers

- Never edit or search `vendor/` (committed on purpose; the nix flake depends on it).
- `./skipper` at the repo root is a build artifact — ignore it.
- Don't duplicate README content. Deploy/config/state semantics live in README.md ("How It Works", "Configuration", "State File") — read the relevant section when touching `internal/deploy` or `internal/config`.
