package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	q := autosync.NewQueue()
	emitted := &[]events.DeployEvent{}
	d := New(Config{
		Runner:    runner,
		Autosync:  autosync.NewController(off(), nil),
		Queue:     q,
		EventSink: func(e events.DeployEvent) { *emitted = append(*emitted, e) },
	})
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

// A deferred deploy must still carry the diff of what is waiting, so the paused
// row in the UI can show the effective change rather than only the file paths.
func TestDeployStack_QueuedEventIncludesDiffs(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	cr := &fakeCommitReader{
		diffs: map[string]string{
			filepath.Join(stackDir, "docker-compose.yml"): "+image: nginx:1.25\n",
		},
	}
	runner := &recordingRunner{}
	var queuedEvt *events.DeployEvent
	d := New(Config{
		Runner:       runner,
		CommitReader: cr,
		RepoDir:      baseDir,
		StateDir:     t.TempDir(),
		Autosync:     autosync.NewController(off(), nil), // paused globally
		Queue:        autosync.NewQueue(),
		EventSink: func(e events.DeployEvent) {
			if e.Status == events.StatusQueued {
				queuedEvt = &e
			}
		},
	})

	stack := config.Stack{Name: "gitea"}
	state := &persistedState{
		Stacks:             map[string]stackFileHashes{"gitea": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if queuedEvt == nil {
		t.Fatal("expected a queued event")
	}
	if queuedEvt.Diffs == nil {
		t.Fatal("expected diffs in the queued event, got nil")
	}
	if !strings.Contains(queuedEvt.Diffs["gitea/docker-compose.yml"], "nginx:1.25") {
		t.Errorf("expected diff content in the queued event, got %+v", queuedEvt.Diffs)
	}
	// It was deferred, not deployed.
	assertCommandNotCalled(t, runner.calls, "up")
}

func TestDeployStack_DeploysAndClearsQueueWhenEnabled(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	q := autosync.NewQueue()
	q.Mark("gitea", []string{"docker-compose.yml"}, "stack") // was queued earlier
	// Autosync on (default).
	d := New(Config{Runner: runner, Autosync: autosync.NewController(nil, nil), Queue: q})

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
	q := autosync.NewQueue()
	q.Mark("gitea", []string{"docker-compose.yml"}, "stack")
	// Autosync on.
	d := New(Config{Runner: runner, Autosync: autosync.NewController(nil, nil), Queue: q})

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
	q := autosync.NewQueue()
	q.Mark("gitea", []string{"docker-compose.yml"}, "global")
	// Paused, but no changes.
	d := New(Config{Runner: runner, Autosync: autosync.NewController(off(), nil), Queue: q})

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

// deployAllStacksCommitBaseDeployer builds a Deployer wired for a full run with
// a commit reader (HeadCommitSHA → "abc123") and a persisted state seeded to
// last_deployed_commit=old-sha. The stack has one changed compose file.
func deployAllStacksCommitBaseDeployer(t *testing.T, paused bool) (*Deployer, *config.Config, string) {
	t.Helper()
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	stateDir := t.TempDir()
	seed := newEmptyState()
	seed.LastDeployedCommit = "old-sha"
	if err := saveDeployState(stateDir, seed); err != nil {
		t.Fatal(err)
	}

	var global *bool
	if paused {
		global = off()
	}
	d := New(Config{
		Runner:       &recordingRunner{},
		CommitReader: &fakeCommitReader{},
		RepoDir:      baseDir,
		StateDir:     stateDir,
		Autosync:     autosync.NewController(global, nil),
		Queue:        autosync.NewQueue(),
	})

	cfg := &config.Config{StacksBaseDir: baseDir, Stacks: []config.Stack{{Name: "gitea"}}}
	return d, cfg, stateDir
}

// A change deferred by paused autosync must NOT advance last_deployed_commit:
// the change has not deployed yet, so the commit base has to stay put or the
// eventual deploy diffs HEAD..HEAD and shows no diff (the reconciled _nixos row
// that only listed flake.lock with no diff, seen in prod).
func TestDeployAllStacks_KeepsCommitBaseWhileQueued(t *testing.T) {
	d, cfg, stateDir := deployAllStacksCommitBaseDeployer(t, true)

	d.DeployAllStacks(context.Background(), cfg)

	after, err := loadPersistedDeployState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if after.LastDeployedCommit != "old-sha" {
		t.Errorf("last_deployed_commit advanced to %q while a change was queued; want it kept at old-sha", after.LastDeployedCommit)
	}
}

// The counterpart: when the run actually deploys everything (nothing queued),
// last_deployed_commit advances to HEAD so future deploys diff against it.
func TestDeployAllStacks_AdvancesCommitBaseWhenDeployed(t *testing.T) {
	d, cfg, stateDir := deployAllStacksCommitBaseDeployer(t, false)

	d.DeployAllStacks(context.Background(), cfg)

	after, err := loadPersistedDeployState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if after.LastDeployedCommit != "abc123" {
		t.Errorf("last_deployed_commit = %q, want it advanced to HEAD abc123 after a clean deploy", after.LastDeployedCommit)
	}
}
