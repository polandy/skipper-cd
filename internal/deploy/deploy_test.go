package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/polandy/orpheus-cd/internal/config"
)

// recordingRunner is a fake Runner that records every command it receives
// instead of executing it. This is the Go equivalent of a mock in Java.
type recordingRunner struct {
	calls []runCall
	// errOnCommand makes the runner return an error when the command matches.
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
	workDir := makeStackDir(t, "DOMAIN=example.com\n")
	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea", WorkingDir: workDir}
	state := map[string]string{} // empty state = first deploy

	if err := d.deployStackIfChanged(stack, "", state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandCalled(t, runner.calls, "pull")
	assertCommandCalled(t, runner.calls, "up")

	if state["gitea"] == "" {
		t.Error("expected state to be updated after deploy")
	}
}

func TestDeployStack_SkipsWhenUnchanged(t *testing.T) {
	workDir := makeStackDir(t, "DOMAIN=example.com\n")
	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea", WorkingDir: workDir}

	// Pre-populate state with the current hash to simulate "already deployed".
	state := map[string]string{}
	hash, _ := computeStackConfigHash(workDir, nil)
	state["gitea"] = hash

	if err := d.deployStackIfChanged(stack, "", state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) != 0 {
		t.Errorf("expected no commands to be run, got %d call(s)", len(runner.calls))
	}
}

func TestDeployStack_FailsOnPullError(t *testing.T) {
	workDir := makeStackDir(t, "")
	runner := &recordingRunner{errOnCommand: "pull"}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "gitea", WorkingDir: workDir}
	state := map[string]string{}

	err := d.deployStackIfChanged(stack, "", state)
	if err == nil {
		t.Fatal("expected error when docker compose pull fails")
	}

	// State must not be updated when the deploy failed.
	if state["gitea"] != "" {
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

	stack := config.Stack{Name: "gitea"} // no working_dir set
	state := map[string]string{}

	if err := d.deployStackIfChanged(stack, baseDir, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) == 0 {
		t.Fatal("expected commands to be run")
	}
	if runner.calls[0].dir != workDir {
		t.Errorf("expected working dir %s, got %s", workDir, runner.calls[0].dir)
	}
}

// makeStackDir creates a temp directory with a docker-compose.yml,
// which is the minimum required for computeStackConfigHash to succeed.
func makeStackDir(t *testing.T, envContent string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), "version: '3'\n")
	if envContent != "" {
		writeFile(t, filepath.Join(dir, ".env"), envContent)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

// assertCommandCalled checks that the runner received a docker compose command
// with the given subcommand (e.g. "pull", "up").
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
