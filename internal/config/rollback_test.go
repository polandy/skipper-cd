package config

import "testing"

func policyPtr(p RollbackPolicy) *RollbackPolicy { return &p }

// TestRollbackPolicyFor covers the per-stack-over-global resolution, whose
// default is RollbackAlways (ADR-0050, ADR-0074).
func TestRollbackPolicyFor(t *testing.T) {
	tests := []struct {
		name     string
		global   *RollbackPolicy
		perStack *RollbackPolicy
		want     RollbackPolicy
	}{
		{name: "both unset defaults to always", want: RollbackAlways},
		{name: "global never, per-stack inherit", global: policyPtr(RollbackNever), want: RollbackNever},
		{name: "global never, per-stack always overrides", global: policyPtr(RollbackNever), perStack: policyPtr(RollbackAlways), want: RollbackAlways},
		{name: "global always, per-stack never overrides", global: policyPtr(RollbackAlways), perStack: policyPtr(RollbackNever), want: RollbackNever},
		{name: "global unset, per-stack patch_only", perStack: policyPtr(RollbackPatchOnly), want: RollbackPatchOnly},
		{name: "global patch_only, per-stack inherit", global: policyPtr(RollbackPatchOnly), want: RollbackPatchOnly},
		{name: "global patch_only, per-stack always overrides", global: policyPtr(RollbackPatchOnly), perStack: policyPtr(RollbackAlways), want: RollbackAlways},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Rollback: tt.global,
				Stacks:   []Stack{{Name: "web", Rollback: tt.perStack}},
			}
			if got := c.RollbackPolicyFor("web"); got != tt.want {
				t.Errorf("RollbackPolicyFor(web) = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRollbackPolicyFor_UnknownStackFallsBackToGlobal ensures a name not in the
// set follows the global default (mirrors SelfHealEnabled).
func TestRollbackPolicyFor_UnknownStackFallsBackToGlobal(t *testing.T) {
	c := &Config{Rollback: policyPtr(RollbackPatchOnly), Stacks: []Stack{{Name: "web", Rollback: policyPtr(RollbackNever)}}}
	if got := c.RollbackPolicyFor("missing"); got != RollbackPatchOnly {
		t.Errorf("unknown stack = %q, want the global patch_only", got)
	}

	on := &Config{Stacks: []Stack{{Name: "web"}}}
	if got := on.RollbackPolicyFor("missing"); got != RollbackAlways {
		t.Errorf("unknown stack with no global override = %q, want always", got)
	}
}

// TestEffectiveRollback_DiscoveredStacks verifies resolution over an explicit
// stack set, as used in stack-discovery mode where c.Stacks is empty.
func TestEffectiveRollback_DiscoveredStacks(t *testing.T) {
	c := &Config{Rollback: policyPtr(RollbackAlways)}
	discovered := []Stack{{Name: "db", Rollback: policyPtr(RollbackPatchOnly)}}
	if got := c.EffectiveRollback(discovered, "db"); got != RollbackPatchOnly {
		t.Errorf("db = %q, want its own patch_only over the global default", got)
	}
	if got := c.EffectiveRollback(discovered, "web"); got != RollbackAlways {
		t.Errorf("web = %q, want the global always", got)
	}
}
