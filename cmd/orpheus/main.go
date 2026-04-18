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
	configPath := flag.String("config", "/etc/orpheus/orpheus.yml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	slog.Info("orpheus-cd starting",
		"stacks", len(cfg.Stacks),
		"port", cfg.Port,
		"metrics_port", cfg.MetricsPort,
	)

	// Run initial deploy on startup
	go func() {
		slog.Info("running initial deploy")
		deploy.RunAll(cfg)
	}()

	// Metrics server
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		addr := fmt.Sprintf(":%d", cfg.MetricsPort)
		slog.Info("metrics server listening", "addr", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("metrics server error", "err", err)
			os.Exit(1)
		}
	}()

	// Webhook server
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", webhook.Handler(cfg))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	addr := fmt.Sprintf(":%d", cfg.Port)
	slog.Info("webhook server listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("webhook server error", "err", err)
		os.Exit(1)
	}
}
