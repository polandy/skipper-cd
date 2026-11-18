package config_test

import (
	"strings"
	"testing"
)

func TestLoad_HealthWatchOmittedSectionIsDisabled(t *testing.T) {
	cfg := loadFromString(t, minimalConfig)
	if cfg.HealthWatch != nil {
		t.Fatalf("expected nil health_watch when omitted, got %+v", cfg.HealthWatch)
	}
}

func TestLoad_HealthWatchDefaults(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
health_watch: {}
`)
	hw := cfg.HealthWatch
	if hw == nil {
		t.Fatal("expected health_watch section")
	}
	if hw.DebouncePolls != 2 {
		t.Errorf("expected default debounce 2, got %d", hw.DebouncePolls)
	}
	if hw.AttributionWindowSeconds != 300 {
		t.Errorf("expected default attribution window 300, got %d", hw.AttributionWindowSeconds)
	}
}

func TestLoad_HealthWatchHonoursExplicitValues(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
health_watch:
  debounce_polls: 3
  attribution_window_seconds: 60
  targets:
    - format: signal
      url: http://localhost:8020
      number: "+4100000000"
      recipients: ["+4111111111"]
      prefix: host-a
`)
	hw := cfg.HealthWatch
	if hw.DebouncePolls != 3 || hw.AttributionWindowSeconds != 60 {
		t.Errorf("explicit values not honoured: %+v", hw)
	}
	if len(hw.Targets) != 1 || hw.Targets[0].Prefix != "host-a" {
		t.Errorf("unexpected targets: %+v", hw.Targets)
	}
}

func TestLoad_HealthWatchRequiresPositivePollInterval(t *testing.T) {
	// The watchdog rides the health poller's cadence (like self-heal), so a
	// disabled health poll cannot host it.
	_, err := loadStringToConfig(t, minimalConfig+`
runtime_health_poll_interval_seconds: 0
health_watch: {}
`)
	if err == nil || !strings.Contains(err.Error(), "runtime_health_poll_interval_seconds") {
		t.Fatalf("expected the poll-interval requirement error, got %v", err)
	}
}

func TestLoad_HealthWatchTargetFormatDefaultsToGeneric(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
health_watch:
  targets:
    - url: https://ntfy.example.com/skipper
`)
	if got := cfg.HealthWatch.Targets[0].Format; got != "generic" {
		t.Errorf("expected default format generic, got %q", got)
	}
}

func TestLoad_HealthWatchRejectsNegativeDebounce(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
health_watch:
  debounce_polls: -2
`)
	if err == nil || !strings.Contains(err.Error(), "debounce_polls") {
		t.Fatalf("expected debounce_polls error, got %v", err)
	}
}

func TestLoad_HealthWatchAlertCooldownDefaultsToThirtyMinutes(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
health_watch: {}
`)
	got := cfg.HealthWatch.AlertCooldownSeconds
	if got == nil || *got != 1800 {
		t.Errorf("expected default alert cooldown 1800, got %v", got)
	}
}

func TestLoad_HealthWatchExplicitZeroDisablesAlertCooldown(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
health_watch:
  alert_cooldown_seconds: 0
`)
	got := cfg.HealthWatch.AlertCooldownSeconds
	if got == nil || *got != 0 {
		t.Errorf("expected explicit 0 to disable the cooldown, got %v", got)
	}
}

func TestLoad_HealthWatchHonoursAlertCooldown(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
health_watch:
  alert_cooldown_seconds: 900
`)
	got := cfg.HealthWatch.AlertCooldownSeconds
	if got == nil || *got != 900 {
		t.Errorf("expected alert cooldown 900, got %v", got)
	}
}

func TestLoad_HealthWatchRejectsNegativeAlertCooldown(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
health_watch:
  alert_cooldown_seconds: -1
`)
	if err == nil || !strings.Contains(err.Error(), "alert_cooldown_seconds") {
		t.Fatalf("expected alert_cooldown_seconds error, got %v", err)
	}
}

func TestLoad_HealthWatchRejectsOnFieldInTarget(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
health_watch:
  targets:
    - url: https://ntfy.example.com/skipper
      on: [failed]
`)
	if err == nil || !strings.Contains(err.Error(), "on") {
		t.Fatalf("expected an error rejecting on:, got %v", err)
	}
}

func TestLoad_HealthWatchValidatesTargetsLikeNotifications(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
health_watch:
  targets:
    - format: signal
      url: http://localhost:8020
`)
	if err == nil || !strings.Contains(err.Error(), "number") {
		t.Fatalf("expected signal number validation, got %v", err)
	}
}
