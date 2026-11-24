//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
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

// TestE2E_RollbackDisabledLeavesFailureHeld (P19): with `rollback: false` a
// failed `up` is not restored (ADR-0050): the stack reports `failed`, never
// `rolled_back`, runs no further `up` or `down`, and the change is held.
func TestE2E_RollbackDisabledLeavesFailureHeld(t *testing.T) {
	s := newSkipper(t, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "2"}, "", []string{"web"})
	s.stackCfg = map[string]string{"web": "    rollback: false\n"}
	s.bootstrap(t)

	es := s.openEvents()
	es.awaitStreamReady("web")

	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "failed")
	s.waitFor("the change to be held", func() bool {
		v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return ok && v == 1
	})

	if got := s.dockerUps("web"); got != 2 {
		t.Errorf("ups = %d, want startup + failed deploy = 2 (no rollback up)", got)
	}
	if got := s.dockerCalls("web", "down"); got != 0 {
		t.Errorf("down calls = %d, want 0: the failed containers stay running", got)
	}
	if es.has("web", "rolled_back") {
		t.Error("rollback: false must not report rolled_back")
	}
}

// TestE2E_RollbackPatchOnlyGatesByUpdateLevel (P35): with `rollback: patch_only`
// a failed patch bump is restored, while a failed minor bump stays running and
// held with the step named in the event error (ADR-0074).
func TestE2E_RollbackPatchOnlyGatesByUpdateLevel(t *testing.T) {
	tests := []struct {
		name, from, to string
		wantStatus     string
		wantUps        int
		wantReason     string
	}{
		{name: "patch bump rolls back", from: "1.25.1", to: "1.25.2", wantStatus: "rolled_back", wantUps: 3},
		{name: "minor bump stays held", from: "1.25", to: "1.26", wantStatus: "failed", wantUps: 2,
			wantReason: "rollback skipped: app 1.25 -> 1.26 is a minor update"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSkipper(t, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "2"}, "", []string{"web"})
			s.stackCfg = map[string]string{"web": "    rollback: patch_only\n"}
			s.seedFiles = map[string]string{"web/docker-compose.yml": "services:\n  app:\n    image: nginx:" + tt.from + "\n"}
			s.bootstrap(t)

			es := s.openEvents()
			es.awaitStreamReady("web")

			s.setStackImage("web", tt.to)
			if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
				t.Fatalf("webhook status = %d, want 202", code)
			}
			es.waitEvent("web", tt.wantStatus)

			if got := s.dockerUps("web"); got != tt.wantUps {
				t.Errorf("ups = %d, want %d", got, tt.wantUps)
			}
			if tt.wantReason == "" {
				return
			}
			s.waitFor("the change to be held", func() bool {
				v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
				return ok && v == 1
			})
			if errs := es.errors("web", "failed"); len(errs) != 1 || !strings.Contains(errs[0], tt.wantReason) {
				t.Errorf("failed event errors = %q, want one naming %q", errs, tt.wantReason)
			}
		})
	}
}

// TestE2E_HealthProbeFailureRollsBackUnhealthy (P16): a stack whose
// deploy_health_check URL stops answering 2xx fails the gate, is rolled back,
// the rollback's rerun of the same gate fails too, and so does the one
// roll-forward (ADR-0072) → `rolled_back_unhealthy`.
func TestE2E_HealthProbeFailureRollsBackUnhealthy(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(probe.Close)

	s := newSkipper(t, nil, "", []string{"web"})
	s.stackCfg = map[string]string{
		"web": fmt.Sprintf("    deploy_health_check:\n      timeout_seconds: 1\n      url: %q\n", probe.URL),
	}
	s.bootstrap(t)

	es := s.openEvents()
	es.awaitStreamReady("web")

	healthy.Store(false)
	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "rolled_back_unhealthy")

	if got := s.dockerUps("web"); got != 4 {
		t.Fatalf("ups = %d, want startup + deploy + rollback + roll-forward = 4", got)
	}
	if es.has("web", "rolled_back") {
		t.Fatal("a rollback that is still unhealthy must not also report rolled_back")
	}
}

// TestE2E_InferredHealthGateWaitsForReadiness (P17): a stack with no
// deploy_health_check whose compose gains a `healthcheck:` has its deploy
// gated — skipper reads the containers' state after the `up` (ADR-0046,
// ADR-0072); a sibling stack without one is not gated.
func TestE2E_InferredHealthGateWaitsForReadiness(t *testing.T) {
	s := startSkipper(t, "web", "api")

	es := s.openEvents()
	es.awaitStreamReady("web")

	healthy := "services:\n  app:\n    image: nginx:1.26\n    healthcheck:\n      test: [\"CMD\", \"true\"]\n"
	writeFile(t, filepath.Join(s.origin, "web", "docker-compose.yml"), healthy)
	git(t, s.origin, "commit", "-am", "web gains a healthcheck")
	s.setStackImage("api", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEventCount("web", "success", 2)
	es.waitEventCount("api", "success", 2)

	if got := s.readinessReads("web"); got != 1 {
		t.Errorf("web readiness reads = %d, want 1 (the deploy, not the startup)", got)
	}
	if got := s.readinessReads("api"); got != 0 {
		t.Errorf("api has no healthcheck; readiness reads = %d, want 0", got)
	}
	if got := s.dockerCalls("web", "--wait"); got != 0 {
		t.Errorf("compose --wait calls = %d, want 0: skipper owns the wait", got)
	}
}

// TestE2E_DisabledHealthGateSkipsWait (P18): `deploy_health_check: false`
// keeps the deploy ungated even though the compose declares a
// `healthcheck:` (ADR-0049); the counter-check is P17, where the same compose
// change without the key is gated.
func TestE2E_DisabledHealthGateSkipsWait(t *testing.T) {
	s := newSkipper(t, nil, "", []string{"web"})
	s.stackCfg = map[string]string{"web": "    deploy_health_check: false\n"}
	s.bootstrap(t)

	es := s.openEvents()
	es.awaitStreamReady("web")

	healthy := "services:\n  app:\n    image: nginx:1.26\n    healthcheck:\n      test: [\"CMD\", \"true\"]\n"
	writeFile(t, filepath.Join(s.origin, "web", "docker-compose.yml"), healthy)
	git(t, s.origin, "commit", "-am", "web gains a healthcheck")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	s.waitFor("web redeployed", func() bool { return s.dockerUps("web") >= 2 })

	if got := s.dockerUps("web"); got != 2 {
		t.Fatalf("ups = %d, want startup + deploy = 2", got)
	}
	if got := s.readinessReads("web"); got != 0 {
		t.Errorf("readiness reads = %d, want 0 with deploy_health_check: false", got)
	}
}

// TestE2E_StillStartingHoldsWithoutRollback (P26): a gated deploy whose
// container is still starting when the budget runs out is `failed` and held,
// never rolled back, and names the retry endpoint (ADR-0072); once the container
// is healthy, a retry deploys it. STUB_DOCKER_READINESS_FILE is written only
// after the startup run, which therefore reads ready.
func TestE2E_StillStartingHoldsWithoutRollback(t *testing.T) {
	readiness := filepath.Join(t.TempDir(), "readiness.json")
	s := newSkipper(t, map[string]string{"STUB_DOCKER_READINESS_FILE": readiness}, "", []string{"web"})
	s.stackCfg = map[string]string{"web": "    deploy_health_check:\n      timeout_seconds: 1\n"}
	s.bootstrap(t)

	es := s.openEvents()
	es.awaitStreamReady("web")

	container := func(health string) {
		writeFile(t, readiness,
			`{"ID":"c1","Name":"web-app-1","Service":"app","State":"running","Health":"`+health+`"}`+"\n")
	}
	container("starting")
	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "failed")

	if got := s.dockerUps("web"); got != 2 {
		t.Fatalf("ups = %d, want startup + deploy = 2 (no rollback, no roll-forward)", got)
	}
	if es.has("web", "rolled_back") || es.has("web", "rolled_back_unhealthy") {
		t.Fatal("an undecided wait must never roll back")
	}
	msg := strings.Join(es.errors("web", "failed"), "\n")
	for _, want := range []string{"still starting", "not rolled back", "POST /api/stacks/web/retry"} {
		if !strings.Contains(msg, want) {
			t.Errorf("failed event error %q lacks %q", msg, want)
		}
	}
	s.waitFor("the change to be held", func() bool {
		v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return ok && v == 1
	})

	container("healthy")
	successes := es.count("web", "success") // the startup deploy's
	if code := s.postRetry("web"); code != http.StatusAccepted {
		t.Fatalf("retry status = %d, want 202", code)
	}
	es.waitEventCount("web", "success", successes+1)
}

// TestE2E_BootstrapRunSkipsPull (P20): the first run against an empty state
// converges every stack without `pull` and builds without `--pull`, so no
// floating tag moves unattended (ADR-0051); the next real change pulls again.
func TestE2E_BootstrapRunSkipsPull(t *testing.T) {
	s := newSkipper(t, nil, "", []string{"web", "app"})
	s.seedFiles = map[string]string{
		"app/docker-compose.yml": "services:\n  app:\n    build: .\n    image: app:local\n",
		"app/Dockerfile":         "FROM nginx:1.27\n",
	}
	s.bootstrap(t)

	if got := s.dockerCalls("web", "pull") + s.dockerCalls("app", "pull"); got != 0 {
		t.Errorf("bootstrap run pulled %d times, want 0", got)
	}
	if got := s.dockerBuilds("app"); got != 1 {
		t.Fatalf("bootstrap builds of app = %d, want 1", got)
	}
	if got := s.dockerCalls("app", "--pull"); got != 0 {
		t.Errorf("bootstrap build carried --pull %d times, want 0", got)
	}

	es := s.openEvents()
	es.awaitStreamReady("web")
	s.setStackImage("web", "1.26")
	writeFile(t, filepath.Join(s.origin, "app", "Dockerfile"), "FROM nginx:1.28\n")
	git(t, s.origin, "commit", "-am", "app base image bump")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	s.waitFor("both stacks redeployed", func() bool {
		return s.dockerUps("web") >= 2 && s.dockerUps("app") >= 2
	})

	if got := s.dockerCalls("web", "pull"); got != 1 {
		t.Errorf("web pulls after a real change = %d, want 1", got)
	}
	if got := s.dockerCalls("app", "--pull"); got != 1 {
		t.Errorf("app build --pull after a real change = %d, want 1", got)
	}
}

// readinessReads counts the stub's recorded `compose ps --all` reads for a
// stack — the deploy gate's readiness wait (the running-image read is
// `ps --format json --all`).
func (s *skipper) readinessReads(stack string) int {
	n := 0
	for _, line := range s.dockerLogLines() {
		cwd, args, ok := strings.Cut(line, "\t")
		if ok && filepath.Base(cwd) == stack && strings.Contains(" "+args+" ", " ps --all ") {
			n++
		}
	}
	return n
}

// TestE2E_HeldChangeWaitsForRetry (P13): a change whose new version failed
// after it started is held (ADR-0062). The stub fails the 2nd `up` (this
// deploy); the startup up (#1), the rollback up (#3) and the retry's up (#4)
// succeed. A second webhook for the same commit reports `held` without touching
// the containers; a retry deploys it once more and releases the hold.
func TestE2E_HeldChangeWaitsForRetry(t *testing.T) {
	s := startSkipperEnv(t, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "2"}, "web")

	es := s.openEvents()
	es.awaitStreamReady("web")

	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "rolled_back")
	upsAfterRollback := s.dockerUps("web")

	// Same commit again: runs serialize, so by the time this run reports the
	// hold, the rollback's run has published it.
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("second webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "held")
	if got := s.dockerUps("web"); got != upsAfterRollback {
		t.Fatalf("a held change must not deploy; web ups = %d, want %d", got, upsAfterRollback)
	}
	if v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`); !ok || v != 1 {
		t.Errorf("skipper_stack_held{web} = %v (ok=%v), want 1", v, ok)
	}

	successes := es.count("web", "success") // the startup deploy's
	if code := s.postRetry("web"); code != http.StatusAccepted {
		t.Fatalf("retry status = %d, want 202", code)
	}
	es.waitEventCount("web", "success", successes+1)
	if got := s.dockerUps("web"); got <= upsAfterRollback {
		t.Fatalf("the retry must deploy; web ups = %d, want > %d", got, upsAfterRollback)
	}
	s.waitFor("the hold to be released", func() bool {
		_, held := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return !held
	})
	if code := s.postRetry("web"); code != http.StatusConflict {
		t.Errorf("retry of a released stack = %d, want 409", code)
	}
}

// TestE2E_RevertReleasesHeldChange (P21): reverting a held change puts the
// stack's inputs back at the recorded ones, so the run converges the containers
// with one plain `up` and releases the hold (ADR-0068). Stub ups: startup #1,
// the failing deploy #2, its rollback #3, the converge #4.
func TestE2E_RevertReleasesHeldChange(t *testing.T) {
	s := startSkipperEnv(t, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "2"}, "web")

	es := s.openEvents()
	es.awaitStreamReady("web")

	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "rolled_back")
	s.waitFor("the hold to be raised", func() bool {
		v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return ok && v == 1
	})
	upsAfterRollback := s.dockerUps("web")

	s.revertLastCommit()
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("revert webhook status = %d, want 202", code)
	}
	s.waitFor("the hold to be released", func() bool {
		_, held := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return !held
	})
	if got := s.dockerUps("web"); got != upsAfterRollback+1 {
		t.Errorf("a revert converges with exactly one up; web ups = %d, want %d", got, upsAfterRollback+1)
	}
}

// TestE2E_RevertKeepsHoldWhenConvergeFails (P22): the converge up of a revert
// fails (stub up #4), so the stack reports `failed` and stays held for the next
// tick to retry (ADR-0068).
func TestE2E_RevertKeepsHoldWhenConvergeFails(t *testing.T) {
	s := startSkipperEnv(t, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "2,4"}, "web")

	es := s.openEvents()
	es.awaitStreamReady("web")

	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "rolled_back")
	s.waitFor("the hold to be raised", func() bool {
		v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return ok && v == 1
	})
	upsAfterRollback := s.dockerUps("web")

	s.revertLastCommit()
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("revert webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "failed")
	if got := s.dockerUps("web"); got != upsAfterRollback+1 {
		t.Errorf("web ups = %d, want %d (the failed converge)", got, upsAfterRollback+1)
	}
	if v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`); !ok || v != 1 {
		t.Errorf("skipper_stack_held{web} = %v (ok=%v), want 1: a failed converge keeps the hold", v, ok)
	}
}

// TestE2E_FailedPostDeployHookHoldsChange (P23): a failing `post_deploy` hook
// rolls the deploy back and holds the change, though every `up` succeeds
// (ADR-0062). The hook passes until the flag file exists; hooks never run on a
// rollback. Stub ups: startup #1, the deploy #2, its rollback #3.
func TestE2E_FailedPostDeployHookHoldsChange(t *testing.T) {
	s := newSkipper(t, nil, "", []string{"web"})
	flag := filepath.Join(s.base, "hook-fails")
	s.stackCfg = map[string]string{
		"web": fmt.Sprintf("    hooks:\n      post_deploy:\n        - %q\n", "test ! -e "+flag),
	}
	s.bootstrap(t)

	es := s.openEvents()
	es.awaitStreamReady("web")

	writeFile(t, flag, "")
	s.setStackImage("web", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "rolled_back")
	s.waitFor("the change to be held", func() bool {
		v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return ok && v == 1
	})
	if got := s.dockerUps("web"); got != 3 {
		t.Fatalf("ups = %d, want startup + deploy + rollback = 3", got)
	}

	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("second webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "held")
	if got := s.dockerUps("web"); got != 3 {
		t.Errorf("ups after the held run = %d, want 3: a held change touches no container", got)
	}
}

// TestE2E_FailedBuildNamesItsCause (P14): a failed build's event carries the
// cause from BuildKit's stderr, not only the exit status (ADR-0063). The stub
// fails the first two builds with an elapsed stamp that differs per call; both
// failures must still read the same, or the history could not collapse them
// (ADR-0056).
func TestE2E_FailedBuildNamesItsCause(t *testing.T) {
	s := startSkipperEnv(t, map[string]string{"STUB_DOCKER_FAIL_BUILDS": "2"}, "web")

	es := s.openEvents()
	es.awaitStreamReady("web")

	s.setStackBuild("web", "FROM nginx:1.27\nRUN apt-get install -y ghostscript=0.0-missing\n")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEventCount("web", "failed", 1)
	// The failed change now backs off (ADR-0064); a retry attempts it again.
	if code := s.postRetry("web"); code != http.StatusAccepted {
		t.Fatalf("retry status = %d, want 202", code)
	}
	es.waitEventCount("web", "failed", 2)

	const want = "docker compose build: exit status 1: " +
		"E: Unable to correct problems, you have held broken packages. — " +
		`process "/bin/sh -c apt-get install -y ghostscript=0.0-missing" did not complete successfully: exit code: 100`
	for i, got := range es.errors("web", "failed") {
		if got != want {
			t.Errorf("failed event %d error =\n  %q\nwant\n  %q", i+1, got, want)
		}
	}
}

// TestE2E_FailedBuildBacksOffThenHolds (P15): a change that fails before it
// starts is retried after a backoff, not on every run, and held after its
// third failure (ADR-0064). The stub fails the first three builds. Each retry
// stands in for the backoff running out, so nothing waits on the clock.
func TestE2E_FailedBuildBacksOffThenHolds(t *testing.T) {
	s := startSkipperEnv(t, map[string]string{"STUB_DOCKER_FAIL_BUILDS": "3"}, "web")

	es := s.openEvents()
	es.awaitStreamReady("web")

	s.setStackBuild("web", "FROM nginx:1.27\nRUN apt-get install -y ghostscript=0.0-missing\n")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEventCount("web", "failed", 1)

	// Same commit inside the backoff: reported held, not built again.
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("second webhook status = %d, want 202", code)
	}
	es.waitEventCount("web", "held", 1)
	if got := s.dockerBuilds("web"); got != 1 {
		t.Fatalf("a backing-off change must not build again; web builds = %d, want 1", got)
	}
	if _, raised := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`); raised {
		t.Error("skipper_stack_held must not be raised during a backoff")
	}

	// The retry button attempts it now; the third failure holds it.
	for attempt := 2; attempt <= 3; attempt++ {
		if code := s.postRetry("web"); code != http.StatusAccepted {
			t.Fatalf("retry %d status = %d, want 202", attempt, code)
		}
		es.waitEventCount("web", "failed", attempt)
	}
	s.waitFor("the change to be held", func() bool {
		v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return ok && v == 1
	})
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEventCount("web", "held", 2)
	if got := s.dockerBuilds("web"); got != 3 {
		t.Fatalf("a held change must not build again; web builds = %d, want 3", got)
	}

	// A push that fixes the Dockerfile releases the hold.
	s.setStackBuild("web", "FROM nginx:1.27\n")
	successes := es.count("web", "success")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEventCount("web", "success", successes+1)
	s.waitFor("the hold to be released", func() bool {
		_, held := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return !held
	})
}

// TestE2E_DependencyOrdering (P12): with `app` depends_on `db`, a run that
// changes both deploys db first, and when db's `up` fails, app is blocked — no
// app `up`, a `blocked` event, and the pending queue lists app for retry
// (ADR-0032).
//
// Up invocations across the process: startup deploys db (#1) then app (#2); the
// webhook run then does db (#3). The stub fails #3, so db rolls back (up #4)
// and its failure blocks app before any app up.
func TestE2E_DependencyOrdering(t *testing.T) {
	s := startSkipperOrdered(t, map[string][]string{"app": {"db"}}, map[string]string{"STUB_DOCKER_FAIL_NTH_UP": "3"}, "db", "app")

	es := s.openEvents()
	es.awaitStreamReady("db")

	appUpsBefore := s.dockerUps("app")

	s.setStackImage("db", "1.26")
	s.setStackImage("app", "1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}

	es.waitEvent("app", "blocked")

	if got := s.dockerUps("app"); got != appUpsBefore {
		t.Fatalf("blocked stack must not deploy; app ups = %d, want %d", got, appUpsBefore)
	}
	if q := s.queueBody(); !strings.Contains(q, "app") {
		t.Fatalf("pending queue does not list the blocked stack app: %s", q)
	}
	if v, ok := metricValue(s.metricsBody(), `skipper_deploys_blocked_total{stack="app"}`); !ok || v < 1 {
		t.Errorf("skipper_deploys_blocked_total{app} = %v (ok=%v), want >= 1", v, ok)
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

// TestE2E_RolloutCutoverDrainsOldAfterCanary (P24): a stack with `rollout`
// cuts a pushed change over without a serving gap (ADR-0040): the canary starts
// beside the running container, and only once it is healthy is the old one
// stopped and removed. The service is routed by Traefik labels with no host
// port, as rollout requires; the stub's container table stands in for Traefik's
// backends (STUB_DOCKER_ROLLOUT_FILE).
func TestE2E_RolloutCutoverDrainsOldAfterCanary(t *testing.T) {
	s := newSkipper(t, nil, "", []string{"web"})
	s.stubEnv = map[string]string{"STUB_DOCKER_ROLLOUT_FILE": filepath.Join(s.base, "containers.jsonl")}
	s.stackCfg = map[string]string{"web": "    rollout:\n      services: [app]\n"}
	s.seedFiles = map[string]string{"web/docker-compose.yml": traefikCompose("1.25")}
	s.bootstrap(t)

	es := s.openEvents()
	es.awaitStreamReady("web")
	startupCalls := len(s.dockerLogLines())

	successes := es.count("web", "success")
	writeFile(t, filepath.Join(s.origin, "web", "docker-compose.yml"), traefikCompose("1.26"))
	git(t, s.origin, "commit", "-am", "bump web to 1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEventCount("web", "success", successes+1)

	var cutover []string
	for _, line := range s.dockerLogLines()[startupCalls:] {
		_, args, _ := strings.Cut(line, "\t")
		if _, up, ok := strings.Cut(args, " up "); ok {
			cutover = append(cutover, "up "+up)
		} else if strings.HasPrefix(args, "stop ") || strings.HasPrefix(args, "rm ") {
			cutover = append(cutover, args)
		}
	}
	want := []string{
		"up -d --no-deps --no-recreate --scale app=2 -- app",
		"stop -- old",
		"rm -- old",
	}
	if !slices.Equal(cutover, want) {
		t.Errorf("cutover calls =\n  %q\nwant\n  %q", cutover, want)
	}
	if es.has("web", "rolled_back") {
		t.Error("a healthy canary must not report rolled_back")
	}
}

// TestE2E_RolloutUnhealthyCanaryKeepsOldContainer (P25): a canary that never
// turns healthy is removed, the old container is never stopped, and the outcome
// is `rolled_back` without the git-restore's second `up` (ADR-0040).
func TestE2E_RolloutUnhealthyCanaryKeepsOldContainer(t *testing.T) {
	s := newSkipper(t, nil, "", []string{"web"})
	table := filepath.Join(s.base, "containers.jsonl")
	s.stubEnv = map[string]string{"STUB_DOCKER_ROLLOUT_FILE": table, "STUB_DOCKER_CANARY_HEALTH": "unhealthy"}
	s.stackCfg = map[string]string{"web": "    rollout:\n      services: [app]\n      health_timeout_seconds: 1\n"}
	s.seedFiles = map[string]string{"web/docker-compose.yml": traefikCompose("1.25")}
	s.bootstrap(t)

	es := s.openEvents()
	es.awaitStreamReady("web")
	startupCalls := len(s.dockerLogLines())

	writeFile(t, filepath.Join(s.origin, "web", "docker-compose.yml"), traefikCompose("1.26"))
	git(t, s.origin, "commit", "-am", "bump web to 1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "rolled_back")

	var calls []string
	for _, line := range s.dockerLogLines()[startupCalls:] {
		_, args, _ := strings.Cut(line, "\t")
		if _, up, ok := strings.Cut(args, " up "); ok {
			calls = append(calls, "up "+up)
		} else if strings.HasPrefix(args, "stop ") || strings.HasPrefix(args, "rm ") {
			calls = append(calls, args)
		}
	}
	want := []string{
		"up -d --no-deps --no-recreate --scale app=2 -- app",
		"stop -- canary",
		"rm -- canary",
	}
	if !slices.Equal(calls, want) {
		t.Errorf("calls =\n  %q\nwant\n  %q (old never stopped, no git-restore up)", calls, want)
	}
	if b, err := os.ReadFile(table); err != nil || !strings.Contains(string(b), `"ID":"old"`) || strings.Contains(string(b), `"ID":"canary"`) {
		t.Errorf("container table = %q (err=%v), want only the old container", b, err)
	}
}

// TestE2E_RolloutStillStartingCanaryIsKeptAndHeld (P36): a canary still
// starting when health_timeout_seconds runs out is undecided (ADR-0072
// amendment): it is neither stopped nor removed, the old container keeps
// serving, and the deploy is `failed` and held — never `rolled_back` — with
// the timeout's shortfall against the healthcheck budget named.
func TestE2E_RolloutStillStartingCanaryIsKeptAndHeld(t *testing.T) {
	s := newSkipper(t, nil, "", []string{"web"})
	table := filepath.Join(s.base, "containers.jsonl")
	s.stubEnv = map[string]string{"STUB_DOCKER_ROLLOUT_FILE": table, "STUB_DOCKER_CANARY_HEALTH": "starting"}
	s.stackCfg = map[string]string{"web": "    rollout:\n      services: [app]\n      health_timeout_seconds: 1\n"}
	s.seedFiles = map[string]string{"web/docker-compose.yml": traefikCompose("1.25")}
	s.bootstrap(t)

	es := s.openEvents()
	es.awaitStreamReady("web")
	startupCalls := len(s.dockerLogLines())

	writeFile(t, filepath.Join(s.origin, "web", "docker-compose.yml"), traefikCompose("1.26"))
	git(t, s.origin, "commit", "-am", "bump web to 1.26")
	if code := s.sendWebhook("refs/heads/main"); code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", code)
	}
	es.waitEvent("web", "failed")

	if es.has("web", "rolled_back") || es.has("web", "rolled_back_unhealthy") {
		t.Fatal("an undecided canary must never report a rollback")
	}
	msg := strings.Join(es.errors("web", "failed"), "\n")
	for _, want := range []string{
		"still starting", "the canary keeps running beside the old version", "not rolled back",
		"POST /api/stacks/web/retry", "rollout.health_timeout_seconds: 1 is below the compose healthcheck budget",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("failed event error %q lacks %q", msg, want)
		}
	}
	for _, line := range s.dockerLogLines()[startupCalls:] {
		_, args, _ := strings.Cut(line, "\t")
		if strings.HasPrefix(args, "stop ") || strings.HasPrefix(args, "rm ") {
			t.Errorf("no container may be stopped or removed, got %q", args)
		}
	}
	if b, err := os.ReadFile(table); err != nil || !strings.Contains(string(b), `"ID":"old"`) || !strings.Contains(string(b), `"ID":"canary"`) {
		t.Errorf("container table = %q (err=%v), want old and canary both kept", b, err)
	}
	s.waitFor("the change to be held", func() bool {
		v, ok := metricValue(s.metricsBody(), `skipper_stack_held{stack="web"}`)
		return ok && v == 1
	})
}

// traefikCompose is a rollout-eligible compose file: routed by Traefik labels,
// no host port, no container_name, and a healthcheck as the readiness signal.
func traefikCompose(tag string) string {
	return "services:\n  app:\n    image: nginx:" + tag + "\n" +
		"    labels:\n" +
		"      traefik.enable: \"true\"\n" +
		"      traefik.http.routers.web.rule: Host(`web.test`)\n" +
		"      traefik.http.services.web.loadbalancer.server.port: \"80\"\n" +
		"    healthcheck:\n      test: [\"CMD\", \"true\"]\n"
}
