// Fast-forwarding a checkout skipper does not own: the working copy a stack's
// relative bind mounts resolve against via --project-directory (ADR-0060).
// Unlike the deploy clone, someone edits this one — so nothing here merges,
// rebases or resets, and uncommitted work is a reason to stop, never something
// to move out of the way.

package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/command"
)

// Reasons a fast-forward did not happen. They are sentinels so a caller can
// tell an operator's half-finished edit apart from a history that genuinely
// needs a human, without matching error strings.
var (
	// ErrDirtyWorktree means tracked files are modified in the checkout.
	// Untracked files do not count: a fast-forward never touches them, and an
	// operator's scratch file must not stop the checkout from advancing.
	ErrDirtyWorktree = errors.New("the checkout has uncommitted changes to tracked files")
	// ErrNotFastForward means the checked-out branch carries commits its
	// upstream does not, so advancing it would be a merge or a reset.
	ErrNotFastForward = errors.New("the checkout has diverged from its upstream branch")
	// ErrNoUpstream means the checked-out branch tracks nothing, so there is no
	// branch to fast-forward towards.
	ErrNoUpstream = errors.New("the checkout's branch has no upstream")
)

// Checkout is a local git working copy that skipper reads but does not own.
// It can be fast-forwarded to its upstream branch and nothing else.
type Checkout struct {
	runner outputRunner
	dir    string
}

// NewCheckout returns a Checkout on dir, running git with the given
// per-command timeout. A non-nil sink additionally receives git's stderr line
// by line. dir may be any directory inside the working copy — git resolves the
// enclosing repository itself.
func NewCheckout(dir string, commandTimeout time.Duration, sink command.LineSink) *Checkout {
	return &Checkout{runner: command.NewShellRunnerWithSink(commandTimeout, sink), dir: dir}
}

func newCheckoutWithRunner(r outputRunner, dir string) *Checkout {
	return &Checkout{runner: r, dir: dir}
}

// Dir returns the checkout directory.
func (c *Checkout) Dir() string { return c.dir }

// FastForward fetches the checkout's upstream branch and fast-forwards the
// working copy onto it, returning the commits it moved from and to — equal
// when it was already current.
//
// It refuses rather than repairs: a dirty tree (ErrDirtyWorktree), a branch
// with no upstream (ErrNoUpstream) and a diverged history (ErrNotFastForward)
// all return without running a single command that writes to the working copy.
// The fast-forward itself is a `merge --ff-only` onto the exact commit the
// ancestry check passed, so nothing can be rewritten between the two.
func (c *Checkout) FastForward(ctx context.Context) (from, to string, err error) {
	from, err = c.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("read HEAD of %s (is it a git checkout?): %w", c.dir, err)
	}
	dirty, err := c.git(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", "", fmt.Errorf("read the status of %s: %w", c.dir, err)
	}
	if dirty != "" {
		// The modified paths stay out of the message on purpose: it keys the
		// report-once dedup (ADR-0055), and every saved file would otherwise
		// re-announce the same standing condition. `git status` in that checkout
		// says which they are.
		return "", "", fmt.Errorf("%w in %s — commit or stash them so the next run can fast-forward it", ErrDirtyWorktree, c.dir)
	}
	if _, err := c.git(ctx, "fetch"); err != nil {
		return "", "", fmt.Errorf("fetch the upstream of %s: %w", c.dir, err)
	}
	to, err = c.git(ctx, "rev-parse", "@{u}")
	if err != nil {
		return "", "", fmt.Errorf("%w (%s) — set one with \"git branch --set-upstream-to=origin/<branch>\" in that checkout: %w", ErrNoUpstream, c.dir, err)
	}
	if to == from {
		return from, to, nil
	}
	base, err := c.git(ctx, "merge-base", from, to)
	if err != nil {
		return "", "", fmt.Errorf("compare %s with its upstream: %w", c.dir, err)
	}
	if base != from {
		return "", "", fmt.Errorf("%w (%s) — it has local commits the upstream does not; reconcile it by hand, skipper only fast-forwards", ErrNotFastForward, c.dir)
	}
	if _, err := c.git(ctx, "merge", "--ff-only", "--quiet", to); err != nil {
		return "", "", fmt.Errorf("fast-forward %s: %w", c.dir, err)
	}
	return from, to, nil
}

// git runs one git command against the checkout and returns its trimmed
// stdout. The working directory is left empty and -C carries the path, so the
// argv itself names the repository the command applies to.
func (c *Checkout) git(ctx context.Context, args ...string) (string, error) {
	out, err := c.runner.Output(ctx, "", "git", append([]string{"-C", c.dir}, args...)...)
	return strings.TrimSpace(string(out)), err
}
