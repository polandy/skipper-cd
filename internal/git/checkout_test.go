package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// outputScript is a fake outputRunner that answers each git invocation from a
// table keyed by the sub-command, and records every argv it saw. The record is
// the positive signal the refusal tests assert against: a test claiming the
// working copy was not touched has to name the commands that did run.
type outputScript struct {
	replies map[string]string
	fails   map[string]error
	calls   [][]string
}

func newOutputScript() *outputScript {
	return &outputScript{replies: map[string]string{}, fails: map[string]error{}}
}

func (s *outputScript) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	s.calls = append(s.calls, append([]string{name}, args...))
	// args are "-C <dir> <subcommand> ..." — the sub-command keys the table.
	key := ""
	if len(args) > 2 {
		key = args[2]
	}
	if err, ok := s.fails[key]; ok {
		return nil, err
	}
	return []byte(s.replies[key]), nil
}

// subcommands lists the git sub-command of every recorded call, in order.
func (s *outputScript) subcommands() []string {
	var got []string
	for _, c := range s.calls {
		if len(c) > 3 {
			got = append(got, c[3])
		}
	}
	return got
}

// writingSubcommands are the git sub-commands that change the working copy.
// A refusal must run none of them.
var writingSubcommands = map[string]bool{"merge": true, "reset": true, "rebase": true, "checkout": true, "pull": true, "clean": true, "stash": true}

func assertNothingWritten(t *testing.T, s *outputScript) {
	t.Helper()
	if len(s.calls) == 0 {
		t.Fatal("expected the refusal to have run at least one git command; recorded none, so this assertion proves nothing")
	}
	for _, sub := range s.subcommands() {
		if writingSubcommands[sub] {
			t.Fatalf("git %s ran against the checkout; a refusal must not touch it (calls: %v)", sub, s.subcommands())
		}
	}
}

const (
	oldSHA = "1111111111111111111111111111111111111111"
	newSHA = "2222222222222222222222222222222222222222"
)

// upToDateScript answers every step of a checkout that is already current.
func upToDateScript() *outputScript {
	s := newOutputScript()
	s.replies["rev-parse"] = oldSHA
	return s
}

func TestFastForward_MergesFastForwardOnlyOntoTheVerifiedUpstreamCommit(t *testing.T) {
	s := newOutputScript()
	// rev-parse answers HEAD first and @{u} second, so the table cannot key both.
	seen := 0
	runner := &sequencedScript{script: s, revParse: []string{oldSHA, newSHA}, seen: &seen}
	s.replies["merge-base"] = oldSHA
	c := newCheckoutWithRunner(runner, "/srv/modules")

	from, to, err := c.FastForward(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if from != oldSHA || to != newSHA {
		t.Fatalf("expected the move %s -> %s, got %s -> %s", oldSHA, newSHA, from, to)
	}
	want := []string{"rev-parse", "status", "fetch", "rev-parse", "merge-base", "merge"}
	if got := strings.Join(s.subcommands(), ","); got != strings.Join(want, ",") {
		t.Fatalf("expected the sequence %v, got %v", want, s.subcommands())
	}
	last := s.calls[len(s.calls)-1]
	// --ff-only onto the exact commit merge-base approved: never a bare merge,
	// and never a ref that could have moved since the ancestry check.
	if strings.Join(last, " ") != fmt.Sprintf("git -C /srv/modules merge --ff-only --quiet %s", newSHA) {
		t.Fatalf("unexpected fast-forward argv: %v", last)
	}
}

func TestFastForward_ReportsNoMoveWhenAlreadyCurrent(t *testing.T) {
	s := upToDateScript()
	c := newCheckoutWithRunner(s, "/srv/modules")

	from, to, err := c.FastForward(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if from != to {
		t.Fatalf("expected an unchanged checkout to report from == to, got %s -> %s", from, to)
	}
	assertNothingWritten(t, s)
}

func TestFastForward_RefusesADirtyWorktreeWithoutTouchingIt(t *testing.T) {
	s := newOutputScript()
	s.replies["rev-parse"] = oldSHA
	s.replies["status"] = " M monitoring/grafana/dashboards/host.json"
	c := newCheckoutWithRunner(s, "/srv/modules")

	if _, _, err := c.FastForward(context.Background()); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("expected ErrDirtyWorktree, got %v", err)
	}
	assertNothingWritten(t, s)
	// It stops before the fetch: a dirty tree is decided locally, without a
	// network round trip on every reconcile tick.
	if got := strings.Join(s.subcommands(), ","); got != "rev-parse,status" {
		t.Fatalf("expected the refusal to stop after status, got %v", s.subcommands())
	}
}

func TestFastForward_IgnoresUntrackedFiles(t *testing.T) {
	s := upToDateScript()
	c := newCheckoutWithRunner(s, "/srv/modules")

	if _, _, err := c.FastForward(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The status call excludes untracked files, so a scratch file left in the
	// checkout can never stop it from advancing.
	for _, call := range s.calls {
		if len(call) > 3 && call[3] == "status" {
			if !argPresent(call, "--untracked-files=no") {
				t.Fatalf("expected the status call to exclude untracked files, got %v", call)
			}
			return
		}
	}
	t.Fatal("no status call recorded")
}

func TestFastForward_RefusesADivergedHistoryWithoutTouchingIt(t *testing.T) {
	s := newOutputScript()
	seen := 0
	runner := &sequencedScript{script: s, revParse: []string{oldSHA, newSHA}, seen: &seen}
	// A merge base that is neither side: the branch carries its own commits.
	s.replies["merge-base"] = "3333333333333333333333333333333333333333"
	c := newCheckoutWithRunner(runner, "/srv/modules")

	if _, _, err := c.FastForward(context.Background()); !errors.Is(err, ErrNotFastForward) {
		t.Fatalf("expected ErrNotFastForward, got %v", err)
	}
	assertNothingWritten(t, s)
}

func TestFastForward_RefusesABranchWithNoUpstream(t *testing.T) {
	s := newOutputScript()
	s.replies["rev-parse"] = oldSHA
	c := newCheckoutWithRunner(&failingUpstream{script: s}, "/srv/modules")

	if _, _, err := c.FastForward(context.Background()); !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("expected ErrNoUpstream, got %v", err)
	}
	assertNothingWritten(t, s)
}

func TestFastForward_SurfacesAFailedFetch(t *testing.T) {
	s := newOutputScript()
	s.replies["rev-parse"] = oldSHA
	s.fails["fetch"] = errors.New("could not resolve host")
	c := newCheckoutWithRunner(s, "/srv/modules")

	_, _, err := c.FastForward(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/srv/modules") {
		t.Fatalf("expected the fetch error to name the checkout, got %v", err)
	}
	assertNothingWritten(t, s)
}

// Every step's failure names the checkout, so a log line says which tree the
// run could not advance rather than only that git exited non-zero.
func TestFastForward_EveryFailedStepNamesTheCheckout(t *testing.T) {
	for _, step := range []string{"rev-parse", "status", "merge-base", "merge"} {
		t.Run(step, func(t *testing.T) {
			s := newOutputScript()
			seen := 0
			runner := &sequencedScript{script: s, revParse: []string{oldSHA, newSHA}, seen: &seen}
			s.replies["merge-base"] = oldSHA
			s.fails[step] = errors.New("exit status 128")
			c := newCheckoutWithRunner(runner, "/srv/modules")

			_, _, err := c.FastForward(context.Background())
			if err == nil || !strings.Contains(err.Error(), "/srv/modules") {
				t.Fatalf("expected the %s failure to name the checkout, got %v", step, err)
			}
		})
	}
}

func TestNewCheckout_KeepsTheGivenDirectory(t *testing.T) {
	if dir := NewCheckout("/srv/modules", 0, nil).Dir(); dir != "/srv/modules" {
		t.Fatalf("expected /srv/modules, got %s", dir)
	}
}

// sequencedScript answers successive rev-parse calls from a list (HEAD, then
// @{u}) and delegates everything else to the script.
type sequencedScript struct {
	script   *outputScript
	revParse []string
	seen     *int
}

func (s *sequencedScript) Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	if len(args) > 2 && args[2] == "rev-parse" {
		s.script.calls = append(s.script.calls, append([]string{name}, args...))
		if err, ok := s.script.fails["rev-parse"]; ok {
			return nil, err
		}
		i := *s.seen
		*s.seen++
		return []byte(s.revParse[i]), nil
	}
	return s.script.Output(ctx, dir, name, args...)
}

// failingUpstream answers HEAD but fails the @{u} lookup, as git does on a
// branch that tracks nothing.
type failingUpstream struct{ script *outputScript }

func (f *failingUpstream) Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	if len(args) > 3 && args[2] == "rev-parse" && args[3] == "@{u}" {
		f.script.calls = append(f.script.calls, append([]string{name}, args...))
		return nil, errors.New("exit status 128")
	}
	return f.script.Output(ctx, dir, name, args...)
}

func argPresent(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}
