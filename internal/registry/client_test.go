package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testClient returns a Client that talks plain HTTP, so httptest servers can
// stand in for a registry (their host:port form parses as the registry host).
func testClient(creds Credentials) *Client {
	c := New(nil, creds)
	c.scheme = "http"
	return c
}

// hostOf strips the scheme from an httptest server URL, leaving host:port —
// the form an image reference carries.
func hostOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestTags_Anonymous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/team/app/tags/list" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "team/app", "tags": []string{"1.0", "1.1"}})
	}))
	defer srv.Close()

	tags, err := testClient(nil).Tags(context.Background(), hostOf(t, srv)+"/team/app:1.0")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if got, want := fmt.Sprint(tags), fmt.Sprint([]string{"1.0", "1.1"}); got != want {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

func TestTags_Paginated(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RawQuery {
		case "n=1000":
			w.Header().Set("Link", `</v2/team/app/tags/list?last=1.1&n=1000>; rel="next"`)
			_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"1.0", "1.1"}})
		case "last=1.1&n=1000":
			_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"1.2"}})
		default:
			t.Errorf("unexpected query %q", r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tags, err := testClient(nil).Tags(context.Background(), hostOf(t, srv)+"/team/app:1.0")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if got, want := fmt.Sprint(tags), fmt.Sprint([]string{"1.0", "1.1", "1.2"}); got != want {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

func TestTags_BearerTokenFlow(t *testing.T) {
	const token = "tok-123"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			// The token request carries the challenge's service and scope, and
			// the configured credentials as basic auth.
			if got := r.URL.Query().Get("service"); got != "test-registry" {
				t.Errorf("token service = %q", got)
			}
			if got := r.URL.Query().Get("scope"); got != "repository:team/app:pull" {
				t.Errorf("token scope = %q", got)
			}
			if u, p, _ := r.BasicAuth(); u != "alice" || p != "s3cret" {
				t.Errorf("token basic auth = %q/%q", u, p)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
		case r.Header.Get("Authorization") == "Bearer "+token:
			_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"2.0"}})
		default:
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Bearer realm=%q,service="test-registry",scope="repository:team/app:pull"`, srv.URL+"/token"))
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	creds := func(host string) (string, string, bool) { return "alice", "s3cret", true }
	tags, err := testClient(creds).Tags(context.Background(), hostOf(t, srv)+"/team/app:1.0")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "2.0" {
		t.Errorf("tags = %v, want [2.0]", tags)
	}
}

func TestTags_BasicChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "bob" || p != "pw" {
			w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"3.0"}})
	}))
	defer srv.Close()

	creds := func(host string) (string, string, bool) { return "bob", "pw", true }
	tags, err := testClient(creds).Tags(context.Background(), hostOf(t, srv)+"/app:1.0")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "3.0" {
		t.Errorf("tags = %v, want [3.0]", tags)
	}
}

func TestTags_UnauthorizedWithoutCreds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := testClient(nil).Tags(context.Background(), hostOf(t, srv)+"/app:1.0"); err == nil {
		t.Fatal("expected an error for an unauthorized request without credentials")
	}
}

func TestManifestDigest(t *testing.T) {
	const digest = "sha256:6529d4b5f8ec2a0e6f7a2f5e2b1f9d3c4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.Path != "/v2/team/app/manifests/1.2.3" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		// A client that does not accept manifest lists would get a per-arch
		// digest — the wrong comparison value for RepoDigests.
		if accept := r.Header.Get("Accept"); !strings.Contains(accept, "manifest.list") || !strings.Contains(accept, "image.index") {
			t.Errorf("Accept header misses manifest-list types: %q", accept)
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := testClient(nil).ManifestDigest(context.Background(), hostOf(t, srv)+"/team/app:1.2.3")
	if err != nil {
		t.Fatalf("ManifestDigest: %v", err)
	}
	if got != digest {
		t.Errorf("digest = %q, want %q", got, digest)
	}
}

func TestManifestDigest_MissingHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := testClient(nil).ManifestDigest(context.Background(), hostOf(t, srv)+"/app:1.0"); err == nil {
		t.Fatal("expected an error when the digest header is missing")
	}
}

func TestTags_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := testClient(nil).Tags(context.Background(), hostOf(t, srv)+"/gone:1.0"); err == nil {
		t.Fatal("expected an error for a 404")
	}
}

func TestLoopbackRegistrySpeaksPlainHTTP(t *testing.T) {
	// A registry on a loopback host is reached over plain HTTP, like docker
	// treats localhost registries — no client construction override needed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"1.0"}})
	}))
	defer srv.Close()

	tags, err := New(nil, nil).Tags(context.Background(), hostOf(t, srv)+"/app:1.0")
	if err != nil {
		t.Fatalf("Tags over loopback: %v", err)
	}
	if len(tags) != 1 || tags[0] != "1.0" {
		t.Errorf("tags = %v", tags)
	}
}
