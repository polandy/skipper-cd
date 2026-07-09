// Package command provides a Runner abstraction over os/exec so that
// callers can inject fake implementations in tests.
package command

import (
	"context"
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
// it is killed and its call returns an error.
type ShellRunner struct {
	timeout time.Duration
}

// NewShellRunner returns a ShellRunner that kills any single command running
// longer than timeout. A zero timeout disables the limit.
func NewShellRunner(timeout time.Duration) ShellRunner {
	return ShellRunner{timeout: timeout}
}

func (r ShellRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	ctx, cancel := r.commandContext(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Output executes a command and returns its captured stdout. Stderr passes
// through to the process stderr so failures remain visible in logs.
func (r ShellRunner) Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	ctx, cancel := r.commandContext(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

func (r ShellRunner) commandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.timeout)
}
