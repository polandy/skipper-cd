//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"
)

// TestE2E_WebhookTriggersDeploy is the harness smoke test: it proves the real
// binary wires a signed webhook through git sync, change detection, and the
// docker invocation, and persists state — end to end.
//
// The startup sync already deploys the initial stack, so the test pushes a
// fresh change afterwards and asserts that the webhook (not startup) drives a
// new `compose … up` for that stack.
func TestE2E_WebhookTriggersDeploy(t *testing.T) {
	s := startSkipper(t, "web")

	// Startup deployed v1 exactly once.
	if got := s.dockerUps("web"); got != 1 {
		t.Fatalf("expected 1 startup deploy of web, got %d", got)
	}

	// Push a new change, then trigger it via a signed webhook.
	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}

	// The webhook-driven deploy runs in the background; wait for the new `up`.
	s.waitFor("webhook-triggered deploy of web", func() bool {
		return s.dockerUps("web") >= 2
	})

	if !s.stateHasStack("web") {
		t.Fatalf("state.yaml does not record stack web")
	}
}

// TestE2E_UnchangedStackSkipped (P2): a webhook with no new commit skips the
// stack — no new docker up, and a `skipped` event on the stream.
func TestE2E_UnchangedStackSkipped(t *testing.T) {
	s := startSkipper(t, "web")

	es := s.openEvents()
	es.awaitStreamReady("web") // startup success replayed → live subscription active

	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "skipped")

	if got := s.dockerUps("web"); got != 1 {
		t.Fatalf("unchanged stack must not redeploy; ups = %d, want 1", got)
	}
}

// TestE2E_StartupSyncDeploys (P3): a change present at boot deploys on startup
// with no webhook at all.
func TestE2E_StartupSyncDeploys(t *testing.T) {
	s := startSkipper(t, "web")

	if got := s.dockerUps("web"); got != 1 {
		t.Fatalf("startup sync should deploy once without a webhook; ups = %d", got)
	}
	if !s.stateHasStack("web") {
		t.Fatalf("state.yaml does not record stack web after startup")
	}
}

// TestE2E_InvalidSignatureRejected (P4): a wrongly signed webhook is rejected
// with 401 and triggers no deploy.
func TestE2E_InvalidSignatureRejected(t *testing.T) {
	s := startSkipper(t, "web")

	if code := s.sendWebhookRaw("refs/heads/main", "deadbeef"); code != http.StatusUnauthorized {
		t.Fatalf("webhook status = %d, want 401", code)
	}
	if got := s.dockerUps("web"); got != 1 {
		t.Fatalf("rejected webhook must not deploy; ups = %d, want 1", got)
	}
}

// TestE2E_WrongBranchIgnored (P5): a signed push for another branch is
// acknowledged with 200 and triggers no deploy.
func TestE2E_WrongBranchIgnored(t *testing.T) {
	s := startSkipper(t, "web")

	if code := s.sendWebhook("refs/heads/other"); code != http.StatusOK {
		t.Fatalf("webhook status = %d, want 200", code)
	}
	if got := s.dockerUps("web"); got != 1 {
		t.Fatalf("wrong-branch push must not deploy; ups = %d, want 1", got)
	}
}

// TestE2E_HealthzReflectsSync (P6): /healthz is 200 while syncs succeed and
// flips to 503 after a sync fails.
func TestE2E_HealthzReflectsSync(t *testing.T) {
	s := startSkipper(t, "web")

	if code := s.healthStatus(); code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200 on healthy start", code)
	}

	// Remove the origin so the sync forced by the next webhook fails.
	s.breakOrigin()
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	s.waitFor("healthz 503 after failing sync", func() bool {
		return s.healthStatus() == http.StatusServiceUnavailable
	})
}

// TestE2E_MetricsExposeCounters (P7): after a webhook-driven deploy, /metrics
// exposes the webhook and per-stack deploy counters.
func TestE2E_MetricsExposeCounters(t *testing.T) {
	s := startSkipper(t, "web")

	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	s.waitFor("webhook-triggered deploy", func() bool { return s.dockerUps("web") >= 2 })

	body := s.metricsBody()
	if v, ok := metricValue(body, "skipper_webhooks_received_total"); !ok || v < 1 {
		t.Errorf("skipper_webhooks_received_total = %v (ok=%v), want >= 1", v, ok)
	}
	if v, ok := metricValue(body, `skipper_deploys_triggered_total{stack="web"}`); !ok || v < 1 {
		t.Errorf("skipper_deploys_triggered_total{web} = %v (ok=%v), want >= 1", v, ok)
	}
}

// TestE2E_RollbackOnFailedUp (P8): when `docker compose up` fails on a change
// that has a previous version, skipper rolls back and emits `rolled_back`.
// The stub fails the 2nd `up` (the initial one of this deploy); the startup up
// (#1) and the rollback up (#3) succeed.
func TestE2E_RollbackOnFailedUp(t *testing.T) {
	s := startSkipperEnv(t, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "2"}, "web")

	es := s.openEvents()
	es.awaitStreamReady("web")

	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "rolled_back")

	if got := s.dockerUps("web"); got < 3 {
		t.Fatalf("expected initial + rollback up (>= 3 total), got %d", got)
	}
}

// TestE2E_PausedStackQueued (P9): with autosync paused, a change is queued
// instead of deployed — a `queued` event, no docker up, and /api/queue lists it.
func TestE2E_PausedStackQueued(t *testing.T) {
	s := startSkipper(t, "web")

	if code := s.postAutosync("", false); code != http.StatusOK {
		t.Fatalf("pause autosync status = %d, want 200", code)
	}

	es := s.openEvents()
	es.awaitStreamReady("web")

	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "queued")

	if got := s.dockerUps("web"); got != 1 {
		t.Fatalf("paused stack must not deploy; ups = %d, want 1", got)
	}
	if q := s.queueBody(); !strings.Contains(q, "web") {
		t.Fatalf("queue does not list web: %s", q)
	}
}
