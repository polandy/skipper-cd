// Package command provides a Runner abstraction over os/exec so that
// callers can inject fake implementations in tests.
package command

import (
	"context"
	"os"
	"os/exec"
)

// Runner executes shell commands. It is an interface so that tests can inject
// a fake implementation instead of running real docker/git commands.
type Runner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) error
}

// ShellRunner is the real Runner that executes commands via os/exec.
type ShellRunner struct{}

func (ShellRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
