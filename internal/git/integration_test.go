package git

// Integration tests against a real local git repository. The unit tests in
// git_test.go assert the exact argv via fakes; these tests catch mistakes the
// fakes cannot see (wrong flags, path handling). They skip when git is not
// installed.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

// runGit runs a git command in dir with a fixed test identity and fails the
// test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)
	out, err := exec.Command("git", fullArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// makeOriginRepo creates a local repository with one committed
// docker-compose.yml on branch main and returns its path.
func makeOriginRepo(t *testing.T) string {
	t.Helper()
	origin := t.TempDir()
	runGit(t, origin, "init", "-b", "main")
	writeFile(t, filepath.Join(origin, "docker-compose.yml"), "services:\n  app:\n    image: nginx:1.25\n")
	runGit(t, origin, "add", ".")
	runGit(t, origin, "commit", "-m", "initial")
	return origin
}

func TestIntegration_SyncClonesAndPulls(t *testing.T) {
	requireGit(t)
	origin := makeOriginRepo(t)
	repoDir := filepath.Join(t.TempDir(), "repo")

	s := NewRepoSync(origin, repoDir, "main", time.Minute, nil)

	// First sync clones.
	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if got := runGit(t, repoDir, "show", "HEAD:docker-compose.yml"); !strings.Contains(got, "nginx:1.25") {
		t.Errorf("expected cloned compose file, got %q", got)
	}

	// A new commit in origin arrives on the next sync.
	writeFile(t, filepath.Join(origin, "docker-compose.yml"), "services:\n  app:\n    image: nginx:1.26\n")
	runGit(t, origin, "commit", "-am", "bump image")

	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if got := runGit(t, repoDir, "show", "HEAD:docker-compose.yml"); !strings.Contains(got, "nginx:1.26") {
		t.Errorf("expected updated compose file, got %q", got)
	}
}

func TestIntegration_SyncFollowsChangedRemoteURL(t *testing.T) {
	requireGit(t)
	originA := makeOriginRepo(t)
	repoDir := filepath.Join(t.TempDir(), "repo")

	if err := NewRepoSync(originA, repoDir, "main", time.Minute, nil).Sync(context.Background()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// The configured repo_url changes to a different remote (as in an
	// ssh:// to https:// migration). Sync must follow it.
	originB := t.TempDir()
	runGit(t, originB, "init", "-b", "main")
	writeFile(t, filepath.Join(originB, "docker-compose.yml"), "services:\n  app:\n    image: nginx:2.0\n")
	runGit(t, originB, "add", ".")
	runGit(t, originB, "commit", "-m", "initial on new remote")

	if err := NewRepoSync(originB, repoDir, "main", time.Minute, nil).Sync(context.Background()); err != nil {
		t.Fatalf("sync after remote change: %v", err)
	}

	if got := runGit(t, repoDir, "remote", "get-url", "origin"); got != originB {
		t.Errorf("expected origin %q, got %q", originB, got)
	}
	if got := runGit(t, repoDir, "show", "HEAD:docker-compose.yml"); !strings.Contains(got, "nginx:2.0") {
		t.Errorf("expected content from new remote, got %q", got)
	}
}

func TestIntegration_RepoReaderReadsCommits(t *testing.T) {
	requireGit(t)
	origin := makeOriginRepo(t)
	repoDir := filepath.Join(t.TempDir(), "repo")

	s := NewRepoSync(origin, repoDir, "main", time.Minute, nil)
	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	firstSHA := runGit(t, repoDir, "rev-parse", "HEAD")

	// Advance origin by one commit and sync.
	writeFile(t, filepath.Join(origin, "docker-compose.yml"), "services:\n  app:\n    image: nginx:1.26\n")
	runGit(t, origin, "commit", "-am", "bump image")
	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	r := NewRepoReader(repoDir, time.Minute, nil)
	composePath := filepath.Join(repoDir, "docker-compose.yml")

	sha, err := r.HeadCommitSHA(context.Background())
	if err != nil {
		t.Fatalf("HeadCommitSHA: %v", err)
	}
	if sha == firstSHA || len(sha) != 40 {
		t.Errorf("expected new 40-char HEAD SHA, got %q (first was %q)", sha, firstSHA)
	}

	old, err := r.FileAtCommit(context.Background(), firstSHA, composePath)
	if err != nil {
		t.Fatalf("FileAtCommit: %v", err)
	}
	if !strings.Contains(string(old), "nginx:1.25") {
		t.Errorf("expected old compose content, got %q", old)
	}

	diff, err := r.DiffSinceCommit(context.Background(), firstSHA, composePath)
	if err != nil {
		t.Fatalf("DiffSinceCommit: %v", err)
	}
	if !strings.Contains(diff, "+    image: nginx:1.26") {
		t.Errorf("expected diff with new image line, got %q", diff)
	}
}
