package icons

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPFetcher_Fetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jellyfin.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg>jf</svg>"))
		case "/boom.svg":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f := &HTTPFetcher{BaseURL: srv.URL, Client: srv.Client()}

	t.Run("hit", func(t *testing.T) {
		data, ct, found, err := f.Fetch(context.Background(), "jellyfin")
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		if !found {
			t.Fatal("found = false, want true")
		}
		if string(data) != "<svg>jf</svg>" || ct != "image/svg+xml" {
			t.Errorf("got %q / %q", data, ct)
		}
	})

	t.Run("definitive miss on 404", func(t *testing.T) {
		_, _, found, err := f.Fetch(context.Background(), "ghost")
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		if found {
			t.Error("found = true, want false for 404")
		}
	})

	t.Run("transient error on 5xx", func(t *testing.T) {
		_, _, found, err := f.Fetch(context.Background(), "boom")
		if err == nil {
			t.Fatal("err = nil, want error for 500")
		}
		if found {
			t.Error("found = true, want false on error")
		}
	})
}
