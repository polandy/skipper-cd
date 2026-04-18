package git

import (
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

func (r *recordingRunner) Run(dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	return nil
}

func TestSync_ClonesWhenCloneDirDoesNotExist(t *testing.T) {
	cloneDir := filepath.Join(t.TempDir(), "repo") // does not exist yet
	runner := &recordingRunner{}
	s := newSyncerWithRunner(runner, "ssh://git@example.com/repo.git", cloneDir)

	if err := s.Sync(); err != nil {
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
	s := newSyncerWithRunner(runner, "ssh://git@example.com/repo.git", cloneDir)

	if err := s.Sync(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) == 0 {
		t.Fatal("expected git pull to be called")
	}
	assertArgPresent(t, runner.calls[0].args, "pull")
	if runner.calls[0].dir != cloneDir {
		t.Errorf("expected pull in %s, got %s", cloneDir, runner.calls[0].dir)
	}
}

func TestNewSyncer_UsesDefaultCloneDirWhenEmpty(t *testing.T) {
	s := NewSyncer("ssh://git@example.com/repo.git", "")
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
