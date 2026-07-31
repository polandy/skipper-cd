package deploy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	var emitted []events.DeployEvent
	d := New(Config{Runner: runner, EventSink: func(e events.DeployEvent) {
		emitted = append(emitted, e)
	}})

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
	var emitted []events.DeployEvent
	d := New(Config{Runner: runner, EventSink: func(e events.DeployEvent) {
		emitted = append(emitted, e)
	}})

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

	// `up`, not `pull`: this is a first run over an empty state, and a
	// bootstrap run deliberately skips the pull (ADR-0051). The test is about
	// a failing docker command producing a failed event, whichever it is.
	runner := &recordingRunner{errOnCommand: "up"}
	var emitted []events.DeployEvent
	d := New(Config{Runner: runner, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
		emitted = append(emitted, e)
	}})

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
	var ids []int64
	d := New(Config{Runner: runner, StartEventID: 100, EventSink: func(e events.DeployEvent) {
		ids = append(ids, e.ID)
	}})

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
	var deploying *events.DeployEvent
	d := New(Config{Runner: runner, EventSink: func(e events.DeployEvent) {
		if e.Status == events.StatusDeploying {
			deploying = &e
		}
	}})

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
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: "/repo", StateDir: t.TempDir()})

	diffs := d.collectDiffs(context.Background(), []string{"/repo/docker-compose.yml"}, "old-sha")

	if diffs == nil {
		t.Fatal("expected diffs to be returned")
	}
	if diffs["/repo/docker-compose.yml"] != "+new line\n-old line\n" {
		t.Errorf("unexpected diff: %q", diffs["/repo/docker-compose.yml"])
	}
}

func TestCollectDiffs_NilWithoutCommitReader(t *testing.T) {
	d := New(Config{Runner: &recordingRunner{}, StateDir: t.TempDir()})
	diffs := d.collectDiffs(context.Background(), []string{"file.yml"}, "old-sha")
	if diffs != nil {
		t.Error("expected nil diffs without commit reader")
	}
}

func TestCollectDiffs_NilWithEmptyCommit(t *testing.T) {
	cr := &fakeCommitReader{diffs: map[string]string{}}
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, StateDir: t.TempDir()})
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
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: "/repo", StateDir: t.TempDir()})

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
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: "/repo", StateDir: t.TempDir()})

	diffs := d.collectDiffs(context.Background(), []string{"/other/file.yml"}, "old-sha")
	if diffs != nil {
		t.Error("expected nil for files outside repo")
	}
}

// --- collectCommits tests ---

func TestCollectCommits_ReturnsCommits(t *testing.T) {
	cr := &fakeCommitReader{
		commits: map[string][]events.CommitInfo{
			"/repo/docker-compose.yml": {{SHA: "def456", Subject: "feat: bump", Author: "Jane Doe"}},
		},
	}
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: "/repo", StateDir: t.TempDir()})

	commits := d.collectCommits(context.Background(), []string{"/repo/docker-compose.yml"}, "old-sha")

	if len(commits) != 1 || commits[0].SHA != "def456" {
		t.Fatalf("expected the def456 commit, got %+v", commits)
	}
}

func TestCollectCommits_NilWithoutCommitReader(t *testing.T) {
	d := New(Config{Runner: &recordingRunner{}, StateDir: t.TempDir()})
	if commits := d.collectCommits(context.Background(), []string{"file.yml"}, "old-sha"); commits != nil {
		t.Error("expected nil commits without commit reader")
	}
}

func TestCollectCommits_NilWithEmptyCommit(t *testing.T) {
	cr := &fakeCommitReader{commits: map[string][]events.CommitInfo{}}
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, StateDir: t.TempDir()})
	if commits := d.collectCommits(context.Background(), []string{"file.yml"}, ""); commits != nil {
		t.Error("expected nil commits with empty last deployed commit")
	}
}

func TestCollectCommits_SkipsFilesOutsideRepo(t *testing.T) {
	cr := &fakeCommitReader{
		commits: map[string][]events.CommitInfo{
			"/other/file.yml": {{SHA: "def456", Subject: "feat: bump"}},
		},
	}
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: "/repo", StateDir: t.TempDir()})
	if commits := d.collectCommits(context.Background(), []string{"/other/file.yml"}, "old-sha"); commits != nil {
		t.Error("expected nil for files outside repo")
	}
}

func TestCollectCommits_NilOnReaderError(t *testing.T) {
	cr := &fakeCommitReader{commitErr: context.DeadlineExceeded}
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: "/repo", StateDir: t.TempDir()})
	if commits := d.collectCommits(context.Background(), []string{"/repo/file.yml"}, "old-sha"); commits != nil {
		t.Error("expected nil commits when the reader errors (deploy must not fail over metadata)")
	}
}

func TestDeployStack_SuccessEventIncludesDiffs(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	cr := &fakeCommitReader{
		diffs: map[string]string{
			composePath: "+image: nginx:1.25\n",
		},
		commits: map[string][]events.CommitInfo{
			composePath: {{SHA: "def456", Subject: "feat: bump gitea image", Author: "Jane Doe"}},
		},
	}
	runner := &recordingRunner{}
	var successEvt *events.DeployEvent
	d := New(Config{Runner: runner, CommitReader: cr, RepoDir: baseDir, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
		if e.Status == events.StatusSuccess {
			successEvt = &e
		}
	}})

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
	if !strings.Contains(successEvt.Diffs["gitea/docker-compose.yml"], "nginx:1.25") {
		t.Error("expected diff content in success event")
	}
	if len(successEvt.Commits) != 1 || successEvt.Commits[0].Subject != "feat: bump gitea image" {
		t.Errorf("expected commit metadata in success event, got %+v", successEvt.Commits)
	}
}

func TestDeployStack_SuccessEventNamesChangedServiceVersions(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.27"))

	runner := &recordingRunner{}
	var successEvt *events.DeployEvent
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
		if e.Status == events.StatusSuccess {
			successEvt = &e
		}
	}})

	stack := config.Stack{Name: "gitea"}
	state := &persistedState{
		Stacks: map[string]stackFileHashes{"gitea": {"old": "oldhash"}},
		// Previously deployed image differs, so the deploy reports old → new.
		Images: map[string]serviceImageByName{"gitea": {"app": "nginx:1.25"}},
	}

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if successEvt == nil {
		t.Fatal("expected success event")
	}
	want := []events.ServiceImageChange{{Service: "app", Old: "nginx:1.25", New: "nginx:1.27"}}
	if !reflect.DeepEqual(successEvt.ImageChanges, want) {
		t.Errorf("success event ImageChanges = %+v, want %+v", successEvt.ImageChanges, want)
	}
}

// outputRunningImage answers the two reads runningImages makes for a
// single-service stack (service `app`, container `stack-app-1`): `ps` carries
// the reference the container runs, `images` the image id behind it — compose
// reports no service on the latter, which is why they have to be joined.
func outputRunningImage(ref, id string) func(int, []string) ([]byte, error) {
	const container = "stack-app-1"
	return func(_ int, args []string) ([]byte, error) {
		switch {
		case slices.Contains(args, "ps"):
			return fmt.Appendf(nil, `[{"Service":"app","Name":%q,"Image":%q}]`, container, ref), nil
		case slices.Contains(args, "images"):
			return fmt.Appendf(nil, `[{"ContainerName":%q,"ID":%q}]`, container, id), nil
		}
		return nil, nil
	}
}

// A floating tag keeps its compose reference across a re-pull, so the compose
// file alone reports no version change on exactly the deploys that move it.
// The running images do see it.
func TestDeployStack_SuccessEventReportsMovedFloatingTag(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "cloud")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nextcloud:34-ghostscript"))

	runner := &recordingRunner{outputFn: outputRunningImage("nextcloud:34-ghostscript", "sha256:9f8e7d6c5b4a0011")}
	var successEvt *events.DeployEvent
	d := New(Config{Runner: runner, Outputter: runner, RepoDir: baseDir, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
		if e.Status == events.StatusSuccess {
			successEvt = &e
		}
	}})

	state := &persistedState{
		Stacks: map[string]stackFileHashes{"cloud": {"old": "oldhash"}},
		// The compose reference is unchanged — only the image behind the tag moved.
		Images:        map[string]serviceImageByName{"cloud": {"app": "nextcloud:34-ghostscript"}},
		RunningImages: map[string]serviceImageByName{"cloud": {"app": "nextcloud:34-ghostscript@a1b2c3d4e5f6"}},
	}

	if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "cloud"}, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if successEvt == nil {
		t.Fatal("expected success event")
	}
	want := []events.ServiceImageChange{
		{Service: "app", Old: "nextcloud:34-ghostscript@a1b2c3d4e5f6", New: "nextcloud:34-ghostscript@9f8e7d6c5b4a"},
	}
	if !reflect.DeepEqual(successEvt.ImageChanges, want) {
		t.Errorf("success event ImageChanges = %+v, want %+v", successEvt.ImageChanges, want)
	}
	// The next deploy measures against what this one left running.
	if got := state.runningImagesFor("cloud")["app"]; got != "nextcloud:34-ghostscript@9f8e7d6c5b4a" {
		t.Errorf("recorded running image = %q, want the version just deployed", got)
	}
}

func TestDeployStack_WithoutRunningBaselineKeepsComposeReferenceDelta(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.27"))

	runner := &recordingRunner{outputFn: outputRunningImage("nginx:1.27", "sha256:9f8e7d6c5b4a0011")}
	var successEvt *events.DeployEvent
	d := New(Config{Runner: runner, Outputter: runner, RepoDir: baseDir, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
		if e.Status == events.StatusSuccess {
			successEvt = &e
		}
	}})

	// No running_images recorded yet — the first deploy after an upgrade. Every
	// service would otherwise read as newly added.
	state := &persistedState{
		Stacks: map[string]stackFileHashes{"gitea": {"old": "oldhash"}},
		Images: map[string]serviceImageByName{"gitea": {"app": "nginx:1.25"}},
	}

	if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "gitea"}, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if successEvt == nil {
		t.Fatal("expected success event")
	}
	want := []events.ServiceImageChange{{Service: "app", Old: "nginx:1.25", New: "nginx:1.27"}}
	if !reflect.DeepEqual(successEvt.ImageChanges, want) {
		t.Errorf("success event ImageChanges = %+v, want the compose-reference delta %+v", successEvt.ImageChanges, want)
	}
	// This deploy's read becomes the next one's baseline.
	if got := state.runningImagesFor("gitea")["app"]; got != "nginx:1.27@9f8e7d6c5b4a" {
		t.Errorf("recorded running image = %q, want this deploy's read", got)
	}
}

func TestDeployStack_FailedRunningImageReadKeepsPreviousBaseline(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.27"))

	runner := &recordingRunner{outputFn: func(_ int, _ []string) ([]byte, error) { return nil, errFakeOutput }}
	d := New(Config{Runner: runner, Outputter: runner, RepoDir: baseDir, StateDir: t.TempDir()})

	state := &persistedState{
		Stacks:        map[string]stackFileHashes{"gitea": {"old": "oldhash"}},
		Images:        map[string]serviceImageByName{"gitea": {"app": "nginx:1.25"}},
		RunningImages: map[string]serviceImageByName{"gitea": {"app": "nginx:1.25@a1b2c3d4e5f6"}},
	}

	if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "gitea"}, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Clearing it would claim the stack runs nothing and cost the next deploy its
	// version delta; the stale baseline is corrected by the next successful read.
	if got := state.runningImagesFor("gitea")["app"]; got != "nginx:1.25@a1b2c3d4e5f6" {
		t.Errorf("recorded running image = %q, want the previous baseline kept", got)
	}
}

func TestDeployStack_SuccessEventMarksTheHealthGate(t *testing.T) {
	tests := []struct {
		name    string
		compose string
		want    bool
	}{
		// The gate is inferred from a compose healthcheck (ADR-0046).
		{name: "gated", compose: composeWithHealthcheck("nginx:1.27"), want: true},
		{name: "ungated", compose: composeWithImage("nginx:1.27"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := t.TempDir()
			stackDir := filepath.Join(baseDir, "gitea")
			if err := os.MkdirAll(stackDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), tc.compose)

			var successEvt *events.DeployEvent
			d := New(Config{Runner: &recordingRunner{}, RepoDir: baseDir, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
				if e.Status == events.StatusSuccess {
					successEvt = &e
				}
			}})

			state := &persistedState{
				Stacks: map[string]stackFileHashes{"gitea": {"old": "oldhash"}},
				Images: map[string]serviceImageByName{"gitea": {"app": "nginx:1.25"}},
			}
			if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "gitea"}, baseDir, "", nil, state); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if successEvt == nil {
				t.Fatal("expected success event")
			}
			if successEvt.HealthGated != tc.want {
				t.Errorf("success event HealthGated = %v, want %v", successEvt.HealthGated, tc.want)
			}
		})
	}
}

func TestDeployStack_UnparseableComposeReportsNoImageRemovals(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Invalid YAML: the compose file fails to parse, so the deploy degrades
	// gracefully (pull all, no build tracking). currentImages is nil here — the
	// image diff must be skipped rather than reporting every prior service as
	// removed, which would produce a misleading "svc: old (removed)" notification.
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), "services: [this is not valid compose")

	runner := &recordingRunner{}
	var terminalEvt *events.DeployEvent
	d := New(Config{Runner: runner, RepoDir: baseDir, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
		if e.Status == events.StatusSuccess || e.Status == events.StatusFailed {
			terminalEvt = &e
		}
	}})

	stack := config.Stack{Name: "gitea"}
	state := &persistedState{
		Stacks: map[string]stackFileHashes{"gitea": {"old": "oldhash"}},
		// A previously deployed image exists; the naive diff would report it removed.
		Images: map[string]serviceImageByName{"gitea": {"app": "nginx:1.25"}},
	}

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if terminalEvt == nil {
		t.Fatal("expected a terminal event")
	}
	if len(terminalEvt.ImageChanges) != 0 {
		t.Errorf("expected no image changes when compose failed to parse, got %+v", terminalEvt.ImageChanges)
	}
}

func TestRebuildNixOS_SuccessEventIncludesDiffs(t *testing.T) {
	baseDir := t.TempDir()
	nixFile := filepath.Join(baseDir, "configuration.nix")
	writeFile(t, nixFile, "{ services.foo.enable = true; }")

	cr := &fakeCommitReader{
		diffs: map[string]string{
			nixFile: "+  services.foo.enable = true;\n",
		},
	}
	var successEvt *events.DeployEvent
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: baseDir, StateDir: t.TempDir(), EventSink: func(e events.DeployEvent) {
		if e.Stack == NixosStateKey && e.Status == events.StatusSuccess {
			successEvt = &e
		}
	}})

	state := newEmptyState()
	state.LastDeployedCommit = "old-sha"

	enabled := true
	cfg := &config.Config{NixOSRebuild: &config.NixOSRebuild{Enabled: &enabled, Flake: ".#host-a"}}

	if ok := d.rebuildNixOSIfChanged(context.Background(), cfg, state); !ok {
		t.Fatal("expected nixos-rebuild to succeed")
	}
	if successEvt == nil {
		t.Fatal("expected a _nixos success event")
	}
	if successEvt.Diffs == nil {
		t.Fatal("expected diffs in the _nixos success event, got nil")
	}
	if !strings.Contains(successEvt.Diffs["configuration.nix"], "services.foo.enable") {
		t.Errorf("expected nix diff content in the _nixos success event, got %q", successEvt.Diffs["configuration.nix"])
	}
}

func TestDeployStack_LogsEventIDForDiffLookupOnSuccess(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	runner := &recordingRunner{}
	var successID int64
	d := New(Config{Runner: runner, EventSink: func(e events.DeployEvent) {
		if e.Status == events.StatusSuccess {
			successID = e.ID
		}
	}})

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if successID == 0 {
		t.Fatal("expected a success event with an ID")
	}
	want := fmt.Sprintf("event_id=%d", successID)
	found := false
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "deploy complete") && strings.Contains(line, want) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'deploy complete' log line with %s, got:\n%s", want, logBuf.String())
	}
}

func TestDeployStack_LogsNoEventIDWithoutSink(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	d := newDeployerWithRunner(&recordingRunner{})

	if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "gitea"}, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(logBuf.String(), "event_id=") {
		t.Errorf("expected no event_id attr without an event sink, got:\n%s", logBuf.String())
	}
}
