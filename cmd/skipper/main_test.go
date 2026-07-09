package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
)

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
	deployer := deploy.NewDeployerWithCommitReader(nil, failingSyncer{}, "", t.TempDir(), 0)
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
