package events

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHistory_AddAndRetrieve(t *testing.T) {
	h := NewHistory("")

	h.Add(DeployEvent{ID: 1, Stack: "gitea", Status: StatusSuccess})
	h.Add(DeployEvent{ID: 2, Stack: "traefik", Status: StatusFailed})

	events := h.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ID != 1 || events[1].ID != 2 {
		t.Errorf("events not in expected order: %+v", events)
	}
}

func TestHistory_TrimsToMaxSize(t *testing.T) {
	h := NewHistory("")

	for i := range 150 {
		h.Add(DeployEvent{ID: int64(i + 1), Stack: "test"})
	}

	events := h.Events()
	if len(events) != maxHistorySize {
		t.Errorf("expected %d events, got %d", maxHistorySize, len(events))
	}
	// Oldest should be event 51 (150 - 100 + 1).
	if events[0].ID != 51 {
		t.Errorf("expected oldest event ID 51, got %d", events[0].ID)
	}
}

func TestHistory_EventsAfterID(t *testing.T) {
	h := NewHistory("")

	for i := range 5 {
		h.Add(DeployEvent{ID: int64(i + 1), Stack: "test"})
	}

	after := h.EventsAfterID(3)
	if len(after) != 2 {
		t.Fatalf("expected 2 events after ID 3, got %d", len(after))
	}
	if after[0].ID != 4 || after[1].ID != 5 {
		t.Errorf("unexpected events: %+v", after)
	}
}

func TestHistory_EventsAfterID_ReturnsEmpty(t *testing.T) {
	h := NewHistory("")
	h.Add(DeployEvent{ID: 1, Stack: "test"})

	after := h.EventsAfterID(999)
	if len(after) != 0 {
		t.Errorf("expected no events after ID 999, got %d", len(after))
	}
}

func TestHistory_MaxEventID(t *testing.T) {
	h := NewHistory("")

	if h.MaxEventID() != 0 {
		t.Errorf("expected 0 for empty history, got %d", h.MaxEventID())
	}

	h.Add(DeployEvent{ID: 10, Stack: "a"})
	h.Add(DeployEvent{ID: 5, Stack: "b"})
	h.Add(DeployEvent{ID: 20, Stack: "c"})

	if h.MaxEventID() != 20 {
		t.Errorf("expected max ID 20, got %d", h.MaxEventID())
	}
}

func TestHistory_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()

	h1 := NewHistory(dir)
	h1.Add(DeployEvent{ID: 1, Stack: "gitea", Status: StatusSuccess, Timestamp: time.Now()})
	h1.Add(DeployEvent{ID: 2, Stack: "traefik", Status: StatusFailed, Error: "timeout"})

	// Load from same directory — should recover events.
	h2 := NewHistory(dir)
	events := h2.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 persisted events, got %d", len(events))
	}
	if events[0].Stack != "gitea" || events[1].Stack != "traefik" {
		t.Errorf("unexpected persisted events: %+v", events)
	}
	if events[1].Error != "timeout" {
		t.Errorf("expected error 'timeout', got %q", events[1].Error)
	}
}

func TestHistory_MaxEventID_FromDisk(t *testing.T) {
	dir := t.TempDir()

	h1 := NewHistory(dir)
	h1.Add(DeployEvent{ID: 42, Stack: "test"})

	h2 := NewHistory(dir)
	if h2.MaxEventID() != 42 {
		t.Errorf("expected max ID 42 from disk, got %d", h2.MaxEventID())
	}
}

func TestHistory_EmptyDirNoPersistence(t *testing.T) {
	h := NewHistory("")
	h.Add(DeployEvent{ID: 1, Stack: "test"})
	// No crash, no file written.
	if len(h.Events()) != 1 {
		t.Error("expected 1 event in memory-only history")
	}
}

func TestHistory_MissingFileIsOK(t *testing.T) {
	dir := t.TempDir()
	// No file exists — should load fine with empty history.
	h := NewHistory(filepath.Join(dir, "subdir"))
	if len(h.Events()) != 0 {
		t.Error("expected empty history for non-existent file")
	}
}

func TestHistory_EventsReturnsCopy(t *testing.T) {
	h := NewHistory("")
	h.Add(DeployEvent{ID: 1, Stack: "test"})

	events := h.Events()
	events[0].Stack = "modified"

	if h.Events()[0].Stack != "test" {
		t.Error("Events() should return a copy, not a reference to internal slice")
	}
}
