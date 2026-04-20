// Package git manages a local clone of a remote Git repository.
// It clones the repository on first use and pulls on subsequent calls.
package git

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/polandy/skipper-cd/internal/command"
)

const defaultCloneDir = "/var/lib/skipper/repo"

// RepoSync keeps a local clone of a remote Git repository up to date.
type RepoSync struct {
	runner   command.Runner
	repoURL  string
	cloneDir string
	branch   string
}

func NewRepoSync(repoURL, cloneDir, branch string) *RepoSync {
	if cloneDir == "" {
		cloneDir = defaultCloneDir
	}
	return &RepoSync{runner: command.ShellRunner{}, repoURL: repoURL, cloneDir: cloneDir, branch: branch}
}

func newRepoSyncWithRunner(r command.Runner, repoURL, cloneDir, branch string) *RepoSync {
	return &RepoSync{runner: r, repoURL: repoURL, cloneDir: cloneDir, branch: branch}
}

// Sync clones the repository if the clone directory does not exist,
// or runs git pull if it does.
func (s *RepoSync) Sync(ctx context.Context) error {
	if _, err := os.Stat(s.cloneDir); os.IsNotExist(err) {
		return s.cloneRepository(ctx)
	}
	return s.pullLatestCommits(ctx)
}

// CloneDir returns the effective local clone directory.
func (s *RepoSync) CloneDir() string {
	return s.cloneDir
}

func (s *RepoSync) cloneRepository(ctx context.Context) error {
	slog.Info("cloning repository", "url", s.repoURL, "dir", s.cloneDir, "branch", s.branch)
	if err := os.MkdirAll(s.cloneDir, 0o755); err != nil {
		return fmt.Errorf("create clone dir: %w", err)
	}
	return s.runner.Run(ctx, "", nil, "git", "clone", "--branch", s.branch, s.repoURL, s.cloneDir)
}

func (s *RepoSync) pullLatestCommits(ctx context.Context) error {
	slog.Info("pulling latest commits", "dir", s.cloneDir, "branch", s.branch)
	if err := s.runner.Run(ctx, s.cloneDir, nil, "git", "fetch", "origin"); err != nil {
		return err
	}
	return s.runner.Run(ctx, s.cloneDir, nil, "git", "reset", "--hard", "origin/"+s.branch)
}

// RepoReader reads commit information from a local git repository.
// It implements the deploy.CommitReader interface.
type RepoReader struct {
	repoDir string
}

func NewRepoReader(repoDir string) *RepoReader {
	return &RepoReader{repoDir: repoDir}
}

// HeadCommitSHA returns the SHA of the current HEAD commit.
func (r *RepoReader) HeadCommitSHA(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "git", "-C", r.repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// DiffSinceCommit returns the git diff of a file between fromSHA and HEAD.
// Returns an empty string when fromSHA is empty (first deploy — no previous commit to diff against).
func (r *RepoReader) DiffSinceCommit(ctx context.Context, fromSHA, filePath string) (string, error) {
	if fromSHA == "" {
		return "", nil
	}
	output, err := exec.CommandContext(ctx, "git", "-C", r.repoDir, "diff", fromSHA+"..HEAD", "--", filePath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff: %w\n%s", err, output)
	}
	return string(output), nil
}
