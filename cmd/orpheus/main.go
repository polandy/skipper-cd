// orpheus-cd is a lightweight, Sablier-aware Docker Compose CD tool.
// It listens for Gitea push webhooks, pulls the repository, and deploys
// changed Docker Compose stacks. Unchanged stacks are skipped via hash tracking.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/polandy/orpheus-cd/internal/config"
	"github.com/polandy/orpheus-cd/internal/deploy"
	"github.com/polandy/orpheus-cd/internal/webhook"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	configPath := flag.String("config", "/etc/orpheus/orpheus.yml", "path to the orpheus.yml config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	slog.Info("orpheus-cd starting",
		"stacks", len(cfg.Stacks),
		"webhook_port", cfg.Port,
		"metrics_port", cfg.MetricsPort,
	)

	deployer := deploy.NewDeployer()

	// Deploy all stacks once on startup to catch any changes made while
	// orpheus-cd was not running (e.g. after a NixOS rebuild).
	go func() {
		slog.Info("running initial deploy on startup")
		deployer.RunAll(cfg)
	}()

	startMetricsServer(cfg.MetricsPort)
	startWebhookServer(cfg, deployer) // blocks until the process exits
}

// startMetricsServer starts the Prometheus metrics endpoint in the background.
func startMetricsServer(port int) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	addr := fmt.Sprintf(":%d", port)
	go func() {
		slog.Info("metrics server listening", "addr", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("metrics server stopped", "err", err)
			os.Exit(1)
		}
	}()
}

// startWebhookServer starts the webhook HTTP server and blocks.
func startWebhookServer(cfg *config.Config, deployer *deploy.Deployer) {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", webhook.Handler(cfg, deployer))
	mux.HandleFunc("/healthz", healthHandler)

	addr := fmt.Sprintf(":%d", cfg.Port)
	slog.Info("webhook server listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("webhook server stopped", "err", err)
		os.Exit(1)
	}
}

// healthHandler responds with 200 OK. Used by Docker and load balancers
// to verify the process is alive.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}
