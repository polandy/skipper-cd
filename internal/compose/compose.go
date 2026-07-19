// Package compose is a minimal read-only parser for docker-compose.yml, shared
// by the deploy path (image/build/rollout logic) and config discovery
// (validation). It models only the fields skipper reads; everything else is
// ignored.
package compose

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// FileName is the compose file skipper reads per stack.
const FileName = "docker-compose.yml"

// File is a parsed docker-compose.yml.
type File struct {
	Services map[string]Service `yaml:"services"`
}

// Service is one compose service. Only the fields skipper acts on are read.
type Service struct {
	Image         string `yaml:"image"`
	Build         any    `yaml:"build"`
	Ports         []any  `yaml:"ports"`
	Healthcheck   any    `yaml:"healthcheck"`
	ContainerName string `yaml:"container_name"`
}

// PublishesPorts reports whether the service publishes any host port.
func (s Service) PublishesPorts() bool { return len(s.Ports) > 0 }

// HasHealthcheck reports whether the service defines a compose healthcheck.
func (s Service) HasHealthcheck() bool { return s.Healthcheck != nil }

// HasContainerName reports whether the service pins a container_name (which
// compose cannot scale).
func (s Service) HasContainerName() bool { return s.ContainerName != "" }

// Parse reads and parses a docker-compose.yml.
func Parse(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	return &f, nil
}
