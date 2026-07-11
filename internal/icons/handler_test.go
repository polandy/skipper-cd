package icons

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// iconMux wires the handlers exactly as production does, so tests exercise the
// {stack} path routing.
func iconMux(svc *Service, locate StackLocator) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /api/icons/{stack}", Handler(svc, locate))
	mux.Handle("POST /api/icons/refresh", RefreshHandler(svc))
	return mux
}

func TestHandler_ServesRepoOverride(t *testing.T) {
	svc, ff := newTestService(t)
	dir := writeStackIcon(t, "icon.svg", "<svg>repo</svg>")
	mux := iconMux(svc, func(name string) (Request, bool) {
		return Request{Name: name, Dir: dir}, true
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/icons/media", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "<svg>repo</svg>" {
		t.Errorf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("content-type = %q", ct)
	}
	if len(ff.calls) != 0 {
		t.Errorf("fetcher called %v, want none", ff.calls)
	}
}

func TestHandler_ServesAutoMatch(t *testing.T) {
	svc, ff := newTestService(t)
	ff.icons["jellyfin"] = []byte("<svg>jf</svg>")
	mux := iconMux(svc, func(name string) (Request, bool) {
		return Request{Name: name, Dir: t.TempDir()}, true
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/icons/jellyfin", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "<svg>jf</svg>" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestHandler_NotFoundForMiss(t *testing.T) {
	svc, _ := newTestService(t)
	mux := iconMux(svc, func(name string) (Request, bool) {
		return Request{Name: name, Dir: t.TempDir()}, true
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/icons/ghost", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_NotFoundForUnknownStack(t *testing.T) {
	svc, ff := newTestService(t)
	mux := iconMux(svc, func(string) (Request, bool) {
		return Request{}, false
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/icons/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if len(ff.calls) != 0 {
		t.Errorf("fetcher called %v for unknown stack, want none", ff.calls)
	}
}

func TestRefreshHandler_ClearsCache(t *testing.T) {
	svc, ff := newTestService(t)
	ff.icons["media"] = []byte("<svg>m</svg>")
	locate := func(name string) (Request, bool) {
		return Request{Name: name, Dir: t.TempDir()}, true
	}
	mux := iconMux(svc, locate)

	get := func() {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/icons/media", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status = %d", rec.Code)
		}
	}

	get() // fetches + caches
	get() // served from cache
	if len(ff.calls) != 1 {
		t.Fatalf("fetcher calls before refresh = %d, want 1", len(ff.calls))
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/icons/refresh", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("refresh status = %d, want 204", rec.Code)
	}

	get() // cache cleared → re-fetch
	if len(ff.calls) != 2 {
		t.Errorf("fetcher calls after refresh = %d, want 2", len(ff.calls))
	}
}
