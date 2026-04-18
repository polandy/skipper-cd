// Package config handles loading and validating the orpheus.yml configuration file.
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
	// it is also used to locate the stack under stacks_base_dir.
	Name string `yaml:"name"`

	// WorkingDir is the absolute path to the directory containing docker-compose.yml.
	// Optional: if omitted, the path is derived as stacks_base_dir/<name>.
	WorkingDir string `yaml:"working_dir"`

	// EnvFiles is a list of paths to env files whose KEY=VALUE pairs are injected
	// into the environment when docker-compose is invoked. This enables
	// ${VAR} substitution inside docker-compose.yml (e.g. for Traefik labels).
	EnvFiles []string `yaml:"env_files"`
}

// WorkDir returns the effective working directory for this stack.
// When working_dir is set explicitly it is used as-is; otherwise the path
// is constructed as <baseDir>/<name>.
func (s Stack) WorkDir(baseDir string) string {
	if s.WorkingDir != "" {
		return s.WorkingDir
	}
	return filepath.Join(baseDir, s.Name)
}

// Config holds the full orpheus-cd configuration.
type Config struct {
	// RepoPath is the absolute path to the git repository that is pulled
	// on each incoming webhook before deploying.
	RepoPath string `yaml:"repo_path"`

	// StacksBaseDir is the default parent directory for all stacks.
	// Individual stacks may override this with an explicit working_dir.
	StacksBaseDir string `yaml:"stacks_base_dir"`

	// WebhookSecret is the shared secret used to validate incoming Gitea
	// webhooks via HMAC-SHA256. Leave empty to disable signature validation.
	WebhookSecret string `yaml:"webhook_secret"`

	// Port is the HTTP port for the webhook endpoint (default: 8080).
	Port int `yaml:"port"`

	// MetricsPort is the HTTP port for the Prometheus /metrics endpoint (default: 9120).
	MetricsPort int `yaml:"metrics_port"`

	// Stacks is the list of Docker Compose projects managed by orpheus-cd.
	Stacks []Stack `yaml:"stacks"`
}

// Load reads and validates the configuration file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Set defaults before unmarshalling so they are overridden by explicit values.
	cfg := &Config{
		Port:        8080,
		MetricsPort: 9120,
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate checks that all required fields are present and consistent.
func validate(cfg *Config) error {
	if cfg.RepoPath == "" {
		return fmt.Errorf("repo_path is required")
	}

	for _, s := range cfg.Stacks {
		if s.WorkingDir == "" && cfg.StacksBaseDir == "" {
			return fmt.Errorf("stack %q: working_dir is required when stacks_base_dir is not set", s.Name)
		}
	}

	return nil
}
