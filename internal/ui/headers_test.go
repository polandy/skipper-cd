package ui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/ui"
)

func TestSecurityHeaders_SetOnEveryResponse(t *testing.T) {
	h := ui.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'none'",
		"script-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'none'",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("Content-Security-Policy = %q, missing %q", csp, directive)
		}
	}
	if strings.Contains(csp, "'unsafe-eval'") || strings.Contains(scriptSrc(csp), "'unsafe-inline'") {
		t.Errorf("script-src must allow neither eval nor inline script: %q", csp)
	}
}

// A handler that sets its own, stricter policy (the sandboxed icon response)
// keeps it: the middleware only supplies the default.
func TestSecurityHeaders_HandlerPolicyWins(t *testing.T) {
	const own = "default-src 'none'; sandbox"
	h := ui.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", own)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/icons/web", nil))
	if got := rec.Header().Values("Content-Security-Policy"); len(got) != 1 || got[0] != own {
		t.Errorf("Content-Security-Policy = %q, want only %q", got, own)
	}
}

func scriptSrc(csp string) string {
	for _, d := range strings.Split(csp, ";") {
		if d = strings.TrimSpace(d); strings.HasPrefix(d, "script-src") {
			return d
		}
	}
	return ""
}
