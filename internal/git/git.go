// Package git manages a local clone of a remote Git repository.
// It clones the repository on first use and pulls on subsequent calls.
package git

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

const defaultCloneDir = "/var/lib/skipper/repo"

// Runner executes shell commands. Defined here independently of the deploy
// package to avoid cross-package dependencies.
type Runner interface {
	Run(dir string, env []string, name string, args ...string) error
}

// ShellRunner is the real Runner that executes commands via os/exec.
type ShellRunner struct{}

func (ShellRunner) Run(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Syncer keeps a local clone of a remote Git repository up to date.
type Syncer struct {
	runner   Runner
	repoURL  string
	cloneDir string
}

func NewSyncer(repoURL, cloneDir string) *Syncer {
	if cloneDir == "" {
		cloneDir = defaultCloneDir
	}
	return &Syncer{runner: ShellRunner{}, repoURL: repoURL, cloneDir: cloneDir}
}

func newSyncerWithRunner(r Runner, repoURL, cloneDir string) *Syncer {
	return &Syncer{runner: r, repoURL: repoURL, cloneDir: cloneDir}
}

// Sync clones the repository if the clone directory does not exist,
// or runs git pull if it does.
func (s *Syncer) Sync() error {
	if _, err := os.Stat(s.cloneDir); os.IsNotExist(err) {
		return s.cloneRepository()
	}
	return s.pullLatestCommits()
}

// CloneDir returns the effective local clone directory.
func (s *Syncer) CloneDir() string {
	return s.cloneDir
}

func (s *Syncer) cloneRepository() error {
	slog.Info("cloning repository", "url", s.repoURL, "dir", s.cloneDir)
	if err := os.MkdirAll(s.cloneDir, 0o755); err != nil {
		return fmt.Errorf("create clone dir: %w", err)
	}
	return s.runner.Run("", nil, "git", "clone", s.repoURL, s.cloneDir)
}

func (s *Syncer) pullLatestCommits() error {
	slog.Info("pulling latest commits", "dir", s.cloneDir)
	if err := s.runner.Run(s.cloneDir, nil, "git", "fetch", "origin"); err != nil {
		return err
	}
	return s.runner.Run(s.cloneDir, nil, "git", "reset", "--hard", "origin/master")
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
func (r *RepoReader) HeadCommitSHA() (string, error) {
	output, err := exec.Command("git", "-C", r.repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// DiffSinceCommit returns the git diff of a file between fromSHA and HEAD.
// Returns an empty string when fromSHA is empty (first deploy — no previous commit to diff against).
func (r *RepoReader) DiffSinceCommit(fromSHA, filePath string) (string, error) {
	if fromSHA == "" {
		return "", nil
	}
	output, err := exec.Command("git", "-C", r.repoDir, "diff", fromSHA+"..HEAD", "--", filePath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff: %w\n%s", err, output)
	}
	return string(output), nil
}
