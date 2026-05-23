package deploy

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// serviceImageByName maps each compose service name to its image reference.
type serviceImageByName map[string]string

// composeFile is a minimal representation of a docker-compose.yml,
// used only to extract image references.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image    string      `yaml:"image"`
	BuildRaw interface{} `yaml:"build"`
}

// extractComposeImages parses a docker-compose.yml and returns a map of
// service name to image reference. Services without an image field (e.g.
// those using build:) are omitted.
func extractComposeImages(composePath string) (serviceImageByName, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}

	images := make(serviceImageByName)
	for name, svc := range cf.Services {
		if svc.Image != "" {
			images[name] = svc.Image
		}
	}
	return images, nil
}

// extractDockerfilePaths parses a docker-compose.yml and returns the absolute paths of
// all Dockerfiles referenced by services with a build: section. Both the string form
// (build: ".") and the map form (build: {context: ".", dockerfile: "Dockerfile"}) are
// supported. Missing Dockerfiles are skipped with a warning.
func extractDockerfilePaths(composePath, workDir string) ([]string, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}

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
		case map[string]interface{}:
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

	return paths, nil
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
