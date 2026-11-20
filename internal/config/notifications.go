package config

import (
	"fmt"
	"net/url"

	"github.com/polandy/skipper-cd/internal/events"
)

// Outbound notification targets (ADR-0020): their shape, the format and
// trigger-status vocabularies they accept, and their validation.

// NotificationTarget configures a single outbound notification sink: where to
// POST, in which provider shape, and which deploy outcomes to report. See
// ADR-0020.
type NotificationTarget struct {
	// Format selects the provider shape of the request body. One of
	// "signal", "ntfy", "generic". Defaults to "generic".
	Format string `yaml:"format"`

	// URL is the endpoint the notification is POSTed to. For "signal" it is the
	// signal-cli-rest-api base (e.g. http://localhost:8020); "/v2/send" is
	// appended by the formatter.
	URL string `yaml:"url"`

	// On lists the terminal deploy statuses that trigger this target. Any
	// subset of "failed", "success", "rolled_back", "rolled_back_unhealthy".
	// Empty means all four.
	On []string `yaml:"on"`

	// Prefix is prepended (as "[<prefix>] ") to the human-readable message of
	// the "signal" and "ntfy" formats, e.g. to label which host/instance sent it. Optional;
	// empty adds no prefix. Ignored by the "generic" format, whose structured
	// payload already carries the event.
	Prefix string `yaml:"prefix"`

	// Headers are static HTTP headers added to the request. Only meaningful for
	// the "generic" and "ntfy" formats (e.g. an Authorization bearer token).
	Headers map[string]string `yaml:"headers"`

	// Number is the Signal sender number ("signal" format only, required).
	Number string `yaml:"number"`

	// Recipients are the Signal recipient numbers ("signal" format only,
	// non-empty).
	Recipients []string `yaml:"recipients"`

	// Digest batches a run's successes into one message: "per_run" (the
	// default) or "off" for one message per terminal event. Deploy
	// notifications only. See docs/dev/notification-digest-spec.md.
	Digest string `yaml:"digest"`
}

// Notification format values for NotificationTarget.Format.
const (
	NotifyFormatSignal  = "signal"
	NotifyFormatGeneric = "generic"
	NotifyFormatNtfy    = "ntfy"
)

// Digest modes for NotificationTarget.Digest.
const (
	NotifyDigestPerRun = "per_run"
	NotifyDigestOff    = "off"
)

// Terminal deploy statuses accepted in NotificationTarget.On: the terminal
// events.Status values.
const (
	NotifyOnFailed              = string(events.StatusFailed)
	NotifyOnSuccess             = string(events.StatusSuccess)
	NotifyOnRolledBack          = string(events.StatusRolledBack)
	NotifyOnRolledBackUnhealthy = string(events.StatusRolledBackUnhealthy)
	// NotifyOnHealExhausted fires when self-heal gave up on a stack it could not
	// restore — the high-signal "a stack is down and I couldn't fix it" alarm
	// (ADR-0029). Part of the default set, so a target with no explicit `on`
	// reports it.
	NotifyOnHealExhausted = string(events.StatusHealExhausted)
)

// applyNotificationsDefaults fills every deploy notification target's omitted
// format and on.
func (c *Config) applyNotificationsDefaults() {
	for i := range c.Notifications {
		t := &c.Notifications[i]
		t.applyDefaults()
		if len(t.On) == 0 {
			t.On = []string{NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy, NotifyOnHealExhausted}
		}
		if t.Digest == "" {
			t.Digest = NotifyDigestPerRun
		}
	}
}

// validateNotifications checks every deploy notification target.
func (c *Config) validateNotifications() error {
	for i, t := range c.Notifications {
		if err := t.validate(); err != nil {
			return fmt.Errorf("notifications[%d]: %w", i, err)
		}
	}
	return nil
}

// applyDefaults fills the omitted format; shared by deploy and health targets.
func (t *NotificationTarget) applyDefaults() {
	if t.Format == "" {
		t.Format = NotifyFormatGeneric
	}
}

// validate checks a single notification target; defaults are applied first.
func (t NotificationTarget) validate() error {
	switch t.Format {
	case NotifyFormatSignal, NotifyFormatGeneric, NotifyFormatNtfy:
	default:
		return fmt.Errorf("unknown format %q, must be %q, %q or %q", t.Format, NotifyFormatSignal, NotifyFormatNtfy, NotifyFormatGeneric)
	}

	if t.URL == "" {
		return fmt.Errorf("url is required (the endpoint the notification is POSTed to)")
	}
	if u, err := url.ParseRequestURI(t.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("url %q must be a valid http(s) URL", t.URL)
	}

	for _, s := range t.On {
		switch s {
		case NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy, NotifyOnHealExhausted:
		default:
			return fmt.Errorf("unknown on value %q, must be one of %q, %q, %q, %q, %q",
				s, NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy, NotifyOnHealExhausted)
		}
	}

	switch t.Digest {
	case "", NotifyDigestPerRun, NotifyDigestOff:
	default:
		return fmt.Errorf("unknown digest value %q, must be %q or %q", t.Digest, NotifyDigestPerRun, NotifyDigestOff)
	}

	// Signal identity fields are required for and exclusive to the signal format.
	if t.Format == NotifyFormatSignal {
		if t.Number == "" {
			return fmt.Errorf("signal format requires number")
		}
		if len(t.Recipients) == 0 {
			return fmt.Errorf("signal format requires at least one recipient")
		}
	} else if t.Number != "" || len(t.Recipients) > 0 {
		return fmt.Errorf("number/recipients are only valid for the signal format")
	}

	return nil
}
