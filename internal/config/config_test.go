package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/ui"
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

func TestLoad_RejectsPortOutOfRange(t *testing.T) {
	for _, port := range []int{0, -1, 65536, 99999} {
		if _, err := loadStringToConfig(t, minimalConfig+fmt.Sprintf("port: %d\n", port)); err == nil {
			t.Errorf("port %d: expected an out-of-range error, got nil", port)
		}
	}
}

func TestLoad_RejectsMetricsPortOutOfRange(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		if _, err := loadStringToConfig(t, minimalConfig+fmt.Sprintf("metrics_port: %d\n", port)); err == nil {
			t.Errorf("metrics_port %d: expected an out-of-range error, got nil", port)
		}
	}
}

func TestLoad_RejectsPortEqualToMetricsPort(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+"port: 8080\nmetrics_port: 8080\n")
	if err == nil {
		t.Fatal("expected an error when port equals metrics_port")
	}
}

func TestLoad_RejectsNegativeCommandTimeout(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+"command_timeout_seconds: -1\n")
	if err == nil {
		t.Fatal("expected an error for a negative command_timeout_seconds")
	}
}

func TestLoad_UIEnabledDefaultsTrue(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.UIEnabled == nil || !*cfg.UIEnabled {
		t.Errorf("expected ui_enabled to default to true, got %v", cfg.UIEnabled)
	}
}

func TestLoad_UIEnabledExplicitFalse(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
ui_enabled: false
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.UIEnabled == nil || *cfg.UIEnabled {
		t.Errorf("expected explicit ui_enabled: false to stick, got %v", cfg.UIEnabled)
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

func TestLoad_LogFormatDefaultsToPretty(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.LogFormat != config.LogFormatPretty {
		t.Errorf("expected default log_format %q, got %q", config.LogFormatPretty, cfg.LogFormat)
	}
}

func TestLoad_LogFormatText(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
log_format: text
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.LogFormat != config.LogFormatText {
		t.Errorf("expected log_format %q, got %q", config.LogFormatText, cfg.LogFormat)
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

func TestLoad_UIThemeDefaultsToCatppuccin(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.UITheme != ui.ThemeCatppuccin {
		t.Errorf("expected default ui_theme %q, got %q", ui.ThemeCatppuccin, cfg.UITheme)
	}
}

func TestLoad_UIThemeAcceptsEveryBuiltInTheme(t *testing.T) {
	for _, theme := range ui.ValidThemes {
		t.Run(theme, func(t *testing.T) {
			content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
ui_theme: ` + theme + `
stacks: []
`
			cfg := loadFromString(t, content)

			if cfg.UITheme != theme {
				t.Errorf("expected ui_theme %q, got %q", theme, cfg.UITheme)
			}
		})
	}
}

func TestLoad_RejectsUnknownUITheme(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
ui_theme: monokai
stacks: []
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for unknown ui_theme, got nil")
	}
}

func TestLoad_UIThemeSwitcherDefaultsToFalse(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.UIThemeSwitcher {
		t.Error("expected ui_theme_switcher to default to false")
	}
}

func TestLoad_UIThemeSwitcherEnabled(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
ui_theme_switcher: true
stacks: []
`
	cfg := loadFromString(t, content)

	if !cfg.UIThemeSwitcher {
		t.Error("expected ui_theme_switcher to be true when set")
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

func TestLoad_IconDefaults(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.Icons.CacheDir != "/var/lib/skipper/icons" {
		t.Errorf("expected default icon cache dir, got %q", cfg.Icons.CacheDir)
	}
	if cfg.Icons.SourceURL == "" {
		t.Error("expected a default icon source URL, got empty")
	}
}

func TestLoad_IconOverrides(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
icons:
  cache_dir: /custom/icons
  source_url: https://icons.example.com/svg
stacks:
  - name: media
    icon: jellyfin
`
	cfg := loadFromString(t, content)

	if cfg.Icons.CacheDir != "/custom/icons" {
		t.Errorf("cache_dir = %q, want /custom/icons", cfg.Icons.CacheDir)
	}
	if cfg.Icons.SourceURL != "https://icons.example.com/svg" {
		t.Errorf("source_url = %q", cfg.Icons.SourceURL)
	}
	if len(cfg.Stacks) != 1 || cfg.Stacks[0].Icon != "jellyfin" {
		t.Errorf("stack icon = %q, want jellyfin", cfg.Stacks[0].Icon)
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
	// webhook_secret is required (push webhooks are the primary trigger). Most
	// tests aren't about it, so inject a default when absent; a test exercising
	// the requirement sets webhook_secret explicitly (e.g. to "").
	if !strings.Contains(content, "webhook_secret") {
		content = "webhook_secret: test-secret\n" + content
	}
	path := filepath.Join(t.TempDir(), "skipper.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return config.Load(path)
}

const minimalConfig = `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
webhook_secret: secret123
`

func TestLoad_RejectsMissingWebhookSecret(t *testing.T) {
	// webhook_secret is required — the webhook is skipper's primary deploy
	// trigger, and an empty secret would leave it unauthenticated.
	_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
webhook_secret: ""
`)
	if err == nil || !strings.Contains(err.Error(), "webhook_secret") {
		t.Fatalf("expected a webhook_secret required error, got %v", err)
	}
}

func TestLoad_HealthPollIntervalDefaultsTo30WhenOmitted(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HealthPollIntervalSeconds == nil {
		t.Fatal("expected default to be applied, got nil")
	}
	if got := *cfg.HealthPollIntervalSeconds; got != 30 {
		t.Errorf("expected default 30, got %d", got)
	}
}

func TestLoad_HealthPollIntervalExplicitZeroIsPreserved(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig+"health_poll_interval_seconds: 0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HealthPollIntervalSeconds == nil || *cfg.HealthPollIntervalSeconds != 0 {
		t.Errorf("expected explicit 0 to be preserved (disabled), got %v", cfg.HealthPollIntervalSeconds)
	}
}

func TestLoad_HealthPollIntervalExplicitValue(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig+"health_poll_interval_seconds: 60\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HealthPollIntervalSeconds == nil || *cfg.HealthPollIntervalSeconds != 60 {
		t.Errorf("expected 60, got %v", cfg.HealthPollIntervalSeconds)
	}
}

func TestLoad_HealthPollIntervalNegativeIsRejected(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+"health_poll_interval_seconds: -5\n")
	if err == nil {
		t.Fatal("expected an error for a negative interval")
	}
}

func TestLoad_ReconcileIntervalDefaultsTo300WhenOmitted(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReconcileIntervalSeconds == nil {
		t.Fatal("expected default to be applied, got nil")
	}
	if got := *cfg.ReconcileIntervalSeconds; got != 300 {
		t.Errorf("expected default 300, got %d", got)
	}
}

func TestLoad_ReconcileIntervalExplicitZeroIsPreserved(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig+"reconcile_interval_seconds: 0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReconcileIntervalSeconds == nil || *cfg.ReconcileIntervalSeconds != 0 {
		t.Errorf("expected explicit 0 to be preserved (disabled), got %v", cfg.ReconcileIntervalSeconds)
	}
}

func TestLoad_ReconcileIntervalExplicitValue(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig+"reconcile_interval_seconds: 120\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReconcileIntervalSeconds == nil || *cfg.ReconcileIntervalSeconds != 120 {
		t.Errorf("expected 120, got %v", cfg.ReconcileIntervalSeconds)
	}
}

func TestLoad_ReconcileIntervalNegativeIsRejected(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+"reconcile_interval_seconds: -5\n")
	if err == nil {
		t.Fatal("expected an error for a negative interval")
	}
}

func TestLoad_RejectsMissingVarsFile(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+"vars_file: /no/such/vars.env\n")
	if err == nil || !strings.Contains(err.Error(), "vars_file") {
		t.Fatalf("expected a vars_file error, got %v", err)
	}
}

func TestLoad_AcceptsExistingVarsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.env")
	if err := os.WriteFile(path, []byte("DOMAIN=example.com\n"), 0o644); err != nil {
		t.Fatalf("failed to write vars file: %v", err)
	}
	cfg, err := loadStringToConfig(t, minimalConfig+"vars_file: "+path+"\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VarsFile != path {
		t.Errorf("expected vars_file %q, got %q", path, cfg.VarsFile)
	}
}

func TestLoad_RejectsRelativeWorkingDir(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: gitea
    working_dir: relative/path
`
	_, err := loadStringToConfig(t, content)
	if err == nil || !strings.Contains(err.Error(), "working_dir must be an absolute path") {
		t.Fatalf("expected a working_dir-must-be-absolute error, got %v", err)
	}
}

func TestLoad_AcceptsAbsoluteWorkingDir(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: gitea
    working_dir: /etc/nixos/modules/gitea
`
	cfg := loadFromString(t, content)
	if cfg.Stacks[0].WorkingDir != "/etc/nixos/modules/gitea" {
		t.Errorf("unexpected working_dir: %s", cfg.Stacks[0].WorkingDir)
	}
}

func TestLoad_WarnsWhenNothingToDeploy(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
`
	cfg := loadFromString(t, content)
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "nothing to deploy") {
		t.Fatalf("expected a single nothing-to-deploy warning, got %v", cfg.Warnings)
	}
}

func TestLoad_NoNothingToDeployWarning_UnderDiscovery(t *testing.T) {
	cfg := loadFromString(t, minimalConfig) // stack_discovery defaults to true, no stacks list
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "nothing to deploy") {
			t.Errorf("did not expect a nothing-to-deploy warning under discovery, got %v", cfg.Warnings)
		}
	}
}

func TestLoad_NoNothingToDeployWarning_WithStacksConfigured(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: gitea
    working_dir: /etc/nixos/modules/gitea
`
	cfg := loadFromString(t, content)
	if len(cfg.Warnings) != 0 {
		t.Errorf("expected no warnings with stacks configured, got %v", cfg.Warnings)
	}
}

func TestLoad_NoNothingToDeployWarning_WithNixOSRebuildEnabled(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
nixos_rebuild:
  flake: ".#nuc"
`
	cfg := loadFromString(t, content)
	if len(cfg.Warnings) != 0 {
		t.Errorf("expected no warnings with nixos_rebuild enabled, got %v", cfg.Warnings)
	}
}
