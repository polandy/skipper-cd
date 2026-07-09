package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestLoad_ValidConfig(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
webhook_secret: secret123
port: 9090
metrics_port: 9999
stacks:
  - name: gitea
    env_files:
      - /run/secrets/compose.env
  - name: traefik
    working_dir: /custom/path
`
	cfg := loadFromString(t, content)

	if cfg.RepoURL != "ssh://git@gitea.example.com/user/nixos.git" {
		t.Errorf("unexpected repo_url: %s", cfg.RepoURL)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if len(cfg.Stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(cfg.Stacks))
	}
}

func TestLoad_DefaultPorts(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.MetricsPort != 9120 {
		t.Errorf("expected default metrics_port 9120, got %d", cfg.MetricsPort)
	}
}

func TestLoad_DefaultTimeoutAndBranch(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.CommandTimeoutSeconds != 300 {
		t.Errorf("expected default command_timeout_seconds 300, got %d", cfg.CommandTimeoutSeconds)
	}
	if cfg.Branch != "main" {
		t.Errorf("expected default branch 'main', got %q", cfg.Branch)
	}
}

func TestLoad_CustomTimeoutAndBranch(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
command_timeout_seconds: 600
branch: main
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.CommandTimeoutSeconds != 600 {
		t.Errorf("expected command_timeout_seconds 600, got %d", cfg.CommandTimeoutSeconds)
	}
	if cfg.Branch != "main" {
		t.Errorf("expected branch 'main', got %q", cfg.Branch)
	}
}

func TestLoad_MissingRepoURL(t *testing.T) {
	content := `
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for missing repo_url, got nil")
	}
}

func TestLoad_MissingWorkingDirWithoutBaseDir(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks:
  - name: gitea
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error when working_dir and stacks_base_dir are both absent")
	}
}

func TestLoad_RejectsDuplicateStackNames(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: gitea
  - name: gitea
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for duplicate stack names, got nil")
	}
}

func TestLoad_RejectsReservedStackName(t *testing.T) {
	// "_nixos" is the reserved state key for NixOS rebuild hashes.
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: _nixos
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for reserved stack name _nixos, got nil")
	}
}

func TestLoad_RejectsEmptyStackName(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - env_files: [/etc/foo.env]
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for stack without name, got nil")
	}
}

func TestLoad_LogFormatDefaultsToText(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.LogFormat != config.LogFormatText {
		t.Errorf("expected default log_format %q, got %q", config.LogFormatText, cfg.LogFormat)
	}
}

func TestLoad_LogFormatJSON(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
log_format: json
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.LogFormat != config.LogFormatJSON {
		t.Errorf("expected log_format %q, got %q", config.LogFormatJSON, cfg.LogFormat)
	}
}

func TestLoad_RejectsUnknownLogFormat(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
log_format: xml
stacks: []
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for unknown log_format, got nil")
	}
}

func TestLoad_NixOSRebuild_OmittedSectionIsDisabled(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.NixOSRebuild.IsEnabled() {
		t.Error("expected NixOSRebuild to be disabled when section is omitted")
	}
}

func TestLoad_NixOSRebuild_EnabledWithFlake(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
nixos_rebuild:
  flake: ".#nuc"
`
	cfg := loadFromString(t, content)

	if !cfg.NixOSRebuild.IsEnabled() {
		t.Error("expected NixOSRebuild to be enabled")
	}
	if cfg.NixOSRebuild.Flake != ".#nuc" {
		t.Errorf("expected flake '.#nuc', got %q", cfg.NixOSRebuild.Flake)
	}
}

func TestLoad_NixOSRebuild_ExplicitlyDisabled(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
nixos_rebuild:
  enabled: false
  flake: ".#nuc"
`
	cfg := loadFromString(t, content)

	if cfg.NixOSRebuild.IsEnabled() {
		t.Error("expected NixOSRebuild to be disabled when enabled: false")
	}
}

func TestLoad_NixOSRebuild_MissingFlakeErrors(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
nixos_rebuild: {}
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error when nixos_rebuild is enabled but flake is missing")
	}
}

func loadFromString(t *testing.T, content string) *config.Config {
	t.Helper()
	cfg, err := loadStringToConfig(t, content)
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}
	return cfg
}

func loadStringToConfig(t *testing.T, content string) (*config.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "skipper.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return config.Load(path)
}
