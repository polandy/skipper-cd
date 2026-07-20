package config_test

import (
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestLoad_StackDiscoveryEnabled(t *testing.T) {
	cfg := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: /var/lib/skipper/repo/stacks
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
stacks_base_dir: /var/lib/skipper/repo/stacks
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
    working_dir: /opt/web
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
stacks_base_dir: /var/lib/skipper/repo/stacks
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

func TestLoad_StackDiscoveryRequiresStacksBaseDir(t *testing.T) {
	_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stack_discovery: true
`)
	if err == nil || !strings.Contains(err.Error(), "stacks_base_dir") {
		t.Fatalf("expected a stacks_base_dir error, got %v", err)
	}
}

func TestLoad_ReservedConfigStackNameRejected(t *testing.T) {
	_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: /var/lib/skipper/repo/stacks
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
stacks_base_dir: /var/lib/skipper/repo/stacks
stack_discovery: true
self_heal: true
`)
	if !on.SelfHealActive() {
		t.Error("SelfHealActive should be true with the global flag on")
	}
	off := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: /var/lib/skipper/repo/stacks
stack_discovery: true
`)
	if off.SelfHealActive() {
		t.Error("SelfHealActive should be false with the global flag off")
	}
}

func TestEffectiveSelfHeal_UsesExplicitStackSet(t *testing.T) {
	cfg := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: /var/lib/skipper/repo/stacks
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
