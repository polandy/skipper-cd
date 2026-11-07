package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestNew_DefaultClientHasTimeout(t *testing.T) {
	// A registry that never answers must not hang the update check forever.
	hc, ok := New(nil, nil).doer.(*http.Client)
	if !ok {
		t.Fatalf("default doer is %T, want *http.Client", New(nil, nil).doer)
	}
	if hc.Timeout != requestTimeout {
		t.Errorf("default client timeout = %v, want %v", hc.Timeout, requestTimeout)
	}
}

func TestTags_EndlessBodyIsCapped(t *testing.T) {
	// An endless tags list must fail once it exceeds the cap — neither the
	// decode nor the drain-before-close may read it to the end.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := []byte(strings.Repeat("a", 64*1024))
		if _, err := w.Write([]byte(`{"tags":["`)); err != nil {
			return
		}
		for {
			if _, err := w.Write(chunk); err != nil {
				return // client hung up
			}
		}
	}))
	defer srv.Close()

	_, err := testClient(nil).Tags(context.Background(), hostOf(t, srv)+"/app:1.0")
	if !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("err = %v, want errBodyTooLarge", err)
	}
}

func TestTags_RefusesPaginationToAnotherHost(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"evil"}})
	}))
	defer foreign.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/v2/app/tags/list?last=1.0>; rel="next"`, foreign.URL))
		_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"1.0"}})
	}))
	defer srv.Close()

	_, err := testClient(nil).Tags(context.Background(), hostOf(t, srv)+"/app:1.0")
	if !errors.Is(err, errForeignURL) {
		t.Fatalf("err = %v, want errForeignURL", err)
	}
	if n := foreignHits.Load(); n != 0 {
		t.Errorf("foreign host received %d requests, want 0", n)
	}
}

func TestTags_RefusesTokenRealmOnUnrelatedDomain(t *testing.T) {
	// The registry is reached by IP; a realm naming a different host must be
	// refused before any request (credentials included) is sent to it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://attacker.example/token",service="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	var sent []string
	c := testClient(func(string) (string, string, bool) { return "alice", "s3cret", true })
	c.doer = doerFunc(func(r *http.Request) (*http.Response, error) {
		sent = append(sent, r.URL.Host)
		return http.DefaultClient.Do(r)
	})
	_, err := c.Tags(context.Background(), hostOf(t, srv)+"/app:1.0")
	if !errors.Is(err, errForeignURL) {
		t.Fatalf("err = %v, want errForeignURL", err)
	}
	for _, h := range sent {
		if h == "attacker.example" {
			t.Errorf("a request was sent to the foreign realm host")
		}
	}
	if len(sent) != 1 {
		t.Errorf("sent %d requests (%v), want only the challenged one", len(sent), sent)
	}
}

func TestCheckRealm_RefusesCleartextForHTTPSRegistry(t *testing.T) {
	ref := Reference{Host: "registry.example.com", Repo: "app", Tag: "1.0"}
	err := checkRealm("http://registry.example.com/token", "https", ref)
	if !errors.Is(err, errForeignURL) {
		t.Fatalf("err = %v, want errForeignURL", err)
	}
}

func TestCheckRealm_AcceptsRegistryFamily(t *testing.T) {
	for _, tc := range []struct{ registry, realm string }{
		{"registry-1.docker.io", "https://auth.docker.io/token"}, // Docker Hub
		{"ghcr.io", "https://ghcr.io/token"},
		{"registry.gitlab.com", "https://gitlab.com/jwt/auth"},
		{"example.com:5050", "https://gitlab.example.com/jwt/auth"},
	} {
		ref := Reference{Host: tc.registry, Repo: "app", Tag: "1"}
		if err := checkRealm(tc.realm, "https", ref); err != nil {
			t.Errorf("checkRealm(%q) for %s: %v", tc.realm, tc.registry, err)
		}
	}
}

func TestCheckRealm_RejectsUnrelatedDomains(t *testing.T) {
	for _, tc := range []struct{ registry, realm string }{
		{"registry.example.com", "https://evil.com/token"},
		{"registry.example.com", "https://example.com.evil.com/token"},
		{"ghcr.io", "https://ghcr.io.evil.io/token"},
		{"10.0.0.5:5000", "https://10.0.0.6/token"},
		{"registry.example.com", "/token"},
	} {
		ref := Reference{Host: tc.registry, Repo: "app", Tag: "1"}
		if err := checkRealm(tc.realm, "https", ref); !errors.Is(err, errForeignURL) {
			t.Errorf("checkRealm(%q) for %s = %v, want errForeignURL", tc.realm, tc.registry, err)
		}
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
