package config_test

import (
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestLoad_NotificationDefaults(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
notifications:
  - url: https://ntfy.example.com/skipper
`
	cfg := loadFromString(t, content)

	if len(cfg.Notifications) != 1 {
		t.Fatalf("expected 1 notification target, got %d", len(cfg.Notifications))
	}
	got := cfg.Notifications[0]
	if got.Format != config.NotifyFormatGeneric {
		t.Errorf("expected default format %q, got %q", config.NotifyFormatGeneric, got.Format)
	}
	want := []string{config.NotifyOnFailed, config.NotifyOnSuccess, config.NotifyOnRolledBack, config.NotifyOnRolledBackUnhealthy, config.NotifyOnHealExhausted}
	if strings.Join(got.On, ",") != strings.Join(want, ",") {
		t.Errorf("expected default on %v, got %v", want, got.On)
	}
}

func TestLoad_NotificationOnRolledBackUnhealthy(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
notifications:
  - url: https://ntfy.example.com/skipper
    on: [rolled_back_unhealthy]
`
	cfg := loadFromString(t, content)

	got := cfg.Notifications[0]
	if len(got.On) != 1 || got.On[0] != config.NotifyOnRolledBackUnhealthy {
		t.Errorf("expected on [rolled_back_unhealthy], got %v", got.On)
	}
}

func TestLoad_NotificationSignalTarget(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
notifications:
  - format: signal
    url: http://localhost:8020
    number: "+491234567890"
    recipients: ["+491234567890"]
`
	cfg := loadFromString(t, content)

	got := cfg.Notifications[0]
	if got.Number != "+491234567890" || len(got.Recipients) != 1 {
		t.Errorf("signal identity not loaded: %+v", got)
	}
}

func TestLoad_NotificationPrefix(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
notifications:
  - format: signal
    url: http://localhost:8020
    prefix: host-b
    number: "+491234567890"
    recipients: ["+491234567890"]
`
	cfg := loadFromString(t, content)

	if got := cfg.Notifications[0].Prefix; got != "host-b" {
		t.Errorf("prefix = %q, want %q", got, "host-b")
	}
}

func TestLoad_NotificationValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "missing url",
			yaml:    "  - format: generic\n",
			wantErr: "url is required",
		},
		{
			name:    "non-http url",
			yaml:    "  - format: generic\n    url: ftp://example.com/x\n",
			wantErr: "valid http(s) URL",
		},
		{
			name:    "unknown format",
			yaml:    "  - format: telegram\n    url: https://example.com/x\n",
			wantErr: `unknown format "telegram"`,
		},
		{
			name:    "unknown on value",
			yaml:    "  - format: generic\n    url: https://example.com/x\n    on: [deploying]\n",
			wantErr: `unknown on value "deploying"`,
		},
		{
			name:    "signal without number",
			yaml:    "  - format: signal\n    url: http://localhost:8020\n    recipients: [\"+49\"]\n",
			wantErr: "signal format requires number",
		},
		{
			name:    "signal without recipients",
			yaml:    "  - format: signal\n    url: http://localhost:8020\n    number: \"+49\"\n",
			wantErr: "signal format requires at least one recipient",
		},
		{
			name:    "number on non-signal format",
			yaml:    "  - format: generic\n    url: https://example.com/x\n    number: \"+49\"\n",
			wantErr: "only valid for the signal format",
		},
	}

	const base = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
notifications:
`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStringToConfig(t, base+tt.yaml)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err)
			}
		})
	}
}
