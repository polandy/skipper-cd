// Package command provides a Runner abstraction over os/exec so that
// callers can inject fake implementations in tests.
package command

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// Runner executes shell commands. It is an interface so that tests can inject
// a fake implementation instead of running real docker/git commands.
type Runner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) error
}

// stackContextKey carries the deploy stack a command runs for, so the runner
// can attribute the command's line output to it in the log (deploy hooks set
// it; ADR-0038). Unexported so only WithStack can set it.
type stackContextKey struct{}

// WithStack returns a context that attributes a command's child-process output
// to the given deploy stack in the log sink. Callers that know which stack a
// command runs for (deploy hooks) set it; docker/git commands leave it unset.
func WithStack(ctx context.Context, stack string) context.Context {
	return context.WithValue(ctx, stackContextKey{}, stack)
}

// stackFromContext returns the stack set by WithStack, or "" if none.
func stackFromContext(ctx context.Context) string {
	s, _ := ctx.Value(stackContextKey{}).(string)
	return s
}

// defaultWaitDelay bounds how long cmd.Wait keeps blocking on a command's I/O
// pipes after the process itself has exited or been killed. A grandchild that
// inherits and holds the stdout pipe open (e.g. a backgrounded process a
// command spawns) would otherwise keep the pipe from reaching EOF, so cmd.Wait
// could block long after the command itself is gone. WaitDelay force-closes the
// pipes once it elapses so Run returns promptly. It only starts counting after
// exit or context cancellation, so a healthy long-running command is
// unaffected. (The nixos-rebuild self-update no longer relies on this: it runs
// fire-and-forget without --pipe, fully detached from this process — see
// internal/nixos.Rebuild and ADR-0014.)
const defaultWaitDelay = 10 * time.Second

// ShellRunner is the real Runner that executes commands via os/exec.
// A non-zero timeout limits each individual command; a command exceeding
// it is killed and its call returns an error. An optional LineSink
// additionally receives the child's output line by line; the output still
// reaches the process stdout/stderr either way.
type ShellRunner struct {
	timeout   time.Duration
	sink      LineSink
	waitDelay time.Duration // bounds Wait's post-exit I/O drain; see defaultWaitDelay
}

// NewShellRunner returns a ShellRunner that kills any single command running
// longer than timeout. A zero timeout disables the limit.
func NewShellRunner(timeout time.Duration) ShellRunner {
	return ShellRunner{timeout: timeout, waitDelay: defaultWaitDelay}
}

// NewShellRunnerWithSink is NewShellRunner with child output additionally
// tee'd line by line into sink. A nil sink behaves like NewShellRunner.
func NewShellRunnerWithSink(timeout time.Duration, sink LineSink) ShellRunner {
	return ShellRunner{timeout: timeout, sink: sink, waitDelay: defaultWaitDelay}
}

func (r ShellRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	ctx, cancel := r.commandContext(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = r.waitDelay
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if r.sink != nil {
		stack := stackFromContext(ctx)
		ow := &lineWriter{sink: r.sink, cmd: name, stream: "stdout", stack: stack}
		ew := &lineWriter{sink: r.sink, cmd: name, stream: "stderr", stack: stack}
		defer func() {
			_ = ow.Close()
			_ = ew.Close()
		}()
		cmd.Stdout = io.MultiWriter(os.Stdout, ow)
		cmd.Stderr = io.MultiWriter(os.Stderr, ew)
	}
	return cmd.Run()
}

// Output executes a command and returns its captured stdout. Stderr passes
// through to the process stderr so failures remain visible in logs; with a
// sink it is additionally captured line by line (stdout is not — it is
// data for the caller, not log output).
func (r ShellRunner) Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	ctx, cancel := r.commandContext(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = r.waitDelay
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if r.sink != nil {
		ew := &lineWriter{sink: r.sink, cmd: name, stream: "stderr"}
		defer func() { _ = ew.Close() }()
		cmd.Stderr = io.MultiWriter(os.Stderr, ew)
	}
	return cmd.Output()
}

func (r ShellRunner) commandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.timeout)
}
