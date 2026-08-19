package events

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/fsatomic"
)

const (
	maxHistorySize  = 100
	historyFileName = "deploy-history.yaml"
)

// History is a thread-safe, bounded ring buffer of deploy events with
// optional YAML persistence to disk.
type History struct {
	mu       sync.RWMutex
	events   []DeployEvent
	filePath string
}

// NewHistory loads existing history from stateDir (if present) and
// returns a ready-to-use History. An empty stateDir disables persistence.
func NewHistory(stateDir string) *History {
	h := &History{}
	if stateDir != "" {
		h.filePath = filepath.Join(stateDir, historyFileName)
		// A missing file is the normal first run; anything else is worth a line.
		if err := h.load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("load deploy history failed; starting empty", "path", h.filePath, "err", err)
		}
	}
	return h
}

// MaxEventID returns the highest event ID in the history, or 0 if empty.
func (h *History) MaxEventID() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.events) == 0 {
		return 0
	}
	var maxID int64
	for _, e := range h.events {
		if e.ID > maxID {
			maxID = e.ID
		}
	}
	return maxID
}

// Add appends an event, trims to maxHistorySize, and persists to disk. An
// event that merely repeats this stack's last outcome collapses into it
// instead of taking a slot (ADR-0056). It returns the event as stored — the
// collapsed form when one was absorbed — so the caller broadcasts what the
// history actually holds rather than the raw occurrence.
func (h *History) Add(event DeployEvent) DeployEvent {
	h.mu.Lock()
	defer h.mu.Unlock()

	stored := h.addLocked(event)
	// Best-effort, but never silent: the persisted history feeds SSE
	// reconnect recovery (EventsAfterID).
	if err := h.save(); err != nil {
		slog.Warn("persist deploy history failed", "path", h.filePath, "err", err)
	}
	return stored
}

// addLocked appends one event, collapsing it into this stack's previous
// outcome when it only repeats it, trims to maxHistorySize, and returns the
// event as stored.
func (h *History) addLocked(event DeployEvent) DeployEvent {
	if i, prev, ok := h.lastOutcomeForStack(event.Stack); ok && event.RepeatsOf(prev) {
		// Drop the absorbed outcome *and* the in-flight phases the repeating run
		// emitted after it — they describe the very attempt this event settles,
		// so keeping them would let the ring fill at half speed instead.
		h.events = append(h.events[:i], h.keepOthers(i+1, event.Stack)...)
		event = event.absorb(prev)
	}
	h.events = append(h.events, event)
	if len(h.events) > maxHistorySize {
		h.events = h.events[len(h.events)-maxHistorySize:]
	}
	return event
}

// keepOthers returns the events from index `from` onwards that belong to
// another stack, in order — the interleaved history a collapse must not touch.
func (h *History) keepOthers(from int, stack string) []DeployEvent {
	kept := make([]DeployEvent, 0, len(h.events)-from)
	for _, e := range h.events[from:] {
		if e.Stack != stack {
			kept = append(kept, e)
		}
	}
	return kept
}

// lastOutcomeForStack returns the newest *terminal* event held for a stack and
// its index. In-flight phases are skipped: every reconcile emits `deploying`
// before its result, so the newest event of a stack is almost never its newest
// outcome.
func (h *History) lastOutcomeForStack(stack string) (int, DeployEvent, bool) {
	for i := len(h.events) - 1; i >= 0; i-- {
		if h.events[i].Stack == stack && Terminal(h.events[i].Status) {
			return i, h.events[i], true
		}
	}
	return 0, DeployEvent{}, false
}

// Events returns a copy of all events in chronological order.
func (h *History) Events() []DeployEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]DeployEvent, len(h.events))
	copy(out, h.events)
	return out
}

// EventByID returns the event with the given ID, or false if not found.
func (h *History) EventByID(id int64) (DeployEvent, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, e := range h.events {
		if e.ID == id {
			return e, true
		}
	}
	return DeployEvent{}, false
}

// EventsAfterID returns events with ID > afterID, for SSE reconnection.
func (h *History) EventsAfterID(afterID int64) []DeployEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []DeployEvent
	for _, e := range h.events {
		if e.ID > afterID {
			out = append(out, e)
		}
	}
	return out
}

// load reads the persisted history and replays it through addLocked, so a file
// written before the collapse existed — a past flood of one standing failure —
// is folded down on startup instead of crowding the ring for another 100 events.
func (h *History) load() error {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		return err
	}
	var stored []DeployEvent
	if err := yaml.Unmarshal(data, &stored); err != nil {
		return err
	}
	for _, e := range stored {
		h.addLocked(e)
	}
	return nil
}

func (h *History) save() error {
	if h.filePath == "" {
		return nil
	}
	data, err := yaml.Marshal(h.events)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	return fsatomic.WriteFile(h.filePath, data, fsatomic.PrivateFileMode)
}
