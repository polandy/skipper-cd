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
)
