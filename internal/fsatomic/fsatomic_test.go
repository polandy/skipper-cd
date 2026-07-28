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
	// WriteFile does not create the tree — callers own that decision, so a
	// typo'd path must surface as an error rather than a directory nobody asked
	// for.
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("parent directory should not have been created, stat err = %v", err)
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

// The package's whole promise is that a failed write cannot damage what is
// already on disk — a truncated state file is worse than a stale one. The
// existing failure test only checks that no temp file is left behind, so the
// promise itself is asserted here.
func TestWriteFile_FailedWriteLeavesTheExistingFileIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	const original = "committed: true\n"
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}

	// A directory in place of the temp file's home makes CreateTemp fail before
	// anything touches path.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("running as root: the directory mode would not deny the write")
	}

	if err := fsatomic.WriteFile(path, []byte("committed: false\n"), 0o640); err == nil {
		t.Fatal("expected an error when the parent directory is not writable")
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("existing file was modified by a failed write: got %q, want %q", got, original)
	}
}

func TestWriteFile_ReplacingAnExistingFileAppliesTheRequestedMode(t *testing.T) {
	// The mode is applied to the temp file before the rename, so a replace must
	// end up with the requested mode and not inherit the old file's.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}

	if err := fsatomic.WriteFile(path, []byte("new"), fsatomic.PrivateFileMode); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fsatomic.PrivateFileMode {
		t.Errorf("mode = %v, want %v", info.Mode().Perm(), fsatomic.PrivateFileMode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("content = %q, want %q", data, "new")
	}
}

// Three error branches in WriteFile stay uncovered on purpose: the failures of
// tmp.Write, tmp.Chmod and tmp.Close. All three do the same thing — close the
// temp file and return, leaving the deferred Remove to clean up — and none is
// reachable from an unprivileged test: they need ENOSPC, a filesystem that
// rejects chmod, or a delayed-writeback error, not something a temp directory
// can be made to produce. Reaching them would mean threading a fake file
// handle through a 42-line package, which buys a coverage number rather than a
// guarantee. The guarantee they protect — a failed write leaves the existing
// file untouched, and no temp file behind — is asserted above through the
// CreateTemp and rename doors, which are reachable.
