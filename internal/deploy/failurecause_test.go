package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/command"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// readTail loads a recorded stderr tail from testdata.
func readTail(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// The tails are the build output of the same broken apt pin on two reconcile
// ticks, as the host journal recorded them: they differ only in BuildKit's
// per-run elapsed stamps (4.640 vs 4.220).
const aptPinCause = `E: Unable to correct problems, you have held broken packages.` +
	` — process "/bin/sh -c apt-get update && apt-get install -y ghostscript…" did not complete successfully: exit code: 100`

func TestFailureCause_BuildKitStepNamesProcessErrorAndVerdict(t *testing.T) {
	if got := failureCause(readTail(t, "buildkit-apt-pin-run1.txt")); got != aptPinCause {
		t.Errorf("failureCause =\n  %q\nwant\n  %q", got, aptPinCause)
	}
}

// ADR-0056 collapses a repeat only when the error text is identical, so the
// per-run timestamps must not survive into the cause.
func TestFailureCause_SameFailureOnAnotherRunYieldsSameText(t *testing.T) {
	first := failureCause(readTail(t, "buildkit-apt-pin-run1.txt"))
	second := failureCause(readTail(t, "buildkit-apt-pin-run2.txt"))
	if first != second {
		t.Errorf("two runs of the same failure differ:\n  %q\n  %q", first, second)
	}
}

func TestFailureCause(t *testing.T) {
	tests := []struct {
		name string
		tail []string
		want string
	}{
		{
			name: "a RUN step that printed nothing is BuildKit's verdict alone",
			tail: []string{
				"#5 [2/2] RUN false",
				"#5 ERROR: process \"/bin/sh -c false\" did not complete successfully: exit code: 1",
				"------",
				" > [2/2] RUN false:",
				"------",
				"failed to solve: process \"/bin/sh -c false\" did not complete successfully: exit code: 1",
			},
			want: `process "/bin/sh -c false" did not complete successfully: exit code: 1`,
		},
		{
			name: "only the failing step's lines are searched for its error",
			tail: []string{
				"#4 0.512 error: an unrelated earlier step's warning",
				"#5 0.100 fatal: unable to access 'https://example.com/repo.git/'",
				"#5 ERROR: process \"/bin/sh -c git clone https://example.com/repo.git\" did not complete successfully: exit code: 128",
			},
			want: `fatal: unable to access 'https://example.com/repo.git/' — process "/bin/sh -c git clone https://example.com/repo.git" did not complete successfully: exit code: 128`,
		},
		{
			name: "a base image that does not resolve is the failed-to-solve line",
			tail: []string{
				"#3 [internal] load metadata for docker.io/library/nextcloud:99",
				"#3 ERROR: docker.io/library/nextcloud:99: not found",
				"------",
				"failed to solve: nextcloud:99: failed to resolve source metadata for docker.io/library/nextcloud:99: not found",
			},
			want: `docker.io/library/nextcloud:99: not found`,
		},
		{
			name: "a pull is the daemon's error line",
			tail: []string{
				" app Pulling",
				"Error response from daemon: manifest for nginx:9.99 not found: manifest unknown",
			},
			want: `Error response from daemon: manifest for nginx:9.99 not found: manifest unknown`,
		},
		{
			name: "without an error-looking line, the last line",
			tail: []string{"dumping database", "pg_dump: connection to server failed", "backup aborted"},
			want: "backup aborted",
		},
		{
			name: "escape sequences and progress redraws are removed",
			tail: []string{"\x1b[31mERROR\x1b[0m: boom", "50%\r75%\r\x1b[2Kdone"},
			want: "ERROR: boom",
		},
		{
			name: "a long line is cut",
			tail: []string{"Error: " + strings.Repeat("x", 300)},
			want: "Error: " + strings.Repeat("x", maxCausePart-len("Error: ")-1) + "…",
		},
		{
			name: "nothing printed",
			tail: []string{"", "   ", "------"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := failureCause(tt.tail); got != tt.want {
				t.Errorf("failureCause =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestWithFailureCause_LeavesErrorsWithoutTailUnchanged(t *testing.T) {
	plain := errors.New("exit status 1")
	if got := withFailureCause(plain); got.Error() != plain.Error() {
		t.Errorf("withFailureCause(plain) = %q, want it unchanged", got)
	}
	if got := withFailureCause(nil); got != nil {
		t.Errorf("withFailureCause(nil) = %v, want nil", got)
	}
}

// The incident this exists for: the event of a failed build named only the
// exit status, the cause stood in the journal alone.
func TestDeployStack_FailedBuildEventNamesTheCause(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "nextcloud")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), "services:\n  app:\n    build: \".\"\n    image: nextcloud:34-ghostscript\n")
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM nextcloud:34.0.4\n")

	tail := readTail(t, "buildkit-apt-pin-run1.txt")
	runner := &recordingRunner{failFn: func(_ string, args []string) error {
		if slices.Contains(args, "build") {
			return &command.ExitError{Err: errors.New("exit status 1"), Tail: tail}
		}
		return nil
	}}
	var got events.DeployEvent
	d := New(Config{Runner: runner, EventSink: func(e events.DeployEvent) { got = e }})

	err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "nextcloud"}, baseDir, "", nil, newEmptyState())
	if err == nil {
		t.Fatal("expected the failed build to fail the deploy")
	}
	want := "docker compose build: exit status 1: " + aptPinCause
	if got.Status != events.StatusFailed || got.Error != want {
		t.Errorf("event = %s %q, want failed %q", got.Status, got.Error, want)
	}
}

// A hook's own output is what says why it failed.
func TestDeployStack_FailedHookEventNamesTheCause(t *testing.T) {
	baseDir, _ := hookStackDir(t, "db")
	const backup = "pg_dump > /backup/pre.sql"
	runner := &recordingRunner{failFn: func(_ string, args []string) error {
		if slices.Contains(args, backup) {
			return &command.ExitError{Err: errors.New("exit status 1"), Tail: []string{"pg_dump: error: connection to server failed: Connection refused"}}
		}
		return nil
	}}
	var got events.DeployEvent
	d := New(Config{Runner: runner, EventSink: func(e events.DeployEvent) { got = e }})

	stack := config.Stack{Name: "db", Hooks: config.Hooks{PreDeploy: []string{backup}}}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err == nil {
		t.Fatal("expected the failed hook to fail the deploy")
	}
	if !strings.HasSuffix(got.Error, "exit status 1: pg_dump: error: connection to server failed: Connection refused") {
		t.Errorf("event error = %q, want it to end with the hook's error line", got.Error)
	}
}
