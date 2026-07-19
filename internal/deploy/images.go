package deploy

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// serviceImageByName maps each compose service name to its image reference.
type serviceImageByName map[string]string

// composeFile is a minimal parsed representation of a docker-compose.yml.
// It is parsed once per stack deploy; all image/build lookups are methods on it.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image    string `yaml:"image"`
	BuildRaw any    `yaml:"build"`
	// Ports and Healthcheck are read only for rollout eligibility (ADR-0040): a
	// rolled service must publish no host ports (two replicas would collide) and
	// define a healthcheck (the readiness signal). Both are captured as raw YAML
	// — only their presence matters here.
	Ports       []any `yaml:"ports"`
	Healthcheck any   `yaml:"healthcheck"`
}

// publishesPorts reports whether the service publishes any host port. A rolled
// service must not, since two replicas cannot bind the same host port.
func (s composeService) publishesPorts() bool {
	return len(s.Ports) > 0
}

// hasHealthcheck reports whether the service defines a compose healthcheck, the
// readiness signal a rollout waits on before draining the old container.
func (s composeService) hasHealthcheck() bool {
	return s.Healthcheck != nil
}

// parseComposeFile reads and parses a docker-compose.yml.
func parseComposeFile(path string) (*composeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	return &cf, nil
}

// images returns a map of service name to image reference. Services without
// an image field (e.g. those using build:) are omitted.
func (cf *composeFile) images() serviceImageByName {
	images := make(serviceImageByName)
	for name, svc := range cf.Services {
		if svc.Image != "" {
			images[name] = svc.Image
		}
	}
	return images
}

// servicesExcept returns the names of all compose services not in the given
// set, sorted for a deterministic command line. It selects the services that a
// rollout deploys in place (everything not being rolled).
func (cf *composeFile) servicesExcept(exclude map[string]bool) []string {
	var names []string
	for name := range cf.Services {
		if !exclude[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// pullableServices returns the names of services whose images should be
// pulled from a registry. Services with a build: field are excluded (they are
// built locally). Services whose image name matches a locally-built image
// (from a build: service) are also excluded, since that image won't exist on
// any registry.
func (cf *composeFile) pullableServices() []string {
	// Collect image names produced by build: services.
	localImages := make(map[string]struct{})
	for _, svc := range cf.Services {
		if svc.BuildRaw != nil && svc.Image != "" {
			localImages[svc.Image] = struct{}{}
		}
	}

	var pullable []string
	for name, svc := range cf.Services {
		if svc.Image == "" || svc.BuildRaw != nil {
			continue
		}
		if _, isLocal := localImages[svc.Image]; isLocal {
			slog.Debug("skipping pull for service using locally-built image", "service", name, "image", svc.Image)
			continue
		}
		pullable = append(pullable, name)
	}
	return pullable
}

// dockerfilePaths returns the absolute paths of all Dockerfiles referenced by
// services with a build: section. Both the string form (build: ".") and the
// map form (build: {context: ".", dockerfile: "Dockerfile"}) are supported.
// Missing Dockerfiles are skipped with a warning.
func (cf *composeFile) dockerfilePaths(workDir string) []string {
	seen := make(map[string]struct{})
	var paths []string

	for name, svc := range cf.Services {
		if svc.BuildRaw == nil {
			continue
		}

		var context, dockerfile string
		switch v := svc.BuildRaw.(type) {
		case string:
			context = v
			dockerfile = "Dockerfile"
		case map[string]any:
			if c, ok := v["context"].(string); ok {
				context = c
			}
			if df, ok := v["dockerfile"].(string); ok {
				dockerfile = df
			} else {
				dockerfile = "Dockerfile"
			}
		default:
			slog.Warn("unrecognized build field type, skipping", "service", name)
			continue
		}

		var dfPath string
		if filepath.IsAbs(dockerfile) {
			dfPath = dockerfile
		} else if context != "" {
			dfPath = filepath.Join(workDir, context, dockerfile)
		} else {
			dfPath = filepath.Join(workDir, dockerfile)
		}
		dfPath = filepath.Clean(dfPath)

		if _, err := os.Stat(dfPath); err != nil {
			slog.Warn("dockerfile not found, skipping", "service", name, "path", dfPath)
			continue
		}

		if _, exists := seen[dfPath]; !exists {
			seen[dfPath] = struct{}{}
			paths = append(paths, dfPath)
		}
	}

	return paths
}

// hasAnyImageChanged returns true if the current images differ from the previous ones.
// The comparison uses the full image reference string (e.g. "redis:7.2", "postgres:16-alpine@sha256:abc...")
// so any change in image name, tag, or digest is detected.
func hasAnyImageChanged(current, previous serviceImageByName) bool {
	if len(current) != len(previous) {
		return true
	}
	for name, img := range current {
		if previous[name] != img {
			return true
		}
	}
	return false
}
