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
// The response is sent immediately with HTTP 202 Accepted; the deploy runs
// in a goroutine so Gitea does not time out waiting for it to complete.
func Handler(cfg *config.Config, deployer *deploy.Deployer) http.HandlerFunc {
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
			signature := r.Header.Get("X-Gitea-Signature")
			if err := verifyHMACSignature(body, signature, cfg.WebhookSecret); err != nil {
				slog.Warn("webhook rejected: invalid signature", "err", err)
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		metrics.WebhooksReceived.Inc()
		slog.Info("webhook accepted, starting deploy in background")

		go func() {
			if err := pullLatestCommits(cfg.RepoPath); err != nil {
				slog.Error("git pull failed, aborting deploy", "err", err)
				return
			}
			deployer.DeployAllStacks(cfg)
		}()

		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, "deploy triggered")
	}
}

func verifyHMACSignature(body []byte, signature, secret string) error {
	signer := hmac.New(sha256.New, []byte(secret))
	signer.Write(body)
	expected := "sha256=" + hex.EncodeToString(signer.Sum(nil))

	// hmac.Equal uses constant-time comparison to prevent timing attacks.
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func pullLatestCommits(repoPath string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull: %w\n%s", err, output)
	}
	slog.Info("git pull successful", "output", string(output))
	return nil
}
