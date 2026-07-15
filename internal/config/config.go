// Package config handles loading and validating the skipper.yml configuration file.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/ui"
)

// Stack represents a single Docker Compose project to be deployed.
type Stack struct {
	// Name is a unique identifier for the stack. When working_dir is omitted,
	// the working directory is derived as stacks_base_dir/<name>.
	Name string `yaml:"name"`

	// WorkingDir is an optional absolute path passed as --project-directory to
	// docker compose. It controls Docker Compose project identity (container
	// labels) and .env file loading. Change detection and the compose file
	// always come from stacks_base_dir/<name>.
	WorkingDir string `yaml:"working_dir"`

	// EnvFiles lists KEY=VALUE files injected into the environment when docker-compose
	// is invoked, enabling ${VAR} substitution inside docker-compose.yml.
	EnvFiles []string `yaml:"env_files"`

	// WatchDirs lists additional directories (relative to the repo root or absolute)
	// whose contents are hashed alongside docker-compose.yml. Any change inside
	// these directories triggers a redeployment.
	WatchDirs []string `yaml:"watch_dirs"`

	// OnDemandContainers lists container names to stop after a successful deployment.
	// Use this for containers managed by an on-demand scheduler:
	// the scheduler will start them again on the next incoming request.
	OnDemandContainers []string `yaml:"on_demand_containers,omitempty"`

	// Icon optionally overrides the icon-set slug used for this stack's UI
	// icon (e.g. "jellyfin" for a stack named "media"). When empty, the icon
	// is auto-matched from the stack name. A repo icon.svg/icon.png in the
	// stack directory takes precedence over both. Purely visual, never hashed.
	Icon string `yaml:"icon,omitempty"`

	// Autosync overrides the global autosync for this stack. nil means inherit
	// the global setting. When autosync is not effective, a detected change is
	// queued instead of deployed. See docs/autosync.md.
	Autosync *bool `yaml:"autosync"`

	// HealthCheck optionally gates a deploy of this stack on a post-deploy
	// health check; on failure the deploy is rolled back. nil disables the
	// gate. See ADR-0022.
	HealthCheck *HealthCheck `yaml:"health_check,omitempty"`
}

// HealthCheck configures the optional post-deploy health gate of a stack.
// When present, `docker compose up` runs with --wait so it fails when the
// services' compose healthchecks do not turn healthy in time, and an optional
// HTTP probe additionally verifies the stack from the outside. Either failure
// triggers the regular rollback path (ADR-0004, ADR-0022).
type HealthCheck struct {
	// TimeoutSeconds bounds the wait: it is passed as --wait-timeout to
	// docker compose up and is also the deadline of the HTTP probe.
	// Defaults to 60.
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// URL, when set, is HTTP-GET-probed after a successful up until it
	// answers 2xx; anything else within TimeoutSeconds rolls the deploy back.
	URL string `yaml:"url"`
}

// Config holds the full skipper-cd configuration.
type Config struct {
	// RepoURL is the Git remote URL. skipper-cd clones it to RepoDir and
	// runs git pull on every incoming webhook.
	RepoURL string `yaml:"repo_url"`

	// RepoDir is the local directory where the repository is cloned.
	// Defaults to /var/lib/skipper/repo when left empty.
	RepoDir string `yaml:"repo_dir"`

	// Branch is the Git branch to track. Defaults to "master".
	Branch string `yaml:"branch"`

	// VarsFile is an optional path to a KEY=VALUE env file whose entries are
	// injected into the environment of every stack deployment, enabling
	// ${VAR} substitution in docker-compose.yml (e.g. for domain names).
	VarsFile string `yaml:"vars_file"`

	// CommandTimeoutSeconds is the maximum number of seconds a single shell
	// command (docker compose pull/up, git clone/fetch) is allowed to run
	// before being killed. Defaults to 300 (5 minutes).
	CommandTimeoutSeconds int `yaml:"command_timeout_seconds"`

	// LogFormat selects the log output format: "text" (logfmt, the default)
	// or "json" for structured logs (e.g. for Loki ingestion).
	LogFormat string `yaml:"log_format"`

	StacksBaseDir string  `yaml:"stacks_base_dir"`
	WebhookSecret string  `yaml:"webhook_secret"`
	Port          int     `yaml:"port"`
	MetricsPort   int     `yaml:"metrics_port"`
	UIEnabled     bool    `yaml:"ui_enabled"`
	Stacks        []Stack `yaml:"stacks"`

	// Autosync is the global default for whether detected changes deploy
	// automatically. nil means true (on). A per-stack Autosync overrides it.
	// See docs/autosync.md.
	Autosync *bool `yaml:"autosync"`

	// NixOSRebuild configures automatic nixos-rebuild when nix files change.
	// Omit the section entirely to disable. When present without an explicit
	// "enabled" key, it defaults to enabled.
	NixOSRebuild *NixOSRebuild `yaml:"nixos_rebuild"`

	// Icons configures service-icon resolution for the web UI. The section is
	// optional; omitting it uses the defaults applied in Load.
	Icons IconsConfig `yaml:"icons"`

	// Notifications lists outbound notification targets that receive a message
	// on every terminal deploy outcome they subscribe to. An empty section
	// disables notifications entirely. See ADR-0020.
	Notifications []NotificationTarget `yaml:"notifications"`

	// UITheme selects the web UI's colour palette (see ui.ValidThemes).
	// Optional; defaults to "catppuccin". Each theme has its own dark/light
	// variant, toggled independently in the browser — this only picks which
	// palette that toggle switches within. Distinguishes multiple skipper-cd
	// instances (e.g. one per host) at a glance. See docs/configuration.md.
	UITheme string `yaml:"ui_theme"`

	// UIThemeSwitcher enables the in-UI theme picker: a per-browser override
	// for trying palettes out, applied without a reload. Optional; defaults to
	// false, so the deployed UITheme is fixed and cannot be switched from the
	// browser (keeping the per-instance colour a reliable at-a-glance marker).
	// See docs/configuration.md.
	UIThemeSwitcher bool `yaml:"ui_theme_switcher"`

	// HealthPollIntervalSeconds sets how often the UI polls its stacks' runtime
	// health (ADR-0027). nil (omitted) defaults to 30; an explicit 0 disables the
	// health view. Only meaningful with ui_enabled; the poll additionally runs
	// only while a UI client is connected. See docs/configuration.md.
	HealthPollIntervalSeconds *int `yaml:"health_poll_interval_seconds"`
}

// NotificationTarget configures a single outbound notification sink: where to
// POST, in which provider shape, and which deploy outcomes to report. See
// ADR-0020.
type NotificationTarget struct {
	// Format selects the provider shape of the request body. One of
	// "signal", "generic". Defaults to "generic".
	Format string `yaml:"format"`

	// URL is the endpoint the notification is POSTed to. For "signal" it is the
	// signal-cli-rest-api base (e.g. http://localhost:8020); "/v2/send" is
	// appended by the formatter.
	URL string `yaml:"url"`

	// On lists the terminal deploy statuses that trigger this target. Any
	// subset of "failed", "success", "rolled_back", "rolled_back_unhealthy".
	// Empty means all four.
	On []string `yaml:"on"`

	// Prefix is prepended (as "[<prefix>] ") to the human-readable message of
	// the "signal" format, e.g. to label which host/instance sent it. Optional;
	// empty adds no prefix. Ignored by the "generic" format, whose structured
	// payload already carries the event.
	Prefix string `yaml:"prefix"`

	// Headers are static HTTP headers added to the request. Only meaningful for
	// the "generic" format (e.g. an Authorization bearer token).
	Headers map[string]string `yaml:"headers"`

	// Number is the Signal sender number ("signal" format only, required).
	Number string `yaml:"number"`

	// Recipients are the Signal recipient numbers ("signal" format only,
	// non-empty).
	Recipients []string `yaml:"recipients"`
}

// Notification format values for NotificationTarget.Format.
const (
	NotifyFormatSignal  = "signal"
	NotifyFormatGeneric = "generic"
)

// Terminal deploy statuses accepted in NotificationTarget.On. They mirror the
// terminal events.Status values without importing that package (config is the
// lowest layer).
const (
	NotifyOnFailed              = "failed"
	NotifyOnSuccess             = "success"
	NotifyOnRolledBack          = "rolled_back"
	NotifyOnRolledBackUnhealthy = "rolled_back_unhealthy"
)

// IconsConfig configures how the web UI resolves and caches stack icons.
type IconsConfig struct {
	// CacheDir is the on-disk directory where fetched icons are cached.
	// Defaults to /var/lib/skipper/icons.
	CacheDir string `yaml:"cache_dir"`

	// SourceURL is the icon-set base URL; icons are fetched from
	// SourceURL/<slug>.svg. Defaults to the dashboard-icons CDN.
	SourceURL string `yaml:"source_url"`
}

// NixOSRebuild configures automatic NixOS rebuilds when .nix files or
// flake.lock change in the repository.
type NixOSRebuild struct {
	Enabled *bool  `yaml:"enabled"` // nil = true when section present
	Flake   string `yaml:"flake"`   // e.g. ".#nuc", required when enabled
}

// IsEnabled returns whether NixOS rebuild is enabled.
// Section omitted → disabled. Section present without enabled key → enabled.
// Explicit enabled value → that value.
func (n *NixOSRebuild) IsEnabled() bool {
	if n == nil {
		return false
	}
	if n.Enabled == nil {
		return true
	}
	return *n.Enabled
}

// Load reads and validates the configuration file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{
		Port:        8080,
		MetricsPort: 9120,
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	if cfg.CommandTimeoutSeconds == 0 {
		cfg.CommandTimeoutSeconds = 300
	}
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	if cfg.LogFormat == "" {
		cfg.LogFormat = LogFormatText
	}
	if cfg.Icons.CacheDir == "" {
		cfg.Icons.CacheDir = defaultIconCacheDir
	}
	if cfg.Icons.SourceURL == "" {
		cfg.Icons.SourceURL = defaultIconSourceURL
	}
	for i := range cfg.Notifications {
		t := &cfg.Notifications[i]
		if t.Format == "" {
			t.Format = NotifyFormatGeneric
		}
		if len(t.On) == 0 {
			t.On = []string{NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy}
		}
	}
	if cfg.UITheme == "" {
		cfg.UITheme = ui.ThemeCatppuccin
	}
	if cfg.HealthPollIntervalSeconds == nil {
		d := defaultHealthPollIntervalSeconds
		cfg.HealthPollIntervalSeconds = &d
	}
	for i := range cfg.Stacks {
		if hc := cfg.Stacks[i].HealthCheck; hc != nil && hc.TimeoutSeconds == 0 {
			hc.TimeoutSeconds = defaultHealthCheckTimeoutSeconds
		}
	}

	return cfg, validateConfig(cfg)
}

// Valid values for the log_format config field.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// Defaults for the icons section. SourceURL is the icon-set root; icons are
// fetched from <source_url>/<format>/<slug>.<format> (svg, then png, webp).
const (
	defaultIconCacheDir  = "/var/lib/skipper/icons"
	defaultIconSourceURL = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"
)

// reservedStackName is used internally as the state key for NixOS rebuild
// hashes and must not collide with a configured stack.
const reservedStackName = "_nixos"

// defaultHealthCheckTimeoutSeconds is applied when a health_check section is
// present without an explicit timeout_seconds.
const defaultHealthCheckTimeoutSeconds = 60

// defaultHealthPollIntervalSeconds is the UI stack-health poll cadence applied
// when health_poll_interval_seconds is omitted (ADR-0027). An explicit 0
// disables the health view.
const defaultHealthPollIntervalSeconds = 30

func validateConfig(cfg *Config) error {
	if cfg.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}

	seen := make(map[string]struct{}, len(cfg.Stacks))
	for _, s := range cfg.Stacks {
		if s.Name == "" {
			return fmt.Errorf("every stack needs a name")
		}
		if s.Name == reservedStackName {
			return fmt.Errorf("stack name %q is reserved", reservedStackName)
		}
		if _, dup := seen[s.Name]; dup {
			return fmt.Errorf("duplicate stack name %q", s.Name)
		}
		seen[s.Name] = struct{}{}

		if s.WorkingDir == "" && cfg.StacksBaseDir == "" {
			return fmt.Errorf("stack %q: working_dir is required when stacks_base_dir is not set", s.Name)
		}

		if err := validateHealthCheck(s.HealthCheck); err != nil {
			return fmt.Errorf("stack %q: health_check: %w", s.Name, err)
		}
	}

	if cfg.NixOSRebuild.IsEnabled() && cfg.NixOSRebuild.Flake == "" {
		return fmt.Errorf("nixos_rebuild.flake is required when nixos_rebuild is enabled")
	}

	if !ui.IsValidTheme(cfg.UITheme) {
		return fmt.Errorf("ui_theme must be one of %s, got %q", strings.Join(ui.ValidThemes, ", "), cfg.UITheme)
	}

	if cfg.LogFormat != LogFormatText && cfg.LogFormat != LogFormatJSON {
		return fmt.Errorf("log_format must be %q or %q, got %q", LogFormatText, LogFormatJSON, cfg.LogFormat)
	}

	if cfg.HealthPollIntervalSeconds != nil && *cfg.HealthPollIntervalSeconds < 0 {
		return fmt.Errorf("health_poll_interval_seconds must be >= 0, got %d", *cfg.HealthPollIntervalSeconds)
	}

	for i, t := range cfg.Notifications {
		if err := validateNotificationTarget(t); err != nil {
			return fmt.Errorf("notifications[%d]: %w", i, err)
		}
	}

	return nil
}

// validateHealthCheck checks a stack's optional health_check section.
// TimeoutSeconds has already been defaulted in Load.
func validateHealthCheck(hc *HealthCheck) error {
	if hc == nil {
		return nil
	}
	if hc.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must not be negative, got %d", hc.TimeoutSeconds)
	}
	if hc.URL != "" {
		if u, err := url.ParseRequestURI(hc.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("url %q must be a valid http(s) URL", hc.URL)
		}
	}
	return nil
}

// validateNotificationTarget checks a single notification target. Format and On
// have already been defaulted in Load.
func validateNotificationTarget(t NotificationTarget) error {
	switch t.Format {
	case NotifyFormatSignal, NotifyFormatGeneric:
	default:
		return fmt.Errorf("unknown format %q", t.Format)
	}

	if t.URL == "" {
		return fmt.Errorf("url is required")
	}
	if u, err := url.ParseRequestURI(t.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("url %q must be a valid http(s) URL", t.URL)
	}

	for _, s := range t.On {
		switch s {
		case NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy:
		default:
			return fmt.Errorf("unknown on value %q", s)
		}
	}

	// Signal identity fields are required for and exclusive to the signal format.
	if t.Format == NotifyFormatSignal {
		if t.Number == "" {
			return fmt.Errorf("signal format requires number")
		}
		if len(t.Recipients) == 0 {
			return fmt.Errorf("signal format requires at least one recipient")
		}
	} else if t.Number != "" || len(t.Recipients) > 0 {
		return fmt.Errorf("number/recipients are only valid for the signal format")
	}

	return nil
}
