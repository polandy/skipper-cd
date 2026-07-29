package config

import (
	"fmt"
	"net/url"
)

// Outbound notification targets (ADR-0020): their shape, the format and
// trigger-status vocabularies they accept, and their validation.

// NotificationTarget configures a single outbound notification sink: where to
// POST, in which provider shape, and which deploy outcomes to report. See
// ADR-0020.
type NotificationTarget struct {
	// Format selects the provider shape of the request body. One of
	// "signal", "generic". Defaults to "generic".
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
	// the "signal" format, e.g. to label which host/instance sent it. Optional;
	// empty adds no prefix. Ignored by the "generic" format, whose structured
	// payload already carries the event.
	Prefix string `yaml:"prefix"`

	// Headers are static HTTP headers added to the request. Only meaningful for
	// the "generic" format (e.g. an Authorization bearer token).
	Headers map[string]string `yaml:"headers"`

	// Number is the Signal sender number ("signal" format only, required).
	Number string `yaml:"number"`

	// Recipients are the Signal recipient numbers ("signal" format only,
	// non-empty).
	Recipients []string `yaml:"recipients"`
}

// Notification format values for NotificationTarget.Format.
const (
	NotifyFormatSignal  = "signal"
	NotifyFormatGeneric = "generic"
)

// Terminal deploy statuses accepted in NotificationTarget.On. They mirror the
// terminal events.Status values without importing that package (config is the
// lowest layer).
const (
	NotifyOnFailed              = "failed"
	NotifyOnSuccess             = "success"
	NotifyOnRolledBack          = "rolled_back"
	NotifyOnRolledBackUnhealthy = "rolled_back_unhealthy"
	// NotifyOnHealExhausted fires when self-heal gave up on a stack it could not
	// restore — the high-signal "a stack is down and I couldn't fix it" alarm
	// (ADR-0029). Part of the default set, so a target with no explicit `on`
	// reports it.
	NotifyOnHealExhausted = "heal_exhausted"
)

func validateNotificationTarget(t NotificationTarget) error {
	switch t.Format {
	case NotifyFormatSignal, NotifyFormatGeneric:
	default:
		return fmt.Errorf("unknown format %q, must be %q or %q", t.Format, NotifyFormatSignal, NotifyFormatGeneric)
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
