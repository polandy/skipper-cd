// Package webhook provides an HTTP handler that receives push events,
// validates their HMAC-SHA256 signature, and triggers a deployment.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// Handler returns an http.HandlerFunc that processes incoming webhooks.
// It supports signature validation for Gitea (X-Gitea-Signature) and
// GitHub/Forgejo (X-Hub-Signature-256). The response is sent immediately
// with HTTP 202 Accepted; the deploy runs in a goroutine so the caller
// does not time out waiting for it to complete.
func Handler(cfg *config.Config, deployer *deploy.Deployer, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
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

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			deployer.SyncAndDeployAll(ctx, cfg)
		}()

		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, "deploy triggered")
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
