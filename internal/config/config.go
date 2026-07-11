// Package config handles loading and validating the skipper.yml configuration file.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
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
}

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
	}

	if cfg.NixOSRebuild.IsEnabled() && cfg.NixOSRebuild.Flake == "" {
		return fmt.Errorf("nixos_rebuild.flake is required when nixos_rebuild is enabled")
	}

	if cfg.LogFormat != LogFormatText && cfg.LogFormat != LogFormatJSON {
		return fmt.Errorf("log_format must be %q or %q, got %q", LogFormatText, LogFormatJSON, cfg.LogFormat)
	}

	return nil
}
