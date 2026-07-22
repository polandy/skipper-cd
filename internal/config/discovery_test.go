package config_test

import (
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestLoad_StackDiscoveryEnabled(t *testing.T) {
	cfg := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: stacks
stack_discovery: true
`)
	if !cfg.StackDiscovery {
		t.Error("StackDiscovery should be true")
	}
}

func TestLoad_StackDiscoveryDefaultsTrue(t *testing.T) {
	// An omitted stack_discovery enables discovery (ADR-0043).
	cfg := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: stacks
`)
	if !cfg.StackDiscovery {
		t.Error("stack_discovery should default to true when omitted")
	}
}

func TestLoad_StackDiscoveryExplicitFalse(t *testing.T) {
	// An explicit false opts into listing the stacks in the config (the probe
	// must distinguish an omitted key from an explicit false).
	cfg := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stack_discovery: false
stacks:
  - name: web
    project_directory: /opt/web
`)
	if cfg.StackDiscovery {
		t.Error("explicit stack_discovery: false must stay false")
	}
}

func TestLoad_StackDiscoveryAllowsStacksListAsOverrides(t *testing.T) {
	// ADR-0043: under discovery the stacks: list is an optional per-stack
	// override map (matched to discovered directories by name), so it no longer
	// conflicts with stack_discovery.
	cfg, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: stacks
stack_discovery: true
stacks:
  - name: gitea
    icon: gitea
`)
	if err != nil {
		t.Fatalf("stacks list under discovery should be allowed: %v", err)
	}
	if len(cfg.Stacks) != 1 || cfg.Stacks[0].Name != "gitea" {
		t.Fatalf("override entry not kept: %+v", cfg.Stacks)
	}
}

func TestLoad_StackDiscoveryOmittedStacksBaseDirScansRepoRoot(t *testing.T) {
	// Omitted stacks_base_dir means the repo root itself is the stacks base,
	// so discovery scans the clone root. It resolves to the effective repo_dir.
	cfg, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
repo_dir: /srv/clone
stack_discovery: true
`)
	if err != nil {
		t.Fatalf("omitted stacks_base_dir under discovery should be allowed: %v", err)
	}
	if cfg.StacksBaseDir != "/srv/clone" {
		t.Errorf("expected stacks_base_dir to resolve to the repo root %q, got %q", "/srv/clone", cfg.StacksBaseDir)
	}
}

func TestLoad_ReservedConfigStackNameRejected(t *testing.T) {
	_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: stacks
stacks:
  - name: _config
`)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected a reserved-name error, got %v", err)
	}
}

func TestSelfHealActive_DiscoveryModeFollowsGlobalFlag(t *testing.T) {
	// The stack set is unknown at startup in discovery mode, so activation
	// (and thus headless polling) follows the global flag alone.
	on := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: stacks
stack_discovery: true
self_heal: true
`)
	if !on.SelfHealActive() {
		t.Error("SelfHealActive should be true with the global flag on")
	}
	off := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: stacks
stack_discovery: true
`)
	if off.SelfHealActive() {
		t.Error("SelfHealActive should be false with the global flag off")
	}
}

func TestEffectiveSelfHeal_UsesExplicitStackSet(t *testing.T) {
	cfg := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: stacks
stack_discovery: true
self_heal: true
`)
	if !cfg.EffectiveSelfHeal(nil, "unknown") {
		t.Error("unknown stack should inherit the global default (on)")
	}
	f := false
	withOverride := []config.Stack{{Name: "web", SelfHeal: &f}}
	if cfg.EffectiveSelfHeal(withOverride, "web") {
		t.Error("per-stack override false should win over global on")
	}
}
