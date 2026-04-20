package git

import (
	"context"
	"path/filepath"
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

func TestSync_ClonesWhenCloneDirDoesNotExist(t *testing.T) {
	cloneDir := filepath.Join(t.TempDir(), "repo") // does not exist yet
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", cloneDir, "master")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) == 0 {
		t.Fatal("expected git clone to be called")
	}
	assertArgPresent(t, runner.calls[0].args, "clone")
}

func TestSync_PullsWhenCloneDirExists(t *testing.T) {
	cloneDir := t.TempDir() // already exists
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", cloneDir, "master")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) < 2 {
		t.Fatalf("expected at least 2 git calls (fetch + reset), got %d", len(runner.calls))
	}
	assertArgPresent(t, runner.calls[0].args, "fetch")
	assertArgPresent(t, runner.calls[1].args, "reset")
	if runner.calls[0].dir != cloneDir {
		t.Errorf("expected fetch in %s, got %s", cloneDir, runner.calls[0].dir)
	}
}

func TestSync_ResetUsesConfiguredBranch(t *testing.T) {
	cloneDir := t.TempDir()
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", cloneDir, "main")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The reset call should reference origin/main
	resetCall := runner.calls[1]
	assertArgPresent(t, resetCall.args, "origin/main")
}

func TestSync_CloneUsesConfiguredBranch(t *testing.T) {
	cloneDir := filepath.Join(t.TempDir(), "repo")
	runner := &recordingRunner{}
	s := newRepoSyncWithRunner(runner, "ssh://git@example.com/repo.git", cloneDir, "develop")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cloneCall := runner.calls[0]
	assertArgPresent(t, cloneCall.args, "--branch")
	assertArgPresent(t, cloneCall.args, "develop")
}

func TestNewRepoSync_UsesDefaultCloneDirWhenEmpty(t *testing.T) {
	s := NewRepoSync("ssh://git@example.com/repo.git", "", "master")
	if s.CloneDir() != defaultCloneDir {
		t.Errorf("expected default clone dir %s, got %s", defaultCloneDir, s.CloneDir())
	}
}

func assertArgPresent(t *testing.T, args []string, expected string) {
	t.Helper()
	for _, arg := range args {
		if arg == expected {
			return
		}
	}
	t.Errorf("expected argument %q to be present in %v", expected, args)
}
