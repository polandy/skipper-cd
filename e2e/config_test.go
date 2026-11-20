//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// statusForHost returns the status of method path with the Host header set to host.
func (s *skipper) statusForHost(method, path, host string, body []byte) int {
	s.t.Helper()
	req, err := http.NewRequest(method, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		s.t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Host = host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// P32 — allowed_hosts guards the UI by Host header, while /webhook and
// /healthz answer any Host (ADR-0067).
func TestE2E_AllowedHostsGuardUIButNotWebhookOrHealthz(t *testing.T) {
	s := startSkipperOpts(t, nil, "allowed_hosts: [\"skipper.example\"]\n", "web")

	if got := s.statusForHost(http.MethodGet, "/", "skipper.example", nil); got != http.StatusOK {
		t.Errorf("UI with an allowed Host = %d, want 200", got)
	}
	if got := s.statusForHost(http.MethodGet, "/", "evil.example", nil); got != http.StatusMisdirectedRequest {
		t.Errorf("UI with a foreign Host = %d, want 421", got)
	}
	if got := s.statusForHost(http.MethodGet, "/api/audit", "evil.example", nil); got != http.StatusMisdirectedRequest {
		t.Errorf("API with a foreign Host = %d, want 421", got)
	}
	if got := s.statusForHost(http.MethodGet, "/healthz", "evil.example", nil); got != http.StatusOK {
		t.Errorf("/healthz with a foreign Host = %d, want 200", got)
	}
	// An unsigned push reaches the handler (401), not the Host guard (421).
	if got := s.statusForHost(http.MethodPost, "/webhook", "evil.example", []byte(`{}`)); got != http.StatusUnauthorized {
		t.Errorf("/webhook with a foreign Host = %d, want 401 from the webhook handler", got)
	}
}

// P33 — An unknown config key fails startup with an error naming the key
// instead of being ignored (ADR-0043).
func TestE2E_UnknownConfigKeyFailsStartup(t *testing.T) {
	s := newSkipper(t, nil, "no_such_setting: true\n", []string{"web"})
	requireGit(t)
	s.initOrigin()
	cfgPath := s.writeConfig(s.base)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, skipperBin, "-config", cfgPath).CombinedOutput()

	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("skipper with an unknown key: err = %v, want exit status 1\n%s", err, out)
	}
	if !strings.Contains(string(out), "no_such_setting") {
		t.Errorf("startup error does not name the key:\n%s", out)
	}
}
