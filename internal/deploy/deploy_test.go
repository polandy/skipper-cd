package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

// recordingRunner is a fake Runner that records every command it receives
// instead of executing it. This is the Go equivalent of a mock in Java.
type recordingRunner struct {
	calls        []runCall
	errOnCommand string
}

type runCall struct {
	dir  string
	name string
	args []string
}

func (r *recordingRunner) Run(dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	if r.errOnCommand != "" && len(args) > 0 && args[len(args)-1] == r.errOnCommand {
		return fmt.Errorf("simulated error for command: %s", r.errOnCommand)
	}
	return nil
}

func TestDeployStack_DeploysWhenHashChanges(t *testing.T) {
	workDir := makeStackDir(t)
	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea", WorkingDir: workDir}
	state := deployState{}

	if err := d.deployStackIfChanged(stack, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandCalled(t, runner.calls, "pull")
	assertCommandCalled(t, runner.calls, "up")

	if state["gitea"] == nil {
		t.Error("expected state to be updated after deploy")
	}
}

func TestDeployStack_SkipsWhenUnchanged(t *testing.T) {
	workDir := makeStackDir(t)
	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea", WorkingDir: workDir}

	// Pre-populate state with the current hashes to simulate "already deployed".
	hashes, err := computePerFileHashes(workDir, nil)
	if err != nil {
		t.Fatalf("unexpected error computing hashes: %v", err)
	}
	state := deployState{"gitea": hashes}

	if err := d.deployStackIfChanged(stack, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) != 0 {
		t.Errorf("expected no commands to be run, got %d call(s)", len(runner.calls))
	}
}

func TestDeployStack_FailsOnPullError(t *testing.T) {
	workDir := makeStackDir(t)
	runner := &recordingRunner{errOnCommand: "pull"}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea", WorkingDir: workDir}
	state := deployState{}

	err := d.deployStackIfChanged(stack, "", nil, state)
	if err == nil {
		t.Fatal("expected error when docker compose pull fails")
	}

	if state["gitea"] != nil {
		t.Error("state should not be updated after a failed deploy")
	}
}

func TestDeployStack_UsesBaseDirWhenWorkingDirAbsent(t *testing.T) {
	baseDir := t.TempDir()
	workDir := filepath.Join(baseDir, "gitea")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), "version: '3'\n")

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea"}
	state := deployState{}

	if err := d.deployStackIfChanged(stack, baseDir, nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) == 0 {
		t.Fatal("expected commands to be run")
	}
	if runner.calls[0].dir != workDir {
		t.Errorf("expected working dir %s, got %s", workDir, runner.calls[0].dir)
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

	hashes, err := computePerFileHashes(workDir, []string{envFile})
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

func makeStackDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), "version: '3'\n")
	return dir
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
