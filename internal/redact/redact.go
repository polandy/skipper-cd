// Package redact masks secrets out of the text skipper publishes (events, /api/logs); see ADR-0070.
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

// urlPasswordMask replaces URL credentials, as net/url's Redacted does.
const urlPasswordMask = "xxxxx"

// minSecretLen is the shortest value registered; shorter ones would mangle unrelated text.
const minSecretLen = 6

// sshScheme carries a login name, not a credential, as a lone userinfo.
const sshScheme = "ssh"

var (
	// secretWord matches a "_"-separated key word naming a secret (API_KEY yes, KEYCLOAK_URL no).
	secretWord = regexp.MustCompile(`(?i)PASS|SECRET|TOKEN|CRED|PRIVATE|SALT|DSN|(KEY|AUTH)$`)
	// plainValue matches a value that is a number or a switch, never a secret.
	plainValue = regexp.MustCompile(`(?i)^([0-9]+|true|false|yes|no|on|off|enabled|disabled)$`)
	// urlUserinfo matches scheme://user[:password]@ in free text.
	urlUserinfo = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9+.-]*)://([^\s/@:'"]+)(:[^\s/@'"]*)?@`)
)

// Secrets is a concurrency-safe set of values to mask; a nil *Secrets masks URL credentials only.
type Secrets struct {
	mu       sync.RWMutex
	values   map[string]struct{}
	replacer *strings.Replacer
}

// New returns an empty set.
func New() *Secrets {
	return &Secrets{values: map[string]struct{}{}}
}

// AddEnv registers the secret values of KEY=VALUE entries; none is ever removed.
func (s *Secrets) AddEnv(env []string) {
	var fresh []string
	s.mu.RLock()
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !isSecret(k, v) {
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

// isSecret reports whether the value of env key k is masked.
func isSecret(k, v string) bool {
	if len(v) < minSecretLen || plainValue.MatchString(v) {
		return false
	}
	return slices.ContainsFunc(strings.Split(k, "_"), secretWord.MatchString)
}

// newReplacer masks the longest value first, so a secret containing a shorter one is masked whole.
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

// Text returns text with every registered value and URL credential masked.
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

// maskURLUserinfo masks a password, and a lone user (a token) except on ssh:// — git.RedactURL's rule.
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
