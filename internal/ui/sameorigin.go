package ui

import (
	"net/http"
	"net/url"
)

// RequireSameOrigin wraps a handler so a state-changing request (anything but
// GET/HEAD) is refused when a browser made it from another site.
//
// The UI's write endpoints carry no token and no session of their own, so
// without this any page a viewer happens to open could POST to them: pausing
// autosync globally is a one-line cross-site fetch, and its effect — no stack
// ever deploys again — is invisible until someone notices updates stopped
// landing. skipper deliberately leaves authentication to a reverse proxy
// (README), but a proxy authenticates the *viewer*, not the *page that made
// the request*, so the gap stays open behind one.
//
// Non-browser clients are unaffected: `POST /api/icons/refresh` is documented
// as a curl call, and curl sends neither header.
func RequireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			http.Error(w, "cross-site request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether the request may change state.
//
// Sec-Fetch-Site is the primary signal: browsers set it on every request and
// page script cannot forge or suppress it, so `cross-site` and `same-site` are
// refused outright. Its absence means the client is not a browser (curl, a
// script, an ops runbook) and is allowed — an attacker's page cannot produce
// that case. Older browsers that predate Fetch Metadata still send Origin, so
// that is checked as a fallback; only the host is compared, since a request
// arriving through a TLS-terminating proxy carries no reliable scheme.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// Not a browser — fall through to the Origin fallback below.
	default: // cross-site, same-site
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}
