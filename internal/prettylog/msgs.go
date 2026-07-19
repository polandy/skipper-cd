package prettylog

// Message strings cmd/skipper emits specifically for pretty-mode narration —
// kept here, not as untyped literals in main, so the emitter and the matcher
// in render.go cannot drift apart (dev-docs/adr/0042-pretty-console-log.md).
const (
	// MsgStacksResolved headers the startup/first-run stack roster.
	MsgStacksResolved = "stacks resolved"
	// MsgStackDiscovered is logged once per stack in the roster.
	MsgStackDiscovered = "stack discovered"
	// MsgStacksDisabled lists stacks discovered but parked (disabled: true),
	// logged once as part of the roster when any exist.
	MsgStacksDisabled = "stacks disabled"
	// MsgRunComplete summarizes one sync-and-deploy run's outcome counts.
	MsgRunComplete = "run complete"
)
