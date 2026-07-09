# ADR-0012: Dependabot patch/minor updates merge automatically

Status: accepted
Date: 2026-07-09

## Context

Dependabot opens weekly PRs for Go modules, GitHub Actions, and Docker base
images. Reviewing routine patch/minor bumps by hand adds latency without
adding much safety: CI already runs `go build`, `go vet`, gofmt,
`go test -race`, golangci-lint, and a vendor-sync check on every PR, and the
test suite asserts the exact docker/git command lines through fake runners.
The one blind spot was Docker base-image bumps — the image build only ran on
release, so a Dockerfile PR was effectively untested.

## Decision

- CI gains a `docker-build` job (`docker build` without push) so base-image
  bumps are verified on the PR.
- Branch protection on `main` requires the `test`, `lint`, and
  `docker-build` checks. It is not enforced for admins, so direct pushes by
  the repository owner keep working.
- A `dependabot-auto-merge.yml` workflow enables GitHub auto-merge
  (squash) on Dependabot PRs whose update type is patch or minor, using
  `dependabot/fetch-metadata`. GitHub merges once the required checks pass.
- Major updates are never auto-merged; they stay open for manual review.
  This is the supply-chain trade-off: routine bumps flow through CI
  unattended, anything with breaking-change potential gets human eyes.

## Consequences

- Squash merges use the PR title (`build(deps): bump …`), which is
  Conventional-Commits-compatible; release-please ignores `build` commits,
  so dependency bumps do not trigger releases on their own.
- Auto-merge is enabled via `GITHUB_TOKEN`, so the resulting push to `main`
  does not trigger push workflows (CI, release-please). The release PR picks
  the commits up on the next regular push. A PAT could lift this if it ever
  matters.
- The release PR from release-please (ADR-0011) runs no CI, so its required
  checks never report; merging it relies on the admin bypass that
  non-enforced branch protection grants.
