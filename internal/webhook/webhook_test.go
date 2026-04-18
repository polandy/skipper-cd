package webhook_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/webhook"
)

type noopSyncer struct{}

func (noopSyncer) Sync() error { return nil }

type failingSyncer struct{}

func (failingSyncer) Sync() error { return fmt.Errorf("simulated sync failure") }

func newTestConfig(t *testing.T, secret string) *config.Config {
	t.Helper()
	return &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		WebhookSecret: secret,
		Stacks:        []config.Stack{},
	}
}

func TestHandler_RejectsNonPostRequests(t *testing.T) {
	handler := webhook.Handler(newTestConfig(t, ""), noopSyncer{}, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandler_AcceptsRequestWithoutSecret(t *testing.T) {
	handler := webhook.Handler(newTestConfig(t, ""), noopSyncer{}, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestHandler_AcceptsRequestWithValidSignature(t *testing.T) {
	secret := "supersecret"
	body := []byte(`{"ref":"refs/heads/master"}`)

	handler := webhook.Handler(newTestConfig(t, secret), noopSyncer{}, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBuffer(body))
	req.Header.Set("X-Gitea-Signature", computeSignature(body, secret))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestHandler_RejectsRequestWithInvalidSignature(t *testing.T) {
	handler := webhook.Handler(newTestConfig(t, "supersecret"), noopSyncer{}, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
	req.Header.Set("X-Gitea-Signature", "invalidsignature")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandler_RejectsMissingSignatureWhenSecretIsConfigured(t *testing.T) {
	handler := webhook.Handler(newTestConfig(t, "supersecret"), noopSyncer{}, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func computeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
