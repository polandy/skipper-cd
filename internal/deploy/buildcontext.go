// Build contexts: pinning what `docker compose build` reads to the repo clone.
//
// Compose resolves a relative build context against the **project directory**,
// and project_directory routinely points somewhere else entirely — a NixOS
// modules dir, not the clone (ADR-0045). Change detection meanwhile hashes the
// Dockerfile in the clone (Invariants 1 and 2), so the two trees can disagree:
// the deploy fires on a Dockerfile the build never reads, rebuilds the same
// image from a stale base, and the run reports a success that changed nothing.
//
// A generated compose override closes the gap by rewriting each relative build
// context to its absolute path under the clone. It is layered on for the whole
// apply — `up` builds too when a service sets `pull_policy: build` — while
// --project-directory stays on every call, so project identity, .env loading and
// every relative bind mount keep resolving there exactly as before (ADR-0057).

package deploy

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// buildContextOverrideFile is the generated compose override: nothing but the
// build context of each service that needs pinning, so compose merges it into
// the stack's own build section and leaves dockerfile, args and target alone.
type buildContextOverrideFile struct {
	Services map[string]buildContextOverrideService `yaml:"services"`
}

type buildContextOverrideService struct {
	Build buildContextOverrideBuild `yaml:"build"`
}

type buildContextOverrideBuild struct {
	Context string `yaml:"context"`
}

// buildContextOverride renders the compose override pinning every relative
// build context of the stack to its absolute path under repoDir, the directory
// holding the stack's compose file in the clone.
//
// Returns nil — no override needed — when no service has a relative build
// context: an absolute one already names the tree it builds from, and a service
// without build: is not built at all.
func buildContextOverride(cf *composeFile, repoDir string) ([]byte, error) {
	services := make(map[string]buildContextOverrideService)
	for name, spec := range cf.buildSpecs() {
		if filepath.IsAbs(spec.context) {
			continue
		}
		context := filepath.Clean(filepath.Join(repoDir, spec.context))
		services[name] = buildContextOverrideService{Build: buildContextOverrideBuild{Context: context}}
	}
	if len(services) == 0 {
		return nil, nil
	}
	return yaml.Marshal(buildContextOverrideFile{Services: services})
}

// withClonedBuildContexts returns a copy of the run whose `docker compose`
// invocations layer on a generated override pinning the build contexts to the
// clone, plus a cleanup func that removes the generated file — call it once for
// the stack's whole apply, so build and up read the same tree. The run is
// returned unchanged, with a no-op cleanup, when there is nothing to pin or no
// project directory to diverge from.
func (r stackRun) withClonedBuildContexts(cf *composeFile) (stackRun, func(), error) {
	noCleanup := func() {}
	if cf == nil || r.projectDir == "" {
		return r, noCleanup, nil
	}
	override, err := buildContextOverride(cf, filepath.Dir(r.composePath))
	if err != nil || override == nil {
		return r, noCleanup, err
	}

	f, err := os.CreateTemp("", "skipper-build-context-*.yml")
	if err != nil {
		return r, noCleanup, fmt.Errorf("create build context override: %w", err)
	}
	cleanup := func() {
		if err := os.Remove(f.Name()); err != nil {
			slog.Warn("could not remove build context override", "stack", r.stack.Name, "path", f.Name(), "err", err)
		}
	}
	if _, err := f.Write(override); err != nil {
		f.Close()
		cleanup()
		return r, noCleanup, fmt.Errorf("write build context override: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return r, noCleanup, fmt.Errorf("write build context override: %w", err)
	}

	r.extraComposeFiles = append(slices.Clone(r.extraComposeFiles), f.Name())
	return r, cleanup, nil
}
