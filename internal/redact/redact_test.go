package redact

import (
	"sync"
	"testing"
)

func TestText_MasksURLCredentials(t *testing.T) {
	var s *Secrets // a nil set still masks URL credentials
	tests := []struct {
		name, in, want string
	}{
		{"user and password", "fatal: unable to access 'https://bot:s3cr3t@git.example/r.git/'", "fatal: unable to access 'https://bot:xxxxx@git.example/r.git/'"},
		{"bare token", "remote https://ghp_abc123@github.com/o/r.git", "remote https://xxxxx@github.com/o/r.git"},
		{"dsn in a value", "dial postgres://app:hunter2@db:5432/app failed", "dial postgres://app:xxxxx@db:5432/app failed"},
		{"ssh login kept", "ssh://git@host/r.git: permission denied", "ssh://git@host/r.git: permission denied"},
		{"ssh password masked", "ssh://git:pw@host/r.git", "ssh://git:xxxxx@host/r.git"},
		{"no userinfo", "pull https://registry.example/v2/ failed", "pull https://registry.example/v2/ failed"},
		{"email is not a url", "contact ops@example.com", "contact ops@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Text(tt.in); got != tt.want {
				t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAddEnv_MasksValuesOfSecretKeysOnly(t *testing.T) {
	s := New()
	s.AddEnv([]string{
		"DB_PASSWORD=hunter2hunter2",
		"API_TOKEN=tok-123456",
		"Github_Secret=abcdef",
		"IMAGE_TAG=1.2.3-alpine",
		"PORT=8080",
		"SHORT_KEY=abcde", // under minSecretLen: masking it would mangle unrelated text
		"NOEQUALS",
	})
	in := "auth hunter2hunter2 tok-123456 abcdef failed for nginx:1.2.3-alpine on 8080 abcde"
	want := "auth *** *** *** failed for nginx:1.2.3-alpine on 8080 abcde"
	if got := s.Text(in); got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

func TestAddEnv_SkipsLookalikeKeysAndPlainValues(t *testing.T) {
	s := New()
	s.AddEnv([]string{
		"GIT_AUTHOR_NAME=Andy Pollari",    // AUTH inside a word, not a credential
		"KEYCLOAK_URL=https://kc.example", // KEY starts a word, not a key
		"SMTP_AUTH=enabled",               // a switch, not a secret
		"API_KEY_ROTATE_DAYS=900000",      // a number
		"BASICAUTH=user-pass-pair",        // a word ending in AUTH
		"OIDC_CLIENTKEY=client-key-value", // a word ending in KEY
	})
	in := "Andy Pollari https://kc.example enabled 900000 user-pass-pair client-key-value"
	want := "Andy Pollari https://kc.example enabled 900000 *** ***"
	if got := s.Text(in); got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

func TestAddEnv_LongestSecretMaskedFirst(t *testing.T) {
	s := New()
	s.AddEnv([]string{"A_TOKEN=abcdef", "B_TOKEN=abcdefgh"})
	if got, want := s.Text("x abcdefgh y"), "x *** y"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

func TestSecrets_ConcurrentAddAndText(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); s.AddEnv([]string{"X_SECRET=value-12345"}) }()
		go func() { defer wg.Done(); _ = s.Text("value-12345") }()
	}
	wg.Wait()
	if got := s.Text("value-12345"); got != Mask {
		t.Errorf("Text = %q, want %q", got, Mask)
	}
}
