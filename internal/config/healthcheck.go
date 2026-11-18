package config

import (
	"fmt"
	"net/url"

	"gopkg.in/yaml.v3"
)

// The deploy health gate (ADR-0022/0046/0047/0049): its shape, the scalar-or-map
// decoding that lets `deploy_health_check: false` disable it, and its validation.

// HealthCheck configures the optional post-deploy health gate of a stack.
// When present, `docker compose up` runs with --wait so it fails when the
// services' compose healthchecks do not turn healthy in time, and an optional
// HTTP probe additionally verifies the stack from the outside. Either failure
// triggers the regular rollback path (ADR-0004, ADR-0022).
//
// In YAML deploy_health_check accepts either a mapping (the fields below) or a
// boolean scalar (ADR-0049): `false` explicitly disables the gate — overriding
// the automatic compose-`healthcheck:` gate of ADR-0046 — and `true` enables it
// at the defaults (equivalent to an empty mapping `{}`). See UnmarshalYAML.
type HealthCheck struct {
	// Enabled is the off/on switch (ADR-0049). nil means the gate was given as a
	// mapping with no enabled: key (or defaulted on); a non-nil false is an
	// explicit opt-out that suppresses the ADR-0046 automatic gate. Usually set
	// via the boolean-scalar form (see UnmarshalYAML); the yaml tag also lets a
	// mapping set it and, more importantly, carries it into the ConfigHash so
	// toggling the opt-out redeploys the stack. See IsDisabled.
	Enabled *bool `yaml:"enabled,omitempty"`

	// TimeoutSeconds bounds the wait: it is passed as --wait-timeout to
	// docker compose up and is also the deadline of the HTTP probe.
	// Defaults to 60.
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// URL, when set, is HTTP-GET-probed after a successful up until it
	// answers 2xx; anything else within TimeoutSeconds rolls the deploy back.
	URL string `yaml:"url"`
}

// UnmarshalYAML lets deploy_health_check be written as a boolean scalar as well
// as a mapping (ADR-0049): `false` records an explicit opt-out (Enabled=false),
// `true` records an explicit opt-in at the defaults (Enabled=true), and any
// mapping decodes into the fields normally. A non-boolean scalar is an error.
func (hc *HealthCheck) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var enabled bool
		if err := value.Decode(&enabled); err != nil {
			return fmt.Errorf("deploy_health_check must be a mapping or a boolean (true/false), got %q", value.Value)
		}
		hc.Enabled = &enabled
		return nil
	}
	// Decode the mapping without recursing back into this method.
	type rawHealthCheck HealthCheck
	var raw rawHealthCheck
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*hc = HealthCheck(raw)
	return nil
}

// IsDisabled reports whether the gate was explicitly turned off with
// deploy_health_check: false (ADR-0049). A nil receiver (the gate is absent)
// is not "disabled" — absence leaves the automatic compose-`healthcheck:` gate
// of ADR-0046 free to apply.
func (hc *HealthCheck) IsDisabled() bool {
	return hc != nil && hc.Enabled != nil && !*hc.Enabled
}

// DefaultHealthCheckTimeoutSeconds is applied when a deploy_health_check
// section is present without an explicit timeout_seconds, and by
// internal/deploy's automatic gate for a stack that declares no
// deploy_health_check but whose compose file has one (ADR-0046).
const DefaultHealthCheckTimeoutSeconds = 60

// validateHealthCheck checks a stack's optional deploy_health_check section.
// TimeoutSeconds has already been defaulted in Load.
func validateHealthCheck(hc *HealthCheck) error {
	if hc == nil {
		return nil
	}
	if hc.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must not be negative, got %d", hc.TimeoutSeconds)
	}
	if hc.URL != "" {
		if u, err := url.ParseRequestURI(hc.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("url %q must be a valid http(s) URL", hc.URL)
		}
	}
	return nil
}
