package deploy

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// removeStackDir deletes a discovered stack's directory, the operator gesture
// that removes it from the deploy repo.
func removeStackDir(t *testing.T, cfg *config.Config, name string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(cfg.StacksBaseDir, name)); err != nil {
		t.Fatalf("remove stack dir %s: %v", name, err)
	}
}

// deployedThenRemoved runs one full deploy of the given stacks, then removes
// the named directories and returns a deployer sharing the same state dir plus
// the events of a second run. Both runs share the state dir, so the second run
// sees the first run's recorded stacks — the situation a removal creates.
func deployedThenRemoved(t *testing.T, stacks []string, remove []string, overrides []config.Stack) (*Deployer, *config.Config, *recordingRunner, *[]events.DeployEvent) {
	t.Helper()
	repoDir, cfg := discoveryRepo(t, stacks, overrides)
	stateDir := t.TempDir()
	first := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir})
	first.DeployAllStacks(context.Background(), cfg)

	for _, name := range remove {
		removeStackDir(t, cfg, name)
	}

	runner := &recordingRunner{}
	var recorded []events.DeployEvent
	second := New(Config{
		Runner:    runner,
		RepoDir:   repoDir,
		StateDir:  stateDir,
		EventSink: func(e events.DeployEvent) { recorded = append(recorded, e) },
	})
	second.DeployAllStacks(context.Background(), cfg)
	return second, cfg, runner, &recorded
}

func TestDeployAllStacks_RemovedStackEmitsRemovedEvent(t *testing.T) {
	_, _, runner, recorded := deployedThenRemoved(t, []string{"alpha", "beta"}, []string{"beta"}, nil)

	if got := eventsWithStatus(*recorded, events.StatusRemoved); len(got) != 1 || got[0] != "beta" {
		t.Errorf("removed events = %v, want [beta]", got)
	}
	// The remaining stack is still evaluated — the positive signal that the run
	// reached its normal decision point rather than aborting on the removal.
	if got := eventsWithStatus(*recorded, events.StatusSkipped); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("skipped events = %v, want [alpha]", got)
	}
	// A removal is a report, never an action: skipper does not tear the stack
	// down (ADR-0036 discarded prune).
	assertCommandNotCalled(t, runner.calls, "down")
	assertCommandNotCalled(t, runner.calls, "up")
}

func TestDeployAllStacks_RemovedStackEmitsOnlyOnce(t *testing.T) {
	d, cfg, _, recorded := deployedThenRemoved(t, []string{"alpha", "beta"}, []string{"beta"}, nil)
	if got := eventsWithStatus(*recorded, events.StatusRemoved); len(got) != 1 {
		t.Fatalf("first run after removal: removed events = %v, want [beta]", got)
	}

	// A third run over the same state must not re-announce the removal — the
	// skipped event proves the run happened and reached the same decision point.
	*recorded = (*recorded)[:0]
	d.DeployAllStacks(context.Background(), cfg)

	if got := eventsWithStatus(*recorded, events.StatusRemoved); len(got) != 0 {
		t.Errorf("removal must be announced once, got a repeat for %v", got)
	}
	if got := eventsWithStatus(*recorded, events.StatusSkipped); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("skipped events = %v, want [alpha] (the run must still have evaluated the set)", got)
	}
}

func TestDeployAllStacks_RemovedStackDropsHashesButKeepsProjectDir(t *testing.T) {
	d, _, _, _ := deployedThenRemoved(t, []string{"alpha", "beta"}, []string{"beta"}, nil)

	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if _, ok := state.Stacks["beta"]; ok {
		t.Error("a removed stack's hashes must be dropped, so a re-added stack deploys again")
	}
	if _, ok := state.Images["beta"]; ok {
		t.Error("a removed stack's images must be dropped")
	}
	// The project dir stays: it is how orphan detection recognizes the still
	// running project as formerly managed rather than unmanaged (ADR-0036).
	if _, ok := state.ProjectDirs["beta"]; !ok {
		t.Error("a removed stack's project dir must be kept for orphan classification")
	}
	if _, ok := state.Stacks["alpha"]; !ok {
		t.Error("the surviving stack's hashes must be untouched")
	}
}

func TestDeployAllStacks_RemovedStackCarriesChangeContext(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "beta"}, nil)
	stateDir := t.TempDir()
	composePath := filepath.Join(cfg.StacksBaseDir, "beta", "docker-compose.yml")
	reader := &fakeCommitReader{
		diffs:   map[string]string{composePath: "--- a/stacks/beta/docker-compose.yml\n+++ /dev/null\n"},
		commits: map[string][]events.CommitInfo{composePath: {{SHA: "dead123", Subject: "drop beta"}}},
	}
	first := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir, CommitReader: reader})
	first.DeployAllStacks(context.Background(), cfg)

	removeStackDir(t, cfg, "beta")

	var recorded []events.DeployEvent
	second := New(Config{
		Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir, CommitReader: reader,
		EventSink: func(e events.DeployEvent) { recorded = append(recorded, e) },
	})
	second.DeployAllStacks(context.Background(), cfg)

	var removed *events.DeployEvent
	for i, e := range recorded {
		if e.Status == events.StatusRemoved {
			removed = &recorded[i]
		}
	}
	if removed == nil {
		t.Fatalf("no removed event, got %v", recorded)
	}
	wantFile := filepath.Join("stacks", "beta", "docker-compose.yml")
	if len(removed.ChangedFiles) != 1 || removed.ChangedFiles[0] != wantFile {
		t.Errorf("ChangedFiles = %v, want [%s]", removed.ChangedFiles, wantFile)
	}
	if len(removed.Commits) != 1 || removed.Commits[0].SHA != "dead123" {
		t.Errorf("Commits = %v, want the commit that deleted the stack", removed.Commits)
	}
	if len(removed.Diffs) != 1 || removed.Diffs[wantFile] == "" {
		t.Errorf("Diffs = %v, want the deletion diff under %s", removed.Diffs, wantFile)
	}
}

func TestDeployAllStacks_DisabledStackEmitsNoRemovedEvent(t *testing.T) {
	// disabled: true parks a stack — its directory is still in the repo, so it
	// was not removed.
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "wip"}, nil)
	stateDir := t.TempDir()
	first := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir})
	first.DeployAllStacks(context.Background(), cfg)

	parked := &config.Config{
		StacksBaseDir:  cfg.StacksBaseDir,
		StackDiscovery: true,
		Stacks:         []config.Stack{{Name: "wip", Disabled: true}},
	}
	var recorded []events.DeployEvent
	second := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir,
		EventSink: func(e events.DeployEvent) { recorded = append(recorded, e) }})
	second.DeployAllStacks(context.Background(), parked)

	if got := eventsWithStatus(recorded, events.StatusRemoved); len(got) != 0 {
		t.Errorf("a disabled stack must not read as removed, got %v", got)
	}
	if got := eventsWithStatus(recorded, events.StatusSkipped); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("skipped events = %v, want [alpha] (the run must still have evaluated the set)", got)
	}
}

func TestDeployAllStacks_BrokenStackEmitsNoRemovedEvent(t *testing.T) {
	// An entry-level error excludes a stack from the deployable set, but its
	// directory is still there — it is broken, not gone.
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "bad"}, nil)
	stateDir := t.TempDir()
	first := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir})
	first.DeployAllStacks(context.Background(), cfg)

	broken := &config.Config{
		StacksBaseDir:  cfg.StacksBaseDir,
		StackDiscovery: true,
		Stacks:         []config.Stack{{Name: "bad", DeployHealthCheck: &config.HealthCheck{URL: "notaurl"}}},
	}
	var recorded []events.DeployEvent
	second := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir,
		EventSink: func(e events.DeployEvent) { recorded = append(recorded, e) }})
	second.DeployAllStacks(context.Background(), broken)

	if got := eventsWithStatus(recorded, events.StatusRemoved); len(got) != 0 {
		t.Errorf("a broken stack must not read as removed, got %v", got)
	}
	if got := eventsWithStatus(recorded, events.StatusFailed); len(got) != 1 || got[0] != "bad" {
		t.Errorf("failed events = %v, want [bad]", got)
	}
}

func TestDeployAllStacks_EmptyStackSetEmitsNoRemovedEvents(t *testing.T) {
	// Every directory gone at once reads far more like a broken clone than like
	// an operator deleting their whole fleet — announce nothing and say why.
	logged := captureLogAt(t, slog.LevelWarn, func(t *testing.T) {
		_, _, _, recorded := deployedThenRemoved(t, []string{"alpha", "beta"}, []string{"alpha", "beta"}, nil)
		if got := eventsWithStatus(*recorded, events.StatusRemoved); len(got) != 0 {
			t.Errorf("an empty stack set must announce no removals, got %v", got)
		}
	})
	if !strings.Contains(logged, "no stacks discovered") {
		t.Errorf("the skipped removal check must be logged, got %q", logged)
	}
}

func TestDeployAllStacks_EmptyStackSetKeepsRecordedHashes(t *testing.T) {
	// The guard must also leave state alone: dropping the hashes would redeploy
	// every stack once the clone is readable again.
	d, _, _, _ := deployedThenRemoved(t, []string{"alpha", "beta"}, []string{"alpha", "beta"}, nil)

	state, err := loadPersistedDeployState(d.stateDir)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if _, ok := state.Stacks["alpha"]; !ok {
		t.Error("hashes must survive a run that discovered nothing")
	}
}

func TestDeployAllStacks_RemovedFromHostConfigEmitsRemovedEvent(t *testing.T) {
	// Without discovery the host config's stacks: list is the set, so dropping
	// an entry from it is the same removal.
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "beta"}, nil)
	stateDir := t.TempDir()
	full := &config.Config{
		StacksBaseDir: cfg.StacksBaseDir,
		Stacks:        []config.Stack{{Name: "alpha"}, {Name: "beta"}},
	}
	first := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir})
	first.DeployAllStacks(context.Background(), full)

	trimmed := &config.Config{StacksBaseDir: cfg.StacksBaseDir, Stacks: []config.Stack{{Name: "alpha"}}}
	var recorded []events.DeployEvent
	second := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir,
		EventSink: func(e events.DeployEvent) { recorded = append(recorded, e) }})
	second.DeployAllStacks(context.Background(), trimmed)

	if got := eventsWithStatus(recorded, events.StatusRemoved); len(got) != 1 || got[0] != "beta" {
		t.Errorf("removed events = %v, want [beta]", got)
	}
}

func TestDeployAllStacks_ReservedStateKeysNeverReadAsRemoved(t *testing.T) {
	// _nixos and _config are state/event keys, not stacks: they are never
	// discovered, so a naive comparison would announce them removed every run.
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, nil)
	stateDir := t.TempDir()
	first := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir})
	first.DeployAllStacks(context.Background(), cfg)

	state, err := loadPersistedDeployState(stateDir)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	state.recordStack(config.ReservedStackName, stackFileHashes{"/etc/nixos/configuration.nix": "abc"})
	state.recordStack(config.ReservedConfigStackName, stackFileHashes{"/repo/stacks/skipper.yaml": "def"})
	if err := saveDeployState(stateDir, state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	var recorded []events.DeployEvent
	second := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir,
		EventSink: func(e events.DeployEvent) { recorded = append(recorded, e) }})
	second.DeployAllStacks(context.Background(), cfg)

	if got := eventsWithStatus(recorded, events.StatusRemoved); len(got) != 0 {
		t.Errorf("reserved state keys must never read as removed, got %v", got)
	}
}

func TestDeployAllStacks_RemovedStackLeavesNothingPending(t *testing.T) {
	// A stack removed while its change waits behind paused autosync must drop
	// out of the pending registry too: nothing will ever deploy it, and a stuck
	// entry pins last_deployed_commit for every other stack (finishRun holds the
	// base back while anything is queued).
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "beta"}, nil)
	stateDir := t.TempDir()
	queue := autosync.NewQueue()
	var recorded []events.DeployEvent
	d := New(Config{
		Runner:       &recordingRunner{},
		RepoDir:      repoDir,
		StateDir:     stateDir,
		CommitReader: &fakeCommitReader{},
		Autosync:     autosync.NewController(boolPtr(false), nil),
		Queue:        queue,
		EventSink:    func(e events.DeployEvent) { recorded = append(recorded, e) },
	})
	d.DeployAllStacks(context.Background(), cfg)
	if !queue.Has("beta") {
		t.Fatalf("setup: beta should be pending behind paused autosync, queue holds %d", queue.Count())
	}

	removeStackDir(t, cfg, "beta")
	d.DeployAllStacks(context.Background(), cfg)

	if queue.Has("beta") {
		t.Error("a removed stack must not stay pending")
	}
	// It is still announced: a deferred change records no hashes, so the removal
	// has to be recognised from the pending entry alone.
	if got := eventsWithStatus(recorded, events.StatusRemoved); len(got) != 1 || got[0] != "beta" {
		t.Errorf("removed events = %v, want [beta]", got)
	}
	// alpha is still queued, so the base stays pinned for it — the positive
	// signal that only the removed stack was dropped.
	if !queue.Has("alpha") {
		t.Error("the surviving stack must keep its pending entry")
	}
}
