package events

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureLogs routes the default slog output into a buffer for the test's
// duration, so an emitted warning is a positive, assertable signal.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestHistory_SaveLeavesOnlyHistoryFile(t *testing.T) {
	dir := t.TempDir()
	h := NewHistory(dir)

	h.Add(DeployEvent{ID: 1, Stack: "gitea", Status: StatusSuccess})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != historyFileName {
		t.Errorf("expected only %s in state dir, got %v", historyFileName, entries)
	}
}

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

// The persisted history feeds SSE reconnect recovery (EventsAfterID), so a
// failed save must at least be visible in the logs, never silently dropped.
func TestHistory_AddWarnsWhenSaveFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: the directory mode below would not deny the write")
	}
	dir := t.TempDir()
	h := NewHistory(dir)

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restoring the directory mode: %v", err)
		}
	})

	buf := captureLogs(t)
	h.Add(DeployEvent{ID: 1, Stack: "gitea", Status: StatusSuccess})

	if !strings.Contains(buf.String(), "persist deploy history failed") {
		t.Errorf("expected a warning about the failed save, got logs: %q", buf.String())
	}
	if len(h.Events()) != 1 {
		t.Error("the event must still be kept in memory despite the failed save")
	}
}

// A corrupt or unreadable history file warns (positive control), while the
// normal first-run missing file stays silent.
func TestNewHistory_WarnsOnUnreadableFileButNotOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	// A directory at the file path makes the read fail deterministically.
	if err := os.Mkdir(filepath.Join(dir, historyFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	buf := captureLogs(t)
	NewHistory(dir)
	if !strings.Contains(buf.String(), "load deploy history failed") {
		t.Errorf("expected a warning about the failed load, got logs: %q", buf.String())
	}

	buf.Reset()
	NewHistory(t.TempDir())
	if buf.Len() != 0 {
		t.Errorf("a missing history file is normal on first run and must not warn, got logs: %q", buf.String())
	}
}

func TestHistory_EventByID_Found(t *testing.T) {
	h := NewHistory("")
	h.Add(DeployEvent{ID: 1, Stack: "gitea", Status: StatusSuccess})
	h.Add(DeployEvent{ID: 2, Stack: "traefik", Status: StatusFailed})

	evt, ok := h.EventByID(2)
	if !ok {
		t.Fatal("expected event to be found")
	}
	if evt.Stack != "traefik" {
		t.Errorf("expected stack 'traefik', got %q", evt.Stack)
	}
}

func TestHistory_EventByID_NotFound(t *testing.T) {
	h := NewHistory("")
	h.Add(DeployEvent{ID: 1, Stack: "gitea"})

	_, ok := h.EventByID(999)
	if ok {
		t.Error("expected event not to be found")
	}
}

func TestHistory_EventByID_WithDiffs(t *testing.T) {
	h := NewHistory("")
	h.Add(DeployEvent{
		ID:    1,
		Stack: "gitea",
		Diffs: map[string]string{"docker-compose.yml": "+new line"},
	})

	evt, ok := h.EventByID(1)
	if !ok {
		t.Fatal("expected event to be found")
	}
	if evt.Diffs == nil || evt.Diffs["docker-compose.yml"] != "+new line" {
		t.Errorf("expected diffs to be preserved, got %v", evt.Diffs)
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
