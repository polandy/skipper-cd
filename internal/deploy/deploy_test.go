package deploy

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// The UI has no notion of the repo clone dir, so events must carry repo-relative
// paths, not the absolute filesystem paths the hashing/diff layers use
// internally. Both changed_files and the diff-map keys are shortened; paths
// outside the repo are left untouched.
func TestEmit_ShortensPathsToRepoRelative(t *testing.T) {
	repoDir := "/var/lib/skipper/repo"
	inside := repoDir + "/system/host-b/default.nix"
	outside := "/etc/somewhere/else.nix"

	var got events.DeployEvent
	d := New(Config{
		RepoDir:   repoDir,
		StateDir:  t.TempDir(),
		EventSink: func(e events.DeployEvent) { got = e },
	})

	d.emit(events.StatusSuccess, NixosStateKey, 0, "", changeSet{
		files: []string{inside, outside},
		diffs: map[string]string{inside: "diff-a", outside: "diff-b"},
	})

	wantFiles := []string{"system/host-b/default.nix", outside}
	if !slices.Equal(got.ChangedFiles, wantFiles) {
		t.Errorf("changed_files = %v, want %v", got.ChangedFiles, wantFiles)
	}
	if _, ok := got.Diffs["system/host-b/default.nix"]; !ok {
		t.Errorf("diff key not shortened to repo-relative: %v", got.Diffs)
	}
	if _, ok := got.Diffs[outside]; !ok {
		t.Errorf("out-of-repo diff key must be left unchanged: %v", got.Diffs)
	}
}

func TestDeployStack_DeploysWhenHashChanges(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandCalled(t, runner.calls, "pull")
	assertCommandCalled(t, runner.calls, "up")

	if state.Stacks["gitea"] == nil {
		t.Error("expected state to be updated after deploy")
	}
}

func TestDeployStack_SkipsWhenUnchanged(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea"}

	// Pre-populate state with the current hashes to simulate "already deployed".
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

	if len(runner.calls) != 0 {
		t.Errorf("expected no commands to be run, got %d call(s)", len(runner.calls))
	}
}

// An unchanged stack must stay off the log at the default level: with the
// reconcile loop running every few minutes, one line per stack per tick is
// what buries the lines that matter (and evicts them from the UI's ring).
// The information is not lost — the run summary counts the skips, and
// log_level=debug brings the per-stack narrative back.
func TestDeployStack_SkipIsLoggedAtDebugLevel(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	hashes, err := computePerFileHashes(stackDir, nil, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error computing hashes: %v", err)
	}

	skipUnchanged := func(t *testing.T) {
		t.Helper()
		state := &persistedState{
			Stacks: map[string]stackFileHashes{"gitea": hashes},
			Images: map[string]serviceImageByName{},
		}
		d := newDeployerWithRunner(&recordingRunner{})
		if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "gitea"}, baseDir, "", nil, state); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	atInfo := captureLogAt(t, slog.LevelInfo, skipUnchanged)
	if strings.Contains(atInfo, "skipping stack") {
		t.Errorf("expected no skip line at info level, got %q", atInfo)
	}

	atDebug := captureLogAt(t, slog.LevelDebug, skipUnchanged)
	if !strings.Contains(atDebug, "skipping stack, no changes detected") || !strings.Contains(atDebug, "gitea") {
		t.Errorf("expected the skip line at debug level, got %q", atDebug)
	}
}

func TestDeployStack_FailsOnPullError(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{errOnCommand: "pull"}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error when docker compose pull fails")
	}

	if state.Stacks["gitea"] != nil {
		t.Error("state should not be updated after a failed deploy")
	}
}

func TestDeployStack_UsesBaseDirWhenProjectDirectoryAbsent(t *testing.T) {
	baseDir := t.TempDir()
	workDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) == 0 {
		t.Fatal("expected commands to be run")
	}
	if runner.calls[0].dir != workDir {
		t.Errorf("expected working dir %s, got %s", workDir, runner.calls[0].dir)
	}
}

func TestDeployStack_ProjectDirectoryUsesProjectDirectoryFlag(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "nextcloud")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nextcloud:30"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	projectDir := "/etc/nixos/modules/nextcloud"
	stack := config.Stack{Name: "nextcloud", ProjectDirectory: projectDir}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All docker compose calls should use -f and --project-directory flags.
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	for _, c := range runner.calls {
		if c.name != "docker" || !slices.Contains(c.args, "compose") {
			continue
		}
		if !slices.Contains(c.args, "-f") {
			t.Errorf("expected -f flag in docker compose call: %v", c.args)
		}
		if !slices.Contains(c.args, composePath) {
			t.Errorf("expected compose path %s in args: %v", composePath, c.args)
		}
		if !slices.Contains(c.args, "--project-directory") {
			t.Errorf("expected --project-directory flag in docker compose call: %v", c.args)
		}
		if !slices.Contains(c.args, projectDir) {
			t.Errorf("expected project dir %s in args: %v", projectDir, c.args)
		}
		if c.dir != projectDir {
			t.Errorf("expected run dir %s, got %s", projectDir, c.dir)
		}
	}
}

func TestDeployStack_NoProjectDirectoryRunsFromRepoClone(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("gitea/gitea:1.21"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without project_directory, docker compose should run from the repo clone dir
	// without -f or --project-directory flags.
	for _, c := range runner.calls {
		if c.name != "docker" || !slices.Contains(c.args, "compose") {
			continue
		}
		if slices.Contains(c.args, "-f") {
			t.Errorf("unexpected -f flag without project_directory: %v", c.args)
		}
		if slices.Contains(c.args, "--project-directory") {
			t.Errorf("unexpected --project-directory flag without project_directory: %v", c.args)
		}
		if c.dir != stackDir {
			t.Errorf("expected run dir %s, got %s", stackDir, c.dir)
		}
	}
}

func TestDeployStack_SkipsPullWhenOnlyConfigChanges(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("redis:7.2"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "mystack"}

	// Simulate a previous deploy with the same image but different file hash.
	state := &persistedState{
		Stacks: map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images: map[string]serviceImageByName{"mystack": {"app": "redis:7.2"}},
	}

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandNotCalled(t, runner.calls, "pull")
	assertCommandCalled(t, runner.calls, "up")
}

func TestDeployStack_PullsWhenImageChanges(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("redis:7.4"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "mystack"}

	// Previous deploy had a different image version.
	state := &persistedState{
		Stacks: map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images: map[string]serviceImageByName{"mystack": {"app": "redis:7.2"}},
	}

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandCalled(t, runner.calls, "pull")
	assertCommandCalled(t, runner.calls, "up")
}

func TestDeployStack_PullsWhenNoStoredImages(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("redis:7.2"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "mystack"}

	// No previous image state — first deploy or upgrade from old state format.
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandCalled(t, runner.calls, "pull")
	assertCommandCalled(t, runner.calls, "up")

	// After deploy, images should be stored in state.
	if state.Images["mystack"] == nil || state.Images["mystack"]["app"] != "redis:7.2" {
		t.Errorf("expected images to be stored in state, got %v", state.Images["mystack"])
	}
}

func TestDeployStack_StoresImagesInStateAfterDeploy(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "db")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("postgres:16-alpine"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "db"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.Images["db"] == nil {
		t.Fatal("expected images to be stored in state")
	}
	if state.Images["db"]["app"] != "postgres:16-alpine" {
		t.Errorf("expected postgres:16-alpine, got %s", state.Images["db"]["app"])
	}
}

func TestDeployStack_RecordsComposeDirAsProjectDirWhenNoProjectDirectory(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "db")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("postgres:16-alpine"))

	d := newDeployerWithRunner(&recordingRunner{})
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "db"}, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no project_directory, the project dir is the compose file's own directory.
	if got := state.ProjectDirs["db"]; got != stackDir {
		t.Errorf("expected project dir %q, got %q", stackDir, got)
	}
}

func TestDeployStack_RecordsProjectDirectoryAsProjectDir(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "db")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("postgres:16-alpine"))

	d := newDeployerWithRunner(&recordingRunner{})
	state := newEmptyState()
	workingDir := t.TempDir()

	stack := config.Stack{Name: "db", ProjectDirectory: workingDir}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// project_directory (--project-directory) is the project's working_dir label.
	if got := state.ProjectDirs["db"]; got != workingDir {
		t.Errorf("expected project dir %q, got %q", workingDir, got)
	}
}

func TestDeployStack_StopsOnDemandContainersAfterDeploy(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "monica")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{
		Name:               "monica",
		OnDemandContainers: []string{"monica-app", "monica-db"},
	}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stopCall *runCall
	for i := range runner.calls {
		if runner.calls[i].name == "docker" && slices.Contains(runner.calls[i].args, "stop") {
			stopCall = &runner.calls[i]
			break
		}
	}
	if stopCall == nil {
		t.Fatal("expected docker stop to be called")
	}
	if !slices.Contains(stopCall.args, "monica-app") || !slices.Contains(stopCall.args, "monica-db") {
		t.Errorf("expected docker stop monica-app monica-db, got %v", stopCall.args)
	}
}

func TestDeployStack_SkipsStopWhenNoOnDemandContainers(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, c := range runner.calls {
		if c.name == "docker" && slices.Contains(c.args, "stop") {
			t.Errorf("expected docker stop NOT to be called, but it was: %v", c.args)
		}
	}
}

func TestDeployStack_RedeploysWhenVarsFileChanges(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	varsFile := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, varsFile, "DOMAIN=example.com\n")

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "mystack"}

	// First deploy to populate state.
	state := newEmptyState()
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, varsFile, nil, state); err != nil {
		t.Fatalf("unexpected error on first deploy: %v", err)
	}

	// Reset runner calls.
	runner.calls = nil

	// Modify the vars file.
	writeFile(t, varsFile, "DOMAIN=new.example.com\n")

	// Second deploy should trigger because vars_file changed.
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, varsFile, nil, state); err != nil {
		t.Fatalf("unexpected error on second deploy: %v", err)
	}

	assertCommandCalled(t, runner.calls, "up")
}

func TestDeployStack_SkipsWhenVarsFileUnchanged(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	varsFile := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, varsFile, "DOMAIN=example.com\n")

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "mystack"}

	// First deploy to populate state.
	state := newEmptyState()
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, varsFile, nil, state); err != nil {
		t.Fatalf("unexpected error on first deploy: %v", err)
	}

	// Reset runner calls.
	runner.calls = nil

	// Second deploy with unchanged vars_file should be skipped.
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, varsFile, nil, state); err != nil {
		t.Fatalf("unexpected error on second deploy: %v", err)
	}

	if len(runner.calls) != 0 {
		t.Errorf("expected no commands when vars_file is unchanged, got %d call(s)", len(runner.calls))
	}
}

func TestSyncAndDeployAll_SerializesParallelCalls(t *testing.T) {
	runner := &recordingRunner{delay: 10 * time.Millisecond}
	syncer := &fakeRepoSyncer{}

	d := New(Config{Runner: runner, Syncer: syncer, StateDir: t.TempDir()})

	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: t.TempDir(),
		Stacks:        []config.Stack{},
	}

	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			d.SyncAndDeployAll(ctx, cfg)
			done <- struct{}{}
		}()
	}

	for range 2 {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for goroutines to finish")
		}
	}

	// Both goroutines ran sync — they should not have overlapped.
	if syncer.called.Load() != 2 {
		t.Errorf("expected syncer to be called 2 times, got %d", syncer.called.Load())
	}
}

func TestTrySyncAndDeployAll_RunsWhenIdle(t *testing.T) {
	syncer := &fakeRepoSyncer{}
	d := New(Config{Runner: &recordingRunner{}, Syncer: syncer, StateDir: t.TempDir()})
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: t.TempDir(),
		Stacks:        []config.Stack{},
	}

	if ran := d.TrySyncAndDeployAll(context.Background(), cfg); !ran {
		t.Error("expected TrySyncAndDeployAll to run when no deploy is in progress")
	}
	if syncer.called.Load() != 1 {
		t.Errorf("expected sync to run once, got %d", syncer.called.Load())
	}
}

func TestTrySyncAndDeployAll_SkipsWhenDeployInProgress(t *testing.T) {
	syncer := &fakeRepoSyncer{}
	d := New(Config{Runner: &recordingRunner{}, Syncer: syncer, StateDir: t.TempDir()})
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: t.TempDir(),
		Stacks:        []config.Stack{},
	}

	// Simulate an in-flight deploy by holding the deploy lock, as a running
	// SyncAndDeployAll would. A reconcile tick must skip, not queue behind it.
	d.mu.Lock()
	defer d.mu.Unlock()

	if ran := d.TrySyncAndDeployAll(context.Background(), cfg); ran {
		t.Error("expected TrySyncAndDeployAll to skip while a deploy holds the lock")
	}
	if syncer.called.Load() != 0 {
		t.Errorf("expected no sync while skipped, got %d", syncer.called.Load())
	}
}

func TestHealth_NilBeforeFirstRun(t *testing.T) {
	d := newDeployerWithRunner(&recordingRunner{})
	if err := d.Health(); err != nil {
		t.Errorf("expected healthy before first run, got %v", err)
	}
}

func TestHealth_ReportsFailedSync(t *testing.T) {
	syncer := &fakeRepoSyncer{err: errors.New("remote unreachable")}
	d := New(Config{Runner: &recordingRunner{}, Syncer: syncer, StateDir: t.TempDir()})
	cfg := &config.Config{RepoURL: "ssh://git@example.com/repo.git", StacksBaseDir: t.TempDir()}

	d.SyncAndDeployAll(context.Background(), cfg)

	if err := d.Health(); err == nil {
		t.Error("expected Health to report the sync failure")
	}
}

func TestHealth_RecoversAfterSuccessfulRun(t *testing.T) {
	syncer := &fakeRepoSyncer{err: errors.New("remote unreachable")}
	d := New(Config{Runner: &recordingRunner{}, Syncer: syncer, StateDir: t.TempDir()})
	cfg := &config.Config{RepoURL: "ssh://git@example.com/repo.git", StacksBaseDir: t.TempDir()}

	d.SyncAndDeployAll(context.Background(), cfg)
	if err := d.Health(); err == nil {
		t.Fatal("expected Health to report the sync failure")
	}

	syncer.err = nil
	d.SyncAndDeployAll(context.Background(), cfg)

	if err := d.Health(); err != nil {
		t.Errorf("expected healthy after successful run, got %v", err)
	}
}

func TestWaitIdle_BlocksUntilRunningDeployFinishes(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	// Deterministic seam instead of a sleep: the runner signals when the deploy
	// goroutine executes its first command — proof it holds the deploy lock —
	// and holds every command in flight until the test releases it, so WaitIdle
	// verifiably starts waiting while the deploy is mid-run.
	started := make(chan struct{})
	proceed := make(chan struct{})
	var startOnce sync.Once
	var upCompleted atomic.Bool
	runner := &recordingRunner{}
	runner.onRunStart = func([]string) {
		startOnce.Do(func() { close(started) })
		<-proceed
	}
	runner.failFn = func(_ string, args []string) error {
		if slices.Contains(args, "up") {
			upCompleted.Store(true) // set as the up command finishes
		}
		return nil
	}

	d := New(Config{Runner: runner, StateDir: t.TempDir()})
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "mystack"}},
	}

	go d.SyncAndDeployAll(context.Background(), cfg)
	<-started // the deploy goroutine holds the lock, its first command is in flight

	waitIdleReturned := make(chan struct{})
	go func() {
		defer close(waitIdleReturned)
		d.WaitIdle()
		// WaitIdle must only return after the running deploy released the
		// lock — by then docker compose up has completed.
		if !upCompleted.Load() {
			t.Error("WaitIdle returned before the in-flight deploy finished its up")
		}
	}()

	close(proceed) // release the deploy so it can finish
	<-waitIdleReturned
	assertCommandCalled(t, runner.calls, "up")
}

func TestDeployStack_BuildsWhenDockerfilePresent(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "myapp")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), `services:
  app:
    build: "."
    image: myapp:latest
`)
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM nginx:1.25\n")

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "myapp"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandCalled(t, runner.calls, "build")
	assertCommandCalled(t, runner.calls, "up")
}

func TestDeployStack_NoBuildWhenNoBuildSection(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandNotCalled(t, runner.calls, "build")
}

func TestDeployStack_DockerfileTrackedInHash(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "myapp")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), `services:
  app:
    build: "."
`)
	dockerfilePath := filepath.Join(stackDir, "Dockerfile")
	writeFile(t, dockerfilePath, "FROM nginx:1.25\n")

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "myapp"}

	// First deploy to populate state.
	state := newEmptyState()
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error on first deploy: %v", err)
	}
	runner.calls = nil

	// Second deploy with unchanged files — should be skipped.
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error on second deploy: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Errorf("expected no commands when nothing changed, got %d call(s)", len(runner.calls))
	}

	// Modify the Dockerfile — third deploy should trigger.
	writeFile(t, dockerfilePath, "FROM nginx:1.27\n")
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error on third deploy: %v", err)
	}
	assertCommandCalled(t, runner.calls, "build")
}

func TestDeployStack_SkipsPullWhenAllServicesLocallyBuilt(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "myapp")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), `services:
  app:
    build: "."
    image: myapp:latest
  worker:
    image: myapp:latest
`)

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "myapp"}
	// Previous images differ so the pull decision is actually evaluated.
	state := newEmptyState()
	state.recordImages("myapp", serviceImageByName{"app": "myapp:old"})

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, call := range runner.calls {
		if slices.Contains(call.args, "pull") && !slices.Contains(call.args, "build") {
			t.Errorf("expected no pull for locally-built-only stack, got %v", call.args)
		}
	}
	if len(runner.calls) == 0 || !slices.Contains(runner.calls[len(runner.calls)-1].args, "up") {
		t.Errorf("expected docker compose up to run, got %v", runner.calls)
	}
}

func TestDeployStack_PullsOnlyRemoteServices(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "nextcloud")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), `services:
  app:
    build: "."
    image: nextcloud:34-ghostscript
  cron:
    image: nextcloud:34-ghostscript
  db:
    image: postgres:16-alpine
  redis:
    image: redis:7.2
`)
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM nextcloud:34\n")

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "nextcloud"}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the pull command and verify it specifies service names.
	var pullCall *runCall
	for i, c := range runner.calls {
		if slices.Contains(c.args, "pull") {
			pullCall = &runner.calls[i]
			break
		}
	}
	if pullCall == nil {
		t.Fatal("expected pull command to be called")
	}

	// pull should include db and redis but not app or cron.
	if !slices.Contains(pullCall.args, "db") {
		t.Errorf("expected db in pull args: %v", pullCall.args)
	}
	if !slices.Contains(pullCall.args, "redis") {
		t.Errorf("expected redis in pull args: %v", pullCall.args)
	}
	if slices.Contains(pullCall.args, "app") {
		t.Errorf("build service app should not be in pull args: %v", pullCall.args)
	}
	if slices.Contains(pullCall.args, "cron") {
		t.Errorf("local image consumer cron should not be in pull args: %v", pullCall.args)
	}

	// Build should still be called.
	assertCommandCalled(t, runner.calls, "build")
}

// --- buildBaseEnv: the vars_file layer of Invariant 6 -------------------------

func TestBuildBaseEnv_NoVarsFileReturnsProcessEnvironment(t *testing.T) {
	t.Setenv("SKIPPER_TEST_BASE", "from-env")

	env, err := buildBaseEnv("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Contains(env, "SKIPPER_TEST_BASE=from-env") {
		t.Error("without a vars_file, buildBaseEnv must return the process environment")
	}
}

func TestBuildBaseEnv_VarsFileOverridesProcessEnvironment(t *testing.T) {
	// Invariant 6: vars_file > os.Environ(). exec.Cmd resolves duplicate keys
	// last-wins, so the vars_file entry must appear after the inherited one.
	t.Setenv("SKIPPER_TEST_VAR", "from-env")
	varsPath := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, varsPath, "# global vars\n\nSKIPPER_TEST_VAR=from-vars\nSKIPPER_TEST_EXTRA=added\n")

	env, err := buildBaseEnv(varsPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iEnviron := slices.Index(env, "SKIPPER_TEST_VAR=from-env")
	iVars := slices.Index(env, "SKIPPER_TEST_VAR=from-vars")
	if iEnviron == -1 || iVars == -1 {
		t.Fatalf("expected both the inherited and the vars_file entry; got environ=%d vars=%d", iEnviron, iVars)
	}
	if iVars < iEnviron {
		t.Errorf("vars_file entry (index %d) must come after the inherited one (index %d) so it wins", iVars, iEnviron)
	}
	if !slices.Contains(env, "SKIPPER_TEST_EXTRA=added") {
		t.Error("vars_file entries without an environ counterpart must be appended too")
	}
	if slices.Contains(env, "# global vars") {
		t.Error("comment lines must not leak into the environment")
	}
}

func TestBuildBaseEnv_UnreadableVarsFileFails(t *testing.T) {
	_, err := buildBaseEnv(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil {
		t.Fatal("a configured vars_file that cannot be read must fail, not be silently skipped")
	}
	// The error names vars_file so the operator knows which config key to fix.
	if !strings.Contains(err.Error(), "vars_file") {
		t.Errorf("error should name vars_file; got %v", err)
	}
}
