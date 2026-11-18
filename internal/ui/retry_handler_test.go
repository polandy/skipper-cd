package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serveRetry routes one POST through a mux so {stack} resolves as in production.
func serveRetry(t *testing.T, stack string, held bool) (code int, body string, requested []string, triggered int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("POST /api/stacks/{stack}/retry", RetryHandler(
		func(s string) bool { requested = append(requested, s); return held },
		func() { triggered++ },
	))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/stacks/"+stack+"/retry", nil))
	return rec.Code, rec.Body.String(), requested, triggered
}

func TestRetryHandler_AcceptsHeldStackAndTriggersRun(t *testing.T) {
	code, body, requested, triggered := serveRetry(t, "signal", true)
	if code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202", code)
	}
	if len(requested) != 1 || requested[0] != "signal" {
		t.Errorf("requested = %v, want [signal]", requested)
	}
	if triggered != 1 {
		t.Errorf("triggered = %d, want one run", triggered)
	}
	if !strings.Contains(body, `"stack":"signal"`) {
		t.Errorf("body = %q, want the stack echoed", body)
	}
}

func TestRetryHandler_RejectsStackWithNothingHeld(t *testing.T) {
	code, body, requested, triggered := serveRetry(t, "web", false)
	if code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", code)
	}
	if len(requested) != 1 {
		t.Errorf("requested = %v, want the request checked once", requested)
	}
	if triggered != 0 {
		t.Errorf("triggered = %d, want no run for a stack with nothing held", triggered)
	}
	if !strings.Contains(body, "web") {
		t.Errorf("body = %q, want the error to name the stack", body)
	}
}
