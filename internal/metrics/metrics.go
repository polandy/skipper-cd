package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WebhooksReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orpheus_webhooks_received_total",
		Help: "Total number of webhooks received",
	})

	DeploysTriggered = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orpheus_deploys_triggered_total",
		Help: "Total number of deploys triggered per stack",
	}, []string{"stack"})

	DeploysSkipped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orpheus_deploys_skipped_total",
		Help: "Total number of deploys skipped (no changes) per stack",
	}, []string{"stack"})

	DeployErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orpheus_deploy_errors_total",
		Help: "Total number of deploy errors per stack",
	}, []string{"stack"})

	LastDeployTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orpheus_last_deploy_timestamp_seconds",
		Help: "Unix timestamp of the last successful deploy per stack",
	}, []string{"stack"})
)
