package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
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
	// The nixos-rebuild runs fire-and-forget and is polled via `systemctl
	// is-active`/`is-failed` exit codes (see internal/nixos.Rebuild). Model the
	// unit as already finished successfully so the poll returns at once: not
	// active (exit non-zero) and not failed (exit non-zero). A rebuild *failure*
	// is instead simulated via errOnCommand:"switch" on the systemd-run start.
	if name == "systemctl" && len(args) > 0 {
		switch args[0] {
		case "is-active":
			return fmt.Errorf("inactive")
		case "is-failed":
			return fmt.Errorf("not failed")
		}
	}
	if r.errOnCommand != "" && slices.Contains(args, r.errOnCommand) {
		return fmt.Errorf("simulated error for command: %s", r.errOnCommand)
	}
	return nil
}

// fakeRepoSyncer implements RepoSyncer for tests.
type fakeRepoSyncer struct {
	called atomic.Int32
	err    error // returned from every Sync call
}

func (f *fakeRepoSyncer) Sync(_ context.Context) error {
	f.called.Add(1)
	return f.err
}

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
		if slices.Contains(c.args, subcommand) {
			return
		}
	}
	t.Errorf("expected command %q to be called, but it was not", subcommand)
}

func assertCommandNotCalled(t *testing.T, calls []runCall, subcommand string) {
	t.Helper()
	for _, c := range calls {
		if slices.Contains(c.args, subcommand) {
			t.Errorf("expected command %q NOT to be called, but it was", subcommand)
			return
		}
	}
}
