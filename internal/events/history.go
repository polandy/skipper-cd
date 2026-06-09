package events

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
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
		_ = h.load() // best-effort
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
	var max int64
	for _, e := range h.events {
		if e.ID > max {
			max = e.ID
		}
	}
	return max
}

// Add appends an event, trims to maxHistorySize, and persists to disk.
func (h *History) Add(event DeployEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.events = append(h.events, event)
	if len(h.events) > maxHistorySize {
		h.events = h.events[len(h.events)-maxHistorySize:]
	}
	_ = h.save() // best-effort
}

// Events returns a copy of all events in chronological order.
func (h *History) Events() []DeployEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]DeployEvent, len(h.events))
	copy(out, h.events)
	return out
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

func (h *History) load() error {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &h.events)
}

func (h *History) save() error {
	if h.filePath == "" {
		return nil
	}
	data, err := yaml.Marshal(h.events)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	return os.WriteFile(h.filePath, data, 0644)
}
