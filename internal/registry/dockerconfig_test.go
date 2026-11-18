package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDockerConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDockerConfigCredentials(t *testing.T) {
	path := writeDockerConfig(t, `{
	  "auths": {
	    "ghcr.io": {"auth": "YWxpY2U6czNjcmV0"},
	    "registry.example.com:5000": {"username": "bob", "password": "pw"},
	    "https://index.docker.io/v1/": {"auth": "aHViOmh1YnB3"}
	  }
	}`)
	creds := DockerConfigCredentials(path)

	tests := []struct {
		host       string
		user, pass string
		ok         bool
	}{
		{"ghcr.io", "alice", "s3cret", true},
		{"registry.example.com:5000", "bob", "pw", true},
		// Docker Hub credentials live under the legacy index URL key, but the
		// API host the client asks for is registry-1.docker.io.
		{"registry-1.docker.io", "hub", "hubpw", true},
		{"unknown.example.com", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			user, pass, ok := creds(tt.host)
			if user != tt.user || pass != tt.pass || ok != tt.ok {
				t.Errorf("creds(%q) = %q/%q/%v, want %q/%q/%v", tt.host, user, pass, ok, tt.user, tt.pass, tt.ok)
			}
		})
	}
}

func TestDockerConfigCredentials_MissingOrBroken(t *testing.T) {
	for name, path := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "nope", "config.json"),
		"broken":  writeDockerConfig(t, "{not json"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := DockerConfigCredentials(path)("ghcr.io"); ok {
				t.Error("expected anonymous for", name)
			}
		})
	}
}
