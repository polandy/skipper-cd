package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler records whether it was reached and answers 200.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

// serve runs a request through g.Wrap and returns the recorder plus whether the
// wrapped handler was reached.
func serve(t *testing.T, g *Gate, r *http.Request) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	reached := false
	rec := httptest.NewRecorder()
	g.Wrap(okHandler(&reached)).ServeHTTP(rec, r)
	return rec, reached
}

func request(remoteAddr string, header http.Header) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	r.RemoteAddr = remoteAddr
	if header != nil {
		r.Header = header
	}
	return r
}

func TestGate_Disabled_AllowsEverything(t *testing.T) {
	g, err := New("", nil, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.Enabled() {
		t.Fatal("empty config must be disabled")
	}
	// No header, no cookie, arbitrary peer — still reaches the handler.
	rec, reached := serve(t, g, request("203.0.113.9:5555", nil))
	if !reached || rec.Code != http.StatusOK {
		t.Fatalf("disabled gate must pass through: reached=%v code=%d", reached, rec.Code)
	}
}

func TestGate_ProxyPath(t *testing.T) {
	g, err := New("Remote-User", []string{"10.0.0.0/8", "127.0.0.1"}, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !g.Enabled() {
		t.Fatal("gate with a header must be enabled")
	}

	tests := []struct {
		name       string
		remoteAddr string
		header     string // Remote-User value; "" = header absent
		wantOK     bool
	}{
		{"trusted peer with header", "10.1.2.3:4000", "alice", true},
		{"trusted bare-ip peer with header", "127.0.0.1:4000", "alice", true},
		{"trusted peer, header absent", "10.1.2.3:4000", "", false},
		{"trusted peer, header blank", "10.1.2.3:4000", " ", true}, // non-empty value is enough
		{"untrusted peer with header", "203.0.113.5:4000", "alice", false},
		{"untrusted peer, header absent", "203.0.113.5:4000", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.header != "" {
				h.Set("Remote-User", tc.header)
			}
			rec, reached := serve(t, g, request(tc.remoteAddr, h))
			if reached != tc.wantOK {
				t.Fatalf("reached=%v want %v (code %d)", reached, tc.wantOK, rec.Code)
			}
			if !tc.wantOK && rec.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d", rec.Code)
			}
		})
	}
}

func TestGate_TokenPath(t *testing.T) {
	g, err := New("", nil, "s3cret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !g.Enabled() {
		t.Fatal("gate with a token must be enabled")
	}

	t.Run("valid cookie from any peer", func(t *testing.T) {
		r := request("203.0.113.5:4000", nil)
		r.AddCookie(&http.Cookie{Name: CookieName, Value: "s3cret"})
		_, reached := serve(t, g, r)
		if !reached {
			t.Fatal("valid cookie must authorize")
		}
	})
	t.Run("wrong cookie", func(t *testing.T) {
		r := request("203.0.113.5:4000", nil)
		r.AddCookie(&http.Cookie{Name: CookieName, Value: "nope"})
		rec, reached := serve(t, g, r)
		if reached || rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong cookie must be 401: reached=%v code=%d", reached, rec.Code)
		}
	})
	t.Run("no credential", func(t *testing.T) {
		rec, reached := serve(t, g, request("203.0.113.5:4000", nil))
		if reached || rec.Code != http.StatusUnauthorized {
			t.Fatalf("missing credential must be 401: reached=%v code=%d", reached, rec.Code)
		}
	})
	t.Run("authorization bearer", func(t *testing.T) {
		h := http.Header{}
		h.Set("Authorization", "Bearer s3cret")
		_, reached := serve(t, g, request("203.0.113.5:4000", h))
		if !reached {
			t.Fatal("valid bearer token must authorize")
		}
	})
	t.Run("a present header does not authorize the token gate", func(t *testing.T) {
		h := http.Header{}
		h.Set("Remote-User", "alice") // no proxy configured, so this must be ignored
		rec, reached := serve(t, g, request("203.0.113.5:4000", h))
		if reached || rec.Code != http.StatusUnauthorized {
			t.Fatalf("header must not open a token-only gate: reached=%v code=%d", reached, rec.Code)
		}
	})
}

func TestGate_BothPathsIndependent(t *testing.T) {
	g, err := New("Remote-User", []string{"10.0.0.0/8"}, "s3cret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Proxy path: trusted peer + header, no token.
	h := http.Header{}
	h.Set("Remote-User", "alice")
	if _, reached := serve(t, g, request("10.1.1.1:2000", h)); !reached {
		t.Fatal("proxy path should authorize")
	}
	// Token path: untrusted peer, valid cookie.
	r := request("203.0.113.5:4000", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "s3cret"})
	if _, reached := serve(t, g, r); !reached {
		t.Fatal("token path should authorize")
	}
	// Neither: untrusted peer, no token.
	if rec, reached := serve(t, g, request("203.0.113.5:4000", nil)); reached || rec.Code != http.StatusUnauthorized {
		t.Fatalf("neither path: want 401, reached=%v code=%d", reached, rec.Code)
	}
}

func TestGate_UnauthorizedBodyCarriesTokenAuthHint(t *testing.T) {
	withToken, _ := New("", nil, "s3cret")
	withoutToken, _ := New("Remote-User", []string{"10.0.0.0/8"}, "")

	for _, tc := range []struct {
		name string
		gate *Gate
		want bool
	}{
		{"token configured", withToken, true},
		{"proxy only", withoutToken, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := serve(t, tc.gate, request("203.0.113.5:4000", nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d", rec.Code)
			}
			var body struct {
				TokenAuth bool `json:"tokenAuth"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", rec.Body.String(), err)
			}
			if body.TokenAuth != tc.want {
				t.Fatalf("tokenAuth=%v want %v", body.TokenAuth, tc.want)
			}
		})
	}
}

func TestNew_InvalidProxy(t *testing.T) {
	for _, entry := range []string{"not-an-ip", "10.0.0.0/99", "300.0.0.1"} {
		if _, err := New("Remote-User", []string{entry}, ""); err == nil {
			t.Fatalf("New with proxy %q should error", entry)
		}
	}
}

func TestGate_IPv6Proxy(t *testing.T) {
	g, err := New("Remote-User", []string{"2001:db8::/32"}, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h := http.Header{}
	h.Set("Remote-User", "alice")
	if _, reached := serve(t, g, request("[2001:db8::1]:4000", h)); !reached {
		t.Fatal("IPv6 peer inside CIDR should authorize")
	}
	if rec, reached := serve(t, g, request("[2001:dead::1]:4000", h)); reached || rec.Code != http.StatusUnauthorized {
		t.Fatalf("IPv6 peer outside CIDR must be 401: reached=%v code=%d", reached, rec.Code)
	}
}
