package config

import "fmt"

// Self-heal (ADR-0029): the per-stack policy, its pacing defaults and the
// rules that make it runnable. Self-heal itself is off unless self_heal is set
// true globally or per stack.

// Defaults for the self-heal pacing constants, applied when the corresponding
// field is omitted (ADR-0029).
const (
	defaultSelfHealMinUnhealthyPolls = 3
	defaultSelfHealMaxAttempts       = 3
	defaultSelfHealCooldownSeconds   = 60
)

// SelfHealEnabled reports whether self-heal is effective for the named stack:
// the per-stack override when set, otherwise the global default (off when
// unset). An unknown name falls back to the global default.
func (c *Config) SelfHealEnabled(name string) bool {
	return c.EffectiveSelfHeal(c.Stacks, name)
}

// EffectiveSelfHeal is SelfHealEnabled over an explicit stack set. Callers in
// stack-discovery mode pass the discovered stacks, which carry the host
// config's per-stack overrides merged in (ADR-0043).
func (c *Config) EffectiveSelfHeal(stacks []Stack, name string) bool {
	return stackFlag(stacks, name, func(s Stack) *bool { return s.SelfHeal }, c.SelfHeal != nil && *c.SelfHeal)
}

// SelfHealActive reports whether self-heal is effective for at least one stack.
// When true the health poller must run headless (not UI/subscriber-gated) so
// self-heal sees degradation on an unattended host (ADR-0029). In
// stack-discovery mode the stack set is unknown at startup, so activation
// follows the global flag alone; a per-stack self_heal: true with the global
// off is not supported there (documented in docs/user/configuration.md).
func (c *Config) SelfHealActive() bool {
	if c.StackDiscovery {
		return c.SelfHeal != nil && *c.SelfHeal
	}
	for _, s := range c.Stacks {
		if c.SelfHealEnabled(s.Name) {
			return true
		}
	}
	return false
}

// applySelfHealDefaults fills the omitted self-heal pacing fields.
func (c *Config) applySelfHealDefaults() {
	if c.SelfHealMinUnhealthyPolls == 0 {
		c.SelfHealMinUnhealthyPolls = defaultSelfHealMinUnhealthyPolls
	}
	if c.SelfHealMaxAttempts == 0 {
		c.SelfHealMaxAttempts = defaultSelfHealMaxAttempts
	}
	if c.SelfHealCooldownSeconds == nil {
		// Only an omitted field takes the default — an explicit 0 disables
		// the cooldown.
		d := defaultSelfHealCooldownSeconds
		c.SelfHealCooldownSeconds = &d
	}
}

// validateSelfHeal checks the self-heal pacing fields; defaults are applied
// first.
func (c *Config) validateSelfHeal() error {
	if c.SelfHealMinUnhealthyPolls < 1 {
		return fmt.Errorf("self_heal_min_unhealthy_polls must be >= 1, got %d", c.SelfHealMinUnhealthyPolls)
	}
	if c.SelfHealMaxAttempts < 1 {
		return fmt.Errorf("self_heal_max_attempts must be >= 1, got %d", c.SelfHealMaxAttempts)
	}
	if *c.SelfHealCooldownSeconds < 0 {
		return fmt.Errorf("self_heal_cooldown_seconds must be >= 0, got %d", *c.SelfHealCooldownSeconds)
	}
	// Self-heal rides the health poll cadence and runs headless, so it needs a
	// positive poll interval even with the UI off.
	if c.SelfHealActive() && !c.healthPollEnabled() {
		return fmt.Errorf("self_heal requires runtime_health_poll_interval_seconds > 0 (it drives the detection cadence)")
	}
	return nil
}
