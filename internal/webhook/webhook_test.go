package webhook_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/polandy/orpheus-cd/internal/config"
	"github.com/polandy/orpheus-cd/internal/deploy"
	"github.com/polandy/orpheus-cd/internal/webhook"
)

// newTestConfig returns a minimal config suitable for webhook tests.
// It points repo_path to a temp dir so git pull does not run against a real repo.
func newTestConfig(t *testing.T, secret string) *config.Config {
	t.Helper()
	return &config.Config{
		RepoPath:      t.TempDir(),
		WebhookSecret: secret,
		Stacks:        []config.Stack{},
	}
}

func TestHandler_RejectsNonPostRequests(t *testing.T) {
	cfg := newTestConfig(t, "")
	handler := webhook.Handler(cfg, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandler_AcceptsRequestWithoutSecret(t *testing.T) {
	cfg := newTestConfig(t, "") // no secret configured
	handler := webhook.Handler(cfg, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
	rec := httptest.NewRecorder()
	handler(rec, req)

	// 202 Accepted because the deploy runs in the background.
	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestHandler_AcceptsRequestWithValidSignature(t *testing.T) {
	secret := "supersecret"
	body := []byte(`{"ref":"refs/heads/master"}`)

	cfg := newTestConfig(t, secret)
	handler := webhook.Handler(cfg, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBuffer(body))
	req.Header.Set("X-Gitea-Signature", computeSignature(body, secret))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestHandler_RejectsRequestWithInvalidSignature(t *testing.T) {
	cfg := newTestConfig(t, "supersecret")
	handler := webhook.Handler(cfg, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
	req.Header.Set("X-Gitea-Signature", "sha256=invalidsignature")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandler_RejectsMissingSignatureWhenSecretIsConfigured(t *testing.T) {
	cfg := newTestConfig(t, "supersecret")
	handler := webhook.Handler(cfg, deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
	// No X-Gitea-Signature header set
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// computeSignature produces the HMAC-SHA256 signature that Gitea would send.
func computeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
