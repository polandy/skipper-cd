package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// tagsPageSize is the page size requested from the tags endpoint; registries
// may cap it lower and paginate via the Link header.
const tagsPageSize = "1000"

// maxTagsPages bounds Link-header pagination so a misbehaving registry cannot
// loop the client forever. 10 pages × up to 1000 tags is far beyond any real
// repository the update check cares about.
const maxTagsPages = 10

// manifestAccept is the Accept header for the digest HEAD. Manifest-list /
// index types lead: their digest is what a pull resolves and what the local
// image's RepoDigests records — a per-arch digest would never match it.
const manifestAccept = "application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json"

// Doer sends an HTTP request. *http.Client satisfies it; tests inject a fake.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Credentials resolves the username/password to use for a registry host.
// ok=false means anonymous. DockerConfigCredentials reads them from the
// host's docker config; a fixed func works for tests.
type Credentials func(host string) (user, pass string, ok bool)

// Client answers the two read-only questions the update check asks a registry:
// which tags a repository has, and which digest a tag currently resolves to.
// Auth follows the standard v2 challenge flow — anonymous first, then a
// bearer-token or basic retry when challenged.
type Client struct {
	doer  Doer
	creds Credentials
	// scheme forces one scheme for every host when non-empty (tests); ""
	// resolves per host via schemeFor.
	scheme string
}

// New builds a Client. A nil doer uses http.DefaultClient; a nil creds is
// fully anonymous.
func New(doer Doer, creds Credentials) *Client {
	if doer == nil {
		doer = http.DefaultClient
	}
	return &Client{doer: doer, creds: creds}
}

// schemeFor picks the scheme for a registry host: HTTPS, except for loopback
// hosts, which are reached over plain HTTP — the same stance docker takes on
// localhost registries (they are implicitly insecure-allowed).
func (c *Client) schemeFor(host string) string {
	if c.scheme != "" {
		return c.scheme
	}
	bare := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		bare = h
	}
	if bare == "localhost" || net.ParseIP(bare).IsLoopback() {
		return "http"
	}
	return "https"
}

// Tags lists the repository's tags for an image reference, following
// pagination.
func (c *Client) Tags(ctx context.Context, imageRef string) ([]string, error) {
	ref, err := ParseReference(imageRef)
	if err != nil {
		return nil, err
	}
	scheme := c.schemeFor(ref.Host)
	next := fmt.Sprintf("%s://%s/v2/%s/tags/list?n=%s", scheme, ref.Host, ref.Repo, tagsPageSize)
	var tags []string
	for page := 0; next != "" && page < maxTagsPages; page++ {
		resp, err := c.do(ctx, http.MethodGet, next, "", ref)
		if err != nil {
			return nil, err
		}
		var body struct {
			Tags []string `json:"tags"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		closeBody(resp)
		if err != nil {
			return nil, fmt.Errorf("%s: parse tags list: %w", ref, err)
		}
		tags = append(tags, body.Tags...)
		next = nextPageURL(resp, scheme, ref.Host)
	}
	return tags, nil
}

// ManifestDigest resolves the digest the reference's tag currently points at,
// via a HEAD request (which, notably, spends no Docker Hub pull tokens).
func (c *Client) ManifestDigest(ctx context.Context, imageRef string) (string, error) {
	ref, err := ParseReference(imageRef)
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.schemeFor(ref.Host), ref.Host, ref.Repo, ref.Tag)
	resp, err := c.do(ctx, http.MethodHead, u, manifestAccept, ref)
	if err != nil {
		return "", err
	}
	defer closeBody(resp)
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("%s: registry returned no Docker-Content-Digest", ref)
	}
	return digest, nil
}

// do performs one request, answering a 401 challenge with a single
// authenticated retry (bearer token or basic, per the challenge). Any other
// non-2xx status is an error. The caller must close the response body.
func (c *Client) do(ctx context.Context, method, rawURL, accept string, ref Reference) (*http.Response, error) {
	resp, err := c.send(ctx, method, rawURL, accept, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		closeBody(resp)
		auth, err := c.authorization(ctx, challenge, ref)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ref, err)
		}
		if resp, err = c.send(ctx, method, rawURL, accept, auth); err != nil {
			return nil, err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		closeBody(resp)
		return nil, fmt.Errorf("%s: registry returned %s", ref, resp.Status)
	}
	return resp, nil
}

func (c *Client) send(ctx context.Context, method, rawURL, accept, authorization string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return c.doer.Do(req)
}

// authorization turns a 401 challenge into the Authorization header value for
// the retry: a bearer token fetched from the challenge's realm, or basic
// credentials. Anonymous bearer-token requests are normal (public images on
// token-guarded registries); an anonymous basic challenge cannot succeed.
func (c *Client) authorization(ctx context.Context, challenge string, ref Reference) (string, error) {
	scheme, params := parseChallenge(challenge)
	user, pass, haveCreds := "", "", false
	if c.creds != nil {
		user, pass, haveCreds = c.creds(ref.Host)
	}
	switch scheme {
	case "bearer":
		token, err := c.fetchToken(ctx, params, ref, user, pass, haveCreds)
		if err != nil {
			return "", err
		}
		return "Bearer " + token, nil
	case "basic":
		if !haveCreds {
			return "", fmt.Errorf("registry requires credentials and none are configured")
		}
		return "Basic " + basicAuth(user, pass), nil
	default:
		return "", fmt.Errorf("unsupported auth challenge %q", challenge)
	}
}

// fetchToken requests a bearer token from the challenge's realm, passing the
// challenge's service and scope (defaulting to pull on the repository) and the
// credentials, when present, as basic auth.
func (c *Client) fetchToken(ctx context.Context, params map[string]string, ref Reference, user, pass string, haveCreds bool) (string, error) {
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("bearer challenge without realm")
	}
	q := url.Values{}
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + ref.Repo + ":pull"
	}
	q.Set("scope", scope)

	auth := ""
	if haveCreds {
		auth = "Basic " + basicAuth(user, pass)
	}
	resp, err := c.send(ctx, http.MethodGet, realm+"?"+q.Encode(), "", auth)
	if err != nil {
		return "", err
	}
	defer closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %s", resp.Status)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if body.Token != "" {
		return body.Token, nil
	}
	if body.AccessToken != "" {
		return body.AccessToken, nil
	}
	return "", fmt.Errorf("token endpoint returned no token")
}

// parseChallenge splits a WWW-Authenticate header into its scheme (lowercased)
// and key="value" parameters.
func parseChallenge(header string) (string, map[string]string) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(header), " ")
	params := map[string]string{}
	for _, part := range strings.Split(rest, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(part), "="); ok {
			params[strings.ToLower(k)] = strings.Trim(v, `"`)
		}
	}
	return strings.ToLower(scheme), params
}

// nextPageURL extracts the rel="next" target from a response's Link header,
// resolving a path-only target against the registry host. Empty when there is
// no next page.
func nextPageURL(resp *http.Response, scheme, host string) string {
	link := resp.Header.Get("Link")
	if link == "" || !strings.Contains(link, `rel="next"`) {
		return ""
	}
	start := strings.Index(link, "<")
	end := strings.Index(link, ">")
	if start < 0 || end <= start {
		return ""
	}
	target := link[start+1 : end]
	if strings.HasPrefix(target, "/") {
		return scheme + "://" + host + target
	}
	return target
}

func closeBody(resp *http.Response) {
	// Drain so the connection is reusable; both are best-effort on a read path
	// whose next request would simply open a new connection.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
