package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// RollbackPolicy is when a failed deploy is restored to the previous compose
// version (ADR-0050, ADR-0074). Written as `true`, `false` or `patch_only`.
type RollbackPolicy string

const (
	// RollbackAlways restores every failed new version (`true`, the default).
	RollbackAlways RollbackPolicy = "always"
	// RollbackNever leaves every failed new version running (`false`).
	RollbackNever RollbackPolicy = "never"
	// RollbackPatchOnly restores a failed new version only when no service's
	// image moved further than a patch bump or a pinned digest (`patch_only`).
	RollbackPatchOnly RollbackPolicy = "patch_only"
)

// UnmarshalYAML accepts the booleans of ADR-0050 and the patch_only keyword.
func (p *RollbackPolicy) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		if value.Value == string(RollbackPatchOnly) {
			*p = RollbackPatchOnly
			return nil
		}
		var on bool
		if value.Tag == "!!bool" && value.Decode(&on) == nil {
			*p = RollbackNever
			if on {
				*p = RollbackAlways
			}
			return nil
		}
	}
	return fmt.Errorf("line %d: rollback must be true, false or patch_only, got %q", value.Line, value.Value)
}

// RollbackPolicyFor resolves the rollback policy of the named stack: the
// per-stack override when set, otherwise the global default, which is
// RollbackAlways unless set. An unknown name falls back to the global default.
func (c *Config) RollbackPolicyFor(name string) RollbackPolicy {
	return c.EffectiveRollback(c.Stacks, name)
}

// EffectiveRollback is RollbackPolicyFor over an explicit stack set (see
// EffectiveSelfHeal).
func (c *Config) EffectiveRollback(stacks []Stack, name string) RollbackPolicy {
	def := RollbackAlways
	if c.Rollback != nil {
		def = *c.Rollback
	}
	return stackFlag(stacks, name, func(s Stack) *RollbackPolicy { return s.Rollback }, def)
}
