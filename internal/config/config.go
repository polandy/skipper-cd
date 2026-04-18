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
	// the working directory is derived as stacks_base_dir/<name>.
	Name string `yaml:"name"`

	// WorkingDir is the absolute path to the directory containing docker-compose.yml.
	// Optional when stacks_base_dir is set.
	WorkingDir string `yaml:"working_dir"`

	// EnvFiles lists KEY=VALUE files injected into the environment when docker-compose
	// is invoked, enabling ${VAR} substitution inside docker-compose.yml.
	EnvFiles []string `yaml:"env_files"`
}

// WorkDir returns the effective working directory for this stack.
func (s Stack) WorkDir(baseDir string) string {
	if s.WorkingDir != "" {
		return s.WorkingDir
	}
	return filepath.Join(baseDir, s.Name)
}

// Config holds the full orpheus-cd configuration.
type Config struct {
	RepoPath      string  `yaml:"repo_path"`
	StacksBaseDir string  `yaml:"stacks_base_dir"`
	WebhookSecret string  `yaml:"webhook_secret"`
	Port          int     `yaml:"port"`
	MetricsPort   int     `yaml:"metrics_port"`
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

	return cfg, validateConfig(cfg)
}

func validateConfig(cfg *Config) error {
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
