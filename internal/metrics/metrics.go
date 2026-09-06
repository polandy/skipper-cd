// Package metrics defines the Prometheus metrics exported by skipper-cd.
// They are registered automatically on import via promauto.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// WebhooksReceived counts every accepted incoming webhook.
	WebhooksReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "skipper_webhooks_received_total",
		Help: "Total number of webhooks received and accepted.",
	})

	// WebhooksRejected counts incoming webhooks rejected before triggering a
	// deploy, by reason ("signature" for a bad/missing HMAC signature,
	// "too_large" for a body over MaxBodyBytes). A rising signature count
	// usually means a misconfigured webhook_secret or unsolicited probing.
	WebhooksRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_webhooks_rejected_total",
		Help: "Total number of webhooks rejected before deploy, by reason (signature|too_large).",
	}, []string{"reason"})

	// DeploysTriggered counts deployments that were actually executed, per stack.
	DeploysTriggered = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_deploys_triggered_total",
		Help: "Total number of deploys triggered per stack.",
	}, []string{"stack"})

	// DeploysSkipped counts deployments that were skipped because the
	// stack configuration had not changed since the last run, per stack.
	DeploysSkipped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_deploys_skipped_total",
		Help: "Total number of deploys skipped (no changes detected) per stack.",
	}, []string{"stack"})

	// StackConfigError marks each stack currently excluded by an entry-level
	// configuration error (a missing stack directory, a broken compose file,
	// an invalid rollout), and the reserved _config key when stack discovery
	// itself failed. Set to 1 while the error stands and deleted once it
	// clears: the matching failed event is emitted only when the error appears
	// or changes (ADR-0055), so this gauge — not DeployErrors — is what an
	// alert on "config has been broken for a while" reads.
	StackConfigError = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "skipper_stack_config_error",
		Help: "Stacks currently excluded by a configuration error (1 = broken).",
	}, []string{"stack"})

	// ProjectDirSyncError marks the project_directory checkout as not
	// fast-forwarded this run — a dirty tree, a diverged history, an
	// unreachable remote (ADR-0060). Set to 1 while the condition stands and
	// back to 0 once it clears: the matching failed event is emitted only when
	// the condition appears or its message changes (ADR-0055), so this gauge is
	// what an alert on "the mounted content has been stale for a while" reads.
	ProjectDirSyncError = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "skipper_project_dir_sync_error",
		Help: "The project_directory checkout could not be fast-forwarded (1 = its content may be stale).",
	})

	// DeployErrors counts failed deployments per stack.
	DeployErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_deploy_errors_total",
		Help: "Total number of deploy errors per stack.",
	}, []string{"stack"})

	// LastDeployTimestamp records the Unix timestamp of the last successful
	// deployment per stack. Use this in Grafana to visualize when each stack
	// was last deployed (stat panel, table, or State timeline via changes()).
	LastDeployTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "skipper_last_deploy_timestamp_seconds",
		Help: "Unix timestamp of the last successful deploy per stack.",
	}, []string{"stack"})

	// DeployRollbacks counts successful rollbacks after failed deploys per stack.
	DeployRollbacks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_deploy_rollbacks_total",
		Help: "Total number of successful rollbacks after failed deploys per stack.",
	}, []string{"stack"})

	// DeployLockWaits counts deploy runs that had to wait for an earlier run
	// to finish. Sustained growth means webhooks queue up and would justify
	// coalescing queued runs (see ADR-0010).
	DeployLockWaits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "skipper_deploy_lock_waits_total",
		Help: "Total number of deploy runs that waited for a running deploy to finish.",
	})

	// DeploysQueued counts deploys deferred because autosync was paused, per
	// stack (see docs/autosync.md).
	DeploysQueued = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_deploys_queued_total",
		Help: "Total number of deploys deferred because autosync was paused, per stack.",
	}, []string{"stack"})

	// DeploysBlocked counts deploys held back because a depends_on dependency
	// failed or was blocked in the same run, per stack (ADR-0032). A blocked
	// stack stays dirty and retries on the next sync.
	DeploysBlocked = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_deploys_blocked_total",
		Help: "Total number of deploys blocked by a failed depends_on dependency, per stack.",
	}, []string{"stack"})

	// AutosyncEnabled reports the effective per-stack autosync state (1 = on,
	// 0 = paused), including the reserved stack "_nixos".
	AutosyncEnabled = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "skipper_autosync_enabled",
		Help: "Effective per-stack autosync state (1 = on, 0 = paused).",
	}, []string{"stack"})

	// AutosyncGlobal reports the effective global autosync state (1 = on, 0 = paused).
	AutosyncGlobal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "skipper_autosync_global",
		Help: "Effective global autosync state (1 = on, 0 = paused).",
	})

	// AutosyncPending reports the number of stacks currently queued (queue depth).
	AutosyncPending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "skipper_autosync_pending",
		Help: "Number of stacks currently queued waiting for autosync to resume.",
	})

	// NotificationsSent counts outbound notification deliveries by target format
	// and outcome ("ok" for a 2xx response, "error" for a transport error or a
	// non-2xx status). See ADR-0020.
	NotificationsSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_notifications_sent_total",
		Help: "Total outbound notification deliveries by format and outcome (ok|error).",
	}, []string{"format", "outcome"})

	// NotificationsDropped counts notification events dropped because the shared
	// delivery buffer was full (a wedged or very slow target). Unlabeled: the
	// buffer is global, so a drop is not attributable to a single format.
	NotificationsDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "skipper_notifications_dropped_total",
		Help: "Total notification events dropped because the delivery buffer was full.",
	})

	// HealthTransitions counts accepted (debounced) per-service health
	// transitions observed by the healthwatch watcher, by resulting status.
	// See ADR-0031.
	HealthTransitions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_health_transitions_total",
		Help: "Total accepted per-service health transitions by resulting status.",
	}, []string{"status"})

	// HealthAlertsSuppressed counts alert-worthy health transitions whose
	// alert the per-service cooldown held back, by target status. See the
	// ADR-0031 amendment.
	HealthAlertsSuppressed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_health_alerts_suppressed_total",
		Help: "Total health alerts suppressed by the alert cooldown, by target status.",
	}, []string{"status"})

	// HealthAlertsSent counts outbound health alert deliveries by target format
	// and outcome ("ok" for a 2xx response, "error" otherwise), mirroring
	// NotificationsSent. See ADR-0031.
	HealthAlertsSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_health_alerts_sent_total",
		Help: "Total outbound health alert deliveries by format and outcome (ok|error).",
	}, []string{"format", "outcome"})

	// UpdateAlertsSent counts outbound update notifications (ADR-0054) by
	// target format and outcome, mirroring NotificationsSent.
	UpdateAlertsSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "skipper_update_alerts_sent_total",
		Help: "Total outbound image-update notifications by format and outcome (ok|error).",
	}, []string{"format", "outcome"})

	// UpdatesAvailable gauges how many services currently have an available
	// image update, as of the last registry update check (ADR-0054).
	UpdatesAvailable = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "skipper_updates_available",
		Help: "Services with an available image update as of the last registry check.",
	})
)
