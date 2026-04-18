// Package git manages a local clone of a remote Git repository.
// It clones the repository on first use and pulls on subsequent calls.
package git

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
	return s.runner.Run(s.cloneDir, nil, "git", "pull")
}
