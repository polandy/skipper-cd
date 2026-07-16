package config_test

import (
	"strings"
	"testing"
)

func TestLoad_SelfHealDefaultsOffWithPacingDefaults(t *testing.T) {
	cfg, err := loadStringToConfig(t, minimalConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SelfHealActive() {
		t.Error("self-heal should be off by default")
	}
	if cfg.SelfHealMinUnhealthyPolls != 3 {
		t.Errorf("min_unhealthy_polls default: want 3, got %d", cfg.SelfHealMinUnhealthyPolls)
	}
	if cfg.SelfHealMaxAttempts != 3 {
		t.Errorf("max_attempts default: want 3, got %d", cfg.SelfHealMaxAttempts)
	}
	if cfg.SelfHealCooldownSeconds != 60 {
		t.Errorf("cooldown default: want 60, got %d", cfg.SelfHealCooldownSeconds)
	}
}

func TestLoad_SelfHealGlobalOnAppliesToStacks(t *testing.T) {
	cfg, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
self_heal: true
stacks:
  - name: web
  - name: db
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SelfHealActive() {
		t.Fatal("expected self-heal active with global self_heal: true")
	}
	if !cfg.SelfHealEnabled("web") || !cfg.SelfHealEnabled("db") {
		t.Error("both stacks should inherit the global self_heal: true")
	}
}

func TestLoad_SelfHealPerStackOverridesGlobal(t *testing.T) {
	cfg, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
self_heal: true
stacks:
  - name: web
  - name: db
    self_heal: false
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SelfHealEnabled("web") {
		t.Error("web should inherit global on")
	}
	if cfg.SelfHealEnabled("db") {
		t.Error("db should honour its per-stack self_heal: false override")
	}
	if !cfg.SelfHealActive() {
		t.Error("self-heal is still active because web has it on")
	}
}

func TestLoad_SelfHealPerStackOptInWithGlobalOff(t *testing.T) {
	cfg, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: web
    self_heal: true
  - name: db
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SelfHealEnabled("web") {
		t.Error("web opted in explicitly")
	}
	if cfg.SelfHealEnabled("db") {
		t.Error("db should stay off (global default off)")
	}
}

func TestLoad_SelfHealRequiresPositiveHealthPollInterval(t *testing.T) {
	_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
self_heal: true
health_poll_interval_seconds: 0
stacks:
  - name: web
`)
	if err == nil || !strings.Contains(err.Error(), "health_poll_interval_seconds") {
		t.Fatalf("expected self_heal-requires-poll error, got %v", err)
	}
}

func TestLoad_SelfHealRejectsNonPositivePacing(t *testing.T) {
	for _, field := range []string{"self_heal_min_unhealthy_polls", "self_heal_max_attempts"} {
		_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
`+field+`: -1
`)
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s: expected validation error, got %v", field, err)
		}
	}
}
