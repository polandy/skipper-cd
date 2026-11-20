package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// fakeStep is one scripted answer: a response, or a transport error, or both
// (the error wins, as it does for a real Doer).
type fakeStep struct {
	resp *http.Response
	err  error
}

// fakeResp builds a well-formed response without a server.
func fakeResp(code int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// scriptedDoer answers each request with the next scripted step and records
// every request it saw, so a test can assert how far a flow got.
type scriptedDoer struct {
	t     *testing.T
	steps []fakeStep
	reqs  []string
}

func (d *scriptedDoer) Do(r *http.Request) (*http.Response, error) {
	d.t.Helper()
	if len(d.reqs) >= len(d.steps) {
		d.t.Errorf("unexpected request %d: %s %s", len(d.reqs)+1, r.Method, r.URL)
		return nil, errors.New("script exhausted")
	}
	d.reqs = append(d.reqs, r.Method+" "+r.URL.String())
	step := d.steps[len(d.reqs)-1]
	return step.resp, step.err
}

// errReader is a response body that dies on the first read.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
func (r errReader) Close() error             { return nil }

// testClientOn runs against a scripted doer, plain HTTP like testClient.
func testClientOn(t *testing.T, steps []fakeStep) (*Client, *scriptedDoer) {
	t.Helper()
	doer := &scriptedDoer{t: t, steps: steps}
	c := New(doer, nil)
	c.scheme = "http"
	return c, doer
}

func TestTags_ErrorPaths(t *testing.T) {
	const ref = "registry.example.com/app:1.0"
	// The realm host shares the registry's site, so checkRealm lets it pass
	// and the rows exercise what comes after it.
	const realm = "http://auth.example.com/token"
	challenged := func(v string) fakeStep {
		return fakeStep{resp: fakeResp(http.StatusUnauthorized, http.Header{"Www-Authenticate": {v}}, "")}
	}
	token := func(body string) fakeStep { return fakeStep{resp: fakeResp(http.StatusOK, nil, body)} }

	tests := []struct {
		name     string
		ref      string
		steps    []fakeStep
		wantErr  string   // substring of the expected error; "" expects success
		wantReqs []string // "METHOD URL" per request the doer must see
	}{
		{
			name:    "unparsable reference fails before any request",
			ref:     "example.com/",
			wantErr: `image reference "example.com/" has no repository`,
		},
		{
			name:     "transport failure surfaces",
			steps:    []fakeStep{{err: errors.New("dial tcp: connection refused")}},
			wantErr:  "dial tcp: connection refused",
			wantReqs: []string{"GET http://registry.example.com/v2/app/tags/list?n=1000"},
		},
		{
			name:     "unsupported challenge scheme is not retried",
			steps:    []fakeStep{challenged(`Digest realm="` + realm + `"`)},
			wantErr:  "unsupported auth challenge",
			wantReqs: []string{"GET http://registry.example.com/v2/app/tags/list?n=1000"},
		},
		{
			name:     "bearer challenge without a realm is refused",
			steps:    []fakeStep{challenged(`Bearer service="reg"`)},
			wantErr:  "bearer challenge without realm",
			wantReqs: []string{"GET http://registry.example.com/v2/app/tags/list?n=1000"},
		},
		{
			name: "token endpoint error status surfaces",
			steps: []fakeStep{
				challenged(`Bearer realm="` + realm + `",service="other",scope="repository:app:pull"`),
				fakeStep{resp: fakeResp(http.StatusInternalServerError, nil, "")},
			},
			wantErr: "token endpoint returned 500 Internal Server Error",
			wantReqs: []string{
				"GET http://registry.example.com/v2/app/tags/list?n=1000",
				"GET " + realm + "?scope=repository%3Aapp%3Apull&service=other",
			},
		},
		{
			name: "token request failing at transport surfaces",
			steps: []fakeStep{
				challenged(`Bearer realm="` + realm + `",service="reg"`),
				{err: errors.New("dial tcp: no route to host")},
			},
			wantErr: "dial tcp: no route to host",
			wantReqs: []string{
				"GET http://registry.example.com/v2/app/tags/list?n=1000",
				"GET " + realm + "?scope=repository%3Aapp%3Apull&service=reg",
			},
		},
		{
			name: "unparsable token body surfaces",
			steps: []fakeStep{
				challenged(`Bearer realm="` + realm + `"`),
				token("not json"),
			},
			wantErr: "parse token response",
			wantReqs: []string{
				"GET http://registry.example.com/v2/app/tags/list?n=1000",
				"GET " + realm + "?scope=repository%3Aapp%3Apull",
			},
		},
		{
			name: "token body without any token fails",
			steps: []fakeStep{
				challenged(`Bearer realm="` + realm + `"`),
				token("{}"),
			},
			wantErr: "token endpoint returned no token",
			wantReqs: []string{
				"GET http://registry.example.com/v2/app/tags/list?n=1000",
				"GET " + realm + "?scope=repository%3Aapp%3Apull",
			},
		},
		{
			name: "authenticated retry failing at transport surfaces",
			steps: []fakeStep{
				challenged(`Bearer realm="` + realm + `",service="reg"`),
				token(`{"token":"tok"}`),
				{err: errors.New("dial tcp: reset by peer")},
			},
			wantErr: "dial tcp: reset by peer",
			wantReqs: []string{
				"GET http://registry.example.com/v2/app/tags/list?n=1000",
				"GET " + realm + "?scope=repository%3Aapp%3Apull&service=reg",
				"GET http://registry.example.com/v2/app/tags/list?n=1000",
			},
		},
		{
			name:    "body dying mid-transfer surfaces as a parse error",
			steps:   []fakeStep{{resp: &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{}, Body: errReader{err: errors.New("disk gone")}}}},
			wantErr: "parse tags list: disk gone",
			wantReqs: []string{
				"GET http://registry.example.com/v2/app/tags/list?n=1000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useRef := ref
			if tt.ref != "" {
				useRef = tt.ref
			}
			c, doer := testClientOn(t, tt.steps)
			_, err := c.Tags(context.Background(), useRef)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Tags(%q) succeeded, want error containing %q", useRef, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("err = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
			} else if err == nil {
				t.Fatalf("Tags(%q) succeeded, want an error", useRef)
			}
			if got, want := fmt.Sprint(doer.reqs), fmt.Sprint(tt.wantReqs); got != want {
				t.Errorf("requests = %s, want %s", got, want)
			}
		})
	}
}

func TestTags_TokenResponseFallsBackToAccessToken(t *testing.T) {
	// Some registries name the token access_token; both spellings must work.
	var calls int
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return fakeResp(http.StatusUnauthorized, http.Header{
				"Www-Authenticate": {`Bearer realm="http://auth.example.com/token"`},
			}, ""), nil
		case 2:
			return fakeResp(http.StatusOK, nil, `{"access_token":"tok"}`), nil
		default:
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("retry authorization = %q, want %q", got, "Bearer tok")
			}
			return fakeResp(http.StatusOK, nil, `{"tags":["1.0"]}`), nil
		}
	}), nil)
	c.scheme = "http"

	tags, err := c.Tags(context.Background(), "registry.example.com/app:1.0")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "1.0" {
		t.Errorf("tags = %v, want [1.0]", tags)
	}
}

func TestTags_DefaultScopeWhenChallengeOmitsIt(t *testing.T) {
	// A challenge without a scope must default to pull on the challenged repo.
	var scope string
	var calls int
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return fakeResp(http.StatusUnauthorized, http.Header{
				"Www-Authenticate": {`Bearer realm="http://auth.example.com/token",service="reg"`},
			}, ""), nil
		case 2:
			scope = r.URL.Query().Get("scope")
			return fakeResp(http.StatusOK, nil, `{"token":"tok"}`), nil
		default:
			return fakeResp(http.StatusOK, nil, `{"tags":["1.0"]}`), nil
		}
	}), nil)
	c.scheme = "http"

	if _, err := c.Tags(context.Background(), "registry.example.com/app:1.0"); err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if scope != "repository:app:pull" {
		t.Errorf("token request scope = %q, want %q", scope, "repository:app:pull")
	}
}

func TestTags_PaginationEdges(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		wantErr  string // "" expects success
		wantTags []string
		wantReqs int
	}{
		{
			// A Link header without a <target> points nowhere: the first page
			// is the whole answer rather than an error.
			name:     "link header without a target ends the walk",
			link:     `rel="next"`,
			wantTags: []string{"1.0"},
			wantReqs: 1,
		},
		{
			name:     "unparsable link target fails the call",
			link:     `<%zz>; rel="next"`,
			wantErr:  "parse pagination link",
			wantReqs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					return nil, errors.New("unexpected second page request")
				}
				return fakeResp(http.StatusOK, http.Header{"Link": {tt.link}}, `{"tags":["1.0"]}`), nil
			}), nil)
			c.scheme = "http"

			tags, err := c.Tags(context.Background(), "registry.example.com/app:1.0")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Tags: %v", err)
			}
			if got, want := fmt.Sprint(tags), fmt.Sprint(tt.wantTags); tt.wantErr == "" && got != want {
				t.Errorf("tags = %s, want %s", got, want)
			}
			if calls != tt.wantReqs {
				t.Errorf("requests = %d, want %d", calls, tt.wantReqs)
			}
		})
	}
}

func TestManifestDigest_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		steps    []fakeStep
		wantErr  string
		wantReqs []string
	}{
		{
			name:    "unparsable reference fails before any request",
			ref:     "example.com/",
			wantErr: `image reference "example.com/" has no repository`,
		},
		{
			name:     "http error status surfaces",
			steps:    []fakeStep{{resp: fakeResp(http.StatusNotFound, nil, "")}},
			wantErr:  "registry returned 404 Not Found",
			wantReqs: []string{"HEAD http://registry.example.com/v2/app/manifests/1.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := "registry.example.com/app:1.0"
			if tt.ref != "" {
				ref = tt.ref
			}
			c, doer := testClientOn(t, tt.steps)
			_, err := c.ManifestDigest(context.Background(), ref)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if got, want := fmt.Sprint(doer.reqs), fmt.Sprint(tt.wantReqs); got != want {
				t.Errorf("requests = %s, want %s", got, want)
			}
		})
	}
}

// schemeFor's HTTPS default cannot be exercised through Tags without dialing
// a real registry, so it is asserted directly.
func TestSchemeFor(t *testing.T) {
	tests := []struct{ host, want string }{
		{"registry.example.com", "https"},
		{"registry.example.com:5000", "https"},
		{"10.0.0.5", "https"},
		{"localhost", "http"},
		{"localhost:5000", "http"},
		{"127.0.0.1", "http"},
		{"[::1]:5000", "http"},
	}
	c := New(nil, nil)
	for _, tt := range tests {
		if got := c.schemeFor(tt.host); got != tt.want {
			t.Errorf("schemeFor(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
	// The explicit override wins over the host rules.
	c.scheme = "http"
	if got := c.schemeFor("registry.example.com"); got != "http" {
		t.Errorf("forced scheme = %q, want http", got)
	}
}

// The client builds every URL it sends itself, so the request-construction
// error branch is only reachable here — the guard is what keeps a future
// caller honest.
func TestSend_UnparsableURLFails(t *testing.T) {
	c := New(nil, nil)
	if _, err := c.send(context.Background(), http.MethodGet, "://no-scheme", "", ""); err == nil {
		t.Fatal("expected an error for a URL the http package cannot parse")
	}
}

// nextPageURL resolves against the page URL, which Tags always builds valid;
// the guard must still refuse to continue on garbage.
func TestNextPageURL_UnparsablePageURLFails(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Link": {`<next>; rel="next"`}}}
	if _, err := nextPageURL(resp, "http://reg\x7f"); err == nil {
		t.Fatal("expected an error for an unparsable page URL")
	}
}
