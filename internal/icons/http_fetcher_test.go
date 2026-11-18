package icons

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// dashboard-icons serves each format from its own directory: /svg/<slug>.svg,
// /png/<slug>.png, /webp/<slug>.webp. The test server mirrors that layout.
func iconSetServer(t *testing.T, routes map[string]struct {
	code int
	body string
}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if route.code != http.StatusOK {
			http.Error(w, "err", route.code)
			return
		}
		_, _ = w.Write([]byte(route.body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPFetcher_Fetch(t *testing.T) {
	srv := iconSetServer(t, map[string]struct {
		code int
		body string
	}{
		"/svg/jellyfin.svg": {200, "<svg>jf</svg>"},
		"/svg/boom.svg":     {500, ""},
	})
	f := &HTTPFetcher{BaseURL: srv.URL, Client: srv.Client()}

	t.Run("svg hit", func(t *testing.T) {
		data, ct, found, err := f.Fetch(context.Background(), "jellyfin")
		if err != nil || !found {
			t.Fatalf("found=%v err=%v", found, err)
		}
		if string(data) != "<svg>jf</svg>" || ct != "image/svg+xml" {
			t.Errorf("got %q / %q", data, ct)
		}
	})

	t.Run("definitive miss when no format exists", func(t *testing.T) {
		_, _, found, err := f.Fetch(context.Background(), "ghost")
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if found {
			t.Error("found=true, want false")
		}
	})

	t.Run("transient error on 5xx", func(t *testing.T) {
		_, _, found, err := f.Fetch(context.Background(), "boom")
		if err == nil {
			t.Fatal("err=nil, want error for 500")
		}
		if found {
			t.Error("found=true, want false on error")
		}
	})
}

func TestHTTPFetcher_FallsBackSvgPngWebp(t *testing.T) {
	srv := iconSetServer(t, map[string]struct {
		code int
		body string
	}{
		// svg preferred when present, even though png also exists.
		"/svg/prefer.svg": {200, "SVG"},
		"/png/prefer.png": {200, "PNG"},
		// png fallback when svg is missing.
		"/png/pngonly.png": {200, "PNG"},
		// webp fallback when svg and png are missing.
		"/webp/webponly.webp": {200, "WEBP"},
		// svg missing, png transient error → surfaces as error, no webp attempt.
		"/png/pngboom.png": {500, ""},
	})
	f := &HTTPFetcher{BaseURL: srv.URL, Client: srv.Client()}

	cases := []struct {
		slug    string
		data    string
		ct      string
		found   bool
		wantErr bool
	}{
		{slug: "prefer", data: "SVG", ct: "image/svg+xml", found: true},
		{slug: "pngonly", data: "PNG", ct: "image/png", found: true},
		{slug: "webponly", data: "WEBP", ct: "image/webp", found: true},
		{slug: "pngboom", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			data, ct, found, err := f.Fetch(context.Background(), tc.slug)
			if tc.wantErr {
				if err == nil {
					t.Fatal("err=nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if found != tc.found {
				t.Fatalf("found=%v want %v", found, tc.found)
			}
			if string(data) != tc.data || ct != tc.ct {
				t.Errorf("got %q / %q, want %q / %q", data, ct, tc.data, tc.ct)
			}
		})
	}
}
