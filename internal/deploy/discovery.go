package deploy

import (
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/config"
)

// ConfigStateKey is the reserved event key for repo stack-config failures in
// stack-discovery mode (ADR-0034), mirroring NixosStateKey. It is exported so
// the UI wiring can recognize the pseudo-stack.
const ConfigStateKey = "_config"

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

// addStackConfigHash records the stack's effective-config hash under the repo
// skipper.yaml path, so a config edit is detected as a change to that file —
// including in the UI's changed-files and diff views (the diff is the real git
// diff of skipper.yaml). A stack without a ConfigHash (legacy mode) adds
// nothing, keeping legacy change detection byte-identical.
func (d *Deployer) addStackConfigHash(hashes stackFileHashes, stack config.Stack) {
	if stack.ConfigHash == "" {
		return
	}
	hashes[filepath.Join(d.repoDir, config.RepoConfigFileName)] = stack.ConfigHash
}
