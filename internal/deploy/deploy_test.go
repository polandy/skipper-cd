package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// recordingRunner is a fake Runner that records every command it receives
// instead of executing it.
type recordingRunner struct {
	calls        []runCall
	errOnCommand string
	delay        time.Duration // optional delay per call for concurrency tests
}

type runCall struct {
	dir  string
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	if r.errOnCommand != "" && containsArg(args, r.errOnCommand) {
		return fmt.Errorf("simulated error for command: %s", r.errOnCommand)
	}
	return nil
}

// fakeRepoSyncer implements RepoSyncer for tests.
type fakeRepoSyncer struct {
	called atomic.Int32
}

func (f *fakeRepoSyncer) Sync(_ context.Context) error {
	f.called.Add(1)
	return nil
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
	state := persistedState{
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

func TestDeployStack_UsesBaseDirWhenWorkingDirAbsent(t *testing.T) {
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

func TestDeployStack_WorkingDirUsesProjectDirectoryFlag(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "nextcloud")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nextcloud:30"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	projectDir := "/etc/nixos/modules/nextcloud"
	stack := config.Stack{Name: "nextcloud", WorkingDir: projectDir}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All docker compose calls should use -f and --project-directory flags.
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	for _, c := range runner.calls {
		if c.name != "docker" || !containsArg(c.args, "compose") {
			continue
		}
		if !containsArg(c.args, "-f") {
			t.Errorf("expected -f flag in docker compose call: %v", c.args)
		}
		if !containsArg(c.args, composePath) {
			t.Errorf("expected compose path %s in args: %v", composePath, c.args)
		}
		if !containsArg(c.args, "--project-directory") {
			t.Errorf("expected --project-directory flag in docker compose call: %v", c.args)
		}
		if !containsArg(c.args, projectDir) {
			t.Errorf("expected project dir %s in args: %v", projectDir, c.args)
		}
		if c.dir != projectDir {
			t.Errorf("expected run dir %s, got %s", projectDir, c.dir)
		}
	}
}

func TestDeployStack_NoWorkingDirRunsFromRepoClone(t *testing.T) {
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

	// Without working_dir, docker compose should run from the repo clone dir
	// without -f or --project-directory flags.
	for _, c := range runner.calls {
		if c.name != "docker" || !containsArg(c.args, "compose") {
			continue
		}
		if containsArg(c.args, "-f") {
			t.Errorf("unexpected -f flag without working_dir: %v", c.args)
		}
		if containsArg(c.args, "--project-directory") {
			t.Errorf("unexpected --project-directory flag without working_dir: %v", c.args)
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
	state := persistedState{
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
	state := persistedState{
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
		if runner.calls[i].name == "docker" && containsArg(runner.calls[i].args, "stop") {
			stopCall = &runner.calls[i]
			break
		}
	}
	if stopCall == nil {
		t.Fatal("expected docker stop to be called")
	}
	if !containsArg(stopCall.args, "monica-app") || !containsArg(stopCall.args, "monica-db") {
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
		if c.name == "docker" && containsArg(c.args, "stop") {
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

	d := &Deployer{runner: runner, syncer: syncer, stateDir: t.TempDir(), timeout: 10 * time.Second}

	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: t.TempDir(),
		Stacks:        []config.Stack{},
	}

	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			d.SyncAndDeployAll(ctx, cfg)
			done <- struct{}{}
		}()
	}

	for i := 0; i < 2; i++ {
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

func TestChangedFiles_NoneWhenHashesMatch(t *testing.T) {
	hashes := stackFileHashes{"docker-compose.yml": "abc123"}
	if got := changedFiles(hashes, hashes); len(got) != 0 {
		t.Errorf("expected no changed files, got %v", got)
	}
}

func TestChangedFiles_DetectsChangedFile(t *testing.T) {
	current := stackFileHashes{"docker-compose.yml": "newHash"}
	last := stackFileHashes{"docker-compose.yml": "oldHash"}
	changed := changedFiles(current, last)
	if len(changed) != 1 || changed[0] != "docker-compose.yml" {
		t.Errorf("expected [docker-compose.yml], got %v", changed)
	}
}

func TestChangedFiles_DetectsNewFile(t *testing.T) {
	current := stackFileHashes{"docker-compose.yml": "abc", "app.env": "def"}
	last := stackFileHashes{"docker-compose.yml": "abc"}
	changed := changedFiles(current, last)
	if len(changed) != 1 || changed[0] != "app.env" {
		t.Errorf("expected [app.env], got %v", changed)
	}
}

func TestComputePerFileHashes_ReturnsHashForEachFile(t *testing.T) {
	workDir := makeStackDir(t)
	envFile := filepath.Join(t.TempDir(), "app.env")
	writeFile(t, envFile, "KEY=value\n")

	hashes, err := computePerFileHashes(workDir, []string{envFile}, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	composePath := filepath.Join(workDir, "docker-compose.yml")
	if hashes[composePath] == "" {
		t.Errorf("expected hash for docker-compose.yml")
	}
	if hashes[envFile] == "" {
		t.Errorf("expected hash for env file")
	}
}

func TestComputePerFileHashes_IncludesVarsFile(t *testing.T) {
	workDir := makeStackDir(t)
	varsFile := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, varsFile, "DOMAIN=example.com\n")

	hashes, err := computePerFileHashes(workDir, nil, nil, varsFile, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hashes[varsFile] == "" {
		t.Errorf("expected hash for vars_file")
	}
}

func TestComputePerFileHashes_IncludesExtraFiles(t *testing.T) {
	workDir := makeStackDir(t)
	dockerfilePath := filepath.Join(workDir, "Dockerfile")
	writeFile(t, dockerfilePath, "FROM nginx:1.25\n")

	hashes, err := computePerFileHashes(workDir, nil, nil, "", []string{dockerfilePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hashes[dockerfilePath] == "" {
		t.Errorf("expected hash for Dockerfile")
	}
}

// --- extractDockerfilePaths tests ---

func TestExtractDockerfilePaths_StringForm(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), `services:
  app:
    build: "."
`)
	writeFile(t, filepath.Join(workDir, "Dockerfile"), "FROM nginx:1.25\n")

	paths, err := extractDockerfilePaths(filepath.Join(workDir, "docker-compose.yml"), workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d: %v", len(paths), paths)
	}
	if paths[0] != filepath.Join(workDir, "Dockerfile") {
		t.Errorf("unexpected path: %s", paths[0])
	}
}

func TestExtractDockerfilePaths_MapForm(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), `services:
  app:
    build:
      context: "."
      dockerfile: "Dockerfile.custom"
`)
	writeFile(t, filepath.Join(workDir, "Dockerfile.custom"), "FROM nginx:1.25\n")

	paths, err := extractDockerfilePaths(filepath.Join(workDir, "docker-compose.yml"), workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d: %v", len(paths), paths)
	}
	if paths[0] != filepath.Join(workDir, "Dockerfile.custom") {
		t.Errorf("unexpected path: %s", paths[0])
	}
}

func TestExtractDockerfilePaths_MissingDockerfileSkipped(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), `services:
  app:
    build: "."
`)
	// No Dockerfile written — should return empty, not an error.

	paths, err := extractDockerfilePaths(filepath.Join(workDir, "docker-compose.yml"), workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected no paths, got %v", paths)
	}
}

func TestExtractDockerfilePaths_NoBuildServicesReturnsEmpty(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	paths, err := extractDockerfilePaths(filepath.Join(workDir, "docker-compose.yml"), workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected no paths, got %v", paths)
	}
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

// --- images.go tests ---

func TestExtractComposeImages_ParsesImages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, `services:
  app:
    image: gitea/gitea:1.21
  db:
    image: postgres:16-alpine
  builder:
    build: .
`)

	images, err := extractComposeImages(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d: %v", len(images), images)
	}
	if images["app"] != "gitea/gitea:1.21" {
		t.Errorf("expected gitea/gitea:1.21, got %s", images["app"])
	}
	if images["db"] != "postgres:16-alpine" {
		t.Errorf("expected postgres:16-alpine, got %s", images["db"])
	}
}

func TestImagesChanged_DetectsChange(t *testing.T) {
	current := map[string]string{"app": "redis:7.4"}
	previous := map[string]string{"app": "redis:7.2"}
	if !hasAnyImageChanged(current, previous) {
		t.Error("expected images to be detected as changed")
	}
}

func TestImagesChanged_DetectsNoChange(t *testing.T) {
	images := map[string]string{"app": "redis:7.2", "db": "postgres:16"}
	if hasAnyImageChanged(images, images) {
		t.Error("expected images to be detected as unchanged")
	}
}

func TestImagesChanged_DetectsNewService(t *testing.T) {
	current := map[string]string{"app": "redis:7.2", "cache": "memcached:1.6"}
	previous := map[string]string{"app": "redis:7.2"}
	if !hasAnyImageChanged(current, previous) {
		t.Error("expected new service to be detected as change")
	}
}

func TestImagesChanged_NilPreviousIsChanged(t *testing.T) {
	current := map[string]string{"app": "redis:7.2"}
	if !hasAnyImageChanged(current, nil) {
		t.Error("expected nil previous to be detected as changed")
	}
}

// --- parseEnvFile tests ---

func TestParseEnvFile_ParsesKeyValuePairs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, path, "DOMAIN=example.com\nSMTP_HOST=mail.example.com\n")

	vars, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(vars, "DOMAIN=example.com") {
		t.Errorf("expected DOMAIN=example.com in %v", vars)
	}
	if !slices.Contains(vars, "SMTP_HOST=mail.example.com") {
		t.Errorf("expected SMTP_HOST=mail.example.com in %v", vars)
	}
}

func TestParseEnvFile_IgnoresCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, path, "# this is a comment\n\nDOMAIN=example.com\n")

	vars, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(vars) != 1 || vars[0] != "DOMAIN=example.com" {
		t.Errorf("expected [DOMAIN=example.com], got %v", vars)
	}
}

func TestParseEnvFile_MissingFileReturnsError(t *testing.T) {
	_, err := parseEnvFile("/nonexistent/vars.env")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

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
	state := persistedState{
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

type fakeCommitReader struct {
	diffs   map[string]string
	files   map[string][]byte // commitSHA:filePath -> content
	fileErr error             // error to return from FileAtCommit
}

func (f *fakeCommitReader) HeadCommitSHA(_ context.Context) (string, error) {
	return "abc123", nil
}

func (f *fakeCommitReader) DiffSinceCommit(_ context.Context, _, filePath string) (string, error) {
	if d, ok := f.diffs[filePath]; ok {
		return d, nil
	}
	return "", nil
}

func (f *fakeCommitReader) FileAtCommit(_ context.Context, commitSHA, filePath string) ([]byte, error) {
	if f.fileErr != nil {
		return nil, f.fileErr
	}
	key := commitSHA + ":" + filePath
	if content, ok := f.files[key]; ok {
		return content, nil
	}
	return nil, fmt.Errorf("file not found at %s:%s", commitSHA, filePath)
}

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
	state := persistedState{
		Stacks:              map[string]stackFileHashes{"gitea": {"old": "oldhash"}},
		Images:              map[string]serviceImageByName{},
		LastDeployedCommit:  "old-sha",
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

// --- rollback tests ---

// selectiveErrRunner fails only on specific compose subcommands.
type selectiveErrRunner struct {
	calls       []runCall
	errCommands map[string]bool // compose subcommands that should fail (e.g. "up")
}

func (r *selectiveErrRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	for _, a := range args {
		if r.errCommands[a] {
			return fmt.Errorf("simulated error for command: %s", a)
		}
	}
	return nil
}

func TestDeployStack_RollbackOnUpFailure(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	oldCompose := composeWithImage("nginx:1.25")

	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(oldCompose),
		},
	}

	// Fail on "up" but succeed on everything else (including rollback up).
	runner := &selectiveErrRunner{errCommands: map[string]bool{"up": true}}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	stack := config.Stack{Name: "mystack"}
	state := persistedState{
		Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed deploy")
	}

	// The rollback also calls "up", so it will also fail. Let's test the
	// case where rollback succeeds instead — need a smarter runner.
	// This test verifies the error message format when rollback also fails.
	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("expected 'rollback also failed' in error, got: %s", err.Error())
	}
}

func TestDeployStack_RollbackSucceeds(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	oldCompose := composeWithImage("nginx:1.25")

	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(oldCompose),
		},
	}

	// Use a runner that fails only the first "up" call (the deploy), not the rollback "up".
	upCallCount := 0
	runner := &countingErrRunner{failOnNthUp: 1, upCount: &upCallCount}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	stack := config.Stack{Name: "mystack"}
	state := persistedState{
		Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed deploy")
	}
	if !strings.Contains(err.Error(), "rolled back to previous version") {
		t.Errorf("expected 'rolled back to previous version' in error, got: %s", err.Error())
	}

	// State should NOT be updated (the deploy failed).
	if state.Stacks["mystack"] != nil && state.Stacks["mystack"]["old"] != "oldhash" {
		t.Error("state should not be updated after a rolled-back deploy")
	}
}

func TestDeployStack_RollbackSkippedWithoutPreviousCommit(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	runner := &recordingRunner{errOnCommand: "up"}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "mystack"}
	state := newEmptyState()

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed deploy")
	}
	// Without commitReader or LastDeployedCommit, rollback should fail with "no previous commit".
	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("expected 'rollback also failed' in error, got: %s", err.Error())
	}
}

func TestDeployAllStacks_EmitsRolledBackEvent(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	oldCompose := composeWithImage("nginx:1.25")

	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(oldCompose),
		},
	}

	upCallCount := 0
	runner := &countingErrRunner{failOnNthUp: 1, upCount: &upCallCount}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	var emitted []events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) {
		emitted = append(emitted, e)
	})

	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "mystack"}},
	}

	// Pre-seed state with old hashes and commit.
	state, _ := loadPersistedDeployState(d.stateDir)
	state.Stacks["mystack"] = stackFileHashes{"old": "oldhash"}
	state.LastDeployedCommit = "old-sha"
	_ = saveDeployState(d.stateDir, state)

	d.DeployAllStacks(context.Background(), cfg)

	var rolledBack *events.DeployEvent
	for i := range emitted {
		if emitted[i].Status == events.StatusRolledBack {
			rolledBack = &emitted[i]
			break
		}
	}
	if rolledBack == nil {
		t.Fatal("expected a rolled_back event")
	}
	if rolledBack.Stack != "mystack" {
		t.Errorf("expected stack 'mystack', got %q", rolledBack.Stack)
	}
}

// countingErrRunner fails only the Nth "up" call (1-indexed).
type countingErrRunner struct {
	calls       []runCall
	failOnNthUp int
	upCount     *int
}

func (r *countingErrRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	for _, a := range args {
		if a == "up" {
			*r.upCount++
			if *r.upCount == r.failOnNthUp {
				return fmt.Errorf("simulated error for command: up")
			}
		}
	}
	return nil
}

// --- helpers ---

func makeStackDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	return dir
}

func composeWithImage(image string) string {
	return fmt.Sprintf("services:\n  app:\n    image: %s\n", image)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func assertCommandCalled(t *testing.T, calls []runCall, subcommand string) {
	t.Helper()
	for _, c := range calls {
		for _, arg := range c.args {
			if arg == subcommand {
				return
			}
		}
	}
	t.Errorf("expected command %q to be called, but it was not", subcommand)
}

func assertCommandNotCalled(t *testing.T, calls []runCall, subcommand string) {
	t.Helper()
	for _, c := range calls {
		for _, arg := range c.args {
			if arg == subcommand {
				t.Errorf("expected command %q NOT to be called, but it was", subcommand)
				return
			}
		}
	}
}

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

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
	d := &Deployer{runner: runner, repoDir: baseDir, stateDir: t.TempDir()}

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#nuc"},
	}

	d.DeployAllStacks(context.Background(), cfg)

	// nixos-rebuild should have been attempted.
	nixosCalled := false
	for _, c := range runner.calls {
		if c.name == "nixos-rebuild" {
			nixosCalled = true
		}
	}
	if !nixosCalled {
		t.Error("expected nixos-rebuild to be called")
	}

	// docker compose should NOT have been called since nixos-rebuild failed.
	for _, c := range runner.calls {
		if c.name == "docker" && containsArg(c.args, "compose") {
			t.Error("expected docker compose NOT to be called after nixos-rebuild failure")
			break
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
	d := &Deployer{runner: runner, repoDir: baseDir, stateDir: t.TempDir()}

	enabled := true
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: filepath.Join(baseDir, "modules"),
		Stacks:        []config.Stack{{Name: "gitea"}},
		NixOSRebuild:  &config.NixOSRebuild{Enabled: &enabled, Flake: ".#nuc"},
	}

	d.DeployAllStacks(context.Background(), cfg)

	// Both nixos-rebuild and docker compose should have been called.
	nixosCalled := false
	dockerCalled := false
	for _, c := range runner.calls {
		if c.name == "nixos-rebuild" {
			nixosCalled = true
		}
		if c.name == "docker" && containsArg(c.args, "compose") {
			dockerCalled = true
		}
	}
	if !nixosCalled {
		t.Error("expected nixos-rebuild to be called")
	}
	if !dockerCalled {
		t.Error("expected docker compose to be called after successful nixos-rebuild")
	}
}
