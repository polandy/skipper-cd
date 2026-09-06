package deploy

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/events"
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

// hasHealthcheck reports whether any service in the compose file declares a
// Docker healthcheck. Backs the automatic deploy_health_check gate (ADR-0046): an
// operator who already opted a service into a compose healthcheck gets
// skipper's --wait + rollback gate for free, without also declaring
// deploy_health_check in the skipper config.
func (cf *composeFile) hasHealthcheck() bool {
	for _, svc := range cf.Services {
		if svc.HasHealthcheck() {
			return true
		}
	}
	return false
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

// buildSpec is one service's build: inputs as written in the compose file:
// the context and the Dockerfile within it, both possibly relative.
type buildSpec struct {
	context    string
	dockerfile string
}

// buildSpecs returns the build: section of every service that has one, in both
// the string form (build: ".") and the map form (build: {context: ".",
// dockerfile: "Dockerfile"}). Omitted fields get the defaults compose itself
// assumes, so every caller reads one normalized shape.
func (cf *composeFile) buildSpecs() map[string]buildSpec {
	specs := make(map[string]buildSpec)
	for name, svc := range cf.Services {
		if svc.Build == nil {
			continue
		}
		spec := buildSpec{context: ".", dockerfile: compose.DefaultDockerfile}
		switch v := svc.Build.(type) {
		case string:
			spec.context = v
		case map[string]any:
			if c, ok := v["context"].(string); ok && c != "" {
				spec.context = c
			}
			if df, ok := v["dockerfile"].(string); ok {
				spec.dockerfile = df
			}
		default:
			slog.Warn("unrecognized build field type, skipping", "service", name)
			continue
		}
		specs[name] = spec
	}
	return specs
}

// dockerfilePath resolves one service's Dockerfile to an absolute path against
// workDir — the stack's directory in the repo clone, which is also the tree the
// build reads from (see buildcontext.go). An absolute dockerfile: is taken as
// written.
func dockerfilePath(workDir string, spec buildSpec) string {
	if filepath.IsAbs(spec.dockerfile) {
		return filepath.Clean(spec.dockerfile)
	}
	return filepath.Clean(filepath.Join(workDir, spec.context, spec.dockerfile))
}

// dockerfileServices maps each build: service's Dockerfile to the services that
// build from it — several services can share one — so a changed Dockerfile can
// be attributed to the containers it rebuilds (attribution.go). Unlike
// dockerfilePaths this is a pure mapping over the compose file: it never stats,
// because only an already-hashed path is ever looked up in it.
func (cf *composeFile) dockerfileServices(workDir string) map[string][]string {
	services := make(map[string][]string)
	for name, spec := range cf.buildSpecs() {
		path := dockerfilePath(workDir, spec)
		services[path] = append(services[path], name)
	}
	for _, names := range services {
		sort.Strings(names)
	}
	return services
}

// dockerfilePaths returns the absolute paths of all Dockerfiles referenced by
// services with a build: section, resolved against workDir. Missing Dockerfiles
// are skipped with a warning.
func (cf *composeFile) dockerfilePaths(workDir string) []string {
	seen := make(map[string]struct{})
	var paths []string

	for name, spec := range cf.buildSpecs() {
		dfPath := dockerfilePath(workDir, spec)

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

// warnUnmatchedOnDemandContainers logs a warning for each on_demand_containers
// entry that does not match any service's container_name in the parsed
// compose file. Compose auto-generates container names (<project>-<service>-1)
// unless container_name: is set, so a name copied from `docker ps` can look
// right but drift the moment the project/service naming changes — declaring
// container_name pins it and makes it checkable here.
func (cf *composeFile) warnUnmatchedOnDemandContainers(stackName string, containers []string) {
	declared := make(map[string]bool, len(cf.Services))
	for _, svc := range cf.Services {
		if svc.ContainerName != "" {
			declared[svc.ContainerName] = true
		}
	}
	for _, name := range containers {
		if !declared[name] {
			slog.Warn("on_demand_containers entry does not match any service's container_name in docker-compose.yml — declare container_name on the corresponding service so docker stop targets it reliably",
				"stack", stackName, "container", name)
		}
	}
}

// imageChanges returns the per-service image reference changes between the
// previously deployed images and the current ones, sorted by service name. A
// service present only in current is reported with an empty Old (no previously
// recorded image); one present only in previous with an empty New (removed).
// Unchanged services — and build: services with no image: ref, absent from both
// maps — are omitted. Feeds the deploy event so notifications name what updated.
func imageChanges(current, previous serviceImageByName) []events.ServiceImageChange {
	names := make(map[string]struct{}, len(current)+len(previous))
	for name := range current {
		names[name] = struct{}{}
	}
	for name := range previous {
		names[name] = struct{}{}
	}
	var changes []events.ServiceImageChange
	for name := range names {
		oldImg, newImg := previous[name], current[name]
		if oldImg == newImg {
			continue
		}
		changes = append(changes, events.ServiceImageChange{Service: name, Old: oldImg, New: newImg})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Service < changes[j].Service })
	return changes
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
