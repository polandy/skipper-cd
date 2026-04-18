package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Stack struct {
	Name       string   `yaml:"name"`
	WorkingDir string   `yaml:"working_dir"`
	EnvFiles   []string `yaml:"env_files"`
}

// ResolvedWorkingDir returns the effective working directory for a stack.
// If working_dir is set explicitly it is used as-is; otherwise it is derived
// from stacks_base_dir/<name>.
func (s Stack) ResolvedWorkingDir(baseDir string) string {
	if s.WorkingDir != "" {
		return s.WorkingDir
	}
	return baseDir + "/" + s.Name
}

type Config struct {
	RepoPath      string  `yaml:"repo_path"`
	StacksBaseDir string  `yaml:"stacks_base_dir"`
	WebhookSecret string  `yaml:"webhook_secret"`
	Port          int     `yaml:"port"`
	MetricsPort   int     `yaml:"metrics_port"`
	Stacks        []Stack `yaml:"stacks"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		Port:        8080,
		MetricsPort: 9120,
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.RepoPath == "" {
		return nil, fmt.Errorf("repo_path is required")
	}

	for _, s := range cfg.Stacks {
		if s.WorkingDir == "" && cfg.StacksBaseDir == "" {
			return nil, fmt.Errorf("stack %q: working_dir required when stacks_base_dir is not set", s.Name)
		}
	}

	return cfg, nil
}
