package deploy

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/polandy/skipper-cd/internal/compose"
)

// serviceImageByName maps each compose service name to its image reference.
type serviceImageByName map[string]string

// composeFile wraps the shared compose parse (internal/compose) with the deploy
// package's image/build/rollout helpers. Parsed once per stack deploy.
type composeFile struct {
	compose.File
}

// parseComposeFile reads and parses a docker-compose.yml.
func parseComposeFile(path string) (*composeFile, error) {
	f, err := compose.Parse(path)
	if err != nil {
		return nil, err
	}
	return &composeFile{File: *f}, nil
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

// servicesExcept returns the compose service names not in the set, sorted — the
// services a rollout deploys in place.
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
		if svc.Build != nil && svc.Image != "" {
			localImages[svc.Image] = struct{}{}
		}
	}

	var pullable []string
	for name, svc := range cf.Services {
		if svc.Image == "" || svc.Build != nil {
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
		if svc.Build == nil {
			continue
		}

		var context, dockerfile string
		switch v := svc.Build.(type) {
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
