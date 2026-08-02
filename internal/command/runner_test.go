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

// The health poller reads vars_file through this seam so its `compose ps`
// interpolates ${VAR} the same way the deploy path does.
func TestShellRunner_OutputRunsWithConfiguredEnv(t *testing.T) {
	requireCommands(t, "sh")

	r := NewShellRunnerWithOutputEnv(0, []string{"DOMAIN=example.com"})
	out, err := r.Output(context.Background(), "", "sh", "-c", "printf %s \"$DOMAIN\"")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "example.com" {
		t.Errorf("expected the configured env to reach the command, got %q", string(out))
	}
}

// A nil env must not blank the child's environment — that would strip PATH and
// break every existing Output caller.
func TestShellRunner_OutputWithNilEnvInheritsProcessEnvironment(t *testing.T) {
	requireCommands(t, "sh")

	t.Setenv("SKIPPER_ENV_PROBE", "inherited")
	for _, r := range []ShellRunner{NewShellRunner(0), NewShellRunnerWithOutputEnv(0, nil)} {
		out, err := r.Output(context.Background(), "", "sh", "-c", "printf %s \"$SKIPPER_ENV_PROBE\"")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(out) != "inherited" {
			t.Errorf("expected the process environment to be inherited, got %q", string(out))
		}
	}
}

// The configured env replaces the process environment rather than layering onto
// it, matching exec.Cmd semantics — so callers must pass a full environment
// (deploy.BaseEnv returns os.Environ() plus vars_file).
func TestShellRunner_OutputEnvReplacesProcessEnvironment(t *testing.T) {
	requireCommands(t, "sh")

	t.Setenv("SKIPPER_ENV_PROBE", "inherited")
	r := NewShellRunnerWithOutputEnv(0, []string{"DOMAIN=example.com"})
	out, err := r.Output(context.Background(), "", "sh", "-c", "printf %s \"$SKIPPER_ENV_PROBE\"")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "" {
		t.Errorf("expected the configured env to replace the process environment, got %q", string(out))
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

func TestShellRunner_RunSendsChildStdoutAndStderrToSink(t *testing.T) {
	requireCommands(t, "sh")

	sink := &recordingSink{}
	runner := NewShellRunnerWithSink(0, sink)

	if err := runner.Run(context.Background(), "", nil, "sh", "-c", "echo out; echo err >&2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stdout, stderr []string
	for _, l := range sink.all() {
		if l.cmd != "sh" {
			t.Errorf("expected cmd %q, got %q", "sh", l.cmd)
		}
		switch l.stream {
		case "stdout":
			stdout = append(stdout, l.line)
		case "stderr":
			stderr = append(stderr, l.line)
		default:
			t.Errorf("unexpected stream %q", l.stream)
		}
	}
	if len(stdout) != 1 || stdout[0] != "out" {
		t.Errorf("unexpected stdout lines: %v", stdout)
	}
	if len(stderr) != 1 || stderr[0] != "err" {
		t.Errorf("unexpected stderr lines: %v", stderr)
	}
}

// A command run under WithStack has its output attributed to that stack in the
// sink (deploy hooks; ADR-0038); a plain command carries no stack.
func TestShellRunner_AttributesOutputToStackFromContext(t *testing.T) {
	requireCommands(t, "sh")
	sink := &recordingSink{}
	runner := NewShellRunnerWithSink(0, sink)

	ctx := WithStack(context.Background(), "nextcloud")
	if err := runner.Run(ctx, "", nil, "sh", "-c", "echo backing up"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := sink.all()
	if len(got) != 1 || got[0].stack != "nextcloud" {
		t.Fatalf("expected the line attributed to nextcloud, got %+v", got)
	}

	sink.lines = nil
	if err := runner.Run(context.Background(), "", nil, "sh", "-c", "echo plain"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := sink.all(); len(got) != 1 || got[0].stack != "" {
		t.Errorf("a command with no WithStack must carry an empty stack, got %+v", got)
	}
}

func TestShellRunner_RunFlushesUnterminatedLastLine(t *testing.T) {
	requireCommands(t, "printf")

	sink := &recordingSink{}
	if err := NewShellRunnerWithSink(0, sink).Run(context.Background(), "", nil, "printf", "no newline"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sink.all()
	if len(got) != 1 || got[0].line != "no newline" {
		t.Errorf("expected the unterminated line flushed after exit, got %+v", got)
	}
}

func TestShellRunner_OutputTeesStderrButReturnsStdoutUncaptured(t *testing.T) {
	requireCommands(t, "sh")

	sink := &recordingSink{}
	out, err := NewShellRunnerWithSink(0, sink).Output(context.Background(), "", "sh", "-c", "echo data; echo progress >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(out) != "data\n" {
		t.Errorf("expected stdout returned as data, got %q", string(out))
	}
	got := sink.all()
	if len(got) != 1 || got[0] != (sinkLine{"sh", "stderr", "progress", ""}) {
		t.Errorf("expected only the stderr line in the sink, got %+v", got)
	}
}

// TestShellRunner_ReturnsWhenGrandchildHoldsPipeOpen reproduces the self-update
// shutdown wedge (ADR-0014): a backgrounded grandchild inherits the command's
// stdout pipe and keeps it open after the parent exits. Because a sink routes
// stdout through an os.Pipe, cmd.Wait blocks until the pipe reaches EOF — i.e.
// until the grandchild exits. WaitDelay bounds that wait and force-closes the
// pipe, so Run returns promptly instead of hanging shutdown forever.
func TestShellRunner_ReturnsWhenGrandchildHoldsPipeOpen(t *testing.T) {
	requireCommands(t, "sh", "sleep")

	// A sink is required to trigger the pipe path (production sets one when the
	// UI is enabled); without it stdout is the real fd and never wedges.
	runner := ShellRunner{sink: &recordingSink{}, waitDelay: 200 * time.Millisecond}

	start := time.Now()
	// sh exits immediately; the backgrounded sleep holds stdout open for 10s.
	_ = runner.Run(context.Background(), "", nil, "sh", "-c", "sleep 10 &")
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Fatalf("Run blocked on a pipe held open by a grandchild (%v); WaitDelay did not force it closed", elapsed)
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
