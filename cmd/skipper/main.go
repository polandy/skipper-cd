// skipper-cd is a lightweight Docker Compose CD tool.
// It listens for push webhooks, pulls the repository, and deploys
// changed Docker Compose stacks. Unchanged stacks are skipped via hash tracking.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/ui"
	"github.com/polandy/skipper-cd/internal/webhook"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// readHeaderTimeout bounds how long a client may take to send request
// headers, protecting the servers against slowloris-style connections.
const readHeaderTimeout = 10 * time.Second

// shutdownTimeout bounds how long in-flight HTTP requests (e.g. open SSE
// streams) may delay shutdown after a termination signal.
const shutdownTimeout = 10 * time.Second

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
	deployer := deploy.NewDeployerWithCommitReader(repoReader, repoSync, repoSync.RepoDir(), stateDir)

	var (
		broadcaster *events.Broadcaster
		history     *events.History
	)
	if cfg.UIEnabled {
		history = events.NewHistory(stateDir)
		broadcaster = events.NewBroadcaster()
		deployer.InitEventID(history.MaxEventID())
		deployer.SetEventSink(func(e events.DeployEvent) {
			if e.Status != events.StatusSkipped {
				history.Add(e)
			}
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

	startServer("metrics", cfg.MetricsPort, metricsMux())
	webhookServer := startServer("webhook", cfg.Port, webhookMux(cfg, deployer, timeout, broadcaster, history))

	// Block until SIGINT/SIGTERM, then shut down gracefully: stop accepting
	// requests, then let an in-flight deploy finish so docker compose is not
	// interrupted mid-run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := webhookServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("webhook server shutdown", "err", err)
	}

	slog.Info("waiting for in-flight deploy to finish")
	deployer.WaitIdle()
	slog.Info("skipper-cd stopped")
}

func metricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

func webhookMux(cfg *config.Config, deployer *deploy.Deployer, timeout time.Duration, broadcaster *events.Broadcaster, history *events.History) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", webhook.Handler(cfg, deployer, timeout))
	mux.HandleFunc("GET /healthz", respondOK)

	if broadcaster != nil {
		mux.Handle("GET /{$}", ui.IndexHandler())
		mux.Handle("GET /api/events", ui.SSEHandler(broadcaster, history))
		mux.Handle("GET /api/events/{id}/diffs", ui.DiffHandler(history))
	}
	return mux
}

// startServer runs an HTTP server in a goroutine and returns it so the
// caller can shut it down. An immediate listen failure exits the process.
func startServer(name string, port int, mux *http.ServeMux) *http.Server {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() {
		slog.Info(name+" server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error(name+" server stopped", "err", err)
			os.Exit(1)
		}
	}()
	return srv
}

func respondOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "ok")
}
