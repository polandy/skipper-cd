package nixos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	if r.errOnCommand != "" {
		for _, a := range args {
			if a == r.errOnCommand {
				return fmt.Errorf("simulated error for command: %s", r.errOnCommand)
			}
		}
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

// --- RebuildIfChanged tests ---

func TestRebuildIfChanged_SkipsWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }")

	runner := &recordingRunner{}
	r := New(runner)

	prevHashes, err := HashFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := r.RebuildIfChanged(context.Background(), dir, ".#nuc", prevHashes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed != nil {
		t.Errorf("expected nil changed files, got %v", changed)
	}
	if len(runner.calls) != 0 {
		t.Errorf("expected no commands, got %d", len(runner.calls))
	}
}

func TestRebuildIfChanged_RunsWhenChanged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }")

	runner := &recordingRunner{}
	r := New(runner)

	// Empty prevHashes → everything is new.
	changed, err := r.RebuildIfChanged(context.Background(), dir, ".#nuc", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changed) == 0 {
		t.Fatal("expected changed files")
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

func TestRebuildIfChanged_ReturnsErrorOnFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }")

	runner := &recordingRunner{errOnCommand: "switch"}
	r := New(runner)

	changed, err := r.RebuildIfChanged(context.Background(), dir, ".#nuc", nil)
	if err == nil {
		t.Fatal("expected error when nixos-rebuild fails")
	}
	if len(changed) == 0 {
		t.Error("expected changed files even on error")
	}
}

func TestRebuildIfChanged_ReturnsChangedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }")
	writeFile(t, filepath.Join(dir, "flake.lock"), "{}")

	runner := &recordingRunner{}
	r := New(runner)

	// Start with only flake.nix hashed.
	prevHashes := map[string]string{
		filepath.Join(dir, "flake.nix"): "stale-hash",
	}

	changed, err := r.RebuildIfChanged(context.Background(), dir, ".#nuc", prevHashes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both flake.nix (changed hash) and flake.lock (new file) should be in the list.
	if len(changed) != 2 {
		t.Fatalf("expected 2 changed files, got %d: %v", len(changed), changed)
	}

	hasFlakeNix, hasFlakeLock := false, false
	for _, f := range changed {
		if f == filepath.Join(dir, "flake.nix") {
			hasFlakeNix = true
		}
		if f == filepath.Join(dir, "flake.lock") {
			hasFlakeLock = true
		}
	}
	if !hasFlakeNix {
		t.Error("expected flake.nix in changed files")
	}
	if !hasFlakeLock {
		t.Error("expected flake.lock in changed files")
	}
}
