package config_test

import (
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

// TestLoad_RollbackValues covers every accepted spelling of rollback, at both
// levels: the booleans of ADR-0050 and the patch_only policy of ADR-0074.
func TestLoad_RollbackValues(t *testing.T) {
	tests := []struct {
		value string
		want  config.RollbackPolicy
	}{
		{"true", config.RollbackAlways},
		{"false", config.RollbackNever},
		{"patch_only", config.RollbackPatchOnly},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cfg := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
rollback: `+tt.value+`
stacks:
  - name: web
    rollback: `+tt.value+`
`)
			if cfg.Rollback == nil || *cfg.Rollback != tt.want {
				t.Errorf("global rollback = %v, want %q", cfg.Rollback, tt.want)
			}
			if r := cfg.Stacks[0].Rollback; r == nil || *r != tt.want {
				t.Errorf("stack rollback = %v, want %q", r, tt.want)
			}
		})
	}
}

func TestLoad_RollbackOmittedResolvesToAlways(t *testing.T) {
	cfg := loadFromString(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: web
`)
	if cfg.Rollback != nil || cfg.Stacks[0].Rollback != nil {
		t.Fatalf("omitted rollback must stay unset, got global=%v stack=%v", cfg.Rollback, cfg.Stacks[0].Rollback)
	}
	if got := cfg.RollbackPolicyFor("web"); got != config.RollbackAlways {
		t.Errorf("RollbackPolicyFor(web) = %q, want always", got)
	}
}

func TestLoad_RollbackRejectsUnknownValue(t *testing.T) {
	for _, value := range []string{"minor_only", "always", "[true]"} {
		t.Run(value, func(t *testing.T) {
			_, err := loadStringToConfig(t, `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stack_discovery: false
stacks:
  - name: web
    rollback: `+value+`
`)
			if err == nil {
				t.Fatalf("rollback: %s loaded without error", value)
			}
			if !strings.Contains(err.Error(), "rollback must be true, false or patch_only") {
				t.Errorf("error does not name the accepted values: %v", err)
			}
		})
	}
}
