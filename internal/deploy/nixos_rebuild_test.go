package deploy

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/nixos"
)

// --- NixOS rebuild integration tests ---

func TestDeployAllStacks_AbortsStacksWhenNixOSFails(t *testing.T) {
	baseDir := t.TempDir()

	// Create a nix file so nixos rebuild detects changes.
	writeFile(t, filepath.Join(baseDir, "flake.nix"), "{ }")

	// Create a docker stack that should NOT be deployed.
	stackDir := filepath.Join(baseDir, "modules", "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{errOnCommand: "switch"}
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: t.TempDir()})

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"},
	}

	d.DeployAllStacks(context.Background(), cfg)

	// nixos-rebuild should have been attempted (via its transient unit).
	if !nixosRebuildCalled(runner.calls) {
		t.Error("expected nixos-rebuild to be called")
	}

	// docker compose should NOT have been called since nixos-rebuild failed.
	for _, c := range runner.calls {
		if c.name == "docker" && slices.Contains(c.args, "compose") {
			t.Error("expected docker compose NOT to be called after nixos-rebuild failure")
			break
		}
	}
}

// nixosRebuildCalled reports whether a nixos-rebuild was dispatched
// (wrapped in its systemd-run transient unit).
func nixosRebuildCalled(calls []runCall) bool {
	for _, c := range calls {
		if c.name == "systemd-run" && slices.Contains(c.args, "nixos-rebuild") {
			return true
		}
	}
	return false
}

// shutdownAwareRunner blocks the nixos-rebuild call until its context is
// canceled — like a real `systemd-run --wait` would while switch-to-
// configuration waits for the skipper unit to stop.
type shutdownAwareRunner struct {
	calls          []runCall
	rebuildStarted chan struct{}
}

func (r *shutdownAwareRunner) Run(ctx context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	if name == "systemd-run" {
		close(r.rebuildStarted)
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func TestDeployAllStacks_ShutdownDuringRebuildAbortsStackDeploys(t *testing.T) {
	baseDir := t.TempDir()
	writeFile(t, filepath.Join(baseDir, "flake.nix"), "{ }")
	stackDir := filepath.Join(baseDir, "modules", "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &shutdownAwareRunner{rebuildStarted: make(chan struct{})}
	shutdownCtx, requestShutdown := context.WithCancel(context.Background())
	defer requestShutdown()
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: t.TempDir(), ShutdownCtx: shutdownCtx})

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"},
	}

	done := make(chan struct{})
	go func() {
		d.DeployAllStacks(context.Background(), cfg)
		close(done)
	}()

	<-runner.rebuildStarted
	requestShutdown()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deploy run did not return after shutdown — rebuild wait was not canceled")
	}

	// The rebuild counts as failed for this run: no stacks deploy; the
	// startup sync after the restart picks them up.
	for _, c := range runner.calls {
		if c.name == "docker" {
			t.Errorf("expected no docker calls after shutdown, got %v", c.args)
		}
	}
}

func TestDeployAllStacks_NixOSSuccessContinuesToDockerStacks(t *testing.T) {
	baseDir := t.TempDir()

	// Create a nix file so nixos rebuild detects changes.
	writeFile(t, filepath.Join(baseDir, "flake.nix"), "{ }")

	// Create a docker stack.
	stackDir := filepath.Join(baseDir, "modules", "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: t.TempDir()})

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"},
	}

	d.DeployAllStacks(context.Background(), cfg)

	// Both nixos-rebuild and docker compose should have been called.
	dockerCalled := false
	for _, c := range runner.calls {
		if c.name == "docker" && slices.Contains(c.args, "compose") {
			dockerCalled = true
		}
	}
	if !nixosRebuildCalled(runner.calls) {
		t.Error("expected nixos-rebuild to be called")
	}
	if !dockerCalled {
		t.Error("expected docker compose to be called after successful nixos-rebuild")
	}
}

// A rebuild that fails while skipper stays alive (e.g. a broken derivation or
// a switch-to-configuration error) must not leave its nix hashes recorded as
// deployed — otherwise the never-applied rebuild is silently marked done and
// the next sync skips it. The hashes are reverted so the rebuild retries.
func TestDeployAllStacks_RetriesNixOSRebuildAfterSurvivingFailure(t *testing.T) {
	baseDir := t.TempDir()
	writeFile(t, filepath.Join(baseDir, "flake.nix"), "{ }")
	stackDir := filepath.Join(baseDir, "modules", "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	stateDir := t.TempDir()
	runner := &recordingRunner{errOnCommand: "switch"}
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: stateDir})

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"},
	}

	// First run: the rebuild fails while skipper is still running.
	d.DeployAllStacks(context.Background(), cfg)

	state, err := loadPersistedDeployState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.hashesFor(NixosStateKey); len(got) != 0 {
		t.Fatalf("expected _nixos hashes to be reverted after a surviving rebuild failure, got %v", got)
	}

	// Second run with unchanged nix files must attempt the rebuild again.
	d.DeployAllStacks(context.Background(), cfg)

	attempts := 0
	for _, c := range runner.calls {
		if c.name == "systemd-run" && slices.Contains(c.args, "nixos-rebuild") {
			attempts++
		}
	}
	if attempts != 2 {
		t.Fatalf("expected nixos-rebuild to be retried (2 attempts), got %d", attempts)
	}
}

// When the rebuild's switch is restarting skipper (shutdown requested), the
// pre-saved hashes must be kept so the startup sync does not rebuild again
// (ADR-0005, ADR-0014). This is the counterpart to the surviving-failure case.
func TestDeployAllStacks_KeepsNixHashesWhenRebuildAbandonedOnShutdown(t *testing.T) {
	baseDir := t.TempDir()
	writeFile(t, filepath.Join(baseDir, "flake.nix"), "{ }")
	stackDir := filepath.Join(baseDir, "modules", "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	stateDir := t.TempDir()
	runner := &shutdownAwareRunner{rebuildStarted: make(chan struct{})}
	shutdownCtx, requestShutdown := context.WithCancel(context.Background())
	defer requestShutdown()
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: stateDir, ShutdownCtx: shutdownCtx})

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"},
	}

	done := make(chan struct{})
	go func() {
		d.DeployAllStacks(context.Background(), cfg)
		close(done)
	}()

	<-runner.rebuildStarted
	requestShutdown()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deploy run did not return after shutdown")
	}

	state, err := loadPersistedDeployState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.hashesFor(NixosStateKey); len(got) == 0 {
		t.Fatal("expected _nixos hashes to be kept after a shutdown-abandoned rebuild")
	}
}

// A rebuild the switch interrupts by restarting skipper-cd (shutdown requested)
// is a normal outcome, not a failure: it must not emit a _nixos failed event,
// and it must leave an in-flight marker so the next startup can reconcile it
// into a success (ADR-0025).
func TestDeployAllStacks_ShutdownDuringRebuildDoesNotEmitFailure(t *testing.T) {
	baseDir := t.TempDir()
	writeFile(t, filepath.Join(baseDir, "flake.nix"), "{ }")
	stackDir := filepath.Join(baseDir, "modules", "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	stateDir := t.TempDir()
	runner := &shutdownAwareRunner{rebuildStarted: make(chan struct{})}
	// All emits happen in the deploy goroutine before it closes done, so reading
	// the slice after <-done is race-free (the channel establishes happens-before).
	var nixStatuses []events.Status
	shutdownCtx, requestShutdown := context.WithCancel(context.Background())
	defer requestShutdown()
	d := New(Config{
		Runner:      runner,
		RepoDir:     baseDir,
		StateDir:    stateDir,
		ShutdownCtx: shutdownCtx,
		EventSink: func(e events.DeployEvent) {
			if e.Stack == NixosStateKey {
				nixStatuses = append(nixStatuses, e.Status)
			}
		},
	})

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"},
	}

	done := make(chan struct{})
	go func() {
		d.DeployAllStacks(context.Background(), cfg)
		close(done)
	}()

	<-runner.rebuildStarted
	requestShutdown()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deploy run did not return after shutdown")
	}

	if slices.Contains(nixStatuses, events.StatusFailed) {
		t.Errorf("shutdown-abandoned rebuild must not emit a _nixos failed event, got %v", nixStatuses)
	}

	state, err := loadPersistedDeployState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.NixOSRebuildInFlight) == 0 {
		t.Fatal("expected an in-flight marker after a shutdown-abandoned rebuild so the next startup can reconcile it")
	}
}

// On the next startup after a self-restart interrupted a rebuild, the in-flight
// marker is reconciled into a _nixos success event (the rebuild applied in its
// transient unit) and cleared, so the UI stops showing the stale failure and no
// redundant rebuild runs (ADR-0025).
func TestRebuildNixOS_ReconcilesInterruptedRebuildIntoSuccess(t *testing.T) {
	baseDir := t.TempDir()
	nixFile := filepath.Join(baseDir, "flake.nix")
	writeFile(t, nixFile, "{ }")

	runner := &recordingRunner{}
	var nixStatuses []events.Status
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
		if e.Stack == NixosStateKey {
			nixStatuses = append(nixStatuses, e.Status)
		}
	}})

	// State left by a rebuild the switch interrupted: hashes were pre-saved (so
	// they match the current files) and the in-flight marker survived the restart.
	currentHashes, _ := nixos.HashFiles(baseDir)
	state := newEmptyState()
	state.recordStack(NixosStateKey, currentHashes)
	state.markNixOSRebuildInFlight([]string{nixFile})

	enabled := true
	cfg := &config.Config{NixOSRebuild: &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"}}

	if ok := d.rebuildNixOSIfChanged(context.Background(), cfg, state); !ok {
		t.Fatal("reconciliation must not abort the run")
	}

	if !slices.Contains(nixStatuses, events.StatusSuccess) {
		t.Errorf("expected a reconciled _nixos success event, got %v", nixStatuses)
	}
	if len(state.NixOSRebuildInFlight) != 0 {
		t.Error("expected the in-flight marker to be cleared after reconciliation")
	}
	if nixosRebuildCalled(runner.calls) {
		t.Error("reconciliation must not trigger another nixos-rebuild when nothing changed")
	}
}

// A reconciled _nixos success must carry the diffs of the reconciled files, not
// just their names: the interrupted run never advanced LastDeployedCommit, so the
// pre-restart baseline is still the right one to diff against. Without this the UI
// shows the changed-file list but no diff for every self-restarting rebuild
// (ADR-0025).
func TestRebuildNixOS_ReconciledSuccessCarriesDiffs(t *testing.T) {
	baseDir := t.TempDir()
	nixFile := filepath.Join(baseDir, "flake.nix")
	writeFile(t, nixFile, "{ }")

	runner := &recordingRunner{}
	reader := &fakeCommitReader{diffs: map[string]string{
		nixFile: "diff --git a/flake.nix b/flake.nix\n+changed",
	}}
	var reconciled events.DeployEvent
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: t.TempDir(), CommitReader: reader, EventSink: func(e events.DeployEvent) {
		if e.Stack == NixosStateKey && e.Status == events.StatusSuccess {
			reconciled = e
		}
	}})

	// State left by a switch-interrupted rebuild: hashes pre-saved, in-flight
	// marker survived, and LastDeployedCommit still points at the pre-change commit
	// because the interrupted run never reached the end-of-run update.
	currentHashes, _ := nixos.HashFiles(baseDir)
	state := newEmptyState()
	state.recordStack(NixosStateKey, currentHashes)
	state.markNixOSRebuildInFlight([]string{nixFile})
	state.LastDeployedCommit = "prev-sha"

	enabled := true
	cfg := &config.Config{NixOSRebuild: &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"}}

	if ok := d.rebuildNixOSIfChanged(context.Background(), cfg, state); !ok {
		t.Fatal("reconciliation must not abort the run")
	}

	if reconciled.Status != events.StatusSuccess {
		t.Fatalf("expected a reconciled _nixos success event, got %q", reconciled.Status)
	}
	if got := reconciled.Diffs["flake.nix"]; got == "" {
		t.Errorf("reconciled success must carry the diff for flake.nix, got diffs=%v", reconciled.Diffs)
	}
}

// TestDeployAllStacks_UnhashableNixFilesFailsInsteadOfSkipping covers the
// nixos phase's own failure path: when the repo's nix files cannot be hashed,
// skipper cannot tell whether the host config changed. Reporting the phase as
// skipped and deploying the stacks anyway would mark a rebuild that never ran
// as done — the silent no-op ADR-0015 exists to prevent.
func TestDeployAllStacks_UnhashableNixFilesFailsInsteadOfSkipping(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "modules", "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	var emitted []events.DeployEvent
	runner := &recordingRunner{}
	// RepoDir points at a path that does not exist, so the hash walk fails.
	d := New(Config{
		Runner:    runner,
		RepoDir:   filepath.Join(baseDir, "gone"),
		StateDir:  t.TempDir(),
		EventSink: func(e events.DeployEvent) { emitted = append(emitted, e) },
	})

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"},
	}

	d.DeployAllStacks(context.Background(), cfg)

	// Positive signal that the phase reported the condition rather than
	// swallowing it: exactly one _nixos event, and it is a failure naming the
	// hash error.
	var nixEvents []events.DeployEvent
	for _, e := range emitted {
		if e.Stack == NixosStateKey {
			nixEvents = append(nixEvents, e)
		}
	}
	if len(nixEvents) != 1 {
		t.Fatalf("expected exactly one %s event, got %d: %+v", NixosStateKey, len(nixEvents), nixEvents)
	}
	if nixEvents[0].Status != events.StatusFailed {
		t.Errorf("%s status = %q, want %q", NixosStateKey, nixEvents[0].Status, events.StatusFailed)
	}
	if !strings.Contains(nixEvents[0].Error, "hash") {
		t.Errorf("event error %q should name the hash failure", nixEvents[0].Error)
	}

	// Invariant 4: the stacks must not deploy when the nixos phase did not.
	for _, c := range runner.calls {
		if c.name == "docker" && slices.Contains(c.args, "compose") {
			t.Errorf("stacks must not deploy when the nix files could not be hashed; got %v", c.args)
			break
		}
	}
	if nixosRebuildCalled(runner.calls) {
		t.Error("no rebuild must be dispatched when the nix files could not be hashed")
	}
}
