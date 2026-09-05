# Local mirror of the CI pipeline (.github/workflows/ci.yml + docs.yml).
# Enter the dev shell first (`nix develop`) so every tool is on PATH, then run
# `make ci` for the daemon-free jobs, or a single target below. Each target
# maps 1:1 to a CI job so a green `make ci` predicts a green pipeline.
.PHONY: ci build vet fmt test vendor-check ui-unit ui-fmt ui-lint e2e e2e-ui lint govulncheck docs docker-build ui-preview ui-preview-smoke docs-screenshots check-host-colors check-playwright-pin check-baselines check-no-sleeps e2e-ui-lint

# Everything CI runs that does NOT need a docker daemon. `docker-build` is left
# out on purpose (see its note) — run it separately when dockerd is up.
ci: test vendor-check ui-fmt ui-lint ui-unit check-host-colors check-playwright-pin check-no-sleeps e2e-ui-lint lint govulncheck e2e docs e2e-ui ui-preview-smoke

## --- test job -------------------------------------------------------------
# The Go packages of this module. `./...` also descends into the gitignored npm
# trees (internal/ui/static/node_modules, e2e/ui/node_modules), and some npm
# packages ship stray .go files (flatted's golang port): after a local `npm ci`
# they get built and tested as our own code, and their 0 %-covered statements
# pull the coverage total below what CI reports from its clean checkout. Resolved
# when a recipe runs, so a fresh clone and one with node_modules agree.
GO_PKGS = $$(go list ./... | grep -v /node_modules/)

build:
	go build $(GO_PKGS)

vet:
	go vet $(GO_PKGS)

fmt:
	test -z "$$(gofmt -l cmd internal)"

test: build vet fmt
	go test $(GO_PKGS) -race -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

# The nix flake builds with vendorHash = null, so vendor/ must stay committed
# and in sync with go.mod. CI runs `git diff --exit-code` on a clean checkout;
# scoping the diff to the vendor inputs lets this pass with unrelated
# uncommitted edits (e.g. mid-feature) while still catching real drift.
vendor-check:
	go mod tidy
	go mod vendor
	git diff --exit-code -- go.mod go.sum vendor/

## --- ui-unit job ----------------------------------------------------------
# JS unit tests for the pure UI helpers and render layer
# (internal/ui/static/app-helpers.js, app-render.js) via
# the Node built-in runner — no build step, no deps.
ui-unit:
	node --test internal/ui/static/app-helpers.test.js internal/ui/static/app-render.test.js

# Prettier/ESLint for the embedded UI's plain-script JS (app.js, app-state.js, app-panels.js, app-hosts.js, app-autosync.js, app-logs.js, app-clog.js,
# app-render.js, app-helpers.js, the .test.js files, sw.js). Dev-only npm devDependencies, scoped to
# internal/ui/static — never shipped in the binary (ADR-0035). app.css keeps
# its hand-tuned compact style and is intentionally not formatted here.
ui-fmt:
	cd internal/ui/static && npm ci && npm run fmt:check

ui-lint:
	cd internal/ui/static && npm ci && npm run lint

## --- e2e job --------------------------------------------------------------
e2e:
	test -z "$$(gofmt -l e2e)"
	go vet -tags e2e ./e2e
	go test -tags e2e -v -count=1 ./e2e

## --- e2e-ui job -----------------------------------------------------------
# Browsers come from the dev shell (PLAYWRIGHT_BROWSERS_PATH), so unlike CI
# there is no `playwright install` step here.
e2e-ui:
	cd e2e/ui && npm ci && npx playwright test

## --- lint job -------------------------------------------------------------
lint:
	golangci-lint run ./...

## --- govulncheck job ------------------------------------------------------
govulncheck:
	govulncheck $(GO_PKGS)

## --- docs job -------------------------------------------------------------
docs:
	mkdocs build --strict

## --- docker-build job -----------------------------------------------------
# Needs a running docker daemon (out of the dev shell's scope); not part of
# `make ci`. trivy comes from the dev shell.
docker-build:
	docker build -t skipper-cd:ci .
	trivy image --scanners vuln --severity CRITICAL,HIGH --ignore-unfixed --exit-code 1 skipper-cd:ci

## --- docs screenshots (rendered, never committed) -------------------------
# Renders docs/assets/screenshots/*.png from a seeded instance, exactly as the
# docs workflow does before `mkdocs build`. Needs the Playwright browsers
# (`cd e2e/ui && npx playwright install chromium`); on a host whose bundled
# Chromium cannot run, set PW_CHROMIUM_EXECUTABLE.
docs-screenshots:
	V=$$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//'); \
	CGO_ENABLED=0 go build -buildvcs=false \
	  -ldflags "-X main.version=$${V:-0.0.0} -X main.commit=$$(git rev-parse --short HEAD)" \
	  -o e2e/ui/.docs-shot-bin ./cmd/skipper
	cd e2e/ui && SKIPPER_E2E_BIN=$(CURDIR)/e2e/ui/.docs-shot-bin \
	  npx playwright test --config screenshots/shots.config.ts

## --- ui-preview (not a CI job) --------------------------------------------
# Boot a seeded skipper instance for manually eyeballing the web UI, then stay
# up until Ctrl-C. Builds from the current checkout; no docker/network needed.
# Override the port with PORT=… (default 3000). See dev-docs/ui-preview.md.
ui-preview:
	node scripts/ui-preview.mjs

# Boot the seeded preview, assert it serves and still produces its spread of
# deploy outcomes, then clean up. Run in CI as skipper's startup smoke test.
ui-preview-smoke:
	node scripts/ui-preview.mjs --smoke

## --- check-host-colors (part of the ui-unit job) --------------------------
# scripts/gen-host-colors.mjs generates the --host-N palette hand-pasted into
# internal/ui/static/app.css (see the block's own "Generated by" comment).
# Nothing else verifies the paste still matches the generator, so diff the two
# token-for-token; a mismatch means someone hand-edited the CSS block or
# retuned the script without regenerating the other. The slot index stays
# unbounded so a newly added --host-N is compared too, not silently ignored.
HOST_COLOR_TOKEN := --host-[0-9]+:\#[0-9a-fA-F]{6}

check-host-colors:
	@gen=$$(mktemp) && com=$$(mktemp) && trap 'rm -f "$$gen" "$$com"' EXIT; \
	node scripts/gen-host-colors.mjs | grep -oE -- '$(HOST_COLOR_TOKEN)' > "$$gen"; \
	grep -oE -- '$(HOST_COLOR_TOKEN)' internal/ui/static/app.css > "$$com"; \
	diff "$$gen" "$$com"

## --- check-playwright-pin (part of the ui-unit job) ------------------------
# The CI container renders the visual baselines and the npm package drives the
# comparison; drift between them shows up as a font-rasterisation pixel diff
# rather than an error, so it needs its own check.
check-playwright-pin:
	@scripts/check-playwright-pin.sh

## --- check-baselines (part of the e2e-ui job) -----------------------------
# Asserts the snapshot baselines are real PNGs and not git-lfs pointers, which
# is what the CI job needs to know: that the artifact hand-off landed. Not part
# of `make ci` — a plain local `make e2e-ui` skips the pixel compares, so
# pointers are harmless there (CLAUDE.md §Binary assets). Run it locally before
# regenerating a baseline or before a RUN_SNAPSHOTS=1 run, where they are not.
# The CI job calls the script directly: the Playwright container ships no make.
check-baselines:
	@scripts/check-baselines.sh

## --- check-no-sleeps (part of the ui-unit job) ----------------------------
# Keeps the Playwright suite free of fixed waits: a waitForTimeout passes on a
# fast runner and flakes on a loaded one, and it hides the real gap — that the
# UI publishes no state to assert on.
check-no-sleeps:
	@scripts/check-no-sleeps.sh

## --- e2e-ui-lint (part of the ui-unit job) ---------------------------------
# Type-aware ESLint over the Playwright suite. The rule that earns it is
# no-floating-promises: a forgotten `await` on an assertion makes a test pass
# without checking anything, and a vacuously green test is worse than none.
# Not Prettier-formatted, deliberately — like app.css, the suite keeps its
# hand-written style.
e2e-ui-lint:
	cd e2e/ui && npm ci && npm run lint
