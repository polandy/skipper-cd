package nixos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// recordingRunner captures all commands without executing them.
type recordingRunner struct {
	calls        []runCall
	errOnCommand string
}

type runCall struct {
	dir  string
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	if r.errOnCommand != "" && slices.Contains(args, r.errOnCommand) {
		return fmt.Errorf("simulated error for command: %s", r.errOnCommand)
	}
	return nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

// --- HashFiles tests ---

func TestHashFiles_FindsNixAndFlakeLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }")
	writeFile(t, filepath.Join(dir, "flake.lock"), "{}")
	if err := os.MkdirAll(filepath.Join(dir, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "modules", "service.nix"), "{ }")

	hashes, err := HashFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hashes) != 3 {
		t.Fatalf("expected 3 hashed files, got %d: %v", len(hashes), hashes)
	}
	for _, name := range []string{"flake.nix", "flake.lock", "modules/service.nix"} {
		if hashes[filepath.Join(dir, name)] == "" {
			t.Errorf("expected hash for %s", name)
		}
	}
}

func TestHashFiles_IgnoresNonNixFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "# readme")
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), "services: {}")
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }")

	hashes, err := HashFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hashes) != 1 {
		t.Errorf("expected 1 hashed file (flake.nix only), got %d: %v", len(hashes), hashes)
	}
}

func TestHashFiles_SkipsGitDirectory(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(gitDir, "config.nix"), "{ }")
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }")

	hashes, err := HashFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hashes) != 1 {
		t.Errorf("expected 1 hashed file, got %d: %v", len(hashes), hashes)
	}
	if hashes[filepath.Join(gitDir, "config.nix")] != "" {
		t.Error("expected .git/config.nix to be skipped")
	}
}

// --- Rebuild tests ---

func TestRebuild_RunsNixosRebuildSwitch(t *testing.T) {
	dir := t.TempDir()
	runner := &recordingRunner{}
	r := New(runner)

	if err := r.Rebuild(context.Background(), dir, ".#nuc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.calls))
	}

	c := runner.calls[0]
	if c.name != "nixos-rebuild" {
		t.Errorf("expected nixos-rebuild, got %s", c.name)
	}
	expectedArgs := []string{"switch", "--flake", ".#nuc"}
	for i, a := range expectedArgs {
		if i >= len(c.args) || c.args[i] != a {
			t.Errorf("expected arg[%d]=%q, got args=%v", i, a, c.args)
		}
	}
	if c.dir != dir {
		t.Errorf("expected dir %s, got %s", dir, c.dir)
	}
}

func TestRebuild_ReturnsErrorOnFailure(t *testing.T) {
	runner := &recordingRunner{errOnCommand: "switch"}
	r := New(runner)

	if err := r.Rebuild(context.Background(), t.TempDir(), ".#nuc"); err == nil {
		t.Fatal("expected error when nixos-rebuild fails")
	}
}

// --- DiffHashes tests ---

func TestDiffHashes_NoneWhenEqual(t *testing.T) {
	hashes := map[string]string{"/repo/flake.nix": "abc"}
	if changed := DiffHashes(hashes, hashes); changed != nil {
		t.Errorf("expected no changed files, got %v", changed)
	}
}

func TestDiffHashes_DetectsChangedAndNewFiles(t *testing.T) {
	current := map[string]string{
		"/repo/flake.nix":  "new-hash",
		"/repo/flake.lock": "lock-hash",
	}
	prev := map[string]string{"/repo/flake.nix": "stale-hash"}

	changed := DiffHashes(current, prev)
	if len(changed) != 2 {
		t.Fatalf("expected 2 changed files (modified + new), got %d: %v", len(changed), changed)
	}
}
