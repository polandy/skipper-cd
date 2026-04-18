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

	// Deploy on startup to catch changes that occurred while orpheus-cd was not running.
	go deployer.DeployAllStacks(cfg)

	startMetricsServer(cfg.MetricsPort)
	startWebhookServer(cfg, deployer)
}

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

func startWebhookServer(cfg *config.Config, deployer *deploy.Deployer) {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", webhook.Handler(cfg, deployer))
	mux.HandleFunc("/healthz", respondOK)

	addr := fmt.Sprintf(":%d", cfg.Port)
	slog.Info("webhook server listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("webhook server stopped", "err", err)
		os.Exit(1)
	}
}

func respondOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}
