package config

import (
	"fmt"

	"github.com/polandy/skipper-cd/internal/nixos"
)

// NixOS change detection modes (nixos_rebuild.change_detection).
const (
	// NixOSChangeDetectionFiles rebuilds when any *.nix file or flake.lock in
	// the repository changed.
	NixOSChangeDetectionFiles = "files"
	// NixOSChangeDetectionDerivation rebuilds when the derivation of the
	// host's system changed (ADR-0065). The default.
	NixOSChangeDetectionDerivation = "derivation"
)

// NixOSRebuild configures automatic NixOS rebuilds when the host's
// configuration in the repository changes.
type NixOSRebuild struct {
	Enabled *bool  `yaml:"enabled"` // nil = true when section present
	Flake   string `yaml:"flake"`   // e.g. ".#host-a", required when enabled
	// ChangeDetection is NixOSChangeDetectionDerivation (also when empty) or
	// NixOSChangeDetectionFiles.
	ChangeDetection string `yaml:"change_detection"`
}

// IsEnabled returns whether NixOS rebuild is enabled.
// Section omitted → disabled. Section present without enabled key → enabled.
// Explicit enabled value → that value.
func (n *NixOSRebuild) IsEnabled() bool {
	if n == nil {
		return false
	}
	if n.Enabled == nil {
		return true
	}
	return *n.Enabled
}

// ComparesDerivation reports whether the rebuild decision compares the
// system derivation rather than the nix file hashes.
func (n *NixOSRebuild) ComparesDerivation() bool {
	return n != nil && n.ChangeDetection != NixOSChangeDetectionFiles
}

// validateNixOSRebuild checks an enabled nixos_rebuild section.
func validateNixOSRebuild(n *NixOSRebuild) error {
	if !n.IsEnabled() {
		return nil
	}
	if n.Flake == "" {
		return fmt.Errorf("nixos_rebuild.flake is required when nixos_rebuild is enabled")
	}
	switch n.ChangeDetection {
	case "", NixOSChangeDetectionFiles, NixOSChangeDetectionDerivation:
	default:
		return fmt.Errorf("nixos_rebuild.change_detection must be %q or %q, got %q",
			NixOSChangeDetectionFiles, NixOSChangeDetectionDerivation, n.ChangeDetection)
	}
	if n.ComparesDerivation() {
		if _, _, ok := nixos.ParseFlake(n.Flake); !ok {
			return fmt.Errorf("nixos_rebuild.flake: %q names no flake reference; change_detection: %s needs <ref>[#<name>], e.g. \".#myhost\"",
				n.Flake, NixOSChangeDetectionDerivation)
		}
	}
	return nil
}
