# TODO

e2e coverage gaps (one PR each, highest risk first; see `docs/dev/e2e-tests.md`):

- e2e: UI mask for the zero-downtime rollout failure once the sub-label exists (the Go path is P25).
- e2e: set Playwright `retries` to 0 once the known timing flakes have a deterministic seam.

- ci: PRs opened by `update-flake-lock` (GITHUB_TOKEN) get no CI run; the checks stay empty until a person pushes to the branch. Decide between a fine-grained PAT/App token for the action (`token:` input) or a `workflow_dispatch` of CI on the bot branch; update the note in `.github/workflows/update-flake-lock.yml` either way.
