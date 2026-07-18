// Package safego launches goroutines that recover from a panic instead of
// crashing the whole process.
package safego

import (
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a new goroutine. A panic inside fn is recovered and logged
// with its stack trace instead of propagating: an unexpected panic in one
// background task (a deploy run, a poller tick, a notification delivery)
// must not take down every other in-flight and future task with it, since Go
// has no per-goroutine isolation — an unrecovered panic in any goroutine
// crashes the entire process.
func Go(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("recovered from panic in background goroutine", "name", name, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
