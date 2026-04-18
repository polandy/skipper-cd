package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/polandy/orpheus-cd/internal/config"
)

func TestLoad_ValidConfig(t *testing.T) {
	content := `
repo_path: /etc/nixos
stacks_base_dir: /etc/nixos/modules
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

	if cfg.RepoPath != "/etc/nixos" {
		t.Errorf("expected repo_path /etc/nixos, got %s", cfg.RepoPath)
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
repo_path: /etc/nixos
stacks_base_dir: /etc/nixos/modules
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

func TestLoad_MissingRepoPath(t *testing.T) {
	content := `
stacks_base_dir: /etc/nixos/modules
stacks: []
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for missing repo_path, got nil")
	}
}

func TestLoad_MissingWorkingDirWithoutBaseDir(t *testing.T) {
	content := `
repo_path: /etc/nixos
stacks:
  - name: gitea
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error when working_dir and stacks_base_dir are both absent")
	}
}

func TestStack_WorkDir_ExplicitOverridesBaseDir(t *testing.T) {
	s := config.Stack{
		Name:       "gitea",
		WorkingDir: "/custom/gitea",
	}
	got := s.WorkDir("/etc/nixos/modules")
	if got != "/custom/gitea" {
		t.Errorf("expected /custom/gitea, got %s", got)
	}
}

func TestStack_WorkDir_DerivedFromBaseDir(t *testing.T) {
	s := config.Stack{Name: "gitea"}
	got := s.WorkDir("/etc/nixos/modules")
	if got != "/etc/nixos/modules/gitea" {
		t.Errorf("expected /etc/nixos/modules/gitea, got %s", got)
	}
}

// loadFromString is a test helper that writes content to a temp file and loads it.
// It calls t.Fatal on any error, keeping test bodies clean.
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
	path := filepath.Join(t.TempDir(), "orpheus.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return config.Load(path)
}
