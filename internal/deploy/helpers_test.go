package deploy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
)

// recordingRunner is a fake Runner that records every command it receives
// instead of executing it.
type recordingRunner struct {
	calls        []runCall
	errOnCommand string
	failFn       func(dir string, args []string) error // optional per-call failure hook, e.g. to fail one stack's up
	delay        time.Duration                         // optional delay per call for concurrency tests

	// onRunStart, when set, is called at the entry of every Run call — a
	// deterministic seam for tests that must know a deploy goroutine has
	// started executing commands (i.e. holds the deploy lock). It may block
	// to hold a command in flight until the test releases it.
	onRunStart func(args []string)

	// outputFn drives Output (rollout's `docker compose ps` reads): it receives
	// the zero-based Output call index so a test can return a changing snapshot
	// (old only, then old+canary). nil returns empty output.
	outputFn    func(call int, args []string) ([]byte, error)
	outputCalls []runCall
}

type runCall struct {
	dir  string
	name string
	args []string
	env  []string
}

func (r *recordingRunner) Run(_ context.Context, dir string, env []string, name string, args ...string) error {
	if r.onRunStart != nil {
		r.onRunStart(args)
	}
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args, env: env})
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
	if r.failFn != nil {
		return r.failFn(dir, args)
	}
	return nil
}

// Output records the call and returns canned stdout via outputFn, letting
// rollout tests serve a changing `docker compose ps` snapshot. It makes
// recordingRunner satisfy the Outputter interface too.
func (r *recordingRunner) Output(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	call := len(r.outputCalls)
	r.outputCalls = append(r.outputCalls, runCall{dir: dir, name: name, args: args})
	if r.outputFn != nil {
		return r.outputFn(call, args)
	}
	return nil, nil
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
	diffs     map[string]string
	commits   map[string][]events.CommitInfo // filePath -> commits touching it
	commitErr error                          // error to return from CommitsSinceCommit
	files     map[string][]byte              // commitSHA:filePath -> content
	fileErr   error                          // error to return from FileAtCommit
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

// CommitsSinceCommit returns the union of canned commits for the given files,
// de-duplicated by SHA (mirroring one git-log call over several pathspecs).
func (f *fakeCommitReader) CommitsSinceCommit(_ context.Context, _ string, filePaths []string) ([]events.CommitInfo, error) {
	if f.commitErr != nil {
		return nil, f.commitErr
	}
	var out []events.CommitInfo
	seen := map[string]bool{}
	for _, p := range filePaths {
		for _, c := range f.commits[p] {
			if seen[c.SHA] {
				continue
			}
			seen[c.SHA] = true
			out = append(out, c)
		}
	}
	return out, nil
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

// newDeployerWithRunner builds a Deployer with only a (fake) runner wired.
// Tests needing more collaborators call New(Config{...}) directly — all
// wiring happens at construction, exactly like production.
func newDeployerWithRunner(r Runner) *Deployer {
	return New(Config{Runner: r})
}

func makeStackDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	return dir
}

func composeWithImage(image string) string {
	return fmt.Sprintf("services:\n  app:\n    image: %s\n", image)
}

// composeWithHealthcheck is composeWithImage plus a Docker healthcheck on the
// service, for tests of the automatic deploy_health_check gate (ADR-0046).
func composeWithHealthcheck(image string) string {
	return fmt.Sprintf("services:\n  app:\n    image: %s\n    healthcheck:\n      test: [\"CMD\", \"true\"]\n", image)
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

// boolPtr returns a pointer to b, for optional *bool config fields in tests.
func boolPtr(b bool) *bool { return &b }

// captureLogAt runs fn with the default logger swapped for one writing to a
// buffer at the given threshold, and returns what was logged. It is how a test
// asserts what a line actually carries — the console renders the diff from the
// record, so the attr is behaviour, not an implementation detail.
func captureLogAt(t *testing.T, level slog.Level, fn func(*testing.T)) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	defer slog.SetDefault(prev)
	fn(t)
	return buf.String()
}
