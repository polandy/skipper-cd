package git

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type recordingRunner struct {
	calls []runCall
}

type runCall struct {
	dir  string
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	return nil
}

func TestSync_ClonesWhenRepoDirDoesNotExist(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repo") // does not exist yet
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", repoDir, "master")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) == 0 {
		t.Fatal("expected git clone to be called")
	}
	assertArgPresent(t, runner.calls[0].args, "clone")
}

func TestSync_PullsWhenCloneExists(t *testing.T) {
	repoDir := makeFakeClone(t) // contains a .git directory
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", repoDir, "master")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) < 2 {
		t.Fatalf("expected at least 2 git calls (fetch + reset), got %d", len(runner.calls))
	}
	assertArgPresent(t, runner.calls[0].args, "fetch")
	assertArgPresent(t, runner.calls[1].args, "reset")
	if runner.calls[0].dir != repoDir {
		t.Errorf("expected fetch in %s, got %s", repoDir, runner.calls[0].dir)
	}
}

func TestSync_ReclonesWhenRepoDirIsNotAClone(t *testing.T) {
	// A failed first clone leaves an empty repoDir behind. Sync must retry
	// the clone instead of running git fetch in a non-repository forever.
	repoDir := t.TempDir() // exists, but has no .git
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", repoDir, "master")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) == 0 {
		t.Fatal("expected git clone to be called")
	}
	assertArgPresent(t, runner.calls[0].args, "clone")
}

func TestSync_ResetUsesConfiguredBranch(t *testing.T) {
	repoDir := makeFakeClone(t)
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", repoDir, "main")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The reset call should reference origin/main
	resetCall := runner.calls[1]
	assertArgPresent(t, resetCall.args, "origin/main")
}

func TestSync_CloneUsesConfiguredBranch(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repo")
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", repoDir, "develop")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cloneCall := runner.calls[0]
	assertArgPresent(t, cloneCall.args, "--branch")
	assertArgPresent(t, cloneCall.args, "develop")
}

func TestSync_CloneLogRedactsCredentials(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	repoDir := filepath.Join(t.TempDir(), "repo")
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "https://andy:sekrit@example.com/repo.git", repoDir, "master")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(buf.String(), "sekrit") {
		t.Errorf("log leaks the credential: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "xxxxx") {
		t.Errorf("expected masked password in log, got: %s", buf.String())
	}
	// The git command itself must still receive the real URL.
	assertArgPresent(t, runner.calls[0].args, "https://andy:sekrit@example.com/repo.git")
}

func TestRedactedURL(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"masks password", "https://andy:sekrit@example.com/repo.git", "https://andy:xxxxx@example.com/repo.git"},
		{"keeps ssh url without password", "ssh://git@example.com:1022/user/repo.git", "ssh://git@example.com:1022/user/repo.git"},
		{"keeps scp-like syntax", "git@example.com:user/repo.git", "git@example.com:user/repo.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactedURL(tt.in); got != tt.want {
				t.Errorf("redactedURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewRepoSync_UsesDefaultRepoDirWhenEmpty(t *testing.T) {
	s := NewRepoSync("ssh://git@example.com/repo.git", "", "master", 0, nil)
	if s.RepoDir() != defaultRepoDir {
		t.Errorf("expected default clone dir %s, got %s", defaultRepoDir, s.RepoDir())
	}
}

// fakeOutputRunner records Output calls and returns canned output.
type fakeOutputRunner struct {
	calls  []runCall
	output []byte
	err    error
}

func (f *fakeOutputRunner) Output(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, runCall{dir: dir, name: name, args: args})
	return f.output, f.err
}

func TestHeadCommitSHA_RunsRevParseAndTrimsOutput(t *testing.T) {
	runner := &fakeOutputRunner{output: []byte("abc123\n")}
	r := newRepoReaderWithRunner(runner, "/repo")

	sha, err := r.HeadCommitSHA(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != "abc123" {
		t.Errorf("expected trimmed SHA abc123, got %q", sha)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 git call, got %d", len(runner.calls))
	}
	call := runner.calls[0]
	if call.name != "git" {
		t.Errorf("expected git, got %s", call.name)
	}
	assertArgPresent(t, call.args, "rev-parse")
	assertArgPresent(t, call.args, "HEAD")
	assertArgPresent(t, call.args, "/repo")
}

func TestHeadCommitSHA_PropagatesRunnerError(t *testing.T) {
	runner := &fakeOutputRunner{err: context.DeadlineExceeded}
	r := newRepoReaderWithRunner(runner, "/repo")

	if _, err := r.HeadCommitSHA(context.Background()); err == nil {
		t.Fatal("expected error when git fails")
	}
}

func TestFileAtCommit_ShowsPathRelativeToRepo(t *testing.T) {
	runner := &fakeOutputRunner{output: []byte("services: {}")}
	r := newRepoReaderWithRunner(runner, "/repo")

	content, err := r.FileAtCommit(context.Background(), "abc123", "/repo/mystack/docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "services: {}" {
		t.Errorf("expected file content, got %q", content)
	}

	call := runner.calls[0]
	assertArgPresent(t, call.args, "show")
	assertArgPresent(t, call.args, "abc123:mystack/docker-compose.yml")
}

func TestDiffSinceCommit_ReturnsEmptyWithoutFromSHA(t *testing.T) {
	runner := &fakeOutputRunner{}
	r := newRepoReaderWithRunner(runner, "/repo")

	diff, err := r.DiffSinceCommit(context.Background(), "", "/repo/mystack/docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff, got %q", diff)
	}
	if len(runner.calls) != 0 {
		t.Errorf("expected no git calls, got %d", len(runner.calls))
	}
}

func TestDiffSinceCommit_DiffsAgainstHead(t *testing.T) {
	runner := &fakeOutputRunner{output: []byte("+ image: nginx:1.26\n")}
	r := newRepoReaderWithRunner(runner, "/repo")

	diff, err := r.DiffSinceCommit(context.Background(), "abc123", "/repo/mystack/docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff != "+ image: nginx:1.26\n" {
		t.Errorf("unexpected diff: %q", diff)
	}

	call := runner.calls[0]
	assertArgPresent(t, call.args, "diff")
	assertArgPresent(t, call.args, "abc123..HEAD")
	assertArgPresent(t, call.args, "/repo/mystack/docker-compose.yml")
}

// makeFakeClone returns a temp dir containing a .git directory, i.e. what a
// successful clone leaves behind.
func makeFakeClone(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repoDir
}

func assertArgPresent(t *testing.T, args []string, expected string) {
	t.Helper()
	if !slices.Contains(args, expected) {
		t.Errorf("expected argument %q to be present in %v", expected, args)
	}
}
