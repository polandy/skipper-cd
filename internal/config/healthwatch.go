package config

import "fmt"

// The health watchdog section (ADR-0031): its shape, defaults and validation.
// Opt-in — omitting the section disables the watchdog entirely.

// HealthWatch configures the own-stack health watchdog (ADR-0031). It rides
// the shared health poller's cadence (runtime_health_poll_interval_seconds), the same
// way self-heal does — it has no poll interval of its own.
type HealthWatch struct {
	// DebouncePolls is how many consecutive health polls a new status must
	// persist before it is accepted (and may alert). Defaults to 2.
	DebouncePolls int `yaml:"debounce_polls"`

	// AttributionWindowSeconds is how long after a stack's deploy a health
	// transition still counts as deploy-correlated. Defaults to 300.
	AttributionWindowSeconds int `yaml:"attribution_window_seconds"`

	// AlertCooldownSeconds is the minimum gap in seconds between delivered
	// alerts of the same service and direction (unhealthy / recovered) — the
	// rate limit against a slow flapper paging on every cycle. Suppressed
	// transitions are still journaled and persisted, and once the cooldown
	// expires a still-diverged service gets the owed alert late (catch-up).
	// Defaults to 1800 when omitted; an explicit 0 disables the cooldown;
	// must be >= 0.
	AlertCooldownSeconds *int `yaml:"alert_cooldown_seconds"`

	// Targets lists the outbound sinks health alerts are delivered to, in the
	// same shape as the notifications targets but without `on:` (a health
	// target receives all alert-worthy transitions). Optional: with no targets
	// the watchdog still logs transitions and persists the history.
	Targets []NotificationTarget `yaml:"targets"`
}

// Defaults for the health_watch section (ADR-0031), applied when the section
// is present. The section itself is opt-in: omitting it disables the watchdog.
const (
	defaultHealthWatchDebouncePolls            = 2
	defaultHealthWatchAttributionWindowSeconds = 300
	defaultHealthWatchAlertCooldownSeconds     = 1800
)

// validateHealthWatch checks the optional health_watch section. Defaults have
// already been applied in Load.
func validateHealthWatch(hw *HealthWatch) error {
	if hw == nil {
		return nil
	}
	if hw.DebouncePolls < 1 {
		return fmt.Errorf("debounce_polls must be >= 1, got %d", hw.DebouncePolls)
	}
	if hw.AttributionWindowSeconds < 0 {
		return fmt.Errorf("attribution_window_seconds must be >= 0, got %d", hw.AttributionWindowSeconds)
	}
	if hw.AlertCooldownSeconds != nil && *hw.AlertCooldownSeconds < 0 {
		return fmt.Errorf("alert_cooldown_seconds must be >= 0, got %d", *hw.AlertCooldownSeconds)
	}
	for i, t := range hw.Targets {
		// Health targets carry no `on:` — they receive every alert-worthy
		// transition; the field belongs to deploy notifications only.
		if len(t.On) > 0 {
			return fmt.Errorf("targets[%d]: on is not valid for health_watch targets", i)
		}
		if err := validateNotificationTarget(t); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
	}
	return nil
}
