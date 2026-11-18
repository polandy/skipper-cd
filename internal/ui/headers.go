package ui

import "net/http"

// contentSecurityPolicy is the default policy for every response on the main
// port (ADR-0067). The app shell is self-contained (ADR-0035), so everything
// is 'self': no inline script or handler attribute, no eval. Inline style
// stays allowed — the renderers emit computed style attributes, and with
// img-src/font-src/connect-src pinned to 'self' an injected style has no
// channel out. data: images carry the baked favicon.
const contentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"manifest-src 'self'; " +
	"worker-src 'self'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// SecurityHeaders wraps a handler so every response carries the hardening
// headers: the CSP above, no MIME sniffing, no framing (the autosync toggle and
// the retry button must not be clickjackable) and no cross-origin Referer. A
// handler that sets its own Content-Security-Policy — the sandboxed icon
// response — replaces the default, since it writes its headers afterwards.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
