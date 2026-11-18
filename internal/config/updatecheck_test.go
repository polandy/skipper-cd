package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestLoad_UpdateCheckDefaultsOn(t *testing.T) {
	cfg := loadFromString(t, minimalConfig)
	if got, want := cfg.UpdateCheckInterval(), 6*time.Hour; got != want {
		t.Errorf("UpdateCheckInterval() = %v, want %v (default on)", got, want)
	}
	if cfg.UpdateCheckNotify() {
		t.Error("UpdateCheckNotify() = true, want false by default (UI-only)")
	}
}

func TestLoad_UpdateCheckExplicitValues(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
update_check:
  interval_seconds: 3600
  notify: true
`)
	if got, want := cfg.UpdateCheckInterval(), time.Hour; got != want {
		t.Errorf("UpdateCheckInterval() = %v, want %v", got, want)
	}
	if !cfg.UpdateCheckNotify() {
		t.Error("UpdateCheckNotify() = false, want true")
	}
}

func TestLoad_UpdateCheckNotifyExplicitlyOff(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
update_check:
  notify: false
`)
	if cfg.UpdateCheckNotify() {
		t.Error("UpdateCheckNotify() = true, want false")
	}
}

func TestLoad_UpdateCheckZeroIntervalDisables(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
update_check:
  interval_seconds: 0
`)
	if got := cfg.UpdateCheckInterval(); got != 0 {
		t.Errorf("UpdateCheckInterval() = %v, want 0 (disabled)", got)
	}
}

func TestLoad_UpdateCheckRejectsNegativeInterval(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
update_check:
  interval_seconds: -1
`)
	if err == nil || !strings.Contains(err.Error(), "interval_seconds") {
		t.Fatalf("expected an interval_seconds error, got %v", err)
	}
}

func TestEffectiveUpdateCheck_PerStackOptOut(t *testing.T) {
	off := false
	cfg := loadFromString(t, minimalConfig)
	stacks := []config.Stack{
		{Name: "web"},
		{Name: "lagging", UpdateCheck: &off},
	}
	if !cfg.EffectiveUpdateCheck(stacks, "web") {
		t.Error("a stack without an override must be checked")
	}
	if cfg.EffectiveUpdateCheck(stacks, "lagging") {
		t.Error("update_check: false must opt the stack out")
	}
	if !cfg.EffectiveUpdateCheck(stacks, "unknown") {
		t.Error("an unknown stack falls back to the default (checked)")
	}
}

func TestLoadRepoStacks_CarriesUpdateCheckOverride(t *testing.T) {
	// The per-stack opt-out must survive the discovery merge — a dropped field
	// here silently re-enables the check for an opted-out stack.
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(filepath.Join(stacksDir, "lagging"), 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "services:\n  app:\n    image: nginx:1.25\n"
	if err := os.WriteFile(filepath.Join(stacksDir, "lagging", "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}

	off := false
	repo, stackErrs, err := config.LoadRepoStacks(stacksDir, []config.Stack{{Name: "lagging", UpdateCheck: &off}}, "")
	if err != nil || len(stackErrs) != 0 {
		t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v", err, stackErrs)
	}
	cfg := loadFromString(t, minimalConfig)
	if cfg.EffectiveUpdateCheck(repo.Stacks, "lagging") {
		t.Error("update_check: false was lost in the discovery merge")
	}
}
