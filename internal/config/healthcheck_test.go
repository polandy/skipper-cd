package config_test

import (
	"strings"
	"testing"
)

func TestLoad_HealthCheckDefaults(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: whoami
    deploy_health_check: {}
`
	cfg := loadFromString(t, content)

	hc := cfg.Stacks[0].DeployHealthCheck
	if hc == nil {
		t.Fatal("expected deploy_health_check to be set")
	}
	if hc.TimeoutSeconds != 60 {
		t.Errorf("expected default timeout_seconds 60, got %d", hc.TimeoutSeconds)
	}
	if hc.URL != "" {
		t.Errorf("expected empty default url, got %q", hc.URL)
	}
}

func TestLoad_HealthCheckExplicitValues(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: whoami
    deploy_health_check:
      timeout_seconds: 120
      url: http://localhost:8080/health
`
	cfg := loadFromString(t, content)

	hc := cfg.Stacks[0].DeployHealthCheck
	if hc.TimeoutSeconds != 120 {
		t.Errorf("expected timeout_seconds 120, got %d", hc.TimeoutSeconds)
	}
	if hc.URL != "http://localhost:8080/health" {
		t.Errorf("unexpected url %q", hc.URL)
	}
}

func TestLoad_HealthCheckOmittedIsNil(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: whoami
`
	cfg := loadFromString(t, content)

	if cfg.Stacks[0].DeployHealthCheck != nil {
		t.Errorf("expected nil deploy_health_check when omitted, got %+v", cfg.Stacks[0].DeployHealthCheck)
	}
}

func TestLoad_HealthCheckValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "non-http url",
			yaml:    "    deploy_health_check:\n      url: ftp://example.com/x\n",
			wantErr: "valid http(s) URL",
		},
		{
			name:    "negative timeout",
			yaml:    "    deploy_health_check:\n      timeout_seconds: -5\n",
			wantErr: "timeout_seconds must not be negative",
		},
	}

	const base = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: whoami
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
			if !strings.Contains(err.Error(), `stack "whoami"`) {
				t.Errorf("expected error to name the stack, got %q", err)
			}
		})
	}
}
