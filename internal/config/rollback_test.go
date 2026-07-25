package config

import "testing"

// TestRollbackEnabled covers the per-stack-over-global resolution, whose default
// is on: rollback happens unless explicitly disabled (ADR-0050).
func TestRollbackEnabled(t *testing.T) {
	tests := []struct {
		name        string
		global      *bool
		perStack    *bool
		wantEnabled bool
	}{
		{name: "both unset defaults on", global: nil, perStack: nil, wantEnabled: true},
		{name: "global off, per-stack inherit", global: boolPtr(false), perStack: nil, wantEnabled: false},
		{name: "global off, per-stack opt-in overrides", global: boolPtr(false), perStack: boolPtr(true), wantEnabled: true},
		{name: "global on, per-stack opt-out overrides", global: boolPtr(true), perStack: boolPtr(false), wantEnabled: false},
		{name: "global unset, per-stack opt-out", global: nil, perStack: boolPtr(false), wantEnabled: false},
		{name: "global on explicit, per-stack inherit", global: boolPtr(true), perStack: nil, wantEnabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Rollback: tt.global,
				Stacks:   []Stack{{Name: "web", Rollback: tt.perStack}},
			}
			if got := c.RollbackEnabled("web"); got != tt.wantEnabled {
				t.Errorf("RollbackEnabled(web) = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

// TestRollbackEnabled_UnknownStackFallsBackToGlobal ensures a name not in the
// set follows the global default (mirrors SelfHealEnabled).
func TestRollbackEnabled_UnknownStackFallsBackToGlobal(t *testing.T) {
	c := &Config{Rollback: boolPtr(false), Stacks: []Stack{{Name: "web"}}}
	if c.RollbackEnabled("missing") {
		t.Error("unknown stack should follow the global default (off)")
	}

	on := &Config{Stacks: []Stack{{Name: "web"}}}
	if !on.RollbackEnabled("missing") {
		t.Error("unknown stack with no global override should default on")
	}
}

// TestEffectiveRollback_DiscoveredStacks verifies resolution over an explicit
// stack set, as used in stack-discovery mode where c.Stacks is empty.
func TestEffectiveRollback_DiscoveredStacks(t *testing.T) {
	c := &Config{Rollback: boolPtr(true)}
	discovered := []Stack{{Name: "db", Rollback: boolPtr(false)}}
	if c.EffectiveRollback(discovered, "db") {
		t.Error("per-stack opt-out in the discovered set should win over the global default")
	}
	if !c.EffectiveRollback(discovered, "web") {
		t.Error("a discovered stack without an override should follow the global default (on)")
	}
}
