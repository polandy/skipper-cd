package deploy

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/fsatomic"
	"github.com/polandy/skipper-cd/internal/metrics"
	dto "github.com/prometheus/client_model/go"
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

// advanceCommitBases pins a stack that failed this run (recorded, not settled,
// no base yet) to the global base it had, before that global moves on; settled
// stacks move to HEAD; reserved run-phase keys never get a base.
func TestAdvanceCommitBases_PinsUnsettledAndAdvancesSettled(t *testing.T) {
	state := newEmptyState()
	state.LastDeployedCommit = "old-sha"
	state.Stacks["ok"] = stackFileHashes{"f": "h"}
	state.Stacks["broken"] = stackFileHashes{"f": "h"}
	state.Stacks[NixosStateKey] = stackFileHashes{"f": "h"}
	state.StackCommits = map[string]string{"earlier": "older-sha"}
	state.markSettled("ok")

	state.advanceCommitBases("head-sha")
	state.LastDeployedCommit = "head-sha" // what finishRun does next

	want := map[string]string{"ok": "head-sha", "broken": "old-sha", "earlier": "older-sha"}
	for stack, sha := range want {
		if got := state.baseCommitFor(stack); got != sha {
			t.Errorf("base for %s = %q, want %q", stack, got, sha)
		}
	}
	if _, ok := state.StackCommits[NixosStateKey]; ok {
		t.Errorf("reserved key %s must not get a stack base", NixosStateKey)
	}
	if len(state.settled) != 0 {
		t.Errorf("settled must be reset after advancing, got %v", state.settled)
	}
}

// A stack with no base of its own falls back to the global base.
func TestBaseCommitFor_FallsBackToGlobal(t *testing.T) {
	state := newEmptyState()
	state.LastDeployedCommit = "global-sha"
	if got := state.baseCommitFor("new"); got != "global-sha" {
		t.Errorf("baseCommitFor = %q, want the global fallback", got)
	}
}

// The per-stack base survives a save/load, and forgetStack drops it with the
// rest of a removed stack's record.
func TestStackCommits_RoundTripAndForget(t *testing.T) {
	dir := t.TempDir()
	state := newEmptyState()
	state.Stacks["web"] = stackFileHashes{"f": "h"}
	state.markSettled("web")
	state.advanceCommitBases("head-sha")
	if err := saveDeployState(dir, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPersistedDeployState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.baseCommitFor("web"); got != "head-sha" {
		t.Fatalf("round-tripped base = %q, want head-sha", got)
	}
	loaded.forgetStack("web")
	if _, ok := loaded.StackCommits["web"]; ok {
		t.Error("forgetStack must drop the stack's commit base")
	}
}

// A run that dies after one stack must not redeploy it next time: the state
// on disk already records that stack's hashes and base while the next stack
// runs, and the stack not yet reached stays unrecorded.
func TestDeployAllStacks_SavesStateAfterEachStack(t *testing.T) {
	baseDir := t.TempDir()
	stateDir := t.TempDir()
	makeStacks(t, baseDir, "alpha", "beta")
	seeded := newEmptyState()
	seeded.LastDeployedCommit = "old-sha" // not a bootstrap run
	if err := saveDeployState(stateDir, seeded); err != nil {
		t.Fatal(err)
	}

	var onDisk *persistedState
	runner := &recordingRunner{failFn: func(dir string, args []string) error {
		if onDisk == nil && filepath.Base(dir) == "beta" && slices.Contains(args, "up") {
			loaded, err := loadPersistedDeployState(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			onDisk = loaded
		}
		return nil
	}}
	d := New(Config{Runner: runner, CommitReader: &fakeCommitReader{head: "head-sha"}, StateDir: stateDir})
	cfg := &config.Config{
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "alpha"}, {Name: "beta"}},
	}

	d.DeployAllStacks(context.Background(), cfg)

	if onDisk == nil {
		t.Fatal("beta's up never ran, so the mid-run state was never read")
	}
	if len(onDisk.hashesFor("alpha")) == 0 {
		t.Error("alpha's hashes must be on disk before beta deploys")
	}
	if got := onDisk.baseCommitFor("alpha"); got != "head-sha" {
		t.Errorf("alpha's base on disk = %q, want head-sha", got)
	}
	if len(onDisk.hashesFor("beta")) != 0 {
		t.Error("beta must not be recorded before it deployed")
	}
}

// Checkpoints write only when the state changed, so an unchanged reconcile
// tick does not rewrite state.yaml once per stack.
func TestStateCheckpoint_WritesOnlyOnChange(t *testing.T) {
	dir := t.TempDir()
	state := newEmptyState()
	state.Stacks["web"] = stackFileHashes{"f": "h"}
	cp := newStateCheckpoint(dir, state)

	if err := cp.save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); !os.IsNotExist(err) {
		t.Fatalf("an unchanged state must not be written, stat err = %v", err)
	}

	state.Stacks["api"] = stackFileHashes{"f": "h2"}
	if err := cp.save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPersistedDeployState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.hashesFor("api")) == 0 {
		t.Error("a changed state must be written")
	}
}

// A bootstrap run is not checkpointed: a kill midway must leave the state
// empty, so the resumed run is a bootstrap run too and pulls nothing (ADR-0051).
func TestDeployAllStacks_BootstrapRunSavesOnlyAtTheEnd(t *testing.T) {
	baseDir := t.TempDir()
	stateDir := t.TempDir()
	makeStacks(t, baseDir, "alpha", "beta")

	betaReached := false
	runner := &recordingRunner{failFn: func(dir string, args []string) error {
		if !betaReached && filepath.Base(dir) == "beta" && slices.Contains(args, "up") {
			betaReached = true
			if _, err := os.Stat(filepath.Join(stateDir, stateFileName)); !os.IsNotExist(err) {
				t.Errorf("a bootstrap run must not save before it ends, stat err = %v", err)
			}
		}
		return nil
	}}
	d := New(Config{Runner: runner, CommitReader: &fakeCommitReader{head: "head-sha"}, StateDir: stateDir})
	cfg := &config.Config{
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "alpha"}, {Name: "beta"}},
	}

	d.DeployAllStacks(context.Background(), cfg)

	if !betaReached {
		t.Fatal("beta's up never ran, so the mid-run check never happened")
	}
	if _, err := os.Stat(filepath.Join(stateDir, stateFileName)); err != nil {
		t.Errorf("the bootstrap run must still save at its end: %v", err)
	}
}

func stateSaveErrors(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	if err := metrics.StateSaveErrors.Write(&m); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

func TestSaveDeployState_CountsAFailedWrite(t *testing.T) {
	// A file where the state dir should be makes every write fail.
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(stateDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := stateSaveErrors(t)

	if err := saveDeployState(stateDir, newEmptyState()); err == nil {
		t.Fatal("expected the save to fail")
	}
	state := newEmptyState()
	cp := newStateCheckpoint(stateDir, state)
	state.Stacks["web"] = stackFileHashes{"f": "h"}
	if err := cp.save(state); err == nil {
		t.Fatal("expected the checkpoint to fail")
	}

	if got := stateSaveErrors(t) - before; got != 2 {
		t.Errorf("state save errors counted = %v, want 2", got)
	}
}

func TestSaveDeployState_DoesNotCountASuccessfulWrite(t *testing.T) {
	before := stateSaveErrors(t)
	if err := saveDeployState(t.TempDir(), newEmptyState()); err != nil {
		t.Fatal(err)
	}
	if got := stateSaveErrors(t) - before; got != 0 {
		t.Errorf("state save errors counted = %v, want 0", got)
	}
}
