package git

// Integration tests against a real local git repository. The unit tests in
// git_test.go assert the exact argv via fakes; these tests catch mistakes the
// fakes cannot see (wrong flags, path handling). They skip when git is not
// installed.

import (
	"context"
	"errors"
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

func TestIntegration_CommitsSinceCommitRestrictedToFiles(t *testing.T) {
	requireGit(t)
	origin := makeOriginRepo(t)
	repoDir := filepath.Join(t.TempDir(), "repo")

	s := NewRepoSync(origin, repoDir, "main", time.Minute, nil)
	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	baseSHA := runGit(t, repoDir, "rev-parse", "HEAD")

	// Two more commits: one touches the compose file, one touches an unrelated
	// file. Only the compose commit must show up when we restrict to it.
	writeFile(t, filepath.Join(origin, "docker-compose.yml"), "services:\n  app:\n    image: nginx:1.26\n")
	runGit(t, origin, "commit", "-am", "bump image to 1.26")
	writeFile(t, filepath.Join(origin, "README.md"), "docs\n")
	runGit(t, origin, "add", ".")
	runGit(t, origin, "commit", "-m", "add readme")
	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	r := NewRepoReader(repoDir, time.Minute, nil)
	composePath := filepath.Join(repoDir, "docker-compose.yml")

	commits, err := r.CommitsSinceCommit(context.Background(), baseSHA, []string{composePath})
	if err != nil {
		t.Fatalf("CommitsSinceCommit: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected exactly the compose commit, got %d: %+v", len(commits), commits)
	}
	if commits[0].Subject != "bump image to 1.26" {
		t.Errorf("unexpected subject: %q", commits[0].Subject)
	}
	if commits[0].Author != "test" {
		t.Errorf("expected author 'test', got %q", commits[0].Author)
	}
	if len(commits[0].SHA) != 40 {
		t.Errorf("expected full 40-char SHA, got %q", commits[0].SHA)
	}
	if commits[0].Date.IsZero() {
		t.Error("expected a parsed commit date")
	}
}

// makeTrackingClone clones origin into a fresh directory with an upstream
// branch configured, as an operator's working copy has.
func makeTrackingClone(t *testing.T, origin string) string {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "work")
	out, err := exec.Command("git", "clone", "--branch", "main", origin, clone).CombinedOutput()
	if err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	return clone
}

func TestIntegration_FastForwardAdvancesTheCheckout(t *testing.T) {
	requireGit(t)
	origin := makeOriginRepo(t)
	clone := makeTrackingClone(t, origin)

	writeFile(t, filepath.Join(origin, "dashboards.json"), "{\"v\":2}\n")
	runGit(t, origin, "add", ".")
	runGit(t, origin, "commit", "-m", "new dashboard")
	want := runGit(t, origin, "rev-parse", "HEAD")

	from, to, err := NewCheckout(clone, time.Minute, nil).FastForward(context.Background())
	if err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	if to != want {
		t.Fatalf("expected the checkout at %s, got %s", want, to)
	}
	if from == to {
		t.Fatal("expected the checkout to have moved")
	}
	if head := runGit(t, clone, "rev-parse", "HEAD"); head != want {
		t.Fatalf("the working copy is at %s, not the reported %s", head, want)
	}
	// The point of the phase: the mounted file is the new one.
	content, err := os.ReadFile(filepath.Join(clone, "dashboards.json"))
	if err != nil || strings.TrimSpace(string(content)) != "{\"v\":2}" {
		t.Fatalf("expected the fast-forwarded content on disk, got %q (%v)", content, err)
	}
}

func TestIntegration_FastForwardIsANoOpWhenAlreadyCurrent(t *testing.T) {
	requireGit(t)
	clone := makeTrackingClone(t, makeOriginRepo(t))

	from, to, err := NewCheckout(clone, time.Minute, nil).FastForward(context.Background())
	if err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	if from != to {
		t.Fatalf("expected no move on an already-current checkout, got %s -> %s", from, to)
	}
}

func TestIntegration_FastForwardKeepsUncommittedWork(t *testing.T) {
	requireGit(t)
	origin := makeOriginRepo(t)
	clone := makeTrackingClone(t, origin)
	head := runGit(t, clone, "rev-parse", "HEAD")

	// The operator is mid-edit on a tracked file, and the upstream has moved on.
	writeFile(t, filepath.Join(clone, "docker-compose.yml"), "services:\n  app:\n    image: nginx:9.9-edit\n")
	writeFile(t, filepath.Join(origin, "dashboards.json"), "{\"v\":2}\n")
	runGit(t, origin, "add", ".")
	runGit(t, origin, "commit", "-m", "new dashboard")

	if _, _, err := NewCheckout(clone, time.Minute, nil).FastForward(context.Background()); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("expected ErrDirtyWorktree, got %v", err)
	}
	if now := runGit(t, clone, "rev-parse", "HEAD"); now != head {
		t.Fatalf("the refusal moved the checkout from %s to %s", head, now)
	}
	edit, err := os.ReadFile(filepath.Join(clone, "docker-compose.yml"))
	if err != nil || !strings.Contains(string(edit), "9.9-edit") {
		t.Fatalf("the refusal lost the operator's edit: %q (%v)", edit, err)
	}
}

func TestIntegration_FastForwardIgnoresUntrackedFiles(t *testing.T) {
	requireGit(t)
	origin := makeOriginRepo(t)
	clone := makeTrackingClone(t, origin)
	writeFile(t, filepath.Join(clone, "scratch.txt"), "notes\n")
	writeFile(t, filepath.Join(origin, "dashboards.json"), "{\"v\":2}\n")
	runGit(t, origin, "add", ".")
	runGit(t, origin, "commit", "-m", "new dashboard")
	want := runGit(t, origin, "rev-parse", "HEAD")

	if _, _, err := NewCheckout(clone, time.Minute, nil).FastForward(context.Background()); err != nil {
		t.Fatalf("an untracked file must not stop the fast-forward, got %v", err)
	}
	if head := runGit(t, clone, "rev-parse", "HEAD"); head != want {
		t.Fatalf("expected the checkout at %s, got %s", want, head)
	}
}

func TestIntegration_FastForwardRefusesADivergedCheckout(t *testing.T) {
	requireGit(t)
	origin := makeOriginRepo(t)
	clone := makeTrackingClone(t, origin)

	// Both sides commit: the checkout now carries a commit the upstream does not.
	writeFile(t, filepath.Join(clone, "local.txt"), "local\n")
	runGit(t, clone, "add", ".")
	runGit(t, clone, "commit", "-m", "local work")
	local := runGit(t, clone, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(origin, "dashboards.json"), "{\"v\":2}\n")
	runGit(t, origin, "add", ".")
	runGit(t, origin, "commit", "-m", "new dashboard")

	if _, _, err := NewCheckout(clone, time.Minute, nil).FastForward(context.Background()); !errors.Is(err, ErrNotFastForward) {
		t.Fatalf("expected ErrNotFastForward, got %v", err)
	}
	if head := runGit(t, clone, "rev-parse", "HEAD"); head != local {
		t.Fatalf("the refusal rewrote the checkout: %s -> %s", local, head)
	}
}

func TestIntegration_FastForwardRefusesABranchWithNoUpstream(t *testing.T) {
	requireGit(t)
	clone := makeTrackingClone(t, makeOriginRepo(t))
	runGit(t, clone, "checkout", "-b", "detour")

	if _, _, err := NewCheckout(clone, time.Minute, nil).FastForward(context.Background()); !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("expected ErrNoUpstream, got %v", err)
	}
}
