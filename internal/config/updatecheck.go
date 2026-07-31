package config

import (
	"fmt"
	"time"
)

// The update_check section (ADR-0054): its shape, defaults and validation.
// The feature is on by default — an omitted section runs the read-only
// registry update check at the default cadence; interval_seconds: 0 disables
// it entirely.

// UpdateCheck configures the read-only registry update check (ADR-0054):
// skipper periodically asks each registry what it offers for the images its
// stacks run and reports the answer in the UI and, optionally, as a
// notification. It acts on nothing — applying an update stays a git commit.
type UpdateCheck struct {
	// IntervalSeconds is how often the check runs. Defaults to 21600 (6h)
	// when omitted; an explicit 0 disables the update check (and with it any
	// outbound registry request outside a deploy).
	IntervalSeconds *int `yaml:"interval_seconds"`

	// Notify sends one message through the configured notifications targets
	// when a service's available update first appears. Defaults to true;
	// without any notifications targets it has no effect.
	Notify *bool `yaml:"notify"`
}

// defaultUpdateCheckIntervalSeconds is the check cadence applied when the
// update_check section or its interval_seconds is omitted (ADR-0054).
const defaultUpdateCheckIntervalSeconds = 21600

// UpdateCheckInterval returns the effective update-check cadence; 0 means the
// check is disabled.
func (c *Config) UpdateCheckInterval() time.Duration {
	if c.UpdateCheck != nil && c.UpdateCheck.IntervalSeconds != nil {
		return time.Duration(*c.UpdateCheck.IntervalSeconds) * time.Second
	}
	return defaultUpdateCheckIntervalSeconds * time.Second
}

// UpdateCheckNotify reports whether an update's first appearance is sent to
// the notifications targets. Defaults to true.
func (c *Config) UpdateCheckNotify() bool {
	if c.UpdateCheck != nil && c.UpdateCheck.Notify != nil {
		return *c.UpdateCheck.Notify
	}
	return true
}

// EffectiveUpdateCheck reports whether the named stack takes part in the
// update check, over an explicit stack set (the discovered stacks under
// discovery, ADR-0034). Per-stack update_check: false opts out; the default is
// on. Like self_heal/autosync/rollback this is runtime policy, never hashed.
func (c *Config) EffectiveUpdateCheck(stacks []Stack, name string) bool {
	for _, s := range stacks {
		if s.Name == name {
			if s.UpdateCheck != nil {
				return *s.UpdateCheck
			}
			break
		}
	}
	return true
}

// validateUpdateCheck checks the optional update_check section.
func validateUpdateCheck(uc *UpdateCheck) error {
	if uc == nil {
		return nil
	}
	if uc.IntervalSeconds != nil && *uc.IntervalSeconds < 0 {
		return fmt.Errorf("interval_seconds must be >= 0 (0 disables the update check), got %d", *uc.IntervalSeconds)
	}
	return nil
}
