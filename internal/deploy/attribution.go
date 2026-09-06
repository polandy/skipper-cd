// Change attribution: naming the services a deploy's changed files actually
// reach, so a row says which container of the stack moved instead of only
// counting files (ADR-0059).
//
// The compose file is the case that matters — a sidecar gaining two environment
// variables redeploys the whole stack, and the row cannot say it was only the
// sidecar. It is resolved by comparing the previously deployed revision of the
// file with the current one service block by service block, never by mapping
// diff hunk line numbers. Everything skipper hashes project-wide (a stack
// env_files entry, the global vars_file, a watch_dirs file) reaches every
// service and is reported stack-wide rather than guessed at.
//
// Attribution is display only: it is not hashed, not a pull input and never a
// deploy trigger.

package deploy

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"gopkg.in/yaml.v3"
)

// attribution carries what resolving a stack's changed files to services needs:
// the compose file (parsed and its path), the stack whose tracked inputs the
// paths come from, and the two host-side paths that are not in it.
type attribution struct {
	composePath   string
	compose       *composeFile // nil when the compose file failed to parse
	stack         config.Stack
	varsFile      string
	stacksBaseDir string
}

// attributeChanges maps every changed file to its change kind and the services
// it reaches, in changed-file order. Returns nil for an empty change set, so a
// skip or a heal carries nothing.
func (d *Deployer) attributeChanges(ctx context.Context, a attribution, changed []string, lastDeployedCommit string) []events.FileChange {
	if len(changed) == 0 {
		return nil
	}
	var dockerfileServices map[string][]string
	if a.compose != nil {
		dockerfileServices = a.compose.dockerfileServices(filepath.Dir(a.composePath))
	}
	envFiles := make(map[string]bool, len(a.stack.EnvFiles))
	for _, f := range a.stack.EnvFiles {
		envFiles[f] = true
	}
	configKey := filepath.Join(a.stacksBaseDir, config.RepoConfigFileName)

	out := make([]events.FileChange, 0, len(changed))
	for _, path := range changed {
		// Every input but the compose file and a Dockerfile is project-wide:
		// compose applies it to the whole project, so it reaches every service.
		fc := events.FileChange{File: d.repoRelative(path), Kind: events.ChangeKindWatch, Wide: true}
		switch {
		case path == a.composePath:
			fc.Kind = events.ChangeKindCompose
			fc.Services, fc.Wide = d.changedComposeServices(ctx, a.composePath, lastDeployedCommit)
		case len(dockerfileServices[path]) > 0:
			fc.Kind, fc.Wide = events.ChangeKindBuild, false
			fc.Services = dockerfileServices[path]
		case envFiles[path]:
			fc.Kind = events.ChangeKindEnv
		case a.varsFile != "" && path == a.varsFile:
			fc.Kind = events.ChangeKindVars
		case path == configKey:
			fc.Kind = events.ChangeKindConfig
		}
		out = append(out, fc)
	}
	// changedFiles walks a map, so its order varies run to run — sort, or the
	// same change would serialize differently into the history each time.
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

// changedComposeServices names the services whose compose block differs from
// the revision this stack last deployed. wide is true when the change could not
// be resolved and must be read as reaching every service: nothing to compare
// against (no commit reader, no previous commit, an unreadable or unparseable
// revision), or a change that is not confined to service blocks. No names and
// no wide means the comparison ran and no service definition changed — a
// comment or formatting edit.
func (d *Deployer) changedComposeServices(ctx context.Context, composePath, lastDeployedCommit string) (services []string, wide bool) {
	if d.commitReader == nil || lastDeployedCommit == "" {
		return nil, true
	}
	oldContent, err := d.commitReader.FileAtCommit(ctx, lastDeployedCommit, composePath)
	if err != nil {
		slog.Debug("could not read the previously deployed compose file, reporting the change stack-wide", "file", composePath, "err", err)
		return nil, true
	}
	newContent, err := os.ReadFile(composePath)
	if err != nil {
		slog.Debug("could not read the compose file for change attribution", "file", composePath, "err", err)
		return nil, true
	}
	names, ok := diffComposeServices(oldContent, newContent)
	if !ok {
		return nil, true
	}
	return names, false
}

// composeRevision is one compose file revision split into its service blocks
// and everything around them (version, x- extension fields, volumes,
// networks…). The inline rest is what makes a change outside the services map —
// an anchor a service only aliases, say — report stack-wide instead of landing
// on no service at all.
type composeRevision struct {
	Services map[string]yaml.Node `yaml:"services"`
	Rest     map[string]yaml.Node `yaml:",inline"`
}

// diffComposeServices returns the services whose definition differs between two
// compose revisions. ok is false when the answer cannot be trusted: either
// revision fails to parse, or something outside the service blocks changed —
// both mean the change is not confined to the services this would name.
func diffComposeServices(oldContent, newContent []byte) (names []string, ok bool) {
	var oldRev, newRev composeRevision
	if err := yaml.Unmarshal(oldContent, &oldRev); err != nil {
		slog.Debug("could not parse the previously deployed compose file, reporting the change stack-wide", "err", err)
		return nil, false
	}
	if err := yaml.Unmarshal(newContent, &newRev); err != nil {
		slog.Debug("could not parse the compose file for change attribution, reporting the change stack-wide", "err", err)
		return nil, false
	}
	if !sameNodeMaps(oldRev.Rest, newRev.Rest) {
		return nil, false
	}
	seen := make(map[string]struct{}, len(oldRev.Services)+len(newRev.Services))
	for name := range oldRev.Services {
		seen[name] = struct{}{}
	}
	for name := range newRev.Services {
		seen[name] = struct{}{}
	}
	for name := range seen {
		oldNode, inOld := oldRev.Services[name]
		newNode, inNew := newRev.Services[name]
		// A service added or removed by this change is one of its services too:
		// the container it names is exactly what appeared or went away.
		if inOld != inNew || !sameNode(oldNode, newNode) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, true
}

// sameNodeMaps reports whether two YAML sub-document maps are equivalent.
func sameNodeMaps(a, b map[string]yaml.Node) bool {
	if len(a) != len(b) {
		return false
	}
	for key, node := range a {
		other, present := b[key]
		if !present || !sameNode(node, other) {
			return false
		}
	}
	return true
}

// sameNode reports whether two YAML sub-documents are equivalent, by
// re-encoding both: a document compared as text rather than walked, so a nested
// shape needs no special case. An alias is compared as the alias it is, which
// is what makes a changed anchor surface as a change to the surrounding
// document instead of silently to every service that references it.
func sameNode(a, b yaml.Node) bool {
	encA, errA := yaml.Marshal(&a)
	encB, errB := yaml.Marshal(&b)
	if errA != nil || errB != nil {
		return false
	}
	return string(encA) == string(encB)
}
