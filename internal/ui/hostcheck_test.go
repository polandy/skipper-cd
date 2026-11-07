package ui_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/polandy/skipper-cd/internal/ui"
)

func TestRequireAllowedHost(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		host    string
		wantOK  bool
	}{
		{"no list allows any host", nil, "attacker.example", true},
		{"listed name matches", []string{"skipper.example.com"}, "skipper.example.com", true},
		{"name without port matches any port", []string{"skipper.lan"}, "skipper.lan:8080", true},
		{"match ignores case", []string{"Skipper.LAN"}, "skipper.lan:8080", true},
		{"name with port requires that port", []string{"skipper.lan:8080"}, "skipper.lan:8080", true},
		{"name with port refuses another port", []string{"skipper.lan:8080"}, "skipper.lan:9000", false},
		{"name with port refuses a portless Host", []string{"skipper.lan:8080"}, "skipper.lan", false},
		{"bracketed IPv6 entry matches", []string{"[::1]"}, "[::1]:8080", true},
		{"bare IPv6 entry matches", []string{"::1"}, "[::1]:8080", true},
		{"rebound name is refused", []string{"skipper.lan", "localhost"}, "attacker.example:8080", false},
		{"suffix is not a match", []string{"skipper.lan"}, "evil-skipper.lan", false},
		{"empty Host is refused", []string{"skipper.lan"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := false
			h := ui.RequireAllowedHost(tt.allowed, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				ran = true
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if ran != tt.wantOK {
				t.Errorf("handler ran = %v, want %v", ran, tt.wantOK)
			}
			wantCode := http.StatusOK
			if !tt.wantOK {
				wantCode = http.StatusMisdirectedRequest
			}
			if rec.Code != wantCode {
				t.Errorf("status = %d, want %d", rec.Code, wantCode)
			}
		})
	}
}
