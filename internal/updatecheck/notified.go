package updatecheck

import (
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/fsatomic"
)

// notifiedFile is the persisted notification dedup: which advertised update
// each service was already alerted about. Report-only state — losing it costs
// at most one repeated notification per standing update.
type notifiedFile struct {
	Notified map[string]string `yaml:"notified"`
}

// loadNotified reads the dedup map from path; a missing file or empty path is
// a fresh start, a broken one is logged and treated the same (worst case: one
// duplicate notification per standing update).
func loadNotified(path string) map[string]string {
	if path == "" {
		return map[string]string{}
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}
	}
	if err != nil {
		slog.Warn("could not read update-check state, standing updates may notify again", "path", path, "err", err)
		return map[string]string{}
	}
	var f notifiedFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		slog.Warn("update-check state corrupt, standing updates may notify again", "path", path, "err", err)
		return map[string]string{}
	}
	if f.Notified == nil {
		return map[string]string{}
	}
	return f.Notified
}

// saveNotified persists the dedup map atomically; an empty path skips
// persistence. A write failure is logged — the process keeps its in-memory
// dedup, only a restart would repeat notifications.
func saveNotified(path string, notified map[string]string) {
	if path == "" {
		return
	}
	data, err := yaml.Marshal(notifiedFile{Notified: notified})
	if err != nil {
		slog.Error("could not marshal update-check state", "err", err)
		return
	}
	if err := fsatomic.WriteFile(path, data, fsatomic.PrivateFileMode); err != nil {
		slog.Error("could not save update-check state", "path", path, "err", err)
	}
}
