//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// deployEvt is the subset of a `deploy` SSE payload the tests assert on.
type deployEvt struct {
	ID     int64  `json:"id"`
	Stack  string `json:"stack"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// eventStream is a live subscriber to /api/events that accumulates the `deploy`
// events it receives (both the replayed history and live ones).
type eventStream struct {
	t       *testing.T
	mu      sync.Mutex
	deploys []deployEvt
}

// openEvents connects to /api/events and reads it in the background until the
// test ends. Because `skipped` events are broadcast live only (never persisted
// to history), callers should open the stream before triggering the run that
// emits them — and gate on a replayed event (see awaitStreamReady) to be sure
// the server-side subscription is active.
func (s *skipper) openEvents() *eventStream {
	s.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/events", nil)
	if err != nil {
		cancel()
		s.t.Fatalf("build events request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		s.t.Fatalf("open events stream: %v", err)
	}
	es := &eventStream{t: s.t}
	go es.read(resp)
	s.t.Cleanup(func() {
		cancel()
		resp.Body.Close()
	})
	return es
}

func (es *eventStream) read(resp *http.Response) {
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)

	var event, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if event == "deploy" && data != "" {
				var e deployEvt
				if json.Unmarshal([]byte(data), &e) == nil {
					es.mu.Lock()
					es.deploys = append(es.deploys, e)
					es.mu.Unlock()
				}
			}
			event, data = "", ""
		}
	}
}

// has reports whether an event for stack with status has been seen.
func (es *eventStream) has(stack, status string) bool {
	es.mu.Lock()
	defer es.mu.Unlock()
	for _, e := range es.deploys {
		if e.Stack == stack && e.Status == status {
			return true
		}
	}
	return false
}

// waitEvent blocks until an event for stack with status arrives, failing the
// test on timeout.
func (es *eventStream) waitEvent(stack, status string) {
	es.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if es.has(stack, status) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	es.t.Fatalf("timed out waiting for %s/%s event", stack, status)
}

// awaitStreamReady waits until the replayed startup `success` event for stack
// has been received, which guarantees the server has finished the history
// replay and is now live-subscribed — so a subsequently triggered live event
// (e.g. `skipped`) cannot be missed.
func (es *eventStream) awaitStreamReady(stack string) {
	es.t.Helper()
	es.waitEvent(stack, "success")
}
