# TODO

e2e coverage gaps (one PR each, highest risk first; see `docs/dev/e2e-tests.md`):

- e2e: pipeline case plus UAZ addition for a `post_deploy` hook failure holding the change.
- e2e: zero-downtime rollout cutover and failure path (stub Traefik/label scenario in the harness); UI mask once the sub-label exists.
- e2e: deploy notifications (success, failure, rollback to the generic target), `update_check.notify`, the `allowed_hosts` check (`/webhook` and `/healthz` exempt) and an unknown config key failing startup.
- e2e: set Playwright `retries` to 0 once the known timing flakes have a deterministic seam.

- ci: PRs opened by `update-flake-lock` (GITHUB_TOKEN) get no CI run; the checks stay empty until a person pushes to the branch. Decide between a fine-grained PAT/App token for the action (`token:` input) or a `workflow_dispatch` of CI on the bot branch; update the note in `.github/workflows/update-flake-lock.yml` either way.
