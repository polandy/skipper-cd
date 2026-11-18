package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// healConfig writes a stack's compose file under a temp base dir and returns a
// config pointing at it.
func healConfig(t *testing.T, stack string) *config.Config {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, stack)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	return &config.Config{StacksBaseDir: base, Stacks: []config.Stack{{Name: stack}}}
}

// recordDeployed persists the clone's current inputs of stack as its deployed
// version at commit, the way a successful deploy leaves the state.
func recordDeployed(t *testing.T, d *Deployer, cfg *config.Config, stack, commit string) {
	t.Helper()
	st, _ := d.effectiveStack(cfg, stack)
	prep, err := d.prepareStackRun(context.Background(), st, cfg.StacksBaseDir, cfg.VarsFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	state := newEmptyState()
	state.LastDeployedCommit = commit
	state.recordStack(stack, prep.currentHashes)
	if err := saveDeployState(d.stateDir, state); err != nil {
		t.Fatal(err)
	}
}

// composeAtUp records the content of the compose file each `up` applied: the
// -f argument when given, else the compose file in the call's directory.
// Read during the call, since a restored file is removed afterwards.
func composeAtUp(t *testing.T, r *recordingRunner) *[]string {
	t.Helper()
	applied := &[]string{}
	r.failFn = func(dir string, args []string) error {
		if !slices.Contains(args, "up") {
			return nil
		}
		path := filepath.Join(dir, "docker-compose.yml")
		if i := slices.Index(args, "-f"); i >= 0 {
			path = args[i+1]
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read applied compose file: %v", err)
		}
		*applied = append(*applied, string(content))
		return nil
	}
	return applied
}

func TestHealStack_RunsCorrectiveUpWithoutClaimingRecovery(t *testing.T) {
	r := &recordingRunner{}
	var got []events.DeployEvent
	d := New(Config{Runner: r, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) { got = append(got, e) }})

	cfg := healConfig(t, "web")
	recordDeployed(t, d, cfg, "web", "v1")
	drift := []events.DriftedService{{Name: "web", Status: "unhealthy"}}
	ran, err := d.HealStack(context.Background(), cfg, "web", drift)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Fatal("expected ran=true when the mutex was free")
	}

	// Exactly one corrective up, and never a --wait health gate or a rollback.
	if len(r.calls) != 1 {
		t.Fatalf("expected exactly one docker call, got %d: %+v", len(r.calls), r.calls)
	}
	want := []string{"compose", "up", "-d", "--remove-orphans"}
	if r.calls[0].name != "docker" || !slices.Equal(r.calls[0].args, want) {
		t.Fatalf("unexpected argv: %s %v", r.calls[0].name, r.calls[0].args)
	}
	assertCommandNotCalled(t, r.calls, "--wait")

	// An up that exits 0 says nothing about recovery: the healed event waits
	// for the engine to see the stack healthy (EmitHealed). The recorded up
	// above is the positive signal that the heal did run.
	if len(got) != 0 {
		t.Fatalf("HealStack must not emit an event of its own, got %+v", got)
	}
}

func TestEmitHealed_CarriesDriftAndDuration(t *testing.T) {
	var got []events.DeployEvent
	d := New(Config{Runner: &recordingRunner{}, EventSink: func(e events.DeployEvent) { got = append(got, e) }})

	drift := []events.DriftedService{{Name: "web", Status: "unhealthy"}}
	d.EmitHealed("web", 90*time.Second, drift)

	if len(got) != 1 || got[0].Status != events.StatusHealed || got[0].Stack != "web" {
		t.Fatalf("expected one healed event for web, got %+v", got)
	}
	if !slices.Equal(got[0].HealDrift, drift) {
		t.Fatalf("expected healed event to carry drift %+v, got %+v", drift, got[0].HealDrift)
	}
	if got[0].DurationMs != 90000 {
		t.Errorf("duration = %dms, want the time to recovery (90000)", got[0].DurationMs)
	}
	if got[0].ChangedFiles != nil || got[0].Diffs != nil {
		t.Fatalf("a heal carries no changed files or diffs, got files=%v diffs=%v", got[0].ChangedFiles, got[0].Diffs)
	}
}

func TestHealStack_SkipsWhenDeployInProgress(t *testing.T) {
	r := &recordingRunner{}
	d := newDeployerWithRunner(r)
	cfg := healConfig(t, "web")

	// Simulate a deploy already holding the lock.
	d.mu.Lock()
	defer d.mu.Unlock()

	ran, err := d.HealStack(context.Background(), cfg, "web", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Fatal("expected ran=false while a deploy holds the mutex")
	}
	if len(r.calls) != 0 {
		t.Fatalf("no docker call expected when skipped, got %+v", r.calls)
	}
}

func TestHealStack_ReturnsErrorWithoutHealedEventOnUpFailure(t *testing.T) {
	r := &recordingRunner{errOnCommand: "up"}
	var got []events.DeployEvent
	d := New(Config{Runner: r, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) { got = append(got, e) }})

	cfg := healConfig(t, "web")
	recordDeployed(t, d, cfg, "web", "v1")
	ran, err := d.HealStack(context.Background(), cfg, "web", nil)
	if !ran {
		t.Fatal("expected ran=true: the up was attempted")
	}
	if err == nil {
		t.Fatal("expected an error when the corrective up fails")
	}
	// A single failed up must not emit a misleading healed (or failed) event —
	// the engine counts the attempt and only heal_exhausted is terminal.
	for _, e := range got {
		if e.Status == events.StatusHealed {
			t.Fatalf("no healed event expected on a failed up, got %+v", got)
		}
	}
}

func TestHealStack_UnknownStack(t *testing.T) {
	r := &recordingRunner{}
	d := newDeployerWithRunner(r)
	cfg := healConfig(t, "web")

	ran, err := d.HealStack(context.Background(), cfg, "nope", nil)
	if !ran || err == nil {
		t.Fatalf("expected ran=true and an error for an unknown stack, got ran=%v err=%v", ran, err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("no docker call expected for an unknown stack, got %+v", r.calls)
	}
}

// A paused stack with a queued change is healed back to the version it runs,
// never forward to the queued one (S2, ADR-0029): pause governs git changes,
// self-heal restores what was last deployed.
func TestHealStack_PausedStackWithQueuedChangeRestoresDeployedVersion(t *testing.T) {
	d, r, q, _ := pausedDeployer(t)
	d.stateDir = t.TempDir()
	cfg := healConfig(t, "web")
	composePath := filepath.Join(cfg.StacksBaseDir, "web", "docker-compose.yml")
	d.commitReader = &fakeCommitReader{files: map[string][]byte{"v1:" + composePath: []byte(composeWithImage("nginx:1.25"))}}
	recordDeployed(t, d, cfg, "web", "v1")

	// A merge lands while paused: the change is queued, not deployed.
	writeFile(t, composePath, composeWithImage("nginx:1.27"))
	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.deployStackIfChanged(context.Background(), cfg.Stacks[0], cfg.StacksBaseDir, "", nil, state); err != nil {
		t.Fatal(err)
	}
	if !q.Has("web") {
		t.Fatal("precondition: expected the change to be queued")
	}

	applied := composeAtUp(t, r)
	ran, err := d.HealStack(context.Background(), cfg, "web", nil)
	if !ran || err != nil {
		t.Fatalf("expected the heal to run, got ran=%v err=%v", ran, err)
	}
	if want := []string{composeWithImage("nginx:1.25")}; !slices.Equal(*applied, want) {
		t.Fatalf("heal applied %q, want the deployed version %q", *applied, want)
	}
}

// A change that failed and was rolled back is held (ADR-0062); a heal must
// restore the rolled-back version, not re-apply the failing one (S2).
func TestHealStack_HeldStackRestoresDeployedVersion(t *testing.T) {
	r := &recordingRunner{}
	cfg := healConfig(t, "web")
	composePath := filepath.Join(cfg.StacksBaseDir, "web", "docker-compose.yml")
	cr := &fakeCommitReader{files: map[string][]byte{"v1:" + composePath: []byte(composeWithImage("nginx:1.25"))}}
	d := New(Config{Runner: r, CommitReader: cr, StateDir: t.TempDir()})
	recordDeployed(t, d, cfg, "web", "v1")

	// The new version fails its up and is rolled back, which holds it.
	writeFile(t, composePath, composeWithImage("nginx:broken"))
	r.failFn = func(dir string, args []string) error {
		if slices.Contains(args, "up") && !slices.Contains(args, "-f") {
			return errors.New("container exited")
		}
		return nil
	}
	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.deployStackIfChanged(context.Background(), cfg.Stacks[0], cfg.StacksBaseDir, "", nil, state); !errors.Is(err, ErrRolledBack) {
		t.Fatalf("precondition: expected a rollback, got %v", err)
	}
	if _, held := state.heldFor("web"); !held {
		t.Fatal("precondition: expected the failed change to be held")
	}
	if err := saveDeployState(d.stateDir, state); err != nil {
		t.Fatal(err)
	}

	applied := composeAtUp(t, r)
	ran, err := d.HealStack(context.Background(), cfg, "web", nil)
	if !ran || err != nil {
		t.Fatalf("expected the heal to run, got ran=%v err=%v", ran, err)
	}
	if want := []string{composeWithImage("nginx:1.25")}; !slices.Equal(*applied, want) {
		t.Fatalf("heal applied %q, want the rolled-back version %q", *applied, want)
	}
}

// A pending change outside the compose file cannot be undone by restoring one
// file, so the heal is deferred: ran=false (no attempt spent), no docker call.
func TestHealStack_DefersWhenAnotherInputHasAPendingChange(t *testing.T) {
	r := &recordingRunner{}
	cfg := healConfig(t, "web")
	envFile := filepath.Join(cfg.StacksBaseDir, "web", "app.env")
	writeFile(t, envFile, "TAG=1\n")
	cfg.Stacks[0].EnvFiles = []string{envFile}
	d := New(Config{Runner: r, CommitReader: &fakeCommitReader{}, StateDir: t.TempDir()})
	recordDeployed(t, d, cfg, "web", "v1")

	writeFile(t, envFile, "TAG=2\n")
	ran, err := d.HealStack(context.Background(), cfg, "web", nil)
	if ran || !errors.Is(err, ErrHealDeferred) {
		t.Fatalf("expected a deferred heal (ran=false, ErrHealDeferred), got ran=%v err=%v", ran, err)
	}
	if !strings.Contains(err.Error(), "app.env") {
		t.Errorf("expected the deferral to name the pending file, got %v", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("no docker call expected for a deferred heal, got %+v", r.calls)
	}
}

// The deferral cases that leave nothing to restore from.
func TestHealStack_DefersWithoutADeployedVersionToRestore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, d *Deployer, cfg *config.Config)
	}{
		{"nothing recorded", func(*testing.T, *Deployer, *config.Config) {}},
		{"compose changed, no base commit", func(t *testing.T, d *Deployer, cfg *config.Config) {
			recordDeployed(t, d, cfg, "web", "")
			writeFile(t, filepath.Join(cfg.StacksBaseDir, "web", "docker-compose.yml"), composeWithImage("nginx:1.27"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recordingRunner{}
			cfg := healConfig(t, "web")
			d := New(Config{Runner: r, CommitReader: &fakeCommitReader{}, StateDir: t.TempDir()})
			tc.setup(t, d, cfg)

			ran, err := d.HealStack(context.Background(), cfg, "web", nil)
			if ran || !errors.Is(err, ErrHealDeferred) {
				t.Fatalf("expected a deferred heal, got ran=%v err=%v", ran, err)
			}
			if len(r.calls) != 0 {
				t.Fatalf("no docker call expected for a deferred heal, got %+v", r.calls)
			}
		})
	}
}

// A base commit that cannot be read is a failed attempt, not a deferral: the
// state says a version exists, so the budget runs and heal_exhausted reports it.
func TestHealStack_CountsAnUnreadableBaseCommitAsAFailedAttempt(t *testing.T) {
	r := &recordingRunner{}
	cfg := healConfig(t, "web")
	d := New(Config{Runner: r, CommitReader: &fakeCommitReader{fileErr: errors.New("object not found")}, StateDir: t.TempDir()})
	recordDeployed(t, d, cfg, "web", "v1")
	writeFile(t, filepath.Join(cfg.StacksBaseDir, "web", "docker-compose.yml"), composeWithImage("nginx:1.27"))

	ran, err := d.HealStack(context.Background(), cfg, "web", nil)
	if !ran || err == nil || errors.Is(err, ErrHealDeferred) {
		t.Fatalf("expected ran=true with a plain error, got ran=%v err=%v", ran, err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("no docker call expected without a restorable compose file, got %+v", r.calls)
	}
}
