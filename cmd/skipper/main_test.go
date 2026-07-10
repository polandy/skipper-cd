package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
)

func TestNewLogHandler_JSONFormatEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(config.LogFormatJSON, &buf))

	logger.Info("hello", "stack", "gitea")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected JSON log output, got %q: %v", buf.String(), err)
	}
	if entry["msg"] != "hello" {
		t.Errorf("expected msg 'hello', got %v", entry["msg"])
	}
	if entry["stack"] != "gitea" {
		t.Errorf("expected stack 'gitea', got %v", entry["stack"])
	}
}

func TestNewLogHandler_TextFormatEmitsLogfmt(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(config.LogFormatText, &buf))

	logger.Info("hello", "stack", "gitea")

	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected text output, got JSON: %q", out)
	}
	if !strings.Contains(out, "msg=hello") || !strings.Contains(out, "stack=gitea") {
		t.Errorf("expected logfmt keys in output, got %q", out)
	}
}

func TestHealthzHandler_OKWhileNoSyncRan(t *testing.T) {
	deployer := deploy.NewDeployer()

	rec := httptest.NewRecorder()
	healthzHandler(deployer)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// failingSyncer always fails, simulating an unreachable git remote.
type failingSyncer struct{}

func (failingSyncer) Sync(context.Context) error { return errors.New("remote unreachable") }

func TestHealthzHandler_ServiceUnavailableAfterFailedSync(t *testing.T) {
	deployer := deploy.NewDeployerWithCommitReader(nil, failingSyncer{}, "", t.TempDir(), 0, nil)
	deployer.SyncAndDeployAll(context.Background(), &config.Config{})

	rec := httptest.NewRecorder()
	healthzHandler(deployer)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "remote unreachable") {
		t.Errorf("expected sync error in body, got %q", rec.Body.String())
	}
}
