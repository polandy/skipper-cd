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

// ParseFlake splits a flake at its last "#" into the flake reference and the
// configuration name, as nixos-rebuild does. name is "" when the flake names
// no configuration (`.` or `.#`); ok is false when the reference is empty.
func ParseFlake(flake string) (ref, name string, ok bool) {
	ref = flake
	if i := strings.LastIndex(flake, flakeAttrSeparator); i >= 0 {
		ref, name = flake[:i], flake[i+len(flakeAttrSeparator):]
	}
	if ref == "" {
		return "", "", false
	}
	return ref, name, true
}

// SystemDerivation evaluates the drvPath of the flake's NixOS system with
// system.configurationRevision forced to null (ADR-0065). It builds nothing.
// A flake that names no configuration evaluates the one named after the host,
// the one nixos-rebuild switches to; hostname is read only then.
// Output that is not a single absolute .drv path is an error, so an empty or
// garbled result never reads as a valid derivation.
func SystemDerivation(ctx context.Context, out Outputter, repoDir, flake string, hostname func() (string, error)) (string, error) {
	ref, name, ok := ParseFlake(flake)
	if !ok {
		return "", fmt.Errorf("flake %q names no flake reference", flake)
	}
	if name == "" {
		host, err := hostname()
		if err != nil {
			return "", fmt.Errorf("flake %q names no configuration and the host name cannot be read: %w", flake, err)
		}
		if host == "" {
			return "", fmt.Errorf("flake %q names no configuration and the host name is empty", flake)
		}
		name = host
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
