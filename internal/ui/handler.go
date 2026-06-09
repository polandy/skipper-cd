// Package ui provides HTTP handlers for the skipper-cd web interface,
// including SSE-based real-time deploy event streaming.
package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
)

//go:embed static/index.html
var staticFS embed.FS

// IndexHandler serves the embedded UI HTML page.
func IndexHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
}

// SSEHandler returns an HTTP handler that streams deploy events via
// Server-Sent Events. On connect it replays history, then streams live
// events. Supports Last-Event-ID for reconnection.
func SSEHandler(broadcaster *events.Broadcaster, history *events.History) http.Handler {
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
		flusher.Flush()

		// Subscribe to live events.
		ch, unsub := broadcaster.Subscribe()
		defer unsub()

		// Keepalive ticker.
		keepalive := time.NewTicker(30 * time.Second)
		defer keepalive.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-ch:
				if err := writeSSE(w, evt); err != nil {
					return
				}
				flusher.Flush()
			case <-keepalive.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})
}

func writeSSE(w http.ResponseWriter, evt events.DeployEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: deploy\ndata: %s\n\n", evt.ID, data)
	return err
}
