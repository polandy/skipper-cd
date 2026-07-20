// Package webhook provides an HTTP handler that receives push events,
// validates their HMAC-SHA256 signature, and triggers a deployment.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/safego"
)

// MaxBodyBytes caps the webhook request body size. Push payloads are small,
// so anything larger is rejected with 413.
const MaxBodyBytes = 1 << 20 // 1 MiB

// Reasons a webhook is rejected, reported on metrics.WebhooksRejected.
const (
	rejectReasonTooLarge  = "too_large"
	rejectReasonSignature = "signature"
	rejectReasonDisabled  = "disabled"
)

// deployTrigger starts a full sync+deploy run. It is implemented by
// *deploy.Deployer and faked in tests.
type deployTrigger interface {
	SyncAndDeployAll(ctx context.Context, cfg *config.Config)
}

// Handler returns an http.HandlerFunc that processes incoming webhooks.
// It supports signature validation for Gitea (X-Gitea-Signature) and
// GitHub/Forgejo (X-Hub-Signature-256). Pushes to branches other than the
// configured one are acknowledged but ignored. The response is sent
// immediately with HTTP 202 Accepted; the deploy runs in a goroutine so the
// caller does not time out waiting for it to complete.
// Method enforcement is left to the mux route pattern ("POST /webhook").
func Handler(cfg *config.Config, deployer deployTrigger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Without a secret the webhook is disabled: accepting unsigned pushes
		// would let anyone who can reach this port trigger a deploy. Deploys
		// still happen via the reconcile loop (ADR-0028); set webhook_secret to
		// enable push-triggered deploys.
		if cfg.WebhookSecret == "" {
			metrics.WebhooksRejected.WithLabelValues(rejectReasonDisabled).Inc()
			http.Error(w, "webhook disabled: set webhook_secret to enable push-triggered deploys", http.StatusForbidden)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				metrics.WebhooksRejected.WithLabelValues(rejectReasonTooLarge).Inc()
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}

		signature := extractSignature(r)
		if err := verifyHMACSignature(body, signature, cfg.WebhookSecret); err != nil {
			metrics.WebhooksRejected.WithLabelValues(rejectReasonSignature).Inc()
			slog.Warn("webhook rejected: invalid signature", "err", err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		metrics.WebhooksReceived.Inc()

		if ref, ok := pushRef(body); ok && !refMatchesBranch(ref, cfg.Branch) {
			slog.Info("webhook ignored, push is for a different branch", "ref", ref, "branch", cfg.Branch)
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "ignoring push to %s\n", ref)
			return
		}

		slog.Info("webhook accepted, starting deploy in background")

		// No run-wide deadline: each shell command is bounded individually
		// by the deployer's per-command timeout.
		safego.Go("webhook-deploy", func() { deployer.SyncAndDeployAll(context.Background(), cfg) })

		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintln(w, "deploy triggered")
	}
}

// pushRef extracts the git ref (e.g. "refs/heads/main") from a push payload.
// Returns ok=false when the body is not JSON or carries no ref — such
// requests (manual curl, other event types) trigger a deploy to be safe.
func pushRef(body []byte) (string, bool) {
	var payload struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Ref == "" {
		return "", false
	}
	return payload.Ref, true
}

// refMatchesBranch reports whether ref points at the configured branch.
// An empty configured branch disables filtering.
func refMatchesBranch(ref, branch string) bool {
	return branch == "" || ref == "refs/heads/"+branch
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
