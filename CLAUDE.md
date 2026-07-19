# skipper-cd

Lightweight Docker Compose CD tool in Go. Receives Git push webhooks (Gitea/GitHub HMAC), keeps a local clone of the deploy repo, and redeploys only stacks whose tracked files changed (SHA-256 hashes persisted in `state.yaml`). Optionally runs `nixos-rebuild` first when nix files change.

## Commands

- Build: `go build ./...` (binary: `go build -o skipper ./cmd/skipper`)
- Test: `go test ./...` — fast, needs no docker/git; tests inject fake runners
- Verify before finishing any change:
  `go build ./... && go vet ./... && go test ./... && test -z "$(gofmt -l cmd internal)"`
  (the `test -z` wrapper is required — `gofmt -l` exits 0 even when files need formatting; the paths `cmd internal` keep `vendor/` out)
- Lint: `golangci-lint run ./...` (config in `.golangci.yml`; locally via `nix run nixpkgs#golangci-lint -- run ./...`). CI runs it plus `go test -race`.
- Nix: `nix build` / `nix develop`. After dependency changes run `go mod tidy && go mod vendor` — the flake uses `vendorHash = null`, so `vendor/` must stay committed and in sync.

## Packages

- `cmd/skipper` — entrypoint; single `-config` flag (default `/etc/skipper/skipper.yml`)
- `internal/config` — YAML config load + validation; in stack-discovery mode `LoadRepoStacks` also validates each stack against its compose file (parse, rollout eligibility, env/watch existence) every sync
- `internal/compose` — minimal read-only `docker-compose.yml` parser (`File`/`Service` + `PublishesPorts`/`HasHealthcheck`/`HasContainerName`); shared by `deploy` (image/build/rollout) and `config` (discovery validation) so there is one parser
- `internal/deploy` — CORE: orchestration (deploy.go), change hashing (hash.go), image/pull logic (images.go, wraps `internal/compose`), state persistence (state.go), env files (envfile.go)
- `internal/command` — `Runner` interface over os/exec; tests inject fakes through it
- `internal/containerlogs` — UI-only live `docker compose logs` over SSE (`/api/container-logs`, ADR-0037); own `LogStreamer` seam so the deploy path's `Runner` stays run-to-completion
- `internal/git` — local clone management (clone, reset --hard, diff, file-at-commit)
- `internal/webhook` — HMAC-verified push handler; responds 202 immediately, deploy runs in a goroutine
- `internal/nixos` — nixos-rebuild; must stay free of docker/state/metrics/events knowledge
- `internal/events` — SSE broadcaster + bounded persisted event history
- `internal/metrics` — Prometheus metrics
- `internal/roster` — builds the Stacks-view inventory (discovered/host stack set + last audit outcome per stack); pure, fed into the `stacks` SSE snapshot (dev-docs/stack-roster-spec.md)
- `internal/ui` — the web UI is **self-contained** (ADR-0035): everything ships inside the binary and is served same-origin — no build step, no bundler, no external/CDN/npm dep, works offline. The app shell (markup and the main app script) is the embedded file `static/index.html`; its stylesheet is extracted into `static/app.css` (ADR-0035), linked from the `<head>`; the pure, DOM-free helpers it calls are extracted into `static/app-helpers.js` (loaded first, exposed as globals) so a `node --test` unit layer can import them (`make ui-unit`, its own CI job); fonts (`static/fonts`) and PWA icons (`static/icons`) are separate embedded files, each served by its own scoped handler. Read `internal/ui/UI_SPEC.md` before UI changes

## Invariants — do not break these

1. Change detection and the compose file always come from the repo clone (`<stacks_base_dir>/<name>/docker-compose.yml`). `working_dir` only sets `--project-directory` (compose project identity + `.env` loading) — never conflate the two.
2. Hashed inputs per stack: compose file, `env_files`, global `vars_file`, `watch_dirs` contents (recursive), and Dockerfiles of `build:` services. In stack-discovery mode (ADR-0034) additionally the stack's deploy-shaping config (`Stack.ConfigHash`, keyed by the `skipper.yaml` path at `stacks_base_dir`) — but never `icon`/`self_heal`/`depends_on`/`hooks`/`rollout`, whose edits must not redeploy. A missing/corrupt `state.yaml` means all stacks redeploy — by design, not a bug.
3. Rollback fetches the old compose file from `state.LastDeployedCommit` into a temp file; `--project-directory` must then point at the original compose dir (the temp file lives in /tmp). A successful rollback still returns an error wrapping `deploy.ErrRolledBack` — `errors.Is` on it drives the `rolled_back` event status in deploy.go. An optional per-stack `health_check` gates the deploy through the same path: `up` then runs with `--wait --wait-timeout`, and an optional HTTP probe must answer 2xx within the timeout — either failure triggers the same rollback (ADR-0022). With `health_check` set, the rollback reruns the same gate (`--wait` + probe); if the restored version also fails it, the error wraps `deploy.ErrRollbackUnhealthy` → `rolled_back_unhealthy` event status. Without `health_check`, the rollback `up` never uses `--wait`.
4. NixOS rebuild runs before stack deploys; if it fails, all stack deploys abort. Nix hashes are saved to state *before* the rebuild because the rebuild may restart the skipper service. Nix state uses the reserved stack key `_nixos`. The rebuild runs in a transient systemd unit outside skipper's cgroup, and the wait for it is abandoned on shutdown — never run it as a direct child or block shutdown on it (ADR-0014). When the rebuild fails *while skipper is still alive* (not a shutdown/self-restart), the pre-saved `_nixos` hashes are reverted so the next sync retries — otherwise a never-applied rebuild is silently marked done (ADR-0015). A rebuild whose switch *self-restarts* skipper (shutdown requested) is NOT a failure: it must not emit a `_nixos` failed event or count an error — instead an in-flight marker (`state.NixOSRebuildInFlight`) is left set and the next startup reconciles it into a `_nixos` success (the rebuild applied in its transient unit), so the UI never shows a stale failure (ADR-0025).
5. `compose pull` is skipped entirely when no `image:` reference changed; otherwise it excludes `build:` services and services whose `image:` matches a locally-built image name.
6. Env precedence (highest wins): `env_files` > `vars_file` > `os.Environ()`.
7. All deploys serialize on one mutex (`SyncAndDeployAll`, TryLock-then-Lock); concurrent webhooks wait — they don't queue duplicates.
8. Stack discovery (`stack_discovery: true`, ADR-0034): the stack set comes from the repo per sync (`config.LoadRepoStacks`), mutually exclusive with a host `stacks:` list. An unparseable repo `skipper.yaml` aborts the stack phase under the reserved `_config` key (nixos phase unaffected); entry-level errors fail only the affected stacks and block their dependents. Out-of-run consumers read the set via `Deployer.CurrentStacks()` (`stacksNow` in main) — never from `cfg.Stacks`, which is empty in discovery mode.

## Testing

Table-style tests with `t.TempDir()` and real files on disk; inject `recordingRunner` fakes (implementing `command.Runner`) and assert the exact docker/git argv — never shell out for real. Test files in `internal/deploy` mirror the source files (`hash_test.go`, `images_test.go`, …); shared fakes and helpers live in `internal/deploy/helpers_test.go`.

Two deliberate exceptions run real commands: `internal/command` tests (the process boundary — faking exec there would test nothing) and `internal/git/integration_test.go` (real `git` against a local repo, catches argv mistakes the fakes cannot; skips when git is missing).

## Engineering principles

Andy's priorities for all work in this repo:

- **Test-first**: a new feature starts with tests that specify its behavior (behavior-revealing names like `TestDeployStack_SkipsWhenUnchanged`), then implement until green. Bug fixes start with a failing test reproducing the bug.
- **Readability first**: small files, doc comments on exported symbols, names that reveal behavior. Readable beats clever.
- **Clear responsibilities & encapsulation**: each package/type owns its data — don't pass raw mutable maps/structs around when a type with methods would hide the representation. Delete dead code instead of keeping it "for later"; unused APIs are complexity.
- **Extensibility**: consumer-side interfaces (like `Runner`, `CommitReader`) instead of concrete coupling; keep packages small with one job.
- **Coverage**: every non-trivial package has tests; check `go test ./... -cover` for regressions when touching a package. A package that implements a safety/correctness invariant (e.g. `internal/fsatomic`'s atomic writes) needs its failure paths tested too, not just the happy path.
- **Go conventions**: gofmt/vet clean, sentinel errors (`errors.Is`) instead of matching error strings, `any` instead of `interface{}`, atomic writes (temp file + rename) for persisted state, no magic strings/numbers (hoist a literal to a named constant once it's repeated across files or compared/switched against), never discard an error that matters with `_ = fn()` — log it at minimum, especially for state-persisting calls.
- **Commit messages** follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/): `<type>(<optional scope>): <description>` with types like `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci` (see existing history: `fix: gitignore actual binary name skipper`).

## Don'ts & pointers

- Never edit or search `vendor/` (committed on purpose; the nix flake depends on it).
- `./skipper` at the repo root is a build artifact — ignore it.
- Don't duplicate doc content. README.md is a slim landing page (quickstart, features, condensed "How It Works"); the reference lives in `docs/` — `docs/configuration.md` (config + stack fields + vars_file + icons), `docs/nixos.md` (nixos_rebuild + NixOS module + working_dir), `docs/docker.md`, `docs/metrics.md`, `docs/state.md`. Read the relevant page when touching `internal/deploy` or `internal/config`, and keep it in sync.
- `docs/` is published as a MkDocs Material site to GitHub Pages (`mkdocs.yml`, `docs/requirements.txt`, `.github/workflows/docs.yml`). After doc changes, verify with `mkdocs build --strict` (CI does the same and it fails on broken links). Nav is hand-written in `mkdocs.yml`. `docs/` holds **only** user-facing pages — contributor/design docs live in `dev-docs/` (see below) and are **not** published; don't link to them from the user manual. The rare unavoidable cross-repo link out of `docs/` (e.g. to `internal/ui/UI_SPEC.md`) must be an absolute GitHub URL, not a `../` path, or strict build breaks.
- Autosync/queue semantics (global + per-stack config toggles, non-persistent UI overrides, the leave-dirty queue + pending registry, `queued` event, `/api/autosync` and `/api/queue`) are specified in [`dev-docs/autosync-spec.md`](dev-docs/autosync-spec.md) and [ADR-0016](dev-docs/adr/0016-autosync-and-queue-via-leave-dirty.md); the UI surface is in [`internal/ui/UI_SPEC.md`](internal/ui/UI_SPEC.md). Read them before touching autosync, the per-stack deploy gate, or the queue.
- Contributor/design docs live in `dev-docs/` (outside `docs/` so MkDocs never publishes them): `dev-docs/adr/` holds Architecture Decision Records (one file per decision — significant design changes need a new ADR; the invariants above are their enforcement summary), plus feature specs (`dev-docs/autosync-spec.md`, `dev-docs/pwa-spec.md`, `dev-docs/service-icons-spec.md`, `dev-docs/stack-roster-spec.md`), the shared UI design language (`dev-docs/ui-design-concept.md` — **one look and feel across every view: reuse the same components + CSS (badges, bound panels, the log panel, tap-tip bubble, status/health tokens) rather than building parallel ones**; also the row/table/expand-panel conventions), and `dev-docs/e2e-tests.md`. These are for people working on skipper-cd, not users.
