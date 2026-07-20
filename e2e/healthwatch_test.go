//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// alertRecorder is a local generic health_watch target capturing every POSTed
// alert payload.
type alertRecorder struct {
	mu     sync.Mutex
	alerts []map[string]any
	server *httptest.Server
}

func newAlertRecorder(t *testing.T) *alertRecorder {
	t.Helper()
	r := &alertRecorder{}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var m map[string]any
		if err := json.NewDecoder(req.Body).Decode(&m); err == nil {
			r.mu.Lock()
			r.alerts = append(r.alerts, m)
			r.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *alertRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.alerts)
}

// firstTo returns the first recorded alert whose "to" field matches, or nil.
func (r *alertRecorder) firstTo(status string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.alerts {
		if a["to"] == status {
			return a
		}
	}
	return nil
}

const (
	psHealthy   = `{"Service":"app","State":"running","Health":"healthy"}`
	psUnhealthy = `{"Service":"app","State":"running","Health":"unhealthy"}`
)

// P10 — Health watch journey: with a health_watch block configured, a service
// turning unhealthy in `compose ps` output produces a health alert POST at the
// generic target (deploy-correlated via the startup deploy), its recovery
// produces the all-clear, and the phase history is persisted in
// healthwatch.yaml. The baseline observation never alerts.
func TestP10_HealthWatchAlertsOnUnhealthyAndRecovery(t *testing.T) {
	rec := newAlertRecorder(t)

	psFile := filepath.Join(t.TempDir(), "ps.json")
	writeFile(t, psFile, psHealthy)

	// The watchdog rides the shared health poller (ADR-0031), so the test sets
	// the poll cadence itself to 1s; enabling health_watch makes that poll run
	// headless (no SSE client is connected in this suite).
	extraCfg := fmt.Sprintf(`runtime_health_poll_interval_seconds: 1
health_watch:
  debounce_polls: 1
  targets:
    - format: generic
      url: %q
`, rec.server.URL)

	s := startSkipperOpts(t, map[string]string{"STUB_DOCKER_PS_FILE": psFile}, extraCfg, "web")

	statePath := filepath.Join(s.stateDir, "healthwatch.yaml")
	s.waitFor("healthwatch baseline persisted", func() bool {
		data, err := os.ReadFile(statePath)
		return err == nil && strings.Contains(string(data), "status: healthy")
	})
	if n := rec.count(); n != 0 {
		t.Fatalf("the baseline observation must not alert, got %d alerts", n)
	}

	// The service turns unhealthy → one alert, correlated with the startup deploy.
	writeFile(t, psFile, psUnhealthy)
	s.waitFor("unhealthy health alert", func() bool { return rec.firstTo("unhealthy") != nil })
	a := rec.firstTo("unhealthy")
	if a["type"] != "health" || a["stack"] != "web" || a["service"] != "app" || a["from"] != "healthy" {
		t.Errorf("unexpected alert payload: %v", a)
	}
	if a["deploy_correlated"] != true {
		t.Errorf("expected the transition correlated with the startup deploy, got %v", a)
	}

	// It recovers → the matching all-clear.
	writeFile(t, psFile, psHealthy)
	s.waitFor("recovery health alert", func() bool { return rec.firstTo("healthy") != nil })
	if a := rec.firstTo("healthy"); a["from"] != "unhealthy" {
		t.Errorf("unexpected recovery payload: %v", a)
	}

	// The journey is on disk: newest-first phases healthy < unhealthy < healthy.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read healthwatch state: %v", err)
	}
	if !strings.Contains(string(data), "status: unhealthy") {
		t.Errorf("expected the unhealthy phase persisted, got:\n%s", data)
	}
}

// P11 — Alert cooldown journey: with alert_cooldown_seconds set, the first
// fail + recovery pair is delivered, but a repeat failure within the cooldown
// is suppressed — recorded and persisted (with the pending catch-up marker),
// yet no third alert is POSTed (ADR-0031 amendment).
func TestP11_HealthWatchCooldownSuppressesRepeatFlap(t *testing.T) {
	rec := newAlertRecorder(t)

	psFile := filepath.Join(t.TempDir(), "ps.json")
	writeFile(t, psFile, psHealthy)

	extraCfg := fmt.Sprintf(`runtime_health_poll_interval_seconds: 1
health_watch:
  debounce_polls: 1
  alert_cooldown_seconds: 3600
  targets:
    - format: generic
      url: %q
`, rec.server.URL)

	s := startSkipperOpts(t, map[string]string{"STUB_DOCKER_PS_FILE": psFile}, extraCfg, "web")

	statePath := filepath.Join(s.stateDir, "healthwatch.yaml")
	s.waitFor("healthwatch baseline persisted", func() bool {
		data, err := os.ReadFile(statePath)
		return err == nil && strings.Contains(string(data), "status: healthy")
	})

	// The first fail + recovery pair pages both directions despite the cooldown.
	writeFile(t, psFile, psUnhealthy)
	s.waitFor("unhealthy health alert", func() bool { return rec.firstTo("unhealthy") != nil })
	writeFile(t, psFile, psHealthy)
	s.waitFor("recovery health alert", func() bool { return rec.firstTo("healthy") != nil })

	// The repeat failure within the cooldown is suppressed: the persisted
	// catch-up marker appears, but no further alert is delivered.
	writeFile(t, psFile, psUnhealthy)
	s.waitFor("suppression persisted", func() bool {
		data, err := os.ReadFile(statePath)
		return err == nil && strings.Contains(string(data), "suppressed: true")
	})
	if n := rec.count(); n != 2 {
		t.Fatalf("the repeat failure must be suppressed by the cooldown, got %d alerts", n)
	}
}
