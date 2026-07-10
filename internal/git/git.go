// Package git manages a local clone of a remote Git repository.
// It clones the repository on first use and pulls on subsequent calls.
package git

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/command"
)

const defaultRepoDir = "/var/lib/skipper/repo"

// RepoSync keeps a local clone of a remote Git repository up to date.
type RepoSync struct {
	runner  command.Runner
	repoURL string
	repoDir string
	branch  string
}

// NewRepoSync returns a RepoSync running git with the given per-command
// timeout. A non-nil sink additionally receives git's output line by line.
func NewRepoSync(repoURL, repoDir, branch string, commandTimeout time.Duration, sink command.LineSink) *RepoSync {
	if repoDir == "" {
		repoDir = defaultRepoDir
	}
	return &RepoSync{runner: command.NewShellRunnerWithSink(commandTimeout, sink), repoURL: repoURL, repoDir: repoDir, branch: branch}
}

func newRepoSyncWithRunner(r command.Runner, repoURL, repoDir, branch string) *RepoSync {
	return &RepoSync{runner: r, repoURL: repoURL, repoDir: repoDir, branch: branch}
}

// Sync clones the repository if no clone exists yet, or fetches and resets
// to the remote branch if it does. The check looks for repoDir/.git rather
// than repoDir itself: a failed first clone leaves an empty repoDir behind,
// and fetching in a non-repository would fail forever.
func (s *RepoSync) Sync(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(s.repoDir, ".git")); os.IsNotExist(err) {
		return s.cloneRepository(ctx)
	}
	return s.pullLatestCommits(ctx)
}

// RepoDir returns the effective local repository directory.
func (s *RepoSync) RepoDir() string {
	return s.repoDir
}

func (s *RepoSync) cloneRepository(ctx context.Context) error {
	slog.Info("cloning repository", "url", redactedURL(s.repoURL), "dir", s.repoDir, "branch", s.branch)
	if err := os.MkdirAll(s.repoDir, 0o755); err != nil {
		return fmt.Errorf("create repo dir: %w", err)
	}
	return s.runner.Run(ctx, "", nil, "git", "clone", "--branch", s.branch, s.repoURL, s.repoDir)
}

func (s *RepoSync) pullLatestCommits(ctx context.Context) error {
	slog.Info("pulling latest commits", "dir", s.repoDir, "branch", s.branch)
	if err := s.runner.Run(ctx, s.repoDir, nil, "git", "fetch", "origin"); err != nil {
		return err
	}
	return s.runner.Run(ctx, s.repoDir, nil, "git", "reset", "--hard", "origin/"+s.branch)
}

// redactedURL returns rawURL with any userinfo password masked, for logging.
// URLs the parser rejects (e.g. scp-like syntax) are returned unchanged —
// they cannot carry a password.
func redactedURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Redacted()
}

// outputRunner runs a command and captures its stdout. It is satisfied by
// command.ShellRunner and faked in tests.
type outputRunner interface {
	Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// RepoReader reads commit information from a local git repository.
// It implements the deploy.CommitReader interface.
type RepoReader struct {
	runner  outputRunner
	repoDir string
}

// NewRepoReader returns a RepoReader running git with the given per-command
// timeout. A non-nil sink additionally receives git's stderr line by line.
func NewRepoReader(repoDir string, commandTimeout time.Duration, sink command.LineSink) *RepoReader {
	return &RepoReader{runner: command.NewShellRunnerWithSink(commandTimeout, sink), repoDir: repoDir}
}

func newRepoReaderWithRunner(r outputRunner, repoDir string) *RepoReader {
	return &RepoReader{runner: r, repoDir: repoDir}
}

// HeadCommitSHA returns the SHA of the current HEAD commit.
func (r *RepoReader) HeadCommitSHA(ctx context.Context) (string, error) {
	output, err := r.runner.Output(ctx, "", "git", "-C", r.repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// FileAtCommit returns the contents of a file at a specific git commit.
func (r *RepoReader) FileAtCommit(ctx context.Context, commitSHA, filePath string) ([]byte, error) {
	relPath, err := filepath.Rel(r.repoDir, filePath)
	if err != nil {
		return nil, fmt.Errorf("make relative path: %w", err)
	}
	output, err := r.runner.Output(ctx, "", "git", "-C", r.repoDir, "show", commitSHA+":"+relPath)
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s: %w", commitSHA, relPath, err)
	}
	return output, nil
}

// DiffSinceCommit returns the git diff of a file between fromSHA and HEAD.
// Returns an empty string when fromSHA is empty (first deploy — no previous commit to diff against).
func (r *RepoReader) DiffSinceCommit(ctx context.Context, fromSHA, filePath string) (string, error) {
	if fromSHA == "" {
		return "", nil
	}
	output, err := r.runner.Output(ctx, "", "git", "-C", r.repoDir, "diff", fromSHA+"..HEAD", "--", filePath)
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(output), nil
}
