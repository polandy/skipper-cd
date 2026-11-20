//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// startUpdateCheck runs "web" on a loopback registry image 1.0 whose registry
// also lists 1.1, with a generic notification target and the given
// update_check body.
func startUpdateCheck(t *testing.T, updateCfg string) (*skipper, *alertRecorder) {
	t.Helper()
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/app/tags/list" {
			fmt.Fprint(w, `{"name":"app","tags":["1.0","1.1"]}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(reg.Close)
	host := strings.TrimPrefix(reg.URL, "http://")

	rec := newAlertRecorder(t)
	extraCfg := fmt.Sprintf("notifications:\n  - format: generic\n    url: %q\n", rec.server.URL)
	// The stub's scripted `compose ps` (UI branch) is what reports the running image.
	psDir := t.TempDir()
	image := host + "/app:1.0"
	writeFile(t, filepath.Join(psDir, "web.json"), fmt.Sprintf("{\"Service\":\"app\",\"Name\":\"web-app-1\",\"Image\":%q}\n", image))
	s := newSkipper(t, map[string]string{"STUB_DOCKER_UI": "1", "STUB_DOCKER_PS_DIR": psDir}, extraCfg, []string{"web"})
	s.updateCfg = updateCfg
	s.seedFiles = map[string]string{"web/docker-compose.yml": "services:\n  app:\n    image: " + image + "\n"}
	s.bootstrap(t)
	return s, rec
}

// updatePayloads returns the recorded alerts of type "update".
func (r *alertRecorder) updatePayloads() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, a := range r.alerts {
		if a["type"] == "update" {
			out = append(out, a)
		}
	}
	return out
}

// P30 — With `update_check.notify: true` a newer registry tag is POSTed once to
// the generic target as an `update` payload (ADR-0054).
func TestE2E_UpdateCheckNotifiesNewerTag(t *testing.T) {
	s, rec := startUpdateCheck(t, "  interval_seconds: 3600\n  notify: true\n")
	s.waitFor("update notification", func() bool { return len(rec.updatePayloads()) == 1 })

	a := rec.updatePayloads()[0]
	if a["stack"] != "web" || a["service"] != "app" || a["latest"] != "1.1" || a["rebuilt"] != false {
		t.Errorf("unexpected update payload: %v", a)
	}
}

// P31 — Without `notify` (default false) the update is only a marker: the
// check ran (gauge), but nothing is sent.
func TestE2E_UpdateCheckDoesNotNotifyByDefault(t *testing.T) {
	s, rec := startUpdateCheck(t, "  interval_seconds: 3600\n")
	s.waitFor("update counted by the check", func() bool {
		v, ok := metricValue(s.metricsBody(), "skipper_updates_available")
		return ok && v == 1
	})

	if n := len(rec.updatePayloads()); n != 0 {
		t.Errorf("update notifications = %d, want 0 with notify unset", n)
	}
}
