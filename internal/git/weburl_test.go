package git

import "testing"

func TestWebURL(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
		want     string
	}{
		{"https with .git suffix", "https://forge.example.com/owner/repo.git", "https://forge.example.com/owner/repo"},
		{"https without suffix", "https://forge.example.com/owner/repo", "https://forge.example.com/owner/repo"},
		{"https keeps a custom web port", "https://forge.example.com:3000/owner/repo.git", "https://forge.example.com:3000/owner/repo"},
		{"http stays http", "http://forge.example.com/owner/repo.git", "http://forge.example.com/owner/repo"},
		{"trailing slash trimmed", "https://forge.example.com/owner/repo/", "https://forge.example.com/owner/repo"},

		// Credentials must never survive into a browse URL the UI links to.
		{"token userinfo stripped", "https://token@forge.example.com/owner/repo.git", "https://forge.example.com/owner/repo"},
		{"user and password stripped", "https://user:s3cr3t@forge.example.com/owner/repo.git", "https://forge.example.com/owner/repo"},
		{"query and fragment dropped", "https://forge.example.com/owner/repo.git?ref=x#frag", "https://forge.example.com/owner/repo"},

		// SSH forms map to https — a forge serves its web UI there, and the SSH
		// port says nothing about the web port.
		{"scp-like", "git@forge.example.com:owner/repo.git", "https://forge.example.com/owner/repo"},
		{"scp-like nested path", "git@forge.example.com:group/sub/repo.git", "https://forge.example.com/group/sub/repo"},
		{"ssh scheme", "ssh://git@forge.example.com/owner/repo.git", "https://forge.example.com/owner/repo"},
		{"ssh scheme with port", "ssh://git@forge.example.com:2222/owner/repo.git", "https://forge.example.com/owner/repo"},
		{"git scheme", "git://forge.example.com/owner/repo.git", "https://forge.example.com/owner/repo"},

		// No browse URL derivable — the caller omits the link instead of guessing.
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"absolute local path", "/srv/git/repo.git", ""},
		{"relative local path", "../repo", ""},
		{"local path containing a colon", "/srv/git/mirrors:old/repo.git", ""},
		{"file scheme", "file:///srv/git/repo.git", ""},
		{"file scheme with a host", "file://nas.example.com/srv/git/repo.git", ""},
		{"unknown scheme with a host", "ftp://forge.example.com/owner/repo.git", ""},
		{"scp-like without a path", "git@forge.example.com:", ""},
		{"scp-like without a host", ":owner/repo.git", ""},
		{"no path", "https://forge.example.com", ""},
		{"no path but slash", "https://forge.example.com/", ""},
		{"scheme without host", "https:///owner/repo", ""},
		{"bare .git path", "https://forge.example.com/.git", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WebURL(tt.cloneURL); got != tt.want {
				t.Errorf("WebURL(%q) = %q, want %q", tt.cloneURL, got, tt.want)
			}
		})
	}
}
