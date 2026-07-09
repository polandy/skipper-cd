// Package webhook provides an HTTP handler that receives push events,
// validates their HMAC-SHA256 signature, and triggers a deployment.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// MaxBodyBytes caps the webhook request body size. Push payloads are small,
// so anything larger is rejected with 413.
const MaxBodyBytes = 1 << 20 // 1 MiB

// Handler returns an http.HandlerFunc that processes incoming webhooks.
// It supports signature validation for Gitea (X-Gitea-Signature) and
// GitHub/Forgejo (X-Hub-Signature-256). The response is sent immediately
// with HTTP 202 Accepted; the deploy runs in a goroutine so the caller
// does not time out waiting for it to complete.
// Method enforcement is left to the mux route pattern ("POST /webhook").
func Handler(cfg *config.Config, deployer *deploy.Deployer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}

		if cfg.WebhookSecret != "" {
			signature := extractSignature(r)
			if err := verifyHMACSignature(body, signature, cfg.WebhookSecret); err != nil {
				slog.Warn("webhook rejected: invalid signature", "err", err)
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		metrics.WebhooksReceived.Inc()
		slog.Info("webhook accepted, starting deploy in background")

		// No run-wide deadline: each shell command is bounded individually
		// by the deployer's per-command timeout.
		go deployer.SyncAndDeployAll(context.Background(), cfg)

		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintln(w, "deploy triggered")
	}
}

// extractSignature returns the HMAC-SHA256 hex signature from the request.
// It checks X-Gitea-Signature first (plain hex), then X-Hub-Signature-256
// (GitHub/Forgejo format: "sha256=<hex>").
func extractSignature(r *http.Request) string {
	if sig := r.Header.Get("X-Gitea-Signature"); sig != "" {
		return sig
	}
	if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
		return strings.TrimPrefix(sig, "sha256=")
	}
	return ""
}

func verifyHMACSignature(body []byte, signature, secret string) error {
	signer := hmac.New(sha256.New, []byte(secret))
	signer.Write(body)
	expected := hex.EncodeToString(signer.Sum(nil))

	// hmac.Equal uses constant-time comparison to prevent timing attacks.
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
