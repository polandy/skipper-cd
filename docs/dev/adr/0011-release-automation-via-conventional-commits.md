# ADR-0011: Releases are automated from Conventional Commits

Status: accepted
Date: 2026-07-09

## Context

The repository enforces Conventional Commits 1.0.0 (see CLAUDE.md), which
encode exactly the information a release needs: `feat` → minor bump,
`fix` → patch bump, `!`/`BREAKING CHANGE` → major bump, plus human-readable
changelog entries. Until now, releases were manual: someone had to decide a
version, write a changelog, push a `v*` tag, and the Docker workflow built
the image from that tag. In practice this meant no releases happened at all.

## Decision

A `release.yml` workflow runs [release-please](https://github.com/googleapis/release-please)
on every push to `main`. It maintains a long-lived release PR that
accumulates the pending changes as a CHANGELOG.md entry and the computed
next semver. Merging that PR creates the GitHub release and the `v*` tag.

Because tags created with the default `GITHUB_TOKEN` do not trigger other
workflows, the release workflow explicitly dispatches `docker.yml`
(`workflow_dispatch` with `--ref <tag>`) so the image build keeps working
without a personal access token.

## Consequences

- Releasing is a one-click action: merge the release PR.
- Version numbers and CHANGELOG.md are derived from commit messages;
  a malformed commit type now affects versioning, not just history hygiene.
- The release PR itself is created by `GITHUB_TOKEN` and therefore does not
  run CI; the commits it contains were already tested on `main`.
- Manual `v*` tags keep working (docker.yml still triggers on tag push),
  but should no longer be needed.

## Amendment (2026-11-19): a release image ships only after CI passed on its tag

release-please now runs with a PAT, so its `v*` tag triggers `docker.yml`
directly (the dispatch above is gone). Until now that build ran whatever the
tag pointed at, green or red. `docker.yml` now first calls the whole CI
workflow (`ci.yml`, `workflow_call`) on the tag, Trivy scan included, and
`build-and-push` runs only if every CI job succeeded. A manual `workflow_dispatch`
against a tag takes the same gate. Feature-branch image builds stay ungated
on purpose: they exist to try a branch on a host, and their PR runs CI anyway.

Rejected: waiting for `main`'s own CI run on the tagged commit (polling or
`workflow_run`). `ci.yml` cancels an in-progress run when the next commit lands
on `main`, so the run a release depends on can vanish. A `workflow_run` trigger
also races release-please, which may create the tag after CI completed.
Rerunning CI on the tag costs one extra full run per release and is deterministic.
