# TODO

e2e coverage gaps (one PR each, highest risk first; see `docs/dev/e2e-tests.md`):

- e2e: rebuild §6 traceability from the suite, rename `UI1–7` to `UAI1–7`, give UAF/UAM cases their own IDs, add UA11/UA12/UC16/P11/P12 to §4, move §4.40/§4.41 into order, add a `check-e2e-trace` CI script (IDs in test titles vs §4/§6, duplicates fail).
- e2e: pipeline cases for the `deploy_health_check` HTTP probe (non-2xx → rollback → `rolled_back_unhealthy`), the gate inferred from a compose `healthcheck:`, `deploy_health_check: false`, `rollback: false`, and the bootstrap run skipping `pull`/`build --pull`.
- e2e: pipeline case plus UAZ addition for a revert releasing a hold (converge `up -d --remove-orphans`, hold kept when the `up` fails) and for a `post_deploy` hook failure holding the change.
- e2e: zero-downtime rollout cutover and failure path (stub Traefik/label scenario in the harness); UI mask once the sub-label exists.
- e2e: deploy notifications (success, failure, rollback to the generic target), `update_check.notify`, the `allowed_hosts` check (`/webhook` and `/healthz` exempt) and an unknown config key failing startup.
- e2e: set Playwright `retries` to 0 once the known timing flakes have a deterministic seam.
