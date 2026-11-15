# ADR-0003: Runner abstraction and fake-based tests

Status: accepted
Date: 2026-07-09 (documents a decision made at project start; integration
tier added 2026-07-09)

## Context

skipper-cd shells out to docker, git and nixos-rebuild. Tests that run those
binaries for real would be slow, environment-dependent and destructive.

## Decision

All process execution goes through the `command.Runner` interface (plus an
`Output` variant for captured stdout). Production uses `ShellRunner`; tests
inject recording fakes and assert the exact argv. Consumers define the
interfaces they need (`Runner`, `CommitReader`, `RepoSyncer`,
`deployTrigger`) rather than depending on concrete types.

Two deliberate exceptions run real commands:

- `internal/command` tests execute trivial binaries (`true`, `echo`,
  `sleep`) — this package *is* the process boundary; faking it would test
  nothing.
- `internal/git/integration_test.go` runs real `git` against a local
  repository to catch argv mistakes the fakes cannot see. It skips when git
  is not installed.

## Consequences

- `go test ./...` is fast, hermetic and needs no docker daemon.
- argv regressions in git handling are caught by the integration tier.
- docker/nixos argv correctness is asserted only against fakes; mistakes
  there surface first in a real deployment.
