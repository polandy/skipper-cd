package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/webhook"
)

func newTestConfig(t *testing.T, secret string) *config.Config {
	t.Helper()
	return &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		WebhookSecret: secret,
		Stacks:        []config.Stack{},
	}
}

func TestHandler_RejectsNonPostRequests(t *testing.T) {
	// Method enforcement happens at the mux level ("POST /webhook"),
	// mirroring the production route registration in main.go.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", webhook.Handler(newTestConfig(t, ""), deploy.NewDeployer()))

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandler_RejectsOversizedBody(t *testing.T) {
	handler := webhook.Handler(newTestConfig(t, ""), deploy.NewDeployer())

	body := bytes.NewReader(make([]byte, webhook.MaxBodyBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rec.Code)
	}
}

func TestHandler_AcceptsRequestWithoutSecret(t *testing.T) {
	handler := webhook.Handler(newTestConfig(t, ""), deploy.NewDeployer())

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

	handler := webhook.Handler(newTestConfig(t, secret), deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBuffer(body))
	req.Header.Set("X-Gitea-Signature", computeSignature(body, secret))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestHandler_AcceptsRequestWithHubSignature256(t *testing.T) {
	secret := "supersecret"
	body := []byte(`{"ref":"refs/heads/master"}`)

	handler := webhook.Handler(newTestConfig(t, secret), deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBuffer(body))
	req.Header.Set("X-Hub-Signature-256", "sha256="+computeSignature(body, secret))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestHandler_RejectsRequestWithInvalidSignature(t *testing.T) {
	handler := webhook.Handler(newTestConfig(t, "supersecret"), deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
	req.Header.Set("X-Gitea-Signature", "invalidsignature")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandler_RejectsMissingSignatureWhenSecretIsConfigured(t *testing.T) {
	handler := webhook.Handler(newTestConfig(t, "supersecret"), deploy.NewDeployer())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// fakeTrigger records SyncAndDeployAll invocations; the channel lets tests
// wait for the deploy goroutine.
type fakeTrigger struct {
	called chan struct{}
}

func newFakeTrigger() *fakeTrigger {
	return &fakeTrigger{called: make(chan struct{}, 1)}
}

func (f *fakeTrigger) SyncAndDeployAll(_ context.Context, _ *config.Config) {
	select {
	case f.called <- struct{}{}:
	default:
	}
}

func (f *fakeTrigger) assertTriggered(t *testing.T) {
	t.Helper()
	select {
	case <-f.called:
	case <-time.After(2 * time.Second):
		t.Fatal("expected deploy to be triggered")
	}
}

func (f *fakeTrigger) assertNotTriggered(t *testing.T) {
	t.Helper()
	select {
	case <-f.called:
		t.Fatal("expected deploy NOT to be triggered")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHandler_TriggersDeployForConfiguredBranch(t *testing.T) {
	cfg := newTestConfig(t, "")
	cfg.Branch = "main"
	trigger := newFakeTrigger()
	handler := webhook.Handler(cfg, trigger)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString(`{"ref":"refs/heads/main"}`))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
	trigger.assertTriggered(t)
}

func TestHandler_IgnoresPushToOtherBranch(t *testing.T) {
	cfg := newTestConfig(t, "")
	cfg.Branch = "main"
	trigger := newFakeTrigger()
	handler := webhook.Handler(cfg, trigger)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString(`{"ref":"refs/heads/feature/foo"}`))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for ignored branch, got %d", rec.Code)
	}
	trigger.assertNotTriggered(t)
}

func TestHandler_TriggersDeployWhenPayloadHasNoRef(t *testing.T) {
	// Manual curl or non-push payloads carry no ref — deploy to be safe.
	cfg := newTestConfig(t, "")
	cfg.Branch = "main"
	trigger := newFakeTrigger()
	handler := webhook.Handler(cfg, trigger)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
	trigger.assertTriggered(t)
}

func TestHandler_TriggersDeployWhenBodyIsNotJSON(t *testing.T) {
	cfg := newTestConfig(t, "")
	cfg.Branch = "main"
	trigger := newFakeTrigger()
	handler := webhook.Handler(cfg, trigger)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
	trigger.assertTriggered(t)
}

func computeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
