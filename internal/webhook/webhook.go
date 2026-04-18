// Package webhook provides an HTTP handler that receives Gitea push events,
// validates their HMAC-SHA256 signature, and triggers a deployment.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"

	"github.com/polandy/orpheus-cd/internal/config"
	"github.com/polandy/orpheus-cd/internal/deploy"
	"github.com/polandy/orpheus-cd/internal/metrics"
)

// Handler returns an http.HandlerFunc that processes incoming Gitea webhooks.
// It validates the request signature (when a secret is configured), then
// performs a git pull and triggers a full deploy run in the background.
//
// The response is sent immediately with HTTP 202 Accepted so that Gitea does
// not time out waiting for the (potentially slow) deploy to finish.
func Handler(cfg *config.Config, deployer *deploy.Deployer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read the full body upfront — it is needed for signature validation.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}

		if cfg.WebhookSecret != "" {
			signature := r.Header.Get("X-Gitea-Signature")
			if err := validateSignature(body, signature, cfg.WebhookSecret); err != nil {
				slog.Warn("webhook rejected: invalid signature", "err", err)
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		metrics.WebhooksReceived.Inc()
		slog.Info("webhook accepted, starting deploy in background")

		// Run the deploy in a goroutine (Go's equivalent of a background thread)
		// so the HTTP response is returned immediately.
		go func() {
			if err := gitPull(cfg.RepoPath); err != nil {
				slog.Error("git pull failed, aborting deploy", "err", err)
				return
			}
			deployer.RunAll(cfg)
		}()

		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, "deploy triggered")
	}
}

// validateSignature checks that the given signature matches the HMAC-SHA256
// of the request body using the shared secret. Gitea sends the signature in
// the X-Gitea-Signature header as "sha256=<hex>".
func validateSignature(body []byte, signature, secret string) error {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// hmac.Equal uses a constant-time comparison to prevent timing attacks.
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// gitPull runs "git pull" in the repository directory to fetch the latest
// configuration before deploying.
func gitPull(repoPath string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = repoPath

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull: %w\n%s", err, out)
	}
	slog.Info("git pull successful", "output", string(out))
	return nil
}
