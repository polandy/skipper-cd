//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// countStatus returns how many recorded payloads carry the given deploy status.
func (r *alertRecorder) countStatus(status string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, a := range r.alerts {
		if a["status"] == status {
			n++
		}
	}
	return n
}

// lastStatus returns the newest recorded payload with the given deploy status, or nil.
func (r *alertRecorder) lastStatus(status string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.alerts) - 1; i >= 0; i-- {
		if r.alerts[i]["status"] == status {
			return r.alerts[i]
		}
	}
	return nil
}

// startNotified starts "web" with a generic notification target at the
// recorder, waits for the startup deploy's success notification, and pushes an
// image bump.
func startNotified(t *testing.T, stubEnv map[string]string, stackCfg string) (*skipper, *alertRecorder) {
	t.Helper()
	rec := newAlertRecorder(t)
	extraCfg := fmt.Sprintf("notifications:\n  - format: generic\n    url: %q\n", rec.server.URL)
	s := newSkipper(t, stubEnv, extraCfg, []string{"web"})
	if stackCfg != "" {
		s.stackCfg = map[string]string{"web": stackCfg}
	}
	s.bootstrap(t)
	s.waitFor("startup success notification", func() bool { return rec.countStatus("success") == 1 })

	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	return s, rec
}

// P27 — A successful deploy is POSTed to the generic target with the event
// payload (ADR-0020).
func TestE2E_NotifiesSuccessToGenericTarget(t *testing.T) {
	s, rec := startNotified(t, nil, "")
	s.waitFor("success notification for the pushed change", func() bool { return rec.countStatus("success") == 2 })

	if a := rec.lastStatus("success"); a["stack"] != "web" {
		t.Errorf("unexpected success payload: %v", a)
	}
}

// P28 — A failed deploy that is not rolled back (`rollback: false`) is POSTed
// as `failed` and carries the error.
func TestE2E_NotifiesFailureToGenericTarget(t *testing.T) {
	s, rec := startNotified(t, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "2"}, "    rollback: false\n")
	s.waitFor("failed notification", func() bool { return rec.countStatus("failed") == 1 })

	a := rec.lastStatus("failed")
	if a["stack"] != "web" || a["error"] == nil || a["error"] == "" {
		t.Errorf("unexpected failed payload: %v", a)
	}
	if n := rec.countStatus("rolled_back"); n != 0 {
		t.Errorf("rollback: false must not notify rolled_back, got %d", n)
	}
}

// P29 — A rolled-back deploy is POSTed as `rolled_back`, never as `failed`.
func TestE2E_NotifiesRollbackToGenericTarget(t *testing.T) {
	s, rec := startNotified(t, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "2"}, "")
	s.waitFor("rolled_back notification", func() bool { return rec.countStatus("rolled_back") == 1 })

	if a := rec.lastStatus("rolled_back"); a["stack"] != "web" {
		t.Errorf("unexpected rolled_back payload: %v", a)
	}
	if n := rec.countStatus("failed"); n != 0 {
		t.Errorf("a rollback must not also notify failed, got %d", n)
	}
	if n := rec.countStatus("success"); n != 1 {
		t.Errorf("success notifications = %d, want only the startup one", n)
	}
}
