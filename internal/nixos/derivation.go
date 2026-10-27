package nixos

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// SystemDrvKey is the reserved key under which the system derivation is
// recorded next to the file hashes. File keys are absolute paths, so a key
// starting with "@" never collides with one (ADR-0065).
const SystemDrvKey = "@system-drv"

// SystemDrvApplyExpr maps a nixosConfigurations entry to the drvPath of its
// system with system.configurationRevision forced to null, so a new commit
// alone does not change it (ADR-0065).
const SystemDrvApplyExpr = `c: (c.extendModules { modules = [ { system.configurationRevision = c.pkgs.lib.mkForce null; } ]; }).config.system.build.toplevel.drvPath`

// flakeAttrSeparator splits a flake reference from its attribute (`.#host`).
const flakeAttrSeparator = "#"

// drvSuffix ends every store derivation path.
const drvSuffix = ".drv"

// Outputter runs a command and returns its captured stdout. The evaluation
// needs the output, which the fire-and-forget command.Runner does not return.
type Outputter interface {
	Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// SplitFlakeAttr splits `<ref>#<name>` into its flake reference and attribute
// name; ok is false unless both parts are non-empty.
func SplitFlakeAttr(flake string) (ref, name string, ok bool) {
	ref, name, found := strings.Cut(flake, flakeAttrSeparator)
	if !found || ref == "" || name == "" {
		return "", "", false
	}
	return ref, name, true
}

// SystemDerivation evaluates the drvPath of the flake's NixOS system with
// system.configurationRevision forced to null (ADR-0065). It builds nothing.
// Output that is not a single absolute .drv path is an error, so an empty or
// garbled result never reads as a valid derivation.
func SystemDerivation(ctx context.Context, out Outputter, repoDir, flake string) (string, error) {
	ref, name, ok := SplitFlakeAttr(flake)
	if !ok {
		return "", fmt.Errorf("flake %q names no attribute; derivation change detection needs <ref>#<name>", flake)
	}
	// Quoted like nixos-rebuild does, so a host name containing a dot stays one attribute.
	installable := fmt.Sprintf(`%s%snixosConfigurations.%q`, ref, flakeAttrSeparator, name)
	stdout, err := out.Output(ctx, repoDir, "nix",
		"--extra-experimental-features", "nix-command flakes",
		"eval", "--raw", installable,
		"--apply", SystemDrvApplyExpr)
	if err != nil {
		return "", fmt.Errorf("evaluate the system derivation of %s: %w", flake, err)
	}
	drv := strings.TrimSpace(string(stdout))
	if !filepath.IsAbs(drv) || !strings.HasSuffix(drv, drvSuffix) || strings.ContainsAny(drv, " \t\r\n") {
		return "", fmt.Errorf("evaluate the system derivation of %s: output %q is not a store derivation path", flake, drv)
	}
	return drv, nil
}
