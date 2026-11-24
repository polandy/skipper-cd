package updatecheck

import (
	"os"
	"path/filepath"
	"testing"
)

func writeState(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The dedup file is report-only state: every unreadable form must degrade to a
// fresh start (worst case one repeated notification), never a load failure.
func TestLoadNotified(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.yaml")
	writeState(t, valid, "notified:\n  gitea/server: tag:1.22.6\n")
	corrupt := filepath.Join(dir, "corrupt.yaml")
	writeState(t, corrupt, "notified:\n  - not a map\n")
	nullMap := filepath.Join(dir, "null.yaml")
	writeState(t, nullMap, "notified:\n")

	tests := []struct {
		name string
		path string
		want map[string]string
	}{
		{"empty path is a fresh start", "", map[string]string{}},
		{"missing file is a fresh start", filepath.Join(dir, "missing.yaml"), map[string]string{}},
		{"valid file round-trips", valid, map[string]string{"gitea/server": "tag:1.22.6"}},
		{"corrupt file is a fresh start", corrupt, map[string]string{}},
		{"a null map is a fresh start", nullMap, map[string]string{}},
		// A directory is not os.ErrNotExist, so this reaches the read-error path.
		{"an unreadable path is a fresh start", dir, map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadNotified(tt.path)
			if len(got) != len(tt.want) {
				t.Fatalf("loadNotified(%q) = %v, want %v", tt.path, got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("loadNotified(%q)[%q] = %q, want %q", tt.path, k, got[k], v)
				}
			}
		})
	}
}

func TestSaveNotified(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty path skips persistence", func(t *testing.T) {
		// No path to write to; the contract is simply that it is a no-op.
		saveNotified("", map[string]string{"a/b": "tag:1"})
	})

	t.Run("round-trips through loadNotified", func(t *testing.T) {
		path := filepath.Join(dir, "state.yaml")
		want := map[string]string{"gitea/server": "digest:sha256:abc"}
		saveNotified(path, want)

		got := loadNotified(path)
		if len(got) != len(want) || got["gitea/server"] != want["gitea/server"] {
			t.Fatalf("loadNotified after save = %v, want %v", got, want)
		}
	})

	t.Run("a write failure is logged, not fatal", func(t *testing.T) {
		// A missing parent directory makes the atomic write fail; the process
		// must keep its in-memory dedup rather than crash.
		saveNotified(filepath.Join(dir, "nope", "state.yaml"), map[string]string{"a/b": "tag:1"})
	})
}
