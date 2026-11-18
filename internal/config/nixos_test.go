package config_test

import (
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func nixosRebuildConfig(section string) string {
	return `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: modules
stacks: []
nixos_rebuild:
` + section
}

func TestLoad_NixOSRebuild_ChangeDetectionDefaultsToDerivation(t *testing.T) {
	cfg := loadFromString(t, nixosRebuildConfig(`  flake: ".#host-a"`))

	if !cfg.NixOSRebuild.ComparesDerivation() {
		t.Error("an unset change_detection must select the derivation mode")
	}
}

func TestLoad_NixOSRebuild_ChangeDetectionDerivation(t *testing.T) {
	cfg := loadFromString(t, nixosRebuildConfig(`  flake: ".#host-a"
  change_detection: derivation`))

	if !cfg.NixOSRebuild.ComparesDerivation() {
		t.Error("change_detection: derivation must select the derivation mode")
	}
	if cfg.NixOSRebuild.ChangeDetection != config.NixOSChangeDetectionDerivation {
		t.Errorf("ChangeDetection = %q", cfg.NixOSRebuild.ChangeDetection)
	}
}

func TestLoad_NixOSRebuild_ChangeDetectionFilesExplicit(t *testing.T) {
	cfg := loadFromString(t, nixosRebuildConfig(`  flake: ".#host-a"
  change_detection: files`))

	if cfg.NixOSRebuild.ComparesDerivation() {
		t.Error("change_detection: files must keep the files mode")
	}
}

func TestLoad_NixOSRebuild_RejectsUnknownChangeDetection(t *testing.T) {
	_, err := loadStringToConfig(t, nixosRebuildConfig(`  flake: ".#host-a"
  change_detection: drv`))
	if err == nil {
		t.Fatal("expected an unknown change_detection to be rejected")
	}
	for _, want := range []string{"change_detection", `"drv"`, "files", "derivation"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %s", err, want)
		}
	}
}

func TestLoad_NixOSRebuild_DerivationAcceptsFlakeWithoutAttribute(t *testing.T) {
	// Like nixos-rebuild, the evaluation falls back to the host name.
	for _, flake := range []string{".", ".#"} {
		cfg := loadFromString(t, nixosRebuildConfig(`  flake: "`+flake+`"`))
		if !cfg.NixOSRebuild.ComparesDerivation() || cfg.NixOSRebuild.Flake != flake {
			t.Errorf("flake %q: got mode derivation=%v, flake %q", flake, cfg.NixOSRebuild.ComparesDerivation(), cfg.NixOSRebuild.Flake)
		}
	}
}

func TestLoad_NixOSRebuild_DerivationNeedsFlakeRef(t *testing.T) {
	_, err := loadStringToConfig(t, nixosRebuildConfig(`  flake: "#host-a"`))
	if err == nil {
		t.Fatal("expected a flake without a reference to be rejected in derivation mode")
	}
	if !strings.Contains(err.Error(), "nixos_rebuild.flake") {
		t.Errorf("error %q should name nixos_rebuild.flake", err)
	}
}

func TestLoad_NixOSRebuild_FilesModeAcceptsAnyFlake(t *testing.T) {
	cfg := loadFromString(t, nixosRebuildConfig(`  flake: "#host-a"
  change_detection: files`))
	if cfg.NixOSRebuild.Flake != "#host-a" {
		t.Errorf("flake = %q", cfg.NixOSRebuild.Flake)
	}
}
