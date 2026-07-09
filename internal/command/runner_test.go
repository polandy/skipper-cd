package command

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// These tests execute trivial real commands (true, echo, sleep): this package
// is the process boundary, so faking exec here would test nothing.

func requireCommands(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s not available: %v", name, err)
		}
	}
}

func TestShellRunner_RunsCommand(t *testing.T) {
	requireCommands(t, "true")

	if err := NewShellRunner(0).Run(context.Background(), "", nil, "true"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShellRunner_KillsCommandAfterTimeout(t *testing.T) {
	requireCommands(t, "sleep")

	runner := NewShellRunner(100 * time.Millisecond)

	start := time.Now()
	err := runner.Run(context.Background(), "", nil, "sleep", "5")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when command exceeds the per-command timeout")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("command was not killed by the timeout, ran %v", elapsed)
	}
}

func TestShellRunner_OutputCapturesStdout(t *testing.T) {
	requireCommands(t, "echo")

	out, err := NewShellRunner(0).Output(context.Background(), "", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("expected %q, got %q", "hello\n", string(out))
	}
}

func TestShellRunner_OutputKillsCommandAfterTimeout(t *testing.T) {
	requireCommands(t, "sleep")

	start := time.Now()
	_, err := NewShellRunner(100*time.Millisecond).Output(context.Background(), "", "sleep", "5")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when command exceeds the per-command timeout")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("command was not killed by the timeout, ran %v", elapsed)
	}
}

func TestShellRunner_RespectsCallerContext(t *testing.T) {
	requireCommands(t, "sleep")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	if err := NewShellRunner(time.Minute).Run(ctx, "", nil, "sleep", "5"); err == nil {
		t.Fatal("expected error when caller context is cancelled")
	}
}
