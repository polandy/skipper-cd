package config_test

import (
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestLoad_HealthCheckDefaults(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
stacks:
  - name: whoami
`
	cfg := loadFromString(t, content)

	if cfg.Stacks[0].DeployHealthCheck != nil {
		t.Errorf("expected nil deploy_health_check when omitted, got %+v", cfg.Stacks[0].DeployHealthCheck)
	}
}

func TestLoad_HealthCheckDisabledScalar(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks:
  - name: whoami
    deploy_health_check: false
`
	cfg := loadFromString(t, content)

	hc := cfg.Stacks[0].DeployHealthCheck
	if hc == nil {
		t.Fatal("expected deploy_health_check to be set (explicit off), got nil")
	}
	if !hc.IsDisabled() {
		t.Errorf("expected deploy_health_check: false to be disabled, got %+v", hc)
	}
}

func TestLoad_HealthCheckEnabledScalar(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks:
  - name: whoami
    deploy_health_check: true
`
	cfg := loadFromString(t, content)

	hc := cfg.Stacks[0].DeployHealthCheck
	if hc == nil {
		t.Fatal("expected deploy_health_check to be set, got nil")
	}
	if hc.IsDisabled() {
		t.Errorf("expected deploy_health_check: true to be enabled, got disabled")
	}
	if hc.TimeoutSeconds != 60 {
		t.Errorf("expected default timeout_seconds 60 for the enabled scalar, got %d", hc.TimeoutSeconds)
	}
}

// A mapping without an explicit off-switch is enabled (the pre-existing shape).
func TestLoad_HealthCheckMappingIsNotDisabled(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks:
  - name: whoami
    deploy_health_check: {}
`
	cfg := loadFromString(t, content)

	if cfg.Stacks[0].DeployHealthCheck.IsDisabled() {
		t.Error("expected an empty-mapping deploy_health_check to be enabled, got disabled")
	}
}

// A nil deploy_health_check (absent) is not "disabled" — absence means the
// automatic compose-healthcheck gate (ADR-0046) may still apply.
func TestHealthCheck_NilIsNotDisabled(t *testing.T) {
	var hc *config.HealthCheck
	if hc.IsDisabled() {
		t.Error("expected a nil deploy_health_check to report not-disabled")
	}
}

// A null/empty deploy_health_check value must stay nil (absent), NOT decode
// into a disabled gate: yaml.v3 skips UnmarshalYAML for a null value on a
// pointer field, so absence keeps the automatic gate free to apply. Locks in
// that subtlety against a refactor of the custom UnmarshalYAML.
func TestLoad_HealthCheckNullIsAbsent(t *testing.T) {
	for _, val := range []string{"", " null", " ~"} {
		content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks:
  - name: whoami
    deploy_health_check:` + val + "\n"
		cfg := loadFromString(t, content)
		if hc := cfg.Stacks[0].DeployHealthCheck; hc != nil {
			t.Errorf("deploy_health_check:%q — expected nil (absent), got %+v (IsDisabled=%v)", val, hc, hc.IsDisabled())
		}
	}
}

func TestLoad_HealthCheckScalarRejectsNonBool(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks:
  - name: whoami
    deploy_health_check: "sometimes"
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Fatal("expected an error for a non-boolean scalar deploy_health_check, got nil")
	}
	if !strings.Contains(err.Error(), "deploy_health_check") {
		t.Errorf("expected the error to name deploy_health_check, got %q", err)
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
stacks_base_dir: modules
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
