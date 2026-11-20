package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/polandy/skipper-cd/internal/uitheme"
)

// The process itself: its two listeners, command timeout, logging, the web UI
// and the health poll cadence the UI, self-heal and the watchdog share. Who may
// reach the main listener is listen.go.

// Default listener ports. They are set before decoding, so an explicit 0 is
// rejected rather than defaulted.
const (
	defaultPort        = 8080
	defaultMetricsPort = 9120
)

// TCP port bounds for the webhook/UI and metrics listeners.
const (
	minPort = 1
	maxPort = 65535
)

// defaultCommandTimeoutSeconds bounds every git/docker/nixos command when
// command_timeout_seconds is omitted or 0.
const defaultCommandTimeoutSeconds = 300

// Valid values for the log_format config field.
const (
	LogFormatPretty = "pretty"
	LogFormatText   = "text"
	LogFormatJSON   = "json"
)

// defaultRuntimeHealthPollIntervalSeconds is the UI stack-health poll cadence applied
// when runtime_health_poll_interval_seconds is omitted (ADR-0027). An explicit 0
// disables the health view.
const defaultRuntimeHealthPollIntervalSeconds = 30

// newConfig returns a Config holding the defaults that must be in place before
// decoding.
func newConfig() *Config {
	return &Config{Port: defaultPort, MetricsPort: defaultMetricsPort}
}

// applyServerDefaults fills the omitted process-level fields.
func (c *Config) applyServerDefaults() {
	if c.CommandTimeoutSeconds == 0 {
		c.CommandTimeoutSeconds = defaultCommandTimeoutSeconds
	}
	if c.LogFormat == "" {
		c.LogFormat = LogFormatPretty
	}
	if c.UITheme == "" {
		c.UITheme = uitheme.ThemeCatppuccin
	}
	if c.HostName == "" {
		// The OS hostname gives the merged multi-host UI (ADR-0048) a stable
		// self-identity label without extra config.
		if h, err := os.Hostname(); err == nil {
			c.HostName = h
		}
	}
	if c.UIEnabled == nil {
		d := true
		c.UIEnabled = &d
	}
	if c.RuntimeHealthPollIntervalSeconds == nil {
		d := defaultRuntimeHealthPollIntervalSeconds
		c.RuntimeHealthPollIntervalSeconds = &d
	}
}

// validateServer checks the process-level fields; defaults are applied first.
func (c *Config) validateServer() error {
	// A negative timeout would build an already-expired context, failing every
	// command from the first sync with an opaque "context deadline exceeded".
	if c.CommandTimeoutSeconds < 0 {
		return fmt.Errorf("command_timeout_seconds must be >= 0, got %d", c.CommandTimeoutSeconds)
	}
	if c.Port < minPort || c.Port > maxPort {
		return fmt.Errorf("port must be between %d and %d, got %d", minPort, maxPort, c.Port)
	}
	if c.MetricsPort < minPort || c.MetricsPort > maxPort {
		return fmt.Errorf("metrics_port must be between %d and %d, got %d", minPort, maxPort, c.MetricsPort)
	}
	if c.Port == c.MetricsPort {
		return fmt.Errorf("port and metrics_port must differ, both are %d", c.Port)
	}
	if !uitheme.IsValidTheme(c.UITheme) {
		return fmt.Errorf("ui_theme must be one of %s, got %q", strings.Join(uitheme.ValidThemes, ", "), c.UITheme)
	}
	if c.LogFormat != LogFormatPretty && c.LogFormat != LogFormatText && c.LogFormat != LogFormatJSON {
		return fmt.Errorf("log_format must be %q, %q or %q, got %q", LogFormatPretty, LogFormatText, LogFormatJSON, c.LogFormat)
	}
	if c.RuntimeHealthPollIntervalSeconds != nil && *c.RuntimeHealthPollIntervalSeconds < 0 {
		return fmt.Errorf("runtime_health_poll_interval_seconds must be >= 0, got %d", *c.RuntimeHealthPollIntervalSeconds)
	}
	return nil
}

// healthPollEnabled reports whether the shared health poll runs at all — what
// self-heal and the health watchdog ride on.
func (c *Config) healthPollEnabled() bool {
	return c.RuntimeHealthPollIntervalSeconds != nil && *c.RuntimeHealthPollIntervalSeconds > 0
}
