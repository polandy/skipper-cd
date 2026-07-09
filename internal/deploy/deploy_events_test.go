package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// --- event sink tests ---

func TestDeployStack_EmitsDeployingAndSuccessEvents(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	var emitted []events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) {
		emitted = append(emitted, e)
	})

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(emitted) != 2 {
		t.Fatalf("expected 2 events (deploying + success), got %d", len(emitted))
	}
	if emitted[0].Status != events.StatusDeploying {
		t.Errorf("expected first event to be deploying, got %s", emitted[0].Status)
	}
	if emitted[0].Stack != "gitea" {
		t.Errorf("expected stack 'gitea', got %q", emitted[0].Stack)
	}
	if emitted[1].Status != events.StatusSuccess {
		t.Errorf("expected second event to be success, got %s", emitted[1].Status)
	}
	if emitted[1].DurationMs < 0 {
		t.Error("expected non-negative duration for success event")
	}
}

func TestDeployStack_EmitsSkippedEvent(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	var emitted []events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) {
		emitted = append(emitted, e)
	})

	stack := config.Stack{Name: "gitea"}

	hashes, err := computePerFileHashes(stackDir, nil, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error computing hashes: %v", err)
	}
	state := &persistedState{
		Stacks: map[string]stackFileHashes{"gitea": hashes},
		Images: map[string]serviceImageByName{},
	}

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(emitted) != 1 {
		t.Fatalf("expected 1 skipped event, got %d", len(emitted))
	}
	if emitted[0].Status != events.StatusSkipped {
		t.Errorf("expected skipped, got %s", emitted[0].Status)
	}
}

func TestDeployAllStacks_EmitsFailedEventOnError(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{errOnCommand: "pull"}
	d := &Deployer{runner: runner, stateDir: t.TempDir()}

	var emitted []events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) {
		emitted = append(emitted, e)
	})

	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "gitea"}},
	}

	d.DeployAllStacks(context.Background(), cfg)

	// Expect: deploying, then failed (from DeployAllStacks error handler).
	var failed *events.DeployEvent
	for i := range emitted {
		if emitted[i].Status == events.StatusFailed {
			failed = &emitted[i]
			break
		}
	}
	if failed == nil {
		t.Fatal("expected a failed event")
	}
	if failed.Error == "" {
		t.Error("expected error message in failed event")
	}
}

func TestDeployStack_NoEventsWithoutSink(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	// No SetEventSink called — should not panic.

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployStack_EventIDsAreMonotonic(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	d.InitEventID(100)

	var ids []int64
	d.SetEventSink(func(e events.DeployEvent) {
		ids = append(ids, e.ID)
	})

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ids) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(ids))
	}
	if ids[0] != 101 {
		t.Errorf("expected first ID 101, got %d", ids[0])
	}
	if ids[1] != 102 {
		t.Errorf("expected second ID 102, got %d", ids[1])
	}
}

func TestDeployStack_DeployingEventIncludesChangedFiles(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	var deploying *events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) {
		if e.Status == events.StatusDeploying {
			deploying = &e
		}
	})

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deploying == nil {
		t.Fatal("expected a deploying event")
	}
	if len(deploying.ChangedFiles) == 0 {
		t.Error("expected changed files in deploying event")
	}
}

// --- collectDiffs tests ---

func TestCollectDiffs_ReturnsDiffs(t *testing.T) {
	cr := &fakeCommitReader{
		diffs: map[string]string{
			"/repo/docker-compose.yml": "+new line\n-old line\n",
		},
	}
	d := &Deployer{runner: &recordingRunner{}, commitReader: cr, repoDir: "/repo", stateDir: t.TempDir()}

	diffs := d.collectDiffs(context.Background(), []string{"/repo/docker-compose.yml"}, "old-sha")

	if diffs == nil {
		t.Fatal("expected diffs to be returned")
	}
	if diffs["/repo/docker-compose.yml"] != "+new line\n-old line\n" {
		t.Errorf("unexpected diff: %q", diffs["/repo/docker-compose.yml"])
	}
}

func TestCollectDiffs_NilWithoutCommitReader(t *testing.T) {
	d := &Deployer{runner: &recordingRunner{}, stateDir: t.TempDir()}
	diffs := d.collectDiffs(context.Background(), []string{"file.yml"}, "old-sha")
	if diffs != nil {
		t.Error("expected nil diffs without commit reader")
	}
}

func TestCollectDiffs_NilWithEmptyCommit(t *testing.T) {
	cr := &fakeCommitReader{diffs: map[string]string{}}
	d := &Deployer{runner: &recordingRunner{}, commitReader: cr, stateDir: t.TempDir()}
	diffs := d.collectDiffs(context.Background(), []string{"file.yml"}, "")
	if diffs != nil {
		t.Error("expected nil diffs with empty last deployed commit")
	}
}

func TestCollectDiffs_TruncatesLargeDiff(t *testing.T) {
	largeDiff := strings.Repeat("x", 12*1024)
	cr := &fakeCommitReader{
		diffs: map[string]string{"/repo/big.yml": largeDiff},
	}
	d := &Deployer{runner: &recordingRunner{}, commitReader: cr, repoDir: "/repo", stateDir: t.TempDir()}

	diffs := d.collectDiffs(context.Background(), []string{"/repo/big.yml"}, "old-sha")

	if diffs == nil {
		t.Fatal("expected diffs")
	}
	if len(diffs["/repo/big.yml"]) > maxDiffPerFile+20 { // +20 for truncation message
		t.Errorf("diff should be truncated, got %d bytes", len(diffs["/repo/big.yml"]))
	}
	if !strings.Contains(diffs["/repo/big.yml"], "truncated") {
		t.Error("truncated diff should contain truncation marker")
	}
}

func TestCollectDiffs_SkipsFilesOutsideRepo(t *testing.T) {
	cr := &fakeCommitReader{
		diffs: map[string]string{"/other/file.yml": "+diff"},
	}
	d := &Deployer{runner: &recordingRunner{}, commitReader: cr, repoDir: "/repo", stateDir: t.TempDir()}

	diffs := d.collectDiffs(context.Background(), []string{"/other/file.yml"}, "old-sha")
	if diffs != nil {
		t.Error("expected nil for files outside repo")
	}
}

func TestDeployStack_SuccessEventIncludesDiffs(t *testing.T) {
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
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	var successEvt *events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) {
		if e.Status == events.StatusSuccess {
			successEvt = &e
		}
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

	if successEvt == nil {
		t.Fatal("expected success event")
	}
	if successEvt.Diffs == nil {
		t.Fatal("expected diffs in success event")
	}
	if !strings.Contains(successEvt.Diffs[filepath.Join(stackDir, "docker-compose.yml")], "nginx:1.25") {
		t.Error("expected diff content in success event")
	}
}
