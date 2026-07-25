package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

// loadStringErr writes content to a temp config file verbatim and returns
// Load's error. Unlike loadStringToConfig it injects nothing, so a test can
// exercise an empty document.
func loadStringErr(t *testing.T, content string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "skipper.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	_, err := config.Load(path)
	return err
}

func TestLoad_RejectsUnknownTopLevelKey(t *testing.T) {
	err := loadStringErr(t, `
repo_url: ssh://git@example.com/user/deploy.git
webhook_secrets: typo-in-this-key
`)
	if err == nil {
		t.Fatal("expected an unknown top-level key to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "webhook_secrets") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

func TestLoad_RejectsUnknownStackKey(t *testing.T) {
	err := loadStringErr(t, `
repo_url: ssh://git@example.com/user/deploy.git
stack_discovery: false
stacks:
  - name: gitea
    env_file: /run/secrets/compose.env
`)
	if err == nil {
		t.Fatal("expected an unknown stack key to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "env_file") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

func TestLoad_RenamedWorkingDirNamesProjectDirectory(t *testing.T) {
	err := loadStringErr(t, `
repo_url: ssh://git@example.com/user/deploy.git
stack_discovery: false
stacks:
  - name: nextcloud
    working_dir: /etc/nixos/modules/nextcloud
`)
	if err == nil {
		t.Fatal("expected the retired working_dir key to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "project_directory") {
		t.Errorf("error should name the replacement key project_directory, got: %v", err)
	}
}

func TestLoad_RenamedHealthCheckNamesDeployHealthCheck(t *testing.T) {
	err := loadStringErr(t, `
repo_url: ssh://git@example.com/user/deploy.git
stack_discovery: false
stacks:
  - name: gitea
    health_check:
      timeout_seconds: 60
`)
	if err == nil {
		t.Fatal("expected the retired health_check key to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "deploy_health_check") {
		t.Errorf("error should name the replacement key deploy_health_check, got: %v", err)
	}
}

func TestLoad_RenamedHealthPollIntervalNamesRuntimeKey(t *testing.T) {
	err := loadStringErr(t, `
repo_url: ssh://git@example.com/user/deploy.git
health_poll_interval_seconds: 30
`)
	if err == nil {
		t.Fatal("expected the retired health_poll_interval_seconds key to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "runtime_health_poll_interval_seconds") {
		t.Errorf("error should name the replacement key, got: %v", err)
	}
}

// A renamed key must be reported by name even when it sits next to an
// otherwise-unknown key, so the operator gets the migration hint rather than a
// bare unknown-field error.
func TestLoad_RenamedKeyReportedBeforeUnknownKey(t *testing.T) {
	err := loadStringErr(t, `
repo_url: ssh://git@example.com/user/deploy.git
health_poll_interval_seconds: 30
totally_unknown: yes
`)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "runtime_health_poll_interval_seconds") {
		t.Errorf("the rename hint should win over the unknown-key error, got: %v", err)
	}
}

// An empty config file must keep loading (defaults only) — strict decoding
// must not turn "nothing to decode" into a parse failure. It still fails
// validation for the missing repo_url, which is the pre-existing behaviour.
func TestLoad_EmptyFileFailsOnValidationNotParsing(t *testing.T) {
	err := loadStringErr(t, "")
	if err == nil {
		t.Fatal("expected an error for a config without repo_url, got nil")
	}
	if strings.Contains(err.Error(), "parse config file") {
		t.Errorf("an empty file must not fail at the parse step, got: %v", err)
	}
}

// The shipped example is the first config most operators ever run. Loading it
// here keeps it from drifting out of sync with the loader (a retired key, or a
// value the validator rejects).
func TestShippedExampleConfigLoads(t *testing.T) {
	cfg, err := config.Load("../../skipper.yml.example")
	if err != nil {
		t.Fatalf("skipper.yml.example must load as shipped: %v", err)
	}
	if len(cfg.Stacks) == 0 {
		t.Error("expected the example to define at least one stack override")
	}
}
