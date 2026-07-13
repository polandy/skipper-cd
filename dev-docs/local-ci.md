# Running CI Locally

The whole CI pipeline (`.github/workflows/ci.yml` + `docs.yml`) can be run on
your own machine — useful on NixOS, where the tools aren't otherwise on PATH.
The flake's dev shell carries the toolchain; a `Makefile` at the repo root
mirrors each CI job so a green `make ci` predicts a green pipeline.

## Quick start

```sh
nix develop          # drop into the dev shell (Go, Node, linters, Playwright…)
make ci              # run every daemon-free CI job in order
```

Or run a single job:

```sh
make test            # go build/vet/gofmt + go test -race + coverage summary
make vendor-check    # go mod tidy && vendor, then diff go.mod/go.sum/vendor
make lint            # golangci-lint run ./...
make govulncheck     # govulncheck ./...
make e2e             # go test -tags e2e ./e2e (stub docker, no daemon)
make e2e-ui          # npm ci + Playwright against the embedded UI
make docs            # mkdocs build --strict
make docker-build    # docker build + trivy scan (NOT in `make ci` — see below)
```

## What `make ci` covers

Every CI job **except** `docker-build`, which needs a running Docker daemon
(out of the dev shell's scope). Run it separately once `dockerd` is up:

```sh
make docker-build
```

## How the dev shell matches CI

- **Go** is pinned to `go_1_25` — go.mod's minor, i.e. what CI's `setup-go`
  installs from `go-version-file`. This matters: a newer toolchain makes
  `govulncheck` flag stdlib CVEs that CI (on the pinned minor) does not. The
  nix *build* (`packages.default`) tracks `pkgs.go` separately and is unchanged.
- **Playwright** uses the NixOS-patched browser from `pkgs.playwright-driver`
  via `PLAYWRIGHT_BROWSERS_PATH`, version-matched to `@playwright/test` 1.61.1
  in `e2e/ui`. So `make e2e-ui` runs `npm ci` but **not** `playwright install` —
  the JS runner comes from npm, the browser from nix.
- **mkdocs**: nixpkgs tracks a slightly newer Material (9.7.x) than
  `docs/requirements.txt` pins (9.6.14). Close enough for a local strict-build
  pre-check; **CI remains the source of truth** for the published site.
- **vendor-check** scopes its `git diff` to `go.mod`, `go.sum`, and `vendor/`
  (CI runs it on a clean checkout), so it still passes with unrelated
  uncommitted edits while you work.
