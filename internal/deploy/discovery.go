package deploy

import (
	"maps"
	"path/filepath"
	"slices"

	"github.com/polandy/skipper-cd/internal/config"
)

// ConfigStateKey is the reserved event key for repo stack-config failures in
// stack-discovery mode (ADR-0034), mirroring NixosStateKey. It is exported so
// the UI wiring can recognize the pseudo-stack. Aliases
// config.ReservedConfigStackName, the single source of truth for the reserved
// value.
const ConfigStateKey = config.ReservedConfigStackName

// CurrentStacks returns the most recently discovered stack set (stack
// discovery, ADR-0034): nil before the first successful discovery and when the
// stacks are listed explicitly. Out-of-run consumers — the health poller,
// self-heal, the UI wiring — read the effective stacks through this. Safe for
// concurrent use.
func (d *Deployer) CurrentStacks() []config.Stack {
	if p := d.discoveredStacks.Load(); p != nil {
		return p.Stacks
	}
	return nil
}

// CurrentDisabledStacks returns the names parked via disabled: true in the
// most recently discovered set, for the UI's disabled line. nil before the
// first discovery and when the stacks are listed explicitly. Safe for concurrent use.
func (d *Deployer) CurrentDisabledStacks() []string {
	if p := d.discoveredStacks.Load(); p != nil {
		return p.Disabled
	}
	return nil
}

// CurrentProjectDirs returns the recorded stack→project-dir map, for orphan
// detection (ADR-0036) — it recognizes a removed stack's project even when its
// dir lay outside stacks_base_dir. Empty before the first run; concurrent-safe.
// Returns a per-call copy the caller may mutate freely.
func (d *Deployer) CurrentProjectDirs() map[string]string {
	if p := d.projectDirs.Load(); p != nil {
		return maps.Clone(*p)
	}
	return map[string]string{}
}

// CurrentTrackedFiles returns the recorded stack→hashed-path map: for each
// stack, the input files whose hashes decide whether it redeploys. It answers
// the roster's "what does skipper watch here, and why has nothing happened"
// from the same state the decision reads. Paths are as recorded (absolute);
// empty before the first run. Concurrent-safe. Returns a per-call copy the
// caller may mutate freely.
func (d *Deployer) CurrentTrackedFiles() map[string][]string {
	p := d.trackedFiles.Load()
	if p == nil {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(*p))
	for stack, files := range *p {
		out[stack] = slices.Clone(files)
	}
	return out
}

// CurrentRunningImages returns the recorded stack→service→running-image map:
// what each stack's containers ran after its last successful deploy
// (ADR-0053), for the out-of-run update check (ADR-0054). Seeded from the
// persisted state at construction, refreshed after every run. Empty before
// any deploy has recorded one. Concurrent-safe; returns a per-call copy the
// caller may mutate freely.
func (d *Deployer) CurrentRunningImages() map[string]map[string]string {
	p := d.runningImagesNow.Load()
	if p == nil {
		return map[string]map[string]string{}
	}
	out := make(map[string]map[string]string, len(*p))
	for stack, images := range *p {
		out[stack] = maps.Clone(images)
	}
	return out
}

// effectiveStack resolves a stack by name from the effective set: the
// discovered stacks in discovery mode, else the host config's list.
func (d *Deployer) effectiveStack(cfg *config.Config, name string) (config.Stack, bool) {
	if !cfg.StackDiscovery {
		return cfg.StackByName(name)
	}
	for _, s := range d.CurrentStacks() {
		if s.Name == name {
			return s, true
		}
	}
	return config.Stack{}, false
}

// addStackConfigHash records the stack's effective-config hash under a synthetic
// per-stack key so a config edit is detected and redeploys that stack. The key
// is a label, not a file read: as of ADR-0043 the config is host-side, so no git
// diff exists for it. A stack without a ConfigHash adds nothing.
func (d *Deployer) addStackConfigHash(hashes stackFileHashes, stack config.Stack, stacksBaseDir string) {
	if stack.ConfigHash == "" {
		return
	}
	hashes[filepath.Join(stacksBaseDir, config.RepoConfigFileName)] = stack.ConfigHash
}
