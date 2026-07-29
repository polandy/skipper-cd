package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
)

// The state surface, streamed and polled: the SSE stream the UI subscribes to,
// and the one-shot snapshot built from the same collector (ADR-0039) so the two
// cannot drift. Anything that changes the payload has to touch both.

// sseKeepaliveInterval is how often an idle SSE stream (deploy events, state
// events, logs) sends a keepalive so intermediary proxies don't time out the
// connection.
const sseKeepaliveInterval = 30 * time.Second

// SSEHandler returns an HTTP handler that streams deploy events via
// Server-Sent Events. On connect it replays deploy history, then sends the
// current UI state (autosync, stacks, health, …) as one event per state name,
// then streams live deploy and state events. Supports Last-Event-ID for
// reconnection of the deploy history.
//
// The baseline rides this stream so that subscribing and reading it are one
// ordered operation — the subscription is established before collect runs, so
// nothing published mid-connect falls into a gap (ADR-0039 amendment). collect
// is the same collector GET /api/v1/snapshot serves, so the two cannot drift; a
// nil collect (or a nil state broadcaster) sends no baseline.
func SSEHandler(deployB *events.Broadcaster[events.DeployEvent], stateB *events.Broadcaster[events.StateEvent], history *events.History, collect func() []events.StateEvent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		// Replay history (optionally filtered by Last-Event-ID).
		var historyEvents []events.DeployEvent
		if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
			if id, err := strconv.ParseInt(lastID, 10, 64); err == nil {
				historyEvents = history.EventsAfterID(id)
			}
		} else {
			historyEvents = history.Events()
		}

		for _, evt := range historyEvents {
			if err := writeSSE(w, evt); err != nil {
				return
			}
		}
		// End-of-replay marker: the deploy history (above) is a separate channel
		// from the /api/v1/snapshot baseline, so the UI cannot otherwise tell
		// "still replaying" from "caught up with zero deploys". This lets it
		// retire the loading skeleton and reveal the genuine-empty state only once
		// the history is known to be empty (T4.17). Re-sent on every reconnect;
		// the UI settles on it once.
		if _, err := fmt.Fprint(w, "event: synced\ndata: {}\n\n"); err != nil {
			return
		}
		flusher.Flush()

		// Subscribe to live deploy events, and state events when available.
		dch, unsubD := deployB.Subscribe()
		defer unsubD()
		var sch <-chan events.StateEvent
		if stateB != nil {
			var unsubS func()
			sch, unsubS = stateB.Subscribe()
			defer unsubS()
		}

		// The baseline is read *after* subscribing, so a state change racing this
		// connect is queued on sch rather than lost between the two.
		if stateB != nil && collect != nil {
			for _, se := range collect() {
				if err := writeSSEState(w, se); err != nil {
					return
				}
			}
			flusher.Flush()
		}

		// Keepalive ticker.
		keepalive := time.NewTicker(sseKeepaliveInterval)
		defer keepalive.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-dch:
				if err := writeSSE(w, evt); err != nil {
					return
				}
				flusher.Flush()
			case se := <-sch: // sch is nil (blocks forever) when no state broadcaster
				if err := writeSSEState(w, se); err != nil {
					return
				}
				flusher.Flush()
			case <-keepalive.C:
				// A failed keepalive means the client is gone.
				if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

func writeSSE(w http.ResponseWriter, evt events.DeployEvent) error {
	payload := evt.SSEPayload()
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: deploy\ndata: %s\n\n", evt.ID, data)
	return err
}

func writeSSEState(w http.ResponseWriter, se events.StateEvent) error {
	data, err := json.Marshal(se.Data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", se.Name, data)
	return err
}

// SnapshotHandler serves GET /api/v1/snapshot — the current UI state as one
// JSON object keyed by state-event name, e.g.
// {"stacks": {...}, "health": {...}, "autosync": {...}, ...}. The values are
// the same payloads the SSE stream publishes under those event names, built
// from the same collector, so the REST snapshot and the live stream cannot
// drift (ADR-0039). It is the read surface external consumers poll — notably
// the multi-host fan-in (ADR-0048) — and the UI's own initial paint, replacing
// the former SSE initial-state burst. `collect` may refresh live subsystems as
// a side effect (the same refresh a fresh SSE connection triggers).
func SnapshotHandler(collect func() []events.StateEvent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snap := make(map[string]any, len(events.AllStateNames))
		for _, se := range collect() {
			snap[se.Name] = se.Data
		}
		w.Header().Set("Content-Type", "application/json")
		// The status header is implicitly 200; a failed body write cannot be
		// reported to the client anymore.
		_ = json.NewEncoder(w).Encode(snap)
	})
}
