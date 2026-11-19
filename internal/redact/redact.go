// Package redact masks secrets out of text skipper publishes — deploy events
// (and through them history, audit and notifications) and the in-memory log
// served at /api/logs — so a command that echoes a secret cannot leak it.
package redact

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Mask replaces a registered secret value; it matches the env-file diff mask.
const Mask = "***"

// urlPasswordMask replaces URL credentials, the same placeholder net/url's
// Redacted uses.
const urlPasswordMask = "xxxxx"

// minSecretLen is the shortest value registered: masking "1" or "on"
// everywhere would mangle unrelated text far more than it protects.
const minSecretLen = 4

// sshScheme carries a login name, not a credential, as a lone userinfo.
const sshScheme = "ssh"

var (
	// secretKey matches an env key whose value is treated as a secret.
	secretKey = regexp.MustCompile(`(?i)PASS|SECRET|TOKEN|KEY|CRED|AUTH|PRIVATE|SALT|DSN`)
	// urlUserinfo matches the userinfo of a URL in free text: scheme, user,
	// optional ":password", then "@".
	urlUserinfo = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9+.-]*)://([^\s/@:'"]+)(:[^\s/@'"]*)?@`)
)

// Secrets is the set of secret values to mask. It is safe for concurrent use;
// a nil *Secrets masks URL credentials only.
type Secrets struct {
	mu       sync.RWMutex
	values   map[string]struct{}
	replacer *strings.Replacer
}

// New returns an empty set.
func New() *Secrets {
	return &Secrets{values: map[string]struct{}{}}
}

// AddEnv registers the values of the KEY=VALUE entries whose key names a
// secret (PASSWORD, TOKEN, API_KEY, …). Values are never removed, so a
// rotated secret stays masked for the life of the process.
func (s *Secrets) AddEnv(env []string) {
	var fresh []string
	s.mu.RLock()
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || len(v) < minSecretLen || !secretKey.MatchString(k) {
			continue
		}
		if _, known := s.values[v]; !known {
			fresh = append(fresh, v)
		}
	}
	s.mu.RUnlock()
	if len(fresh) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range fresh {
		s.values[v] = struct{}{}
	}
	s.replacer = newReplacer(s.values)
}

// newReplacer masks the longest value first, so a secret containing a shorter
// one is masked whole.
func newReplacer(values map[string]struct{}) *strings.Replacer {
	sorted := make([]string, 0, len(values))
	for v := range values {
		sorted = append(sorted, v)
	}
	slices.SortFunc(sorted, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	pairs := make([]string, 0, 2*len(sorted))
	for _, v := range sorted {
		pairs = append(pairs, v, Mask)
	}
	return strings.NewReplacer(pairs...)
}

// Text returns text with every registered value and every URL credential
// masked.
func (s *Secrets) Text(text string) string {
	if s != nil {
		s.mu.RLock()
		r := s.replacer
		s.mu.RUnlock()
		if r != nil {
			text = r.Replace(text)
		}
	}
	return maskURLUserinfo(text)
}

// maskURLUserinfo masks a password and a lone username (a bare access token),
// except an ssh:// login name — the rule git.RedactURL applies to repo_url.
func maskURLUserinfo(text string) string {
	return urlUserinfo.ReplaceAllStringFunc(text, func(m string) string {
		sub := urlUserinfo.FindStringSubmatch(m)
		scheme, user, password := sub[1], sub[2], sub[3]
		switch {
		case password != "":
			return scheme + "://" + user + ":" + urlPasswordMask + "@"
		case strings.EqualFold(scheme, sshScheme):
			return m
		default:
			return scheme + "://" + urlPasswordMask + "@"
		}
	})
}
