// Package compose is a minimal read-only parser for docker-compose.yml, shared
// by the deploy path (image/build/rollout logic) and config discovery
// (validation). It models only the fields skipper reads; everything else is
// ignored.
package compose

import (
	"fmt"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/regularfile"
)

// FileName is the compose file skipper reads per stack.
const FileName = "docker-compose.yml"

// MaxFileBytes caps a compose file read: the file comes from a Git push.
const MaxFileBytes = 1 << 20

// DefaultDockerfile is the file a build: section without an explicit
// dockerfile: builds from — compose's own default.
const DefaultDockerfile = "Dockerfile"

// File is a parsed docker-compose.yml.
type File struct {
	Services map[string]Service `yaml:"services"`
}

// Service is one compose service. Only the fields skipper acts on are read.
type Service struct {
	Image         string       `yaml:"image"`
	Build         any          `yaml:"build"`
	Ports         []any        `yaml:"ports"`
	Healthcheck   *Healthcheck `yaml:"healthcheck"`
	ContainerName string       `yaml:"container_name"`
}

// Healthcheck is a service's compose healthcheck; only its timing is read.
// Every field is kept as written and parsed on use, so an uninterpolated
// `${VAR}` falls back to Docker's default instead of failing the parse.
type Healthcheck struct {
	Interval    string `yaml:"interval"`
	Timeout     string `yaml:"timeout"`
	StartPeriod string `yaml:"start_period"`
	Retries     string `yaml:"retries"`
}

// Docker's healthcheck defaults, used for any timing a healthcheck leaves out.
const (
	defaultHealthInterval = 30 * time.Second
	defaultHealthTimeout  = 30 * time.Second
	defaultHealthRetries  = 3
)

// ReadinessBudget is how long the healthcheck's own timing lets a container
// stay "starting" before Docker decides: start_period + (retries+1) × interval
// + timeout. An unparsable value falls back to Docker's default.
func (h *Healthcheck) ReadinessBudget() time.Duration {
	if h == nil {
		return 0
	}
	retries, err := strconv.Atoi(h.Retries)
	if err != nil || retries <= 0 {
		retries = defaultHealthRetries
	}
	interval := durationOr(h.Interval, defaultHealthInterval)
	return durationOr(h.StartPeriod, 0) + time.Duration(retries+1)*interval + durationOr(h.Timeout, defaultHealthTimeout)
}

// durationOr parses a compose duration, or returns def when it is empty,
// unparsable or not positive.
func durationOr(s string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// PublishesPorts reports whether the service publishes any host port.
func (s Service) PublishesPorts() bool { return len(s.Ports) > 0 }

// HasHealthcheck reports whether the service defines a compose healthcheck.
func (s Service) HasHealthcheck() bool { return s.Healthcheck != nil }

// ReadinessBudget is the longest healthcheck readiness budget across the
// file's services (see Healthcheck.ReadinessBudget); 0 without any healthcheck.
func (f *File) ReadinessBudget() time.Duration {
	var budget time.Duration
	for _, svc := range f.Services {
		budget = max(budget, svc.Healthcheck.ReadinessBudget())
	}
	return budget
}

// HasContainerName reports whether the service pins a container_name (which
// compose cannot scale).
func (s Service) HasContainerName() bool { return s.ContainerName != "" }

// Parse reads and parses a docker-compose.yml.
func Parse(path string) (*File, error) {
	data, err := regularfile.ReadFile(path, MaxFileBytes, regularfile.FollowLinks)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	return &f, nil
}
