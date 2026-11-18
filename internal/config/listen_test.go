package config_test

import (
	"strings"
	"testing"
)

const listenBase = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
`

func TestLoad_ListenAddressDefaultsToAllInterfaces(t *testing.T) {
	cfg := loadFromString(t, listenBase)
	if got := cfg.ListenAddr(); got != ":8080" {
		t.Errorf("ListenAddr() = %q, want :8080", got)
	}
}

func TestLoad_ListenAddressBindsToGivenIP(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"127.0.0.1", "127.0.0.1:8080"},
		{"::1", "[::1]:8080"},
		{"localhost", "localhost:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			cfg := loadFromString(t, listenBase+"listen_address: \""+tt.addr+"\"\n")
			if got := cfg.ListenAddr(); got != tt.want {
				t.Errorf("ListenAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoad_RejectsInvalidListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr string
	}{
		{"host and port", "127.0.0.1:8080", "the port is set by port"},
		{"hostname", "skipper.lan", "an IP address"},
		{"url", "http://127.0.0.1", "an IP address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStringToConfig(t, listenBase+"listen_address: \""+tt.addr+"\"\n")
			if err == nil {
				t.Fatalf("listen_address %q: expected an error", tt.addr)
			}
			if !strings.Contains(err.Error(), "listen_address") || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to name listen_address and contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_AllowedHostsParsed(t *testing.T) {
	cfg := loadFromString(t, listenBase+"allowed_hosts: [skipper.example.com, \"localhost:8080\", \"[::1]\"]\n")
	want := []string{"skipper.example.com", "localhost:8080", "[::1]"}
	if strings.Join(cfg.AllowedHosts, ",") != strings.Join(want, ",") {
		t.Errorf("AllowedHosts = %v, want %v", cfg.AllowedHosts, want)
	}
}

func TestLoad_RejectsInvalidAllowedHosts(t *testing.T) {
	tests := []struct {
		name    string
		entry   string
		wantErr string
	}{
		{"empty", "", "is empty"},
		{"url", "https://skipper.example.com", "not a URL"},
		{"path", "skipper.example.com/ui", "not a URL"},
		{"wildcard", "*", "wildcards are not supported"},
		{"bad port", "skipper.example.com:http", "port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStringToConfig(t, listenBase+"allowed_hosts: [\""+tt.entry+"\"]\n")
			if err == nil {
				t.Fatalf("allowed_hosts entry %q: expected an error", tt.entry)
			}
			if !strings.Contains(err.Error(), "allowed_hosts[0]") || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to name allowed_hosts[0] and contain %q", err, tt.wantErr)
			}
		})
	}
}
