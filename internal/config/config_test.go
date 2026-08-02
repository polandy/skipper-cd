package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/uitheme"
)

func TestLoad_ValidConfig(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: secret123
port: 9090
metrics_port: 9999
stacks:
  - name: gitea
    env_files:
      - /run/secrets/compose.env
  - name: traefik
    project_directory: /custom/path
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
stacks: []
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for missing repo_url, got nil")
	}
}

func TestLoad_AllowsOmittedProjectDirectoryAndStacksBaseDir(t *testing.T) {
	// With both omitted the stack's compose file is located at the repo root
	// (<repo_dir>/<name>/docker-compose.yml) — a valid repo-root layout, no
	// project_directory needed.
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: gitea
`
	cfg, err := loadStringToConfig(t, content)
	if err != nil {
		t.Fatalf("omitting both project_directory and stacks_base_dir should be allowed: %v", err)
	}
	if cfg.StacksBaseDir != git.DefaultRepoDir {
		t.Errorf("expected stacks_base_dir to resolve to the repo root %q, got %q", git.DefaultRepoDir, cfg.StacksBaseDir)
	}
}

func TestLoad_RejectsDuplicateStackNames(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
log_format: xml
stacks: []
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Error("expected error for unknown log_format, got nil")
	}
}

func TestLoad_LogLevelDefaultsToInfo(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.LogLevel != config.LogLevelInfo {
		t.Errorf("expected default log_level %q, got %q", config.LogLevelInfo, cfg.LogLevel)
	}
}

func TestLoad_LogLevelDebug(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
log_level: debug
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.LogLevel != config.LogLevelDebug {
		t.Errorf("expected log_level %q, got %q", config.LogLevelDebug, cfg.LogLevel)
	}
}

func TestLoad_RejectsUnknownLogLevel(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
log_level: verbose
stacks: []
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Fatal("expected error for unknown log_level, got nil")
	}
	// The message must name the key and the accepted values, not just complain.
	for _, want := range []string{"log_level", "debug", "verbose"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to mention %q, got: %v", want, err)
		}
	}
}

func TestLoad_UIThemeDefaultsToCatppuccin(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
`
	cfg := loadFromString(t, content)

	if cfg.UITheme != uitheme.ThemeCatppuccin {
		t.Errorf("expected default ui_theme %q, got %q", uitheme.ThemeCatppuccin, cfg.UITheme)
	}
}

func TestLoad_UIThemeAcceptsEveryBuiltInTheme(t *testing.T) {
	for _, theme := range uitheme.ValidThemes {
		t.Run(theme, func(t *testing.T) {
			content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
stacks: []
nixos_rebuild:
  flake: ".#host-a"
`
	cfg := loadFromString(t, content)

	if !cfg.NixOSRebuild.IsEnabled() {
		t.Error("expected NixOSRebuild to be enabled")
	}
	if cfg.NixOSRebuild.Flake != ".#host-a" {
		t.Errorf("expected flake '.#host-a', got %q", cfg.NixOSRebuild.Flake)
	}
}

func TestLoad_NixOSRebuild_ExplicitlyDisabled(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
nixos_rebuild:
  enabled: false
  flake: ".#host-a"
`
	cfg := loadFromString(t, content)

	if cfg.NixOSRebuild.IsEnabled() {
		t.Error("expected NixOSRebuild to be disabled when enabled: false")
	}
}

func TestLoad_NixOSRebuild_MissingFlakeErrors(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
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
stacks_base_dir: modules
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
stacks_base_dir: modules
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
	// webhook_secret is optional, but most tests want a working push endpoint and
	// aren't about it, so inject a default when absent; a test exercising the
	// empty-secret behaviour sets webhook_secret explicitly (e.g. to "").
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
stacks_base_dir: modules
webhook_secret: secret123
`

func TestLoad_AllowsEmptyWebhookSecretWhenReconcileConverges(t *testing.T) {
	// An empty webhook_secret is valid: it disables the /webhook endpoint, and
	// reconcile (on by default at 300s) remains the convergence baseline.
	cfg, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: ""
`)
	if err != nil {
		t.Fatalf("empty webhook_secret with reconcile on should load, got %v", err)
	}
	if cfg.WebhookSecret != "" {
		t.Fatalf("expected empty webhook_secret, got %q", cfg.WebhookSecret)
	}
	if cfg.ReconcileIntervalSeconds == nil || *cfg.ReconcileIntervalSeconds != 300 {
		t.Fatalf("expected reconcile default 300, got %v", cfg.ReconcileIntervalSeconds)
	}
}

func TestLoad_AllowsEmptyWebhookSecretWithExplicitReconcile(t *testing.T) {
	// Empty secret paired with an explicit positive reconcile interval is the
	// canonical reconcile-only setup and must load.
	if _, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: ""
reconcile_interval_seconds: 120
`); err != nil {
		t.Fatalf("empty webhook_secret with reconcile_interval_seconds: 120 should load, got %v", err)
	}
}

func TestLoad_RejectsEmptyWebhookSecretWithReconcileDisabled(t *testing.T) {
	// The dead combination: no webhook (empty secret disables the endpoint) AND
	// reconcile off means nothing deploys past the startup sync — reject it.
	_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: ""
reconcile_interval_seconds: 0
`)
	if err == nil {
		t.Fatal("expected an error for empty webhook_secret + reconcile disabled, got nil")
	}
	for _, want := range []string{"webhook_secret", "reconcile_interval_seconds"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should name %q, got %v", want, err)
		}
	}
}

func TestLoad_HealthPollIntervalDefaultsTo30WhenOmitted(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RuntimeHealthPollIntervalSeconds == nil {
		t.Fatal("expected default to be applied, got nil")
	}
	if got := *cfg.RuntimeHealthPollIntervalSeconds; got != 30 {
		t.Errorf("expected default 30, got %d", got)
	}
}

func TestLoad_HealthPollIntervalExplicitZeroIsPreserved(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig+"runtime_health_poll_interval_seconds: 0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RuntimeHealthPollIntervalSeconds == nil || *cfg.RuntimeHealthPollIntervalSeconds != 0 {
		t.Errorf("expected explicit 0 to be preserved (disabled), got %v", cfg.RuntimeHealthPollIntervalSeconds)
	}
}

func TestLoad_HealthPollIntervalExplicitValue(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig+"runtime_health_poll_interval_seconds: 60\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RuntimeHealthPollIntervalSeconds == nil || *cfg.RuntimeHealthPollIntervalSeconds != 60 {
		t.Errorf("expected 60, got %v", cfg.RuntimeHealthPollIntervalSeconds)
	}
}

func TestLoad_HealthPollIntervalNegativeIsRejected(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+"runtime_health_poll_interval_seconds: -5\n")
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

func TestLoad_RejectsRelativeProjectDirectory(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks:
  - name: gitea
    project_directory: relative/path
`
	_, err := loadStringToConfig(t, content)
	if err == nil || !strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("expected a project_directory-must-be-absolute error, got %v", err)
	}
}

func TestLoad_AcceptsAbsoluteProjectDirectory(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: gitea
    project_directory: /etc/nixos/modules/gitea
`
	cfg := loadFromString(t, content)
	if cfg.Stacks[0].ProjectDirectory != "/etc/nixos/modules/gitea" {
		t.Errorf("unexpected project_directory: %s", cfg.Stacks[0].ProjectDirectory)
	}
}

func TestLoad_ProjectDirectoryBaseDerivesPerStackProjectDirectory(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
project_directory_base: /etc/nixos/modules
stacks:
  - name: gitea
  - name: nextcloud
`
	cfg := loadFromString(t, content)
	if got, want := cfg.Stacks[0].ProjectDirectory, "/etc/nixos/modules/gitea"; got != want {
		t.Errorf("stack gitea: expected project_directory %q, got %q", want, got)
	}
	if got, want := cfg.Stacks[1].ProjectDirectory, "/etc/nixos/modules/nextcloud"; got != want {
		t.Errorf("stack nextcloud: expected project_directory %q, got %q", want, got)
	}
}

func TestLoad_ExplicitProjectDirectoryOverridesProjectDirectoryBase(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
project_directory_base: /etc/nixos/modules
stacks:
  - name: gitea
    project_directory: /srv/custom/gitea
`
	cfg := loadFromString(t, content)
	if got, want := cfg.Stacks[0].ProjectDirectory, "/srv/custom/gitea"; got != want {
		t.Errorf("expected explicit project_directory to win, got %q, want %q", got, want)
	}
}

func TestLoad_RejectsRelativeProjectDirectoryBase(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
project_directory_base: relative/modules
`
	_, err := loadStringToConfig(t, content)
	if err == nil || !strings.Contains(err.Error(), "project_directory_base") || !strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("expected a project_directory_base-must-be-absolute error, got %v", err)
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
    project_directory: /etc/nixos/modules/gitea
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
  flake: ".#host-a"
`
	cfg := loadFromString(t, content)
	if len(cfg.Warnings) != 0 {
		t.Errorf("expected no warnings with nixos_rebuild enabled, got %v", cfg.Warnings)
	}
}

func TestLoad_RejectsRelativeRepoDir(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+"repo_dir: relative/repo\n")
	if err == nil || !strings.Contains(err.Error(), "repo_dir") || !strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("expected a repo_dir-must-be-absolute error, got %v", err)
	}
}

func TestLoad_DerivesRepoWebURLFromRepoURLWhenUnset(t *testing.T) {
	// minimalConfig clones over ssh — the forge serves its web UI over https.
	cfg := loadFromString(t, minimalConfig)
	if got, want := cfg.EffectiveRepoWebURL(), "https://gitea.example.com/user/nixos"; got != want {
		t.Errorf("EffectiveRepoWebURL() = %q, want %q", got, want)
	}
}

func TestLoad_ExplicitRepoWebURLWinsOverTheDerivedOne(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+"repo_web_url: https://code.example.com/ops/deploy/\n")
	// The trailing slash is trimmed so callers append a path verbatim.
	if got, want := cfg.EffectiveRepoWebURL(), "https://code.example.com/ops/deploy"; got != want {
		t.Errorf("EffectiveRepoWebURL() = %q, want %q", got, want)
	}
}

func TestLoad_NoRepoWebURLWhenNoneCanBeDerived(t *testing.T) {
	// A clone from a local path has no forge — the UI then shows plain SHAs.
	cfg := loadFromString(t, "repo_url: /srv/git/deploy.git\nwebhook_secret: secret123\n")
	if got := cfg.EffectiveRepoWebURL(); got != "" {
		t.Errorf("EffectiveRepoWebURL() = %q, want empty", got)
	}
}

func TestLoad_RejectsNonHTTPRepoWebURL(t *testing.T) {
	for _, bad := range []string{"javascript:alert(1)", "git@forge.example.com:owner/repo.git", "/srv/git/repo", "https://"} {
		t.Run(bad, func(t *testing.T) {
			_, err := loadStringToConfig(t, minimalConfig+"repo_web_url: "+strconv.Quote(bad)+"\n")
			if err == nil || !strings.Contains(err.Error(), "repo_web_url") || !strings.Contains(err.Error(), "http(s) URL") {
				t.Fatalf("expected a repo_web_url-must-be-http error, got %v", err)
			}
		})
	}
}

func TestLoad_AcceptsAbsoluteRepoDir(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+"repo_dir: /var/lib/skipper/repo\n")
	if cfg.RepoDir != "/var/lib/skipper/repo" {
		t.Errorf("unexpected repo_dir: %s", cfg.RepoDir)
	}
}

func TestLoad_AcceptsOmittedRepoDir(t *testing.T) {
	cfg := loadFromString(t, minimalConfig)
	if cfg.RepoDir != git.DefaultRepoDir {
		t.Errorf("expected repo_dir to default to %q, got %q", git.DefaultRepoDir, cfg.RepoDir)
	}
}

func TestLoad_ResolvesRelativeStacksBaseDirAgainstDefaultRepoDir(t *testing.T) {
	// minimalConfig omits repo_dir and sets stacks_base_dir: modules.
	cfg := loadFromString(t, minimalConfig)
	if want := filepath.Join(git.DefaultRepoDir, "modules"); cfg.StacksBaseDir != want {
		t.Errorf("expected stacks_base_dir %q, got %q", want, cfg.StacksBaseDir)
	}
}

func TestLoad_ResolvesRelativeStacksBaseDirAgainstCustomRepoDir(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
repo_dir: /srv/clone
stacks_base_dir: stacks
webhook_secret: secret123
`
	cfg := loadFromString(t, content)
	if want := "/srv/clone/stacks"; cfg.StacksBaseDir != want {
		t.Errorf("expected stacks_base_dir %q, got %q", want, cfg.StacksBaseDir)
	}
}

func TestLoad_EmptyStacksBaseDirResolvesToRepoRoot(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
repo_dir: /srv/clone
stack_discovery: false
stacks:
  - name: gitea
    project_directory: /etc/nixos/modules/gitea
`
	cfg := loadFromString(t, content)
	if want := "/srv/clone"; cfg.StacksBaseDir != want {
		t.Errorf("expected empty stacks_base_dir to resolve to the repo root %q, got %q", want, cfg.StacksBaseDir)
	}
}

func TestLoad_RejectsAbsoluteStacksBaseDir(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
webhook_secret: secret123
`
	_, err := loadStringToConfig(t, content)
	if err == nil || !strings.Contains(err.Error(), "stacks_base_dir") || !strings.Contains(err.Error(), "must be relative") {
		t.Fatalf("expected a stacks_base_dir-must-be-relative error, got %v", err)
	}
}

func TestLoad_RejectsStacksBaseDirEscapingRepo(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: ../outside
webhook_secret: secret123
`
	_, err := loadStringToConfig(t, content)
	if err == nil || !strings.Contains(err.Error(), "stacks_base_dir") || !strings.Contains(err.Error(), "inside repo_dir") {
		t.Fatalf("expected a stacks_base_dir-escape error, got %v", err)
	}
}

func TestLoad_RejectsRelativeIconsCacheDir(t *testing.T) {
	content := minimalConfig + `
icons:
  cache_dir: relative/icons
`
	_, err := loadStringToConfig(t, content)
	if err == nil || !strings.Contains(err.Error(), "icons.cache_dir") || !strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("expected an icons.cache_dir-must-be-absolute error, got %v", err)
	}
}

func TestLoad_AcceptsAbsoluteIconsCacheDir(t *testing.T) {
	content := minimalConfig + `
icons:
  cache_dir: /srv/skipper/icons
`
	cfg := loadFromString(t, content)
	if cfg.Icons.CacheDir != "/srv/skipper/icons" {
		t.Errorf("unexpected icons.cache_dir: %s", cfg.Icons.CacheDir)
	}
}

func TestLoad_DefaultIconsCacheDirIsAbsolute(t *testing.T) {
	cfg := loadFromString(t, minimalConfig)
	if !strings.HasPrefix(cfg.Icons.CacheDir, "/") {
		t.Errorf("expected default icons.cache_dir to be absolute, got %q", cfg.Icons.CacheDir)
	}
}

func TestLoad_RejectsRelativeEnvFileInHostListMode(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: gitea
    project_directory: /etc/nixos/modules/gitea
    env_files:
      - relative/compose.env
`
	_, err := loadStringToConfig(t, content)
	if err == nil || !strings.Contains(err.Error(), "env_files") || !strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("expected an env_files-must-be-absolute error, got %v", err)
	}
}

func TestLoad_RejectsRelativeWatchDirInHostListMode(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: gitea
    project_directory: /etc/nixos/modules/gitea
    watch_dirs:
      - relative/provisioning
`
	_, err := loadStringToConfig(t, content)
	if err == nil || !strings.Contains(err.Error(), "watch_dirs") || !strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("expected a watch_dirs-must-be-absolute error, got %v", err)
	}
}

func TestLoad_AcceptsAbsoluteEnvFilesAndWatchDirsInHostListMode(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: gitea
    project_directory: /etc/nixos/modules/gitea
    env_files:
      - /run/secrets/gitea.env
    watch_dirs:
      - /etc/nixos/modules/gitea/provisioning
`
	cfg := loadFromString(t, content)
	if cfg.Stacks[0].EnvFiles[0] != "/run/secrets/gitea.env" {
		t.Errorf("unexpected env_files: %v", cfg.Stacks[0].EnvFiles)
	}
}

func TestLoad_AcceptsRelativeEnvFilesUnderDiscovery(t *testing.T) {
	// Under discovery, relative env_files/watch_dirs are resolved against
	// stacks_base_dir by LoadRepoStacks, not checked here — config.Load never
	// sees the repo clone that would let it validate them.
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: secret123
stacks:
  - name: gitea
    env_files:
      - relative/compose.env
`
	cfg := loadFromString(t, content)
	if cfg.Stacks[0].EnvFiles[0] != "relative/compose.env" {
		t.Errorf("unexpected env_files: %v", cfg.Stacks[0].EnvFiles)
	}
}

func TestLoad_WarnsWhenPerStackSelfHealDeadUnderDiscovery(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: secret123
stacks:
  - name: gitea
    self_heal: true
`
	cfg := loadFromString(t, content)
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], `stack "gitea"`) || !strings.Contains(cfg.Warnings[0], "never takes effect") {
		t.Fatalf("expected a dead self_heal-override warning, got %v", cfg.Warnings)
	}
}

func TestLoad_NoSelfHealWarning_WhenGlobalSelfHealOn(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: secret123
self_heal: true
stacks:
  - name: gitea
    self_heal: true
`
	cfg := loadFromString(t, content)
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "self_heal") {
			t.Errorf("did not expect a self_heal warning with the global flag on, got %v", cfg.Warnings)
		}
	}
}

func TestLoad_NoSelfHealWarning_InHostListMode(t *testing.T) {
	// Outside discovery, cfg.Stacks is the real stack set, so a per-stack
	// self_heal override is a real, effective override — not the discovery-only
	// footgun this warning targets.
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: gitea
    project_directory: /etc/nixos/modules/gitea
    self_heal: true
`
	cfg := loadFromString(t, content)
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "self_heal") {
			t.Errorf("did not expect a self_heal warning in host-list mode, got %v", cfg.Warnings)
		}
	}
}

func TestLoad_WarnsWhenHookTimeoutExceedsCommandTimeout(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: secret123
command_timeout_seconds: 60
stacks:
  - name: gitea
    hooks:
      pre_deploy: ["pg_dump db > /backup/db.sql"]
      timeout_seconds: 120
`
	cfg := loadFromString(t, content)
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], `stack "gitea"`) || !strings.Contains(cfg.Warnings[0], "hooks.timeout_seconds") {
		t.Fatalf("expected a hooks.timeout_seconds-exceeds-command_timeout_seconds warning, got %v", cfg.Warnings)
	}
}

func TestLoad_NoHookTimeoutWarning_WhenWithinCommandTimeout(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: secret123
stacks:
  - name: gitea
    hooks:
      pre_deploy: ["pg_dump db > /backup/db.sql"]
      timeout_seconds: 60
`
	cfg := loadFromString(t, content)
	if len(cfg.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", cfg.Warnings)
	}
}

func TestLoad_NoHookTimeoutWarning_WhenUnset(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
webhook_secret: secret123
stacks:
  - name: gitea
    hooks:
      pre_deploy: ["pg_dump db > /backup/db.sql"]
`
	cfg := loadFromString(t, content)
	if len(cfg.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", cfg.Warnings)
	}
}
