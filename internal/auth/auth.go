// Package auth gates the skipper-cd web UI's data API behind either a trusted
// reverse proxy or a shared token.
//
// A request is authorized when EITHER path succeeds:
//
//   - Proxy path: the request's network peer is within one of the configured
//     trusted_proxies AND it carries a non-empty trusted header set by that
//     proxy after it authenticated the user. Because the check is anchored on
//     the real TCP peer (not a forwardable header), a direct client cannot
//     spoof it.
//   - Token path: the request presents a shared token, either in the
//     skipper_auth cookie (how the PWA login stores it) or as an
//     "Authorization: Bearer <token>" header (handy for scripts). The token is
//     compared in constant time.
//
// The gate fails closed: with either path configured, a request satisfying
// neither is rejected with 401. An empty configuration disables the gate
// entirely, preserving skipper-cd's default open UI.
package auth

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// CookieName is the cookie the PWA login stores the shared token in, and the
// cookie the token path reads. Exported so the UI layer can stay in sync.
const CookieName = "skipper_auth"

// Gate authorizes requests for the proxy and/or token paths. The zero value is
// a disabled gate whose Wrap is a no-op; build a configured one with New.
type Gate struct {
	header  string       // trusted header name; "" disables the proxy path
	proxies []*net.IPNet // upstreams allowed to assert the header
	token   string       // shared token; "" disables the token path

	// limiter throttles failed token attempts per client IP. Non-nil only when
	// the token path is enabled.
	limiter *attemptLimiter
}

// New builds a Gate. An empty header disables the proxy path and an empty token
// disables the token path; with both empty the gate is disabled. Each proxy
// entry may be a CIDR ("10.0.0.0/8") or a bare IP ("127.0.0.1", "2001:db8::1");
// an unparseable entry is an error.
func New(header string, proxies []string, token string) (*Gate, error) {
	g := &Gate{header: header, token: token}
	if header != "" {
		nets, err := parseProxies(proxies)
		if err != nil {
			return nil, err
		}
		g.proxies = nets
	}
	if token != "" {
		g.limiter = newAttemptLimiter(maxTokenAttempts, tokenAttemptWindow)
	}
	return g, nil
}

// Enabled reports whether the gate authorizes requests (any path configured).
func (g *Gate) Enabled() bool { return g.header != "" || g.token != "" }

// TokenAuthEnabled reports whether the shared-token (PWA) path is configured.
func (g *Gate) TokenAuthEnabled() bool { return g.token != "" }

// Wrap returns a handler that authorizes requests before delegating to next.
// A disabled gate returns next unchanged. A trusted upstream is authorized
// outright; every other request goes through the token path, which is throttled
// per client IP against brute force.
func (g *Gate) Wrap(next http.Handler) http.Handler {
	if !g.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A trusted proxy is never rate-limited.
		if g.proxyTrusted(r) {
			next.ServeHTTP(w, r)
			return
		}
		key := peerHost(r.RemoteAddr)
		if g.limiter != nil && g.limiter.blocked(key) {
			g.rejectRateLimited(w)
			return
		}
		if g.tokenValid(r) {
			if g.limiter != nil {
				g.limiter.clear(key) // a good token forgives earlier misses
			}
			next.ServeHTTP(w, r)
			return
		}
		// Count only real token guesses, so merely loading the page (no
		// credential) never locks anyone out.
		if g.limiter != nil && g.tokenAttempted(r) {
			g.limiter.fail(key)
		}
		g.reject(w)
	})
}

// tokenAttempted reports whether r presented a token credential at all (right or
// wrong) — a cookie or a bearer scheme.
func (g *Gate) tokenAttempted(r *http.Request) bool {
	if g.token == "" {
		return false
	}
	if _, err := r.Cookie(CookieName); err == nil {
		return true
	}
	scheme, _, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	return ok && strings.EqualFold(scheme, "Bearer")
}

// tokenValid reports whether r presents the shared token via cookie or bearer.
func (g *Gate) tokenValid(r *http.Request) bool {
	if g.token == "" {
		return false
	}
	if c, err := r.Cookie(CookieName); err == nil && tokenEqual(c.Value, g.token) {
		return true
	}
	if scheme, rest, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok &&
		strings.EqualFold(scheme, "Bearer") && tokenEqual(strings.TrimSpace(rest), g.token) {
		return true
	}
	return false
}

// proxyTrusted reports whether r carries the trusted header and comes from an
// allowlisted upstream.
func (g *Gate) proxyTrusted(r *http.Request) bool {
	if g.header == "" || r.Header.Get(g.header) == "" {
		return false
	}
	return g.trustedPeer(r.RemoteAddr)
}

// trustedPeer reports whether remoteAddr's IP is within a trusted proxy network.
func (g *Gate) trustedPeer(remoteAddr string) bool {
	ip := net.ParseIP(peerHost(remoteAddr))
	if ip == nil {
		return false
	}
	for _, n := range g.proxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// peerHost returns the host part of a "host:port" RemoteAddr, or the input
// unchanged when it carries no port.
func peerHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// reject writes the 401 response. The body carries a tokenAuth hint so the UI
// knows whether to offer the token login field.
func (g *Gate) reject(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	hint := "false"
	if g.TokenAuthEnabled() {
		hint = "true"
	}
	// Small fixed shape; hand-written to avoid an allocation and an error path.
	_, _ = fmt.Fprintf(w, `{"error":"unauthorized","tokenAuth":%s}`+"\n", hint)
}

// rejectRateLimited writes the 429 response for a locked-out client. The limiter
// only exists on the token path, so tokenAuth is always true here.
func (g *Gate) rejectRateLimited(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(int(g.limiter.window.Seconds())))
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = fmt.Fprint(w, `{"error":"too many attempts","tokenAuth":true}`+"\n")
}

// tokenEqual compares two tokens in constant time. Empty inputs never match.
func tokenEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// parseProxies parses each CIDR-or-IP entry into a network.
func parseProxies(entries []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(entries))
	for _, e := range entries {
		n, err := parseCIDROrIP(e)
		if err != nil {
			return nil, err
		}
		nets = append(nets, n)
	}
	return nets, nil
}

// parseCIDROrIP parses a CIDR ("10.0.0.0/8") or a bare IP (treated as a /32 or
// /128 host network).
func parseCIDROrIP(entry string) (*net.IPNet, error) {
	if strings.Contains(entry, "/") {
		_, n, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", entry, err)
		}
		return n, nil
	}
	ip := net.ParseIP(entry)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP %q", entry)
	}
	bits := 8 * net.IPv6len
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		bits = 8 * net.IPv4len
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}, nil
}
