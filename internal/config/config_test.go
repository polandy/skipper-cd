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

func TestLoad_MissingStacksBaseDir(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks:
  - name: gitea
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error when stacks_base_dir is absent")
	}
}

func TestStack_Dir_DerivedFromBaseDir(t *testing.T) {
	s := config.Stack{Name: "gitea"}
	got := s.Dir("/var/lib/skipper/repo/modules")
	if got != "/var/lib/skipper/repo/modules/gitea" {
		t.Errorf("expected /var/lib/skipper/repo/modules/gitea, got %s", got)
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
