package deploy

import (
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/config"
)

// ConfigStateKey is the reserved event key for repo stack-config failures in
// stack-discovery mode (ADR-0034), mirroring NixosStateKey. It is exported so
// the UI wiring can recognize the pseudo-stack. Aliases
// config.ReservedConfigStackName, the single source of truth for the reserved
// value.
const ConfigStateKey = config.ReservedConfigStackName

// CurrentStacks returns the most recently discovered stack set (stack
// discovery, ADR-0034): nil before the first successful discovery and in
// legacy (host stacks list) mode. Out-of-run consumers — the health poller,
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
// first discovery and in legacy mode. Safe for concurrent use.
func (d *Deployer) CurrentDisabledStacks() []string {
	if p := d.discoveredStacks.Load(); p != nil {
		return p.Disabled
	}
	return nil
}

// CurrentProjectDirs returns the recorded stack→project-dir map of the last
// deploy run, for orphan detection (ADR-0036): it lets a removed stack's
// running project be recognized by its working_dir even when that dir lay
// outside stacks_base_dir. Empty before the first run. Safe for concurrent use.
func (d *Deployer) CurrentProjectDirs() map[string]string {
	if p := d.projectDirs.Load(); p != nil {
		return *p
	}
	return map[string]string{}
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

// addStackConfigHash records the stack's effective-config hash under the
// skipper.yaml path (at the stacks base dir), so a config edit is detected as a
// change to that file — including in the UI's changed-files and diff views (the
// diff is the real git diff of skipper.yaml). A stack without a ConfigHash
// (legacy mode) adds nothing, keeping legacy change detection byte-identical.
func (d *Deployer) addStackConfigHash(hashes stackFileHashes, stack config.Stack, stacksBaseDir string) {
	if stack.ConfigHash == "" {
		return
	}
	hashes[filepath.Join(stacksBaseDir, config.RepoConfigFileName)] = stack.ConfigHash
}
