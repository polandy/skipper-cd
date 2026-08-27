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

// --- repeated-outcome collapse (ADR-0056) ---

// repeatEvent builds a failing event for stack at id/minute with the given error.
func repeatEvent(id int64, stack, errText string, minute int) DeployEvent {
	return DeployEvent{
		ID:        id,
		Stack:     stack,
		Status:    StatusFailed,
		Error:     errText,
		Timestamp: time.Date(2026, 8, 18, 2, minute, 0, 0, time.UTC),
	}
}

func TestHistory_CollapsesRepeatedFailure(t *testing.T) {
	h := NewHistory("")
	const boom = "no stack directory /repo/modules/ryot with a docker-compose.yml"

	h.Add(repeatEvent(1, "ryot", boom, 0))
	h.Add(repeatEvent(2, "ryot", boom, 5))
	h.Add(repeatEvent(3, "ryot", boom, 10))

	got := h.Events()
	if len(got) != 1 {
		t.Fatalf("expected the repeats to collapse into 1 event, got %d: %+v", len(got), got)
	}
	e := got[0]
	if e.ID != 3 {
		t.Errorf("collapsed event should carry the newest id, got %d", e.ID)
	}
	if e.RepeatCount != 3 {
		t.Errorf("RepeatCount = %d, want 3", e.RepeatCount)
	}
	if want := time.Date(2026, 8, 18, 2, 0, 0, 0, time.UTC); !e.FirstSeen.Equal(want) {
		t.Errorf("FirstSeen = %v, want the first occurrence %v", e.FirstSeen, want)
	}
	if want := time.Date(2026, 8, 18, 2, 10, 0, 0, time.UTC); !e.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want the newest occurrence %v", e.Timestamp, want)
	}
	if e.SupersedesID != 2 {
		t.Errorf("SupersedesID = %d, want the replaced event 2", e.SupersedesID)
	}
}

func TestHistory_CollapseKeepsOtherStacksHistory(t *testing.T) {
	h := NewHistory("")
	const boom = "compose file missing"

	// The flood that pushed every other stack out of the 100-slot ring.
	for i := range 150 {
		h.Add(repeatEvent(int64(i+1), "ryot", boom, i%60))
	}
	h.Add(DeployEvent{ID: 1000, Stack: "gitea", Status: StatusSuccess})

	got := h.Events()
	if len(got) != 2 {
		t.Fatalf("expected 1 collapsed ryot event + 1 gitea event, got %d: %+v", len(got), got)
	}
	if got[1].Stack != "gitea" {
		t.Errorf("gitea's deploy should have survived the flood, got %+v", got)
	}
	if got[0].RepeatCount != 150 {
		t.Errorf("RepeatCount = %d, want 150", got[0].RepeatCount)
	}
}

func TestHistory_DoesNotCollapseWhenErrorChanges(t *testing.T) {
	h := NewHistory("")

	h.Add(repeatEvent(1, "ryot", "compose file missing", 0))
	h.Add(repeatEvent(2, "ryot", "port 8080 already allocated", 5))

	if got := h.Events(); len(got) != 2 {
		t.Fatalf("a different error is a different incident; want 2 events, got %d: %+v", len(got), got)
	}
}

func TestHistory_DoesNotCollapseSuccess(t *testing.T) {
	h := NewHistory("")

	h.Add(DeployEvent{ID: 1, Stack: "gitea", Status: StatusSuccess})
	h.Add(DeployEvent{ID: 2, Stack: "gitea", Status: StatusSuccess})

	if got := h.Events(); len(got) != 2 {
		t.Fatalf("successive deploys are distinct outcomes; want 2 events, got %d: %+v", len(got), got)
	}
}

func TestHistory_DoesNotCollapseAcrossAnotherOutcome(t *testing.T) {
	h := NewHistory("")
	const boom = "compose file missing"

	h.Add(repeatEvent(1, "ryot", boom, 0))
	h.Add(DeployEvent{ID: 2, Stack: "ryot", Status: StatusSuccess})
	h.Add(repeatEvent(3, "ryot", boom, 10))

	got := h.Events()
	if len(got) != 3 {
		t.Fatalf("the failure recurred after a success — a new incident; want 3 events, got %d: %+v", len(got), got)
	}
	if got[2].RepeatCount != 0 {
		t.Errorf("the recurrence starts a fresh count, got RepeatCount = %d", got[2].RepeatCount)
	}
}

// A collapsed repeat must be reachable through the reconnect replay, or a UI
// that saw the earlier event would never learn the count moved.
func TestHistory_CollapsedRepeatIsDeliveredOnReconnect(t *testing.T) {
	h := NewHistory("")
	const boom = "compose file missing"

	h.Add(repeatEvent(1, "ryot", boom, 0))
	h.Add(repeatEvent(2, "ryot", boom, 5))

	after := h.EventsAfterID(1)
	if len(after) != 1 || after[0].ID != 2 || after[0].RepeatCount != 2 {
		t.Fatalf("EventsAfterID(1) should replay the collapsed repeat, got %+v", after)
	}
}

// A history written before the collapse existed (or by an older skipper) is
// healed on load, so a past flood stops crowding the ring after one restart.
func TestHistory_LoadCollapsesExistingRepeats(t *testing.T) {
	dir := t.TempDir()
	const boom = "compose file missing"

	h := NewHistory(dir)
	h.Add(DeployEvent{ID: 1, Stack: "gitea", Status: StatusSuccess})
	// Write the flood past the collapse by going straight at the slice.
	h.mu.Lock()
	for i := range 20 {
		h.events = append(h.events, repeatEvent(int64(i+2), "ryot", boom, i))
	}
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	h.mu.Unlock()

	reloaded := NewHistory(dir)
	got := reloaded.Events()
	if len(got) != 2 {
		t.Fatalf("expected gitea + 1 collapsed ryot event after load, got %d: %+v", len(got), got)
	}
	if got[1].RepeatCount != 20 {
		t.Errorf("RepeatCount = %d, want 20", got[1].RepeatCount)
	}
	if got[1].ID != 21 {
		t.Errorf("collapsed event should keep the newest id 21, got %d", got[1].ID)
	}
}

// The live path never emits two failures back to back: each reconcile emits
// `deploying` first. Comparing against the newest event of the stack would
// therefore never see the repeat — the collapse must look past the in-progress
// phases of the run that produced it.
func TestHistory_CollapsesAcrossTheDeployingEventBetweenRepeats(t *testing.T) {
	h := NewHistory("")
	const boom = "docker compose up: exit status 1"

	h.Add(DeployEvent{ID: 1, Stack: "wiki", Status: StatusDeploying})
	h.Add(repeatEvent(2, "wiki", boom, 0))
	h.Add(DeployEvent{ID: 3, Stack: "wiki", Status: StatusDeploying})
	h.Add(repeatEvent(4, "wiki", boom, 5))
	h.Add(DeployEvent{ID: 5, Stack: "wiki", Status: StatusDeploying})
	h.Add(repeatEvent(6, "wiki", boom, 10))

	got := h.Events()
	// The first deploying stays: it belongs to the run that first failed, and
	// nothing has superseded it. Everything the repeats brought is gone.
	if len(got) != 2 {
		t.Fatalf("want the first deploying + one collapsed failure, got %d: %+v", len(got), got)
	}
	if got[0].Status != StatusDeploying || got[1].RepeatCount != 3 {
		t.Fatalf("unexpected shape: %+v", got)
	}
	if got[1].SupersedesID != 4 {
		t.Errorf("SupersedesID = %d, want the replaced terminal event 4", got[1].SupersedesID)
	}
}

// A collapse must only touch its own stack: another stack deploying between two
// repeats is unrelated history, and dropping it would trade one eviction bug
// for a worse one.
func TestHistory_CollapseKeepsInterleavedOtherStacks(t *testing.T) {
	h := NewHistory("")
	const boom = "docker compose up: exit status 1"

	h.Add(repeatEvent(1, "wiki", boom, 0))
	h.Add(DeployEvent{ID: 2, Stack: "gitea", Status: StatusDeploying})
	h.Add(DeployEvent{ID: 3, Stack: "gitea", Status: StatusSuccess})
	h.Add(DeployEvent{ID: 4, Stack: "wiki", Status: StatusDeploying})
	h.Add(repeatEvent(5, "wiki", boom, 5))

	got := h.Events()
	if len(got) != 3 {
		t.Fatalf("want gitea's two events + one collapsed wiki failure, got %d: %+v", len(got), got)
	}
	if got[0].ID != 2 || got[1].ID != 3 {
		t.Errorf("gitea's events must survive untouched and in order, got %+v", got)
	}
	if got[2].Stack != "wiki" || got[2].RepeatCount != 2 {
		t.Errorf("want the collapsed wiki failure last, got %+v", got[2])
	}
}

func TestStatusClassification(t *testing.T) {
	tests := []struct {
		status     Status
		terminal   bool
		repeatable bool
	}{
		{StatusDeploying, false, false},
		{StatusQueued, false, false},
		{StatusBlocked, false, false},
		{StatusSkipped, false, false},
		{StatusSuccess, true, false},
		{StatusHealed, true, false},
		{StatusRemoved, true, false},
		{StatusFailed, true, true},
		{StatusRolledBack, true, true},
		{StatusRolledBackUnhealthy, true, true},
		{StatusHealExhausted, true, true},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := Terminal(tc.status); got != tc.terminal {
				t.Errorf("Terminal(%s) = %t, want %t", tc.status, got, tc.terminal)
			}
			if got := Repeatable(tc.status); got != tc.repeatable {
				t.Errorf("Repeatable(%s) = %t, want %t", tc.status, got, tc.repeatable)
			}
		})
	}
}
