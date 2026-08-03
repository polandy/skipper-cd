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
	"github.com/polandy/skipper-cd/internal/events"
)

// DefaultRepoDir is the local clone directory used when repo_dir is omitted.
// config.Load applies it up front so stacks_base_dir can be resolved against
// the effective clone path; NewRepoSync keeps the same fallback for callers
// that construct a RepoSync directly.
const DefaultRepoDir = "/var/lib/skipper/repo"

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
		repoDir = DefaultRepoDir
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
	slog.Info("cloning repository", "url", RedactURL(s.repoURL), "dir", s.repoDir, "branch", s.branch)
	if err := os.MkdirAll(s.repoDir, 0o755); err != nil {
		return fmt.Errorf("create repo dir: %w", err)
	}
	return s.runner.Run(ctx, "", nil, "git", "clone", "--branch", s.branch, s.repoURL, s.repoDir)
}

func (s *RepoSync) pullLatestCommits(ctx context.Context) error {
	// Debug: every reconcile tick syncs, so at info this reports the poll
	// cadence, not an event. A sync that moves the branch still shows up —
	// git fetch narrates the new refs and the deploy lines name the commit.
	slog.Debug("pulling latest commits", "dir", s.repoDir, "branch", s.branch)
	// Pin origin to the configured URL on every sync: the clone may predate
	// a repo_url config change (e.g. an ssh:// to https:// migration) and
	// would otherwise fetch from the stale remote forever.
	if err := s.runner.Run(ctx, s.repoDir, nil, "git", "remote", "set-url", "origin", s.repoURL); err != nil {
		return err
	}
	if err := s.runner.Run(ctx, s.repoDir, nil, "git", "fetch", "origin"); err != nil {
		return err
	}
	// --quiet: the reset prints "HEAD is now at <sha> <subject>" on every
	// sync, changed or not, and child output is captured into the log.
	return s.runner.Run(ctx, s.repoDir, nil, "git", "reset", "--hard", "--quiet", "origin/"+s.branch)
}

// redactedPlaceholder is what net/url's Redacted substitutes for a password;
// reused here so a masked username reads the same way.
const redactedPlaceholder = "xxxxx"

// sshScheme carries a login name rather than a credential in its userinfo,
// so it is the one scheme exempt from masking a lone username.
const sshScheme = "ssh"

// RedactURL returns rawURL with any credential in its userinfo masked, for
// logging and for the -validate report.
//
// A password is always masked. A lone username is masked too — a bare
// userinfo is in practice an access token (https://<token>@host/repo.git) —
// except on ssh://, where it is a login name (git@) and not a secret. The
// exemption is the allow-list rather than the masking, so a scheme not
// considered here errs towards hiding. URLs the parser rejects (e.g. scp-like
// syntax) are returned unchanged: they cannot carry a password.
func RedactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if u.User != nil && u.Scheme != sshScheme {
		if _, hasPassword := u.User.Password(); !hasPassword {
			u.User = url.User(redactedPlaceholder)
		}
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

// maxCommitsPerEvent bounds how many commits a single event carries, keeping
// the on-demand payload small when a stack changed across a long history.
const maxCommitsPerEvent = 50

// commitLogFormat separates commit fields with the unit-separator byte (0x1f),
// which cannot appear in a name or a subject, so parsing never splits wrongly.
// Fields: full SHA, author name, author date (RFC3339), subject.
const commitLogFormat = "--pretty=format:%H%x1f%an%x1f%aI%x1f%s"

// CommitsSinceCommit returns the commits in the range fromSHA..HEAD that touched
// any of the given files, newest first. The result feeds the diff panel's commit
// header. Returns nil when fromSHA is empty (first deploy) or no files are given.
func (r *RepoReader) CommitsSinceCommit(ctx context.Context, fromSHA string, filePaths []string) ([]events.CommitInfo, error) {
	if fromSHA == "" || len(filePaths) == 0 {
		return nil, nil
	}
	args := []string{"-C", r.repoDir, "log", "--no-merges",
		fmt.Sprintf("--max-count=%d", maxCommitsPerEvent), commitLogFormat,
		fromSHA + "..HEAD", "--"}
	args = append(args, filePaths...)
	output, err := r.runner.Output(ctx, "", "git", args...)
	if err != nil {
		return nil, fmt.Errorf("git log %s..HEAD: %w", fromSHA, err)
	}
	return parseCommitLog(string(output)), nil
}

// parseCommitLog turns commitLogFormat output into CommitInfo values, skipping
// any malformed line rather than failing the whole deploy over one bad record.
func parseCommitLog(output string) []events.CommitInfo {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}
	var commits []events.CommitInfo
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.SplitN(line, "\x1f", 4)
		if len(fields) != 4 {
			continue
		}
		date, _ := time.Parse(time.RFC3339, fields[2])
		commits = append(commits, events.CommitInfo{
			SHA:     fields[0],
			Author:  fields[1],
			Date:    date,
			Subject: fields[3],
		})
	}
	return commits
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
