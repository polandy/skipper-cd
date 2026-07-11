package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

func off() *bool { b := false; return &b }

// pausedDeployer returns a Deployer whose autosync is paused for every stack
// (global off), plus its recording runner, queue, and captured events.
func pausedDeployer(t *testing.T) (*Deployer, *recordingRunner, *autosync.Queue, *[]events.DeployEvent) {
	t.Helper()
	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	q := autosync.NewQueue()
	d.SetAutosync(autosync.NewController(off(), nil), q)

	emitted := &[]events.DeployEvent{}
	d.SetEventSink(func(e events.DeployEvent) { *emitted = append(*emitted, e) })
	return d, runner, q, emitted
}

func TestDeployStack_DefersWhenAutosyncPaused(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	d, runner, q, emitted := pausedDeployer(t)
	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A single queued event, no docker/git commands.
	if len(*emitted) != 1 || (*emitted)[0].Status != events.StatusQueued {
		t.Fatalf("expected one queued event, got %+v", *emitted)
	}
	if len((*emitted)[0].ChangedFiles) == 0 {
		t.Error("queued event should carry the changed files")
	}
	assertCommandNotCalled(t, runner.calls, "up")
	assertCommandNotCalled(t, runner.calls, "pull")

	// Hashes must NOT be recorded — the stack stays dirty.
	if len(state.hashesFor("gitea")) != 0 {
		t.Error("paused stack must not have its hashes recorded")
	}

	// The queue records it.
	if q.Count() != 1 {
		t.Fatalf("queue count = %d, want 1", q.Count())
	}
	items := q.Snapshot([]string{"gitea"})
	if items[0].Stack != "gitea" || items[0].Reason != "global" {
		t.Errorf("unexpected queue item %+v", items[0])
	}
}

func TestDeployStack_DeploysAndClearsQueueWhenEnabled(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	q := autosync.NewQueue()
	q.Mark("gitea", []string{"docker-compose.yml"}, "stack") // was queued earlier
	d.SetAutosync(autosync.NewController(nil, nil), q)       // autosync on (default)

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandCalled(t, runner.calls, "up")
	if len(state.hashesFor("gitea")) == 0 {
		t.Error("deployed stack should have its hashes recorded")
	}
	if q.Count() != 0 {
		t.Errorf("queue should be cleared after deploy, count = %d", q.Count())
	}
}

func TestDeployStack_ClearsQueueEvenWhenDeployFails(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{errOnCommand: "pull"} // deploy will fail
	d := newDeployerWithRunner(runner)
	q := autosync.NewQueue()
	q.Mark("gitea", []string{"docker-compose.yml"}, "stack")
	d.SetAutosync(autosync.NewController(nil, nil), q) // autosync on

	err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "gitea"}, baseDir, "", nil, newEmptyState())
	if err == nil {
		t.Fatal("expected the deploy to fail")
	}
	// Once autosync is effective the stack is no longer an autosync deferral,
	// even if the deploy itself fails.
	if q.Count() != 0 {
		t.Errorf("queue should be cleared when the stack is deployed, count = %d", q.Count())
	}
}

func TestDeployStack_UnchangedClearsQueue(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	q := autosync.NewQueue()
	q.Mark("gitea", []string{"docker-compose.yml"}, "global")
	d.SetAutosync(autosync.NewController(off(), nil), q) // paused, but no changes

	// Pre-seed state so the stack is unchanged.
	hashes, err := computePerFileHashes(stackDir, nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	state := &persistedState{
		Stacks: map[string]stackFileHashes{"gitea": hashes},
		Images: map[string]serviceImageByName{},
	}

	if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "gitea"}, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if q.Count() != 0 {
		t.Errorf("unchanged stack should clear the queue, count = %d", q.Count())
	}
}
