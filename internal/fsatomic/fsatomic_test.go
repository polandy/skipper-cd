package fsatomic_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/polandy/skipper-cd/internal/fsatomic"
)

func TestWriteFile_CreatesFileWithContentAndPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")

	if err := fsatomic.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", data, "hello")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("perm = %v, want 0644", info.Mode().Perm())
	}
}

func TestWriteFile_OverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fsatomic.WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("content = %q, want %q", data, "new")
	}
}

func TestWriteFile_MissingDirReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "state.yaml")

	if err := fsatomic.WriteFile(path, []byte("x"), 0o644); err == nil {
		t.Fatal("expected an error for a missing parent directory")
	}
}

func TestWriteFile_RenameFailureLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	// path names an existing non-empty directory, so the final os.Rename onto
	// it fails (a directory can only be replaced by another empty directory).
	path := filepath.Join(dir, "state.yaml")
	if err := os.MkdirAll(filepath.Join(path, "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := fsatomic.WriteFile(path, []byte("x"), 0o644); err == nil {
		t.Fatal("expected an error when the target is an existing non-empty directory")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.yaml" {
		t.Errorf("expected the deferred cleanup to remove the temp file, got %v", entries)
	}
}

func TestWriteFile_LeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")

	if err := fsatomic.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.yaml" {
		t.Errorf("expected only state.yaml in dir, got %v", entries)
	}
}
