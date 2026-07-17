# skipper-cd Prometheus Metrics — Reference

skipper-cd exposes the following metrics on the `/metrics` endpoint (port configured via [`metrics_port`](configuration.md#top-level-fields), default `9120`).

| Metric | Type | Description |
|---|---|---|
| `skipper_webhooks_received_total` | counter | Total number of webhook requests received and accepted. |
| `skipper_deploys_triggered_total` | counter | Total number of stack deploys triggered, labelled by `stack`. |
| `skipper_deploys_skipped_total` | counter | Total number of stack deploys skipped (no changes), labelled by `stack`. |
| `skipper_deploy_errors_total` | counter | Total number of failed deploys, labelled by `stack`. |
| `skipper_deploy_rollbacks_total` | counter | Total number of successful rollbacks after failed deploys, labelled by `stack`. |
| `skipper_last_deploy_timestamp_seconds` | gauge | Unix timestamp of the last successful deploy, labelled by `stack`. |
| `skipper_deploy_lock_waits_total` | counter | Deploy runs that had to wait for a running deploy to finish (queueing indicator). |
| `skipper_deploys_queued_total` | counter | Deploys deferred because autosync was paused, labelled by `stack` (see [Autosync](autosync.md)). |
| `skipper_autosync_enabled` | gauge | Effective per-stack autosync (`1`/`0`), labelled by `stack` (incl. `_nixos`). |
| `skipper_autosync_global` | gauge | Effective global autosync (`1`/`0`). |
| `skipper_autosync_pending` | gauge | Number of stacks currently queued (queue depth). |
| `skipper_notifications_sent_total` | counter | Outbound notification deliveries, labelled by `format` and `outcome` (`ok`/`error`) (see [Notifications](configuration.md#notifications)). |
| `skipper_notifications_dropped_total` | counter | Notification events dropped because the delivery buffer was full. |
| `skipper_health_transitions_total` | counter | Accepted per-service health transitions observed by the [health watch](configuration.md#health-watch), labelled by resulting `status`. |
| `skipper_health_alerts_sent_total` | counter | Outbound health alert deliveries, labelled by `format` and `outcome` (`ok`/`error`). |
| `skipper_health_alerts_suppressed_total` | counter | Health alerts held back by the [alert cooldown](configuration.md#health-watch), labelled by target `status`. |

## Recommended Alerts

Two alerts cover the failure modes that matter in practice:

- **Deploy failed** — `increase(skipper_deploy_errors_total[5m]) > 0`.
  Covers failed stack deploys and failed `nixos-rebuild` runs (counted
  under the reserved stack label `_nixos`).
- **skipper-cd down** — `up{job="<your skipper job>"} == 0` for a few
  minutes. Without this, a dead skipper-cd drops webhooks silently and
  deployments stall with no signal: the error counter only moves when a
  deploy *runs* and fails. Allow a grace period spanning at least two
  scrape intervals — when skipper-cd deploys a NixOS configuration that
  changes its own service, the rebuild restarts skipper-cd and the
  endpoint briefly disappears.
