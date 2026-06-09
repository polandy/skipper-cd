// skipper-cd is a lightweight Docker Compose CD tool.
// It listens for push webhooks, pulls the repository, and deploys
// changed Docker Compose stacks. Unchanged stacks are skipped via hash tracking.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/ui"
	"github.com/polandy/skipper-cd/internal/webhook"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	configPath := flag.String("config", "/etc/skipper/skipper.yml", "path to the skipper.yml config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	timeout := time.Duration(cfg.CommandTimeoutSeconds) * time.Second

	slog.Info("skipper-cd starting",
		"stacks", len(cfg.Stacks),
		"webhook_port", cfg.Port,
		"metrics_port", cfg.MetricsPort,
		"branch", cfg.Branch,
		"command_timeout", timeout,
	)

	repoSync := git.NewRepoSync(cfg.RepoURL, cfg.RepoDir, cfg.Branch)
	repoReader := git.NewRepoReader(repoSync.RepoDir())
	stateDir := filepath.Dir(repoSync.RepoDir())
	deployer := deploy.NewDeployerWithCommitReader(repoReader, repoSync, repoSync.RepoDir(), stateDir, timeout)

	var (
		broadcaster *events.Broadcaster
		history     *events.History
	)
	if cfg.UIEnabled {
		history = events.NewHistory(stateDir)
		broadcaster = events.NewBroadcaster()
		deployer.InitEventID(history.MaxEventID())
		deployer.SetEventSink(func(e events.DeployEvent) {
			history.Add(e)
			broadcaster.Publish(e)
		})
		slog.Info("web UI enabled")
	}

	// Sync repo and deploy on startup to catch changes that occurred while skipper-cd was not running.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		deployer.SyncAndDeployAll(ctx, cfg)
	}()

	startMetricsServer(cfg.MetricsPort)
	startWebhookServer(cfg, deployer, timeout, broadcaster, history)
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

func startWebhookServer(cfg *config.Config, deployer *deploy.Deployer, timeout time.Duration, broadcaster *events.Broadcaster, history *events.History) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", webhook.Handler(cfg, deployer, timeout))
	mux.HandleFunc("GET /healthz", respondOK)

	if broadcaster != nil {
		mux.Handle("GET /{$}", ui.IndexHandler())
		mux.Handle("GET /api/events", ui.SSEHandler(broadcaster, history))
	}

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
