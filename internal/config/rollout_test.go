package config_test

import (
	"strings"
	"testing"
)

func TestLoad_RolloutParsed(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
stacks:
  - name: dashboard
    rollout:
      services: [web, api]
      health_timeout_seconds: 45
`)
	s, ok := cfg.StackByName("dashboard")
	if !ok {
		t.Fatal("stack dashboard not found")
	}
	if s.Rollout == nil {
		t.Fatal("rollout section not parsed")
	}
	if len(s.Rollout.Services) != 2 || s.Rollout.Services[0] != "web" || s.Rollout.Services[1] != "api" {
		t.Errorf("services = %v, want [web api]", s.Rollout.Services)
	}
	if s.Rollout.HealthTimeoutSeconds != 45 {
		t.Errorf("health_timeout_seconds = %d, want 45", s.Rollout.HealthTimeoutSeconds)
	}
}

func TestLoad_RolloutOptional(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
stacks:
  - name: web
`)
	s, _ := cfg.StackByName("web")
	if s.Rollout != nil {
		t.Errorf("expected no rollout, got %+v", s.Rollout)
	}
}

func TestLoad_RejectsEmptyRolloutServices(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
stacks:
  - name: web
    rollout:
      services: []
`)
	if err == nil {
		t.Fatal("expected error for an empty rollout services list")
	}
	if !strings.Contains(err.Error(), "rollout") {
		t.Errorf("error should mention rollout, got: %v", err)
	}
}

func TestLoad_RejectsBlankRolloutService(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
stacks:
  - name: web
    rollout:
      services: ["  "]
`)
	if err == nil {
		t.Fatal("expected error for a blank rollout service name")
	}
}

func TestLoad_RejectsNegativeRolloutTimeout(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
stacks:
  - name: web
    rollout:
      services: [web]
      health_timeout_seconds: -1
`)
	if err == nil {
		t.Fatal("expected error for negative rollout health_timeout_seconds")
	}
}

func TestLoad_RejectsNegativeDrainSeconds(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
stacks:
  - name: web
    rollout:
      services: [web]
      drain_seconds: -1
`)
	if err == nil {
		t.Fatal("expected error for negative rollout drain_seconds")
	}
}

func TestLoad_RolloutDrainSecondsParsed(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
stacks:
  - name: web
    rollout:
      services: [web]
      drain_seconds: 5
`)
	s, _ := cfg.StackByName("web")
	if s.Rollout == nil || s.Rollout.DrainSeconds != 5 {
		t.Errorf("drain_seconds = %+v, want 5", s.Rollout)
	}
}
