// Config validation orchestration: the order the sections' own validate
// methods run in (refuse to start), and the cross-section warnings (start,
// but warn).

package config

import (
	"fmt"

	"github.com/polandy/skipper-cd/internal/uitheme"
)

// collectWarnings checks for valid-but-suspicious configs that don't warrant
// refusing to start. Runs only once validateConfig has already accepted cfg.
func collectWarnings(cfg *Config) []string {
	var warnings []string
	// Discovery off, no explicit stacks, and no nixos_rebuild: skipper has
	// nothing to deploy or manage. Under discovery this is not suspicious —
	// the repo may simply not have been synced yet.
	if !cfg.StackDiscovery && len(cfg.Stacks) == 0 && !cfg.NixOSRebuild.IsEnabled() {
		warnings = append(warnings, "stack_discovery is off, no stacks are configured, and nixos_rebuild is disabled — skipper-cd has nothing to deploy; set stack_discovery: true, add entries under stacks:, or configure nixos_rebuild")
	}

	// Under discovery, SelfHealActive follows the global self_heal flag alone —
	// the stack set is unknown at startup, so a per-stack override cannot
	// activate the poller by itself. A stacks: override that sets self_heal:
	// true while the global flag is off therefore never takes effect.
	if cfg.StackDiscovery && (cfg.SelfHeal == nil || !*cfg.SelfHeal) {
		for _, s := range cfg.Stacks {
			if s.SelfHeal != nil && *s.SelfHeal {
				warnings = append(warnings, fmt.Sprintf("stack %q sets self_heal: true, but stack_discovery is on and the global self_heal is off — under discovery only the global flag activates self-heal, so this override never takes effect; set the top-level self_heal: true instead", s.Name))
			}
		}
	}

	// command_timeout_seconds is the hard ceiling of every command, so a hook
	// timeout above it never has its intended effect.
	for _, s := range cfg.Stacks {
		if s.Hooks.TimeoutSeconds > cfg.CommandTimeoutSeconds {
			warnings = append(warnings, fmt.Sprintf("stack %q: hooks.timeout_seconds (%d) exceeds command_timeout_seconds (%d) — the hook is capped to the lower value; raise command_timeout_seconds or lower hooks.timeout_seconds", s.Name, s.Hooks.TimeoutSeconds, cfg.CommandTimeoutSeconds))
		}
	}

	// Per-host identity colours are drawn from a fixed palette (uitheme.HostColorCount
	// slots); the merged multi-host UI (ADR-0048) counts this host plus every
	// peer. Beyond the palette size two hosts hash to the same colour, so the
	// colour stops uniquely identifying a host.
	if totalHosts := len(cfg.Peers) + 1; totalHosts > uitheme.HostColorCount {
		warnings = append(warnings, fmt.Sprintf("%d hosts are configured (this host plus %d peers) but only %d distinct host colours are available — some hosts will share a colour in the merged UI; reduce the number of peers to keep colours unique", totalHosts, len(cfg.Peers), uitheme.HostColorCount))
	}

	return warnings
}

// validateConfig checks every section once its defaults are applied; the
// first section that fails rejects the config.
func validateConfig(cfg *Config) error {
	for _, check := range []func() error{
		cfg.validateSource,
		cfg.validateHostPaths,
		cfg.validateServer,
		cfg.Icons.validate,
		cfg.validateStacks,
		cfg.NixOSRebuild.validate,
		cfg.validateSelfHeal,
		cfg.validateNotifications,
		func() error { return validatePeers(cfg.Peers) },
		func() error { return validateListen(cfg.ListenAddress, cfg.AllowedHosts) },
		cfg.validateHealthWatch,
		cfg.validateUpdateCheck,
	} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}
