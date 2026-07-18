package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/polandy/skipper-cd/internal/logbuf"
)

// LogsSSEHandler streams captured log entries via Server-Sent Events. On
// connect it subscribes first, then replays the buffered backlog
// (optionally filtered by Last-Event-ID), then streams live entries,
// skipping any entry already sent during replay.
func LogsSSEHandler(log *logbuf.Log) http.Handler {
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

		// Subscribe before snapshotting the backlog so no entry falls into
		// the gap; live entries already covered by the replay are skipped
		// via lastSent below.
		ch, unsub := log.Subscribe()
		defer unsub()

		var backlog []logbuf.Entry
		if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
			if id, err := strconv.ParseInt(lastID, 10, 64); err == nil {
				backlog = log.EntriesAfter(id)
			}
		} else {
			backlog = log.Entries()
		}

		var lastSent int64
		for _, e := range backlog {
			if err := writeLogSSE(w, e); err != nil {
				return
			}
			lastSent = e.ID
		}
		flusher.Flush()

		keepalive := time.NewTicker(sseKeepaliveInterval)
		defer keepalive.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-ch:
				if e.ID <= lastSent {
					continue
				}
				if err := writeLogSSE(w, e); err != nil {
					return
				}
				lastSent = e.ID
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

func writeLogSSE(w http.ResponseWriter, e logbuf.Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", e.ID, data)
	return err
}
