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

func TestLoad_StackDiscoveryConflictsWithStacksList(t *testing.T) {
	_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/deploy.git
stacks_base_dir: /var/lib/skipper/repo/stacks
stack_discovery: true
stacks:
  - name: gitea
`)
	if err == nil || !strings.Contains(err.Error(), "stack_discovery") {
		t.Fatalf("expected a stack_discovery conflict error, got %v", err)
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
