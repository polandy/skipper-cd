package config_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLoad_HostNameDefaultsToOSHostname(t *testing.T) {
	const content = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)
	want, err := os.Hostname()
	if err != nil {
		t.Skipf("os.Hostname() failed: %v", err)
	}
	if cfg.HostName != want {
		t.Errorf("HostName = %q, want OS hostname %q", cfg.HostName, want)
	}
}

func TestLoad_HostNameOverride(t *testing.T) {
	const content = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
host_name: host-a
`
	cfg := loadFromString(t, content)
	if cfg.HostName != "host-a" {
		t.Errorf("HostName = %q, want host-a", cfg.HostName)
	}
}

func TestLoad_PeersParsed(t *testing.T) {
	const content = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
peers:
  - name: host-b
    url: http://host-b:8001
  - name: host-c
    url: http://host-c:8001
`
	cfg := loadFromString(t, content)
	if len(cfg.Peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(cfg.Peers))
	}
	if cfg.Peers[0].Name != "host-b" || cfg.Peers[0].URL != "http://host-b:8001" {
		t.Errorf("peer[0] = %+v, want {host-b, http://host-b:8001}", cfg.Peers[0])
	}
	if cfg.Peers[1].Name != "host-c" {
		t.Errorf("peer[1].Name = %q, want host-c", cfg.Peers[1].Name)
	}
}

func TestLoad_PeersOmittedIsEmpty(t *testing.T) {
	const content = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)
	if len(cfg.Peers) != 0 {
		t.Fatalf("expected no peers when omitted, got %d", len(cfg.Peers))
	}
}

func TestLoad_WarnsWhenMoreHostsThanColours(t *testing.T) {
	// This host + 6 peers = 7 hosts, past the 6-slot host-colour palette.
	var b strings.Builder
	b.WriteString(`
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
peers:
`)
	for i := range 6 {
		fmt.Fprintf(&b, "  - name: host-%d\n    url: http://host-%d:8001\n", i, i)
	}
	cfg := loadFromString(t, b.String())

	var found bool
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "distinct host colours") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a host-colour palette warning with 7 hosts, got %v", cfg.Warnings)
	}
}

func TestLoad_NoColourWarningWithinPalette(t *testing.T) {
	// This host + 2 peers = 3 hosts, well within the palette — no warning.
	const content = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
peers:
  - name: host-b
    url: http://host-b:8001
  - name: host-c
    url: http://host-c:8001
`
	cfg := loadFromString(t, content)
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "distinct host colours") {
			t.Errorf("unexpected colour warning within palette: %q", w)
		}
	}
}

func TestLoad_PeersValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "missing name",
			yaml:    "  - url: http://host-b:8001\n",
			wantErr: "name is required",
		},
		{
			name:    "missing url",
			yaml:    "  - name: host-b\n",
			wantErr: "url is required",
		},
		{
			name:    "non-http url",
			yaml:    "  - name: host-b\n    url: ftp://host-b/x\n",
			wantErr: "valid http(s) URL",
		},
		{
			name:    "duplicate name",
			yaml:    "  - name: host-b\n    url: http://host-b:8001\n  - name: host-b\n    url: http://other:8001\n",
			wantErr: "duplicate peer name",
		},
	}

	const base = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
peers:
`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStringToConfig(t, base+tt.yaml)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
