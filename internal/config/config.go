// Package config handles loading and validating the skipper.yml configuration file.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Stack represents a single Docker Compose project to be deployed.
type Stack struct {
	// Name is a unique identifier for the stack. When working_dir is omitted,
	// the working directory is derived as stacks_base_dir/<name>.
	Name string `yaml:"name"`

	// WorkingDir is the absolute path to the directory containing docker-compose.yml.
	// Optional when stacks_base_dir is set.
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
}

// WorkDir returns the effective working directory for this stack.
func (s Stack) WorkDir(baseDir string) string {
	if s.WorkingDir != "" {
		return s.WorkingDir
	}
	return filepath.Join(baseDir, s.Name)
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

	// VarsFile is an optional path to a YAML file of key→value pairs that are
	// injected as KEY=VALUE env vars into every stack deployment, enabling
	// ${VAR} substitution in docker-compose.yml (e.g. for domain names).
	VarsFile string `yaml:"vars_file"`

	// CommandTimeoutSeconds is the maximum number of seconds a single shell
	// command (docker compose pull/up, git clone/fetch) is allowed to run
	// before being killed. Defaults to 300 (5 minutes).
	CommandTimeoutSeconds int `yaml:"command_timeout_seconds"`

	StacksBaseDir string  `yaml:"stacks_base_dir"`
	WebhookSecret string  `yaml:"webhook_secret"`
	Port          int     `yaml:"port"`
	MetricsPort   int     `yaml:"metrics_port"`
	UIEnabled     bool    `yaml:"ui_enabled"`
	Stacks        []Stack `yaml:"stacks"`
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

	return cfg, validateConfig(cfg)
}

func validateConfig(cfg *Config) error {
	if cfg.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}

	for _, s := range cfg.Stacks {
		if s.WorkingDir == "" && cfg.StacksBaseDir == "" {
			return fmt.Errorf("stack %q: working_dir is required when stacks_base_dir is not set", s.Name)
		}
	}

	return nil
}
