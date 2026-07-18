# Local mirror of the CI pipeline (.github/workflows/ci.yml + docs.yml).
# Enter the dev shell first (`nix develop`) so every tool is on PATH, then run
# `make ci` for the daemon-free jobs, or a single target below. Each target
# maps 1:1 to a CI job so a green `make ci` predicts a green pipeline.
.PHONY: ci build vet fmt test vendor-check e2e e2e-ui lint govulncheck docs docker-build ui-preview

# Everything CI runs that does NOT need a docker daemon. `docker-build` is left
# out on purpose (see its note) — run it separately when dockerd is up.
ci: test vendor-check lint govulncheck e2e docs e2e-ui

## --- test job -------------------------------------------------------------
build:
	go build ./...

vet:
	go vet ./...

fmt:
	test -z "$$(gofmt -l cmd internal)"

test: build vet fmt
	go test ./... -race -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

# The nix flake builds with vendorHash = null, so vendor/ must stay committed
# and in sync with go.mod. CI runs `git diff --exit-code` on a clean checkout;
# scoping the diff to the vendor inputs lets this pass with unrelated
# uncommitted edits (e.g. mid-feature) while still catching real drift.
vendor-check:
	go mod tidy
	go mod vendor
	git diff --exit-code -- go.mod go.sum vendor/

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
	govulncheck ./...

## --- docs job -------------------------------------------------------------
docs:
	mkdocs build --strict

## --- docker-build job -----------------------------------------------------
# Needs a running docker daemon (out of the dev shell's scope); not part of
# `make ci`. trivy comes from the dev shell.
docker-build:
	docker build -t skipper-cd:ci .
	trivy image --scanners vuln --severity CRITICAL,HIGH --ignore-unfixed --exit-code 1 skipper-cd:ci

## --- ui-preview (not a CI job) --------------------------------------------
# Boot a seeded skipper instance for manually eyeballing the web UI, then stay
# up until Ctrl-C. Builds from the current checkout; no docker/network needed.
# Override the port with PORT=… (default 3000). See dev-docs/ui-preview.md.
ui-preview:
	node scripts/ui-preview.mjs
