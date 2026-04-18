package deploy

import (
	"os"
	"os/exec"
)

// Runner executes shell commands. It is an interface so that tests can inject
// a fake implementation instead of running real docker/git commands.
// This is the Go equivalent of a mockable dependency in Java.
type Runner interface {
	// Run executes a command in the given directory with the given environment.
	Run(dir string, env []string, name string, args ...string) error
}

// ShellRunner is the real Runner that executes commands via os/exec.
type ShellRunner struct{}

func (ShellRunner) Run(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	return cmd.Run()
}
