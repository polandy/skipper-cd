package ui_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/polandy/skipper-cd/internal/ui"
)

// guarded returns a handler that records whether it ran.
func guarded(ran *bool) http.Handler {
	return ui.RequireSameOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*ran = true
		w.WriteHeader(http.StatusOK)
	}))
}

func TestRequireSameOrigin(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		secFetch string
		origin   string
		wantCode int
		wantRuns bool
	}{
		{
			name:     "cross-site POST is refused",
			method:   http.MethodPost,
			secFetch: "cross-site",
			origin:   "https://evil.example.com",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "same-site POST is refused",
			method:   http.MethodPost,
			secFetch: "same-site",
			origin:   "https://other.example.com",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "same-origin POST from the UI is allowed",
			method:   http.MethodPost,
			secFetch: "same-origin",
			origin:   "http://skipper.example.com",
			wantCode: http.StatusOK,
			wantRuns: true,
		},
		{
			name:     "user-initiated navigation is allowed",
			method:   http.MethodPost,
			secFetch: "none",
			wantCode: http.StatusOK,
			wantRuns: true,
		},
		{
			name:     "non-browser client without fetch metadata is allowed",
			method:   http.MethodPost,
			wantCode: http.StatusOK,
			wantRuns: true,
		},
		{
			name:     "legacy browser with a foreign Origin is refused",
			method:   http.MethodPost,
			origin:   "https://evil.example.com",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "legacy browser with a matching Origin is allowed",
			method:   http.MethodPost,
			origin:   "http://skipper.example.com",
			wantCode: http.StatusOK,
			wantRuns: true,
		},
		{
			name:     "reads are never refused",
			method:   http.MethodGet,
			secFetch: "cross-site",
			origin:   "https://evil.example.com",
			wantCode: http.StatusOK,
			wantRuns: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ran bool
			req := httptest.NewRequest(tc.method, "http://skipper.example.com/api/autosync", nil)
			req.Host = "skipper.example.com"
			if tc.secFetch != "" {
				req.Header.Set("Sec-Fetch-Site", tc.secFetch)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			rec := httptest.NewRecorder()
			guarded(&ran).ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if ran != tc.wantRuns {
				t.Errorf("handler ran = %t, want %t", ran, tc.wantRuns)
			}
		})
	}
}
