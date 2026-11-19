package nixos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

	unitShow    string // `systemctl show` stdout for the ended unit
	journal     string // `journalctl` stdout for the unit's invocation
	outputCalls []runCall
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

// Output serves the post-mortem reads of a failed unit: its properties and
// its journal.
func (r *fakeUnitRunner) Output(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	r.outputCalls = append(r.outputCalls, runCall{dir: dir, name: name, args: args})
	switch name {
	case "systemctl":
		return []byte(r.unitShow), nil
	case "journalctl":
		return []byte(r.journal), nil
	}
	return nil, errors.New("unexpected output command " + name)
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
	r := New(runner, runner)
	r.pollInterval = 0 // no real sleeping in tests

	if err := r.Rebuild(context.Background(), dir, ".#host-a", time.Hour); err != nil {
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
	r := New(runner, runner)
	r.pollInterval = 0

	err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a", time.Hour)
	if err == nil {
		t.Fatal("expected error when the rebuild unit ends failed")
	}
	if !runner.hasCall("systemctl", "is-failed") {
		t.Errorf("expected an is-failed probe, got calls=%v", runner.calls)
	}
}

func TestRebuild_ReturnsErrorWhenStartFails(t *testing.T) {
	runner := &fakeUnitRunner{failStart: true}
	r := New(runner, runner)
	r.pollInterval = 0

	if err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a", time.Hour); err == nil {
		t.Fatal("expected error when systemd-run fails to start the unit")
	}
}

// TestRebuild_BoundsTheUnitRuntime proves the timeout is enforced by systemd on
// the unit itself, so it holds even while skipper restarts during the switch.
func TestRebuild_BoundsTheUnitRuntime(t *testing.T) {
	runner := &fakeUnitRunner{}
	r := New(runner, runner)
	r.pollInterval = 0

	if err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a", 90*time.Minute); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := findCall(t, runner, "systemd-run")
	if !slices.Contains(c.args, "--property=RuntimeMaxSec=5400") {
		t.Errorf("expected --property=RuntimeMaxSec=5400 in args, got %v", c.args)
	}
	if i := slices.Index(c.args, "nixos-rebuild"); i < slices.Index(c.args, "--property=RuntimeMaxSec=5400") {
		t.Errorf("the property must precede the command, got %v", c.args)
	}
}

func TestRebuild_FailureNamesTheTimeoutKey(t *testing.T) {
	runner := &fakeUnitRunner{activePolls: 1, unitFailed: true}
	r := New(runner, runner)
	r.pollInterval = 0

	err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a", time.Hour)
	if err == nil {
		t.Fatal("expected error when the rebuild unit ends failed")
	}
	for _, want := range []string{"journalctl -u skipper-nixos-rebuild.service", "nixos_rebuild.timeout_seconds", "3600"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %s", err, want)
		}
	}
}

// switchJournal is the tail switch-to-configuration leaves when the new
// configuration activated but units failed to start (exit status 4).
const switchJournal = `restarting the following units: nginx.service
warning: the following units failed: docker-compose-nextcloud.service, nextcloud-oidc-provider.service
× nextcloud-oidc-provider.service - OIDC provider
switching to system configuration /nix/store/abc-nixos-system failed (status 4)
`

func TestRebuild_ExitStatusFourReportsTheFailedUnits(t *testing.T) {
	runner := &fakeUnitRunner{
		activePolls: 1, unitFailed: true,
		unitShow: "ExecMainStatus=4\nInvocationID=0f3e\n",
		journal:  switchJournal,
	}
	r := New(runner, runner)
	r.pollInterval = 0

	err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a", time.Hour)

	var unitsFailed *UnitsFailedError
	if !errors.As(err, &unitsFailed) || !errors.Is(err, ErrActivatedUnitsFailed) {
		t.Fatalf("expected ErrActivatedUnitsFailed, got %v", err)
	}
	wantUnits := []string{"docker-compose-nextcloud.service", "nextcloud-oidc-provider.service"}
	if !slices.Equal(unitsFailed.Units, wantUnits) {
		t.Errorf("units = %v, want %v", unitsFailed.Units, wantUnits)
	}
	for _, want := range append(wantUnits, "activated", "journalctl -u skipper-nixos-rebuild.service") {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %s", err, want)
		}
	}
	// The journal read is scoped to this run's invocation, not older runs of the reused unit name.
	if !slices.ContainsFunc(runner.outputCalls, func(c runCall) bool {
		return c.name == "journalctl" && slices.Contains(c.args, "_SYSTEMD_INVOCATION_ID=0f3e")
	}) {
		t.Errorf("expected a journalctl read of invocation 0f3e, got %v", runner.outputCalls)
	}
}

func TestRebuild_ExitStatusFourWithoutAUnitListStillMeansActivated(t *testing.T) {
	runner := &fakeUnitRunner{activePolls: 1, unitFailed: true, unitShow: "ExecMainStatus=4\nInvocationID=\n"}
	r := New(runner, runner)
	r.pollInterval = 0

	err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a", time.Hour)

	if !errors.Is(err, ErrActivatedUnitsFailed) {
		t.Fatalf("expected ErrActivatedUnitsFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "some units failed") {
		t.Errorf("error %q should say that some units failed", err)
	}
}

func TestRebuild_OtherExitStatusesStayARebuildFailure(t *testing.T) {
	for name, show := range map[string]string{
		"activation script failed": "ExecMainStatus=2\nInvocationID=0f3e\n",
		"build failed":             "ExecMainStatus=1\nInvocationID=0f3e\n",
		"killed by the timeout":    "ExecMainStatus=15\nInvocationID=0f3e\n",
		"status unreadable":        "",
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeUnitRunner{activePolls: 1, unitFailed: true, unitShow: show, journal: switchJournal}
			r := New(runner, runner)
			r.pollInterval = 0

			err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a", time.Hour)

			if err == nil || errors.Is(err, ErrActivatedUnitsFailed) {
				t.Fatalf("expected a plain rebuild failure, got %v", err)
			}
			// Positive signal: the status was read before it was ruled out.
			if !slices.ContainsFunc(runner.outputCalls, func(c runCall) bool { return c.name == "systemctl" && slices.Contains(c.args, "show") }) {
				t.Errorf("expected a systemctl show read, got %v", runner.outputCalls)
			}
		})
	}
}

func TestRebuild_WithoutAnOutputterAFailedUnitStaysARebuildFailure(t *testing.T) {
	runner := &fakeUnitRunner{activePolls: 1, unitFailed: true, unitShow: "ExecMainStatus=4\n"}
	r := New(runner, nil)
	r.pollInterval = 0

	err := r.Rebuild(context.Background(), t.TempDir(), ".#host-a", time.Hour)

	if err == nil || errors.Is(err, ErrActivatedUnitsFailed) {
		t.Fatalf("expected a plain rebuild failure, got %v", err)
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
	r := New(runner, runner)
	r.pollInterval = 0

	err := r.Rebuild(ctx, t.TempDir(), ".#host-a", time.Hour)
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

func TestDiffHashes_IgnoresReservedDrvKey(t *testing.T) {
	current := map[string]string{"/repo/flake.nix": "abc", SystemDrvKey: "/nix/store/new.drv"}
	prev := map[string]string{"/repo/flake.nix": "abc", SystemDrvKey: "/nix/store/old.drv"}
	if changed := DiffHashes(current, prev); changed != nil {
		t.Errorf("the reserved derivation key must never read as a changed file, got %v", changed)
	}
}
