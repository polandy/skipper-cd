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
