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

func Handler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if cfg.WebhookSecret != "" {
			if err := validateSignature(body, r.Header.Get("X-Gitea-Signature"), cfg.WebhookSecret); err != nil {
				slog.Warn("webhook signature validation failed", "err", err)
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		metrics.WebhooksReceived.Inc()
		slog.Info("webhook received, triggering deploy")

		go func() {
			if err := gitPull(cfg.RepoPath); err != nil {
				slog.Error("git pull failed", "err", err)
				return
			}
			deploy.RunAll(cfg)
		}()

		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, "deploy triggered")
	}
}

func validateSignature(body []byte, signature, secret string) error {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func gitPull(repoPath string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull: %w\n%s", err, out)
	}
	slog.Info("git pull complete", "output", string(out))
	return nil
}
