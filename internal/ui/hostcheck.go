package ui

import (
	"net"
	"net/http"
	"strings"
)

// RequireAllowedHost wraps a handler so it answers only requests whose Host
// header names this server (ADR-0067).
//
// RequireSameOrigin compares Origin against Host, which DNS rebinding defeats:
// a page on attacker.example that rebinds its name to skipper's IP is
// same-origin from the browser's view, so it can read every GET API and pass
// the POST guard. Its requests still carry `Host: attacker.example`, and that
// is what this refuses. An entry with a port matches only that host:port; one
// without matches the name on any port. With no entries every Host is
// answered, as before the setting existed.
func RequireAllowedHost(allowed []string, next http.Handler) http.Handler {
	if len(allowed) == 0 {
		return next
	}
	names := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		names[normalizeHost(a)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := normalizeHost(r.Host)
		if host == "" || !(names[host] || names[hostname(host)]) {
			http.Error(w, "host not allowed: add it to allowed_hosts", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// normalizeHost lower-cases a host[:port] and bracket-strips a bare IPv6
// literal, so "::1", "[::1]" and "[::1]:8080" compare by the same name.
func normalizeHost(h string) string {
	h = strings.ToLower(h)
	if _, _, err := net.SplitHostPort(h); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
}

// hostname drops the port from a normalized host[:port].
func hostname(h string) string {
	if name, _, err := net.SplitHostPort(h); err == nil {
		return name
	}
	return h
}
