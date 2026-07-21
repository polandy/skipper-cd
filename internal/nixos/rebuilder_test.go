package nixos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeUnitRunner records commands and scripts the transient-unit lifecycle that
// Rebuild polls: systemd-run starts it, then `systemctl is-active` reports it
// active for activePolls probes before going inactive, at which point
// `systemctl is-failed` reflects unitFailed. onCall fires per call so a test can
// e.g. cancel the context mid-poll.
type fakeUnitRunner struct {
	calls       []runCall
	failStart   bool // make the systemd-run start fail
	activePolls int  // how many is-active probes report "active" before inactive
	unitFailed  bool // is-failed result once the unit is inactive
	activeSeen  int
	onCall      func(name string, args []string)
}

type runCall struct {
	dir  string
	name string
	args []string
}

func (r *fakeUnitRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	if r.onCall != nil {
		r.onCall(name, args)
	}
	switch {
	case name == "systemd-run":
		if r.failStart {
			return errors.New("simulated systemd-run start failure")
		}
		return nil
	case name == "systemctl" && len(args) > 0 && args[0] == "is-active":
		r.activeSeen++
		if r.activeSeen <= r.activePolls {
			return nil // still active
		}
		return errors.New("inactive")
	case name == "systemctl" && len(args) > 0 && args[0] == "is-failed":
		if r.unitFailed {
			return nil // exit 0 == unit is in failed state
		}
		return errors.New("not failed")
	default: // reset-failed and anything else succeed silently
		return nil
	}
}

// callNames returns the command names in call order, for asserting sequence.
func (r *fakeUnitRunner) hasCall(name string, argMatch string) bool {
	return slices.ContainsFunc(r.calls, func(c runCall) bool {
		return c.name == name && (argMatch == "" || slices.Contains(c.args, argMatch))
	})
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

// findCall returns the first recorded call for a command name.
func findCall(t *testing.T, r *fakeUnitRunner, name string) runCall {
	t.Helper()
	for _, c := range r.calls {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no %s call recorded; calls=%v", name, r.calls)
	return runCall{}
}

func TestRebuild_StartsFireAndForgetTransientUnitThenReportsSuccess(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeUnitRunner{activePolls: 2} // active for two probes, then success
	r := New(runner)
	r.pollInterval = 0 // no real sleeping in tests

	if err := r.Rebuild(context.Background(), dir, ".#host-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A stale failed unit is cleared before starting so the fixed name is reusable.
	if !runner.hasCall("systemctl", "reset-failed") {
		t.Errorf("expected a systemctl reset-failed call, got calls=%v", runner.calls)
	}

	c := findCall(t, runner, "systemd-run")
	// Fire-and-forget: NO --wait/--pipe (they would keep a client in skipper's
	// cgroup and deadlock the self-restart) and NO --collect (a failed unit must
	// linger so is-failed can see it). See ADR-0014.
	for _, forbidden := range []string{"--wait", "--pipe", "--collect"} {
		if slices.Contains(c.args, forbidden) {
			t.Errorf("did not expect %q in fire-and-forget args, got %v", forbidden, c.args)
		}
	}
	// --same-dir IS required: it runs the unit in the repo dir so `--flake .#host`
	// resolves (unrelated to the wedge). --unit names it.
	for _, want := range []string{"--unit=skipper-nixos-rebuild", "--same-dir"} {
		if !slices.Contains(c.args, want) {
			t.Errorf("expected %q in args, got %v", want, c.args)
		}
	}
	if !slices.ContainsFunc(c.args, func(a string) bool { return strings.HasPrefix(a, "--setenv=PATH=") }) {
		t.Errorf("expected PATH to be propagated, got args=%v", c.args)
	}
	wantTail := []string{"nixos-rebuild", "switch", "--flake", ".#host-a"}
	if len(c.args) < len(wantTail) || !slices.Equal(c.args[len(c.args)-len(wantTail):], wantTail) {
		t.Errorf("expected args to end with %v, got %v", wantTail, c.args)
	}
	if c.dir != dir {
		t.Errorf("expected dir %s, got %s", dir, c.dir)
	}
	// It actually polled the unit before returning success.
	if !runner.hasCall("systemctl", "is-active") {
		t.Errorf("expected is-active polling, got calls=%v", runner.calls)
	}
}

func TestRebuild_ReturnsErrorWhenUnitFails(t *testing.T) {
	runner := &fakeUnitRunner{activePolls: 1, unitFailed: true}
	r := New(runner)
	r.pollInterval = 0

	err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a")
	if err == nil {
		t.Fatal("expected error when the rebuild unit ends failed")
	}
	if !runner.hasCall("systemctl", "is-failed") {
		t.Errorf("expected an is-failed probe, got calls=%v", runner.calls)
	}
}

func TestRebuild_ReturnsErrorWhenStartFails(t *testing.T) {
	runner := &fakeUnitRunner{failStart: true}
	r := New(runner)
	r.pollInterval = 0

	if err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a"); err == nil {
		t.Fatal("expected error when systemd-run fails to start the unit")
	}
}

// TestRebuild_AbandonsWaitOnContextCancel proves the self-restart path: when the
// switch restarts skipper (shutdown cancels the context) the poll abandons and
// returns promptly instead of blocking, letting the detached unit finish the
// switch. The unit here stays "active" forever, so only cancellation ends it.
func TestRebuild_AbandonsWaitOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeUnitRunner{activePolls: 1 << 30} // never goes inactive on its own
	runner.onCall = func(name string, args []string) {
		if name == "systemctl" && len(args) > 0 && args[0] == "is-active" {
			cancel() // simulate shutdown arriving during the rebuild
		}
	}
	r := New(runner)
	r.pollInterval = 0

	err := r.Rebuild(ctx, t.TempDir(), ".#host-a")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled (abandoned wait), got %v", err)
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
