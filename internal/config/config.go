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

type Config struct {
	RepoPath      string  `yaml:"repo_path"`
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

	return cfg, nil
}
