package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/polandy/skipper-cd/internal/events"
)

// TestSnapshotHandler_SerializesStateByName verifies the endpoint returns each
// collected state event as a top-level JSON key holding that event's payload —
// the same payload the SSE stream sends under the same name.
func TestSnapshotHandler_SerializesStateByName(t *testing.T) {
	collect := func() []events.StateEvent {
		return []events.StateEvent{
			{Name: events.StateStacks, Data: map[string]any{"disabled": []string{"web"}}},
			{Name: events.StateHealth, Data: map[string]any{"stacks": map[string]any{"db": "healthy"}}},
		}
	}

	rec := httptest.NewRecorder()
	SnapshotHandler(collect).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not a JSON object: %v", err)
	}
	if _, ok := got[events.StateStacks]; !ok {
		t.Errorf("snapshot missing %q key; got keys %v", events.StateStacks, keysOf(got))
	}
	var stacks struct {
		Disabled []string `json:"disabled"`
	}
	if err := json.Unmarshal(got[events.StateStacks], &stacks); err != nil {
		t.Fatalf("stacks payload malformed: %v", err)
	}
	if len(stacks.Disabled) != 1 || stacks.Disabled[0] != "web" {
		t.Errorf("stacks.disabled = %v, want [web]", stacks.Disabled)
	}
}

// TestSnapshotHandler_EmptyCollectorIsEmptyObject verifies a collector that
// yields nothing still returns a valid empty JSON object, not null.
func TestSnapshotHandler_EmptyCollectorIsEmptyObject(t *testing.T) {
	rec := httptest.NewRecorder()
	SnapshotHandler(func() []events.StateEvent { return nil }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil))

	if got := rec.Body.String(); got != "{}\n" {
		t.Errorf("empty snapshot body = %q, want %q", got, "{}\n")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
