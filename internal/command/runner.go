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

// ShellRunner is the real Runner that executes commands via os/exec.
// A non-zero timeout limits each individual command; a command exceeding
// it is killed and its call returns an error. An optional LineSink
// additionally receives the child's output line by line; the output still
// reaches the process stdout/stderr either way.
type ShellRunner struct {
	timeout time.Duration
	sink    LineSink
}

// NewShellRunner returns a ShellRunner that kills any single command running
// longer than timeout. A zero timeout disables the limit.
func NewShellRunner(timeout time.Duration) ShellRunner {
	return ShellRunner{timeout: timeout}
}

// NewShellRunnerWithSink is NewShellRunner with child output additionally
// tee'd line by line into sink. A nil sink behaves like NewShellRunner.
func NewShellRunnerWithSink(timeout time.Duration, sink LineSink) ShellRunner {
	return ShellRunner{timeout: timeout, sink: sink}
}

func (r ShellRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	ctx, cancel := r.commandContext(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if r.sink != nil {
		ow := &lineWriter{sink: r.sink, cmd: name, stream: "stdout"}
		ew := &lineWriter{sink: r.sink, cmd: name, stream: "stderr"}
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
