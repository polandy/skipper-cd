# skipper-cd Prometheus Metrics — Reference

skipper-cd exposes the following metrics on the `/metrics` endpoint (port configured via [`metrics_port`](configuration.md#top-level-fields), default `9120`).

| Metric | Type | Description |
|---|---|---|
| `skipper_webhooks_received_total` | counter | Total number of webhook requests received, labelled by `status`. |
| `skipper_deploys_triggered_total` | counter | Total number of stack deploys triggered, labelled by `stack`. |
| `skipper_deploys_skipped_total` | counter | Total number of stack deploys skipped (no changes), labelled by `stack`. |
| `skipper_deploy_errors_total` | counter | Total number of failed deploys, labelled by `stack`. |
| `skipper_deploy_rollbacks_total` | counter | Total number of successful rollbacks after failed deploys, labelled by `stack`. |
| `skipper_last_deploy_timestamp` | gauge | Unix timestamp of the last successful deploy, labelled by `stack`. |
| `skipper_deploy_lock_waits_total` | counter | Deploy runs that had to wait for a running deploy to finish (queueing indicator). |
| `skipper_deploys_queued_total` | counter | Deploys deferred because autosync was paused, labelled by `stack` (see [Autosync](autosync.md)). |
| `skipper_autosync_enabled` | gauge | Effective per-stack autosync (`1`/`0`), labelled by `stack` (incl. `_nixos`). |
| `skipper_autosync_global` | gauge | Effective global autosync (`1`/`0`). |
| `skipper_autosync_pending` | gauge | Number of stacks currently queued (queue depth). |
