package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/polandy/skipper-cd/internal/fsatomic"
)

func TestSaveDeployState_RoundTripsAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	state := newEmptyState()
	state.Stacks["mystack"] = stackFileHashes{"file": "hash"}

	if err := saveDeployState(dir, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != stateFileName {
		t.Errorf("expected only %s in state dir, got %v", stateFileName, entries)
	}

	info, err := os.Stat(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fsatomic.PrivateFileMode {
		t.Errorf("perm = %v, want %v", info.Mode().Perm(), fsatomic.PrivateFileMode)
	}

	loaded, err := loadPersistedDeployState(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded.Stacks["mystack"]["file"] != "hash" {
		t.Errorf("expected round-tripped state, got %+v", loaded)
	}
}

func TestSaveDeployState_RoundTripsProjectDirs(t *testing.T) {
	dir := t.TempDir()
	state := newEmptyState()
	state.recordProjectDir("web", "/repo/stacks/web")

	if err := saveDeployState(dir, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	loaded, err := loadPersistedDeployState(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := loaded.ProjectDirs["web"]; got != "/repo/stacks/web" {
		t.Errorf("expected round-tripped project dir, got %q", got)
	}
	// projectDirs() returns a defensive copy.
	copyOut := loaded.projectDirs()
	copyOut["web"] = "mutated"
	if loaded.ProjectDirs["web"] == "mutated" {
		t.Error("projectDirs() must return a copy, not the backing map")
	}
}
