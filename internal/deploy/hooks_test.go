package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// hookStackDir lays out a one-stack repo and returns (baseDir, stackDir).
func hookStackDir(t *testing.T, name string) (string, string) {
	t.Helper()
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	return baseDir, stackDir
}

// indexOfShellCommand returns the index of the first `sh -c <cmd>` call, or -1.
func indexOfShellCommand(calls []runCall, cmd string) int {
	for i, c := range calls {
		if c.name == "sh" && slices.Contains(c.args, cmd) {
			return i
		}
	}
	return -1
}

func indexOfCompose(calls []runCall, subcommand string) int {
	for i, c := range calls {
		if c.name == "docker" && slices.Contains(c.args, subcommand) {
			return i
		}
	}
	return -1
}

func assertEnvContains(t *testing.T, env []string, want string) {
	t.Helper()
	if !slices.Contains(env, want) {
		t.Errorf("hook env missing %q; env=%v", want, env)
	}
}

func TestDeployStack_RunsPreDeployHookBeforePull(t *testing.T) {
	baseDir, stackDir := hookStackDir(t, "paperless")
	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	const backup = "pg_dump > /backup/pre.sql"
	stack := config.Stack{Name: "paperless", Hooks: config.Hooks{PreDeploy: []string{backup}}}

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hookIdx := indexOfShellCommand(runner.calls, backup)
	if hookIdx == -1 {
		t.Fatalf("pre_deploy hook was not run; calls=%v", runner.calls)
	}
	pullIdx := indexOfCompose(runner.calls, "pull")
	if pullIdx == -1 || hookIdx >= pullIdx {
		t.Errorf("pre_deploy hook must run before pull (hook=%d pull=%d)", hookIdx, pullIdx)
	}

	call := runner.calls[hookIdx]
	if call.dir != stackDir {
		t.Errorf("hook cwd = %q, want stack dir %q", call.dir, stackDir)
	}
	assertEnvContains(t, call.env, "SKIPPER_STACK=paperless")
	assertEnvContains(t, call.env, "SKIPPER_HOOK=pre_deploy")
}

func TestDeployStack_RunsPostDeployHookAfterUp(t *testing.T) {
	baseDir, _ := hookStackDir(t, "web")
	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	const smoke = "curl -fsS http://localhost/health"
	stack := config.Stack{Name: "web", Hooks: config.Hooks{PostDeploy: []string{smoke}}}

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hookIdx := indexOfShellCommand(runner.calls, smoke)
	if hookIdx == -1 {
		t.Fatalf("post_deploy hook was not run; calls=%v", runner.calls)
	}
	upIdx := indexOfCompose(runner.calls, "up")
	if upIdx == -1 || hookIdx <= upIdx {
		t.Errorf("post_deploy hook must run after up (hook=%d up=%d)", hookIdx, upIdx)
	}
	assertEnvContains(t, runner.calls[hookIdx].env, "SKIPPER_HOOK=post_deploy")
}

func TestDeployStack_PreDeployFailureAbortsBeforePull(t *testing.T) {
	baseDir, _ := hookStackDir(t, "db")
	const backup = "pg_dump > /backup/pre.sql"
	runner := &recordingRunner{errOnCommand: backup}
	d := newDeployerWithRunner(runner)

	var got events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) { got = e })

	stack := config.Stack{Name: "db", Hooks: config.Hooks{PreDeploy: []string{backup}}}
	state := newEmptyState()

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error when the pre_deploy hook fails")
	}
	// A failed backup must stop the deploy before anything is touched.
	assertCommandNotCalled(t, runner.calls, "pull")
	assertCommandNotCalled(t, runner.calls, "up")
	if state.Stacks["db"] != nil {
		t.Error("state must not be recorded after a pre_deploy failure")
	}
	if got.Status != events.StatusFailed {
		t.Errorf("event status = %q, want failed (no rollback for a pre-hook failure)", got.Status)
	}
}

func TestDeployStack_PostDeployFailureRollsBack(t *testing.T) {
	baseDir, stackDir := hookStackDir(t, "app")
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	cr := &fakeCommitReader{files: map[string][]byte{
		"old-sha:" + composePath: []byte(composeWithImage("nginx:1.24")),
	}}

	const smoke = "curl -fsS http://localhost/health"
	runner := &recordingRunner{errOnCommand: smoke}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	var got events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) { got = e })

	stack := config.Stack{Name: "app", Hooks: config.Hooks{PostDeploy: []string{smoke}}}
	state := &persistedState{
		Stacks:             map[string]stackFileHashes{"app": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error when the post_deploy hook fails")
	}
	if !errors.Is(err, ErrRolledBack) {
		t.Errorf("post_deploy failure must roll back (errors.Is ErrRolledBack); got %v", err)
	}
	if got.Status != events.StatusRolledBack {
		t.Errorf("event status = %q, want rolled_back", got.Status)
	}
	// The hook validated the new version; it must not re-run against the
	// restored old version — it appears exactly once across the whole sequence.
	var hookRuns int
	for _, c := range runner.calls {
		if c.name == "sh" && slices.Contains(c.args, smoke) {
			hookRuns++
		}
	}
	if hookRuns != 1 {
		t.Errorf("post_deploy hook ran %d times, want 1 (not re-run on the rollback leg)", hookRuns)
	}
}

// The running-hook indicator publishes {stack,phase,index,total} as each hook
// starts and clears to the zero value when a phase's hooks finish (ADR-0038).
func TestRunHooks_PublishesHookRunPhases(t *testing.T) {
	baseDir, _ := hookStackDir(t, "web")
	var runs []HookRun
	d := New(Config{Runner: &recordingRunner{}, HookRunSink: func(h HookRun) { runs = append(runs, h) }})

	stack := config.Stack{Name: "web", Hooks: config.Hooks{
		PreDeploy:  []string{"a", "b"},
		PostDeploy: []string{"c"},
	}}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []HookRun{
		{Stack: "web", Phase: "pre_deploy", Index: 1, Total: 2},
		{Stack: "web", Phase: "pre_deploy", Index: 2, Total: 2},
		{}, // pre-phase hooks finished → cleared
		{Stack: "web", Phase: "post_deploy", Index: 1, Total: 1},
		{}, // post-phase hooks finished → cleared
	}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("hookrun sequence:\n got %+v\nwant %+v", runs, want)
	}
	if d.CurrentHookRun() != (HookRun{}) {
		t.Errorf("running-hook state must be cleared after the deploy, got %+v", d.CurrentHookRun())
	}
}

func TestDeployStack_NoHooksRunNoShell(t *testing.T) {
	baseDir, _ := hookStackDir(t, "plain")
	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "plain"}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range runner.calls {
		if c.name == "sh" {
			t.Errorf("no hooks configured, but a shell command ran: %v", c.args)
		}
	}
}

// deadlineCapturingRunner records whether the context of each `sh` hook call
// carried a deadline, proving runHooks applies its per-hook timeout via the
// context only when timeout_seconds > 0.
type deadlineCapturingRunner struct {
	hookHadDeadline bool
	sawHook         bool
}

func (r *deadlineCapturingRunner) Run(ctx context.Context, _ string, _ []string, name string, args ...string) error {
	if name == "sh" {
		r.sawHook = true
		_, r.hookHadDeadline = ctx.Deadline()
	}
	return nil
}

func TestRunHooks_AppliesTimeoutOnlyWhenSet(t *testing.T) {
	baseDir, _ := hookStackDir(t, "svc")

	for _, tc := range []struct {
		name         string
		timeout      int
		wantDeadline bool
	}{
		{"explicit timeout wraps the context", 5, true},
		{"zero leaves the global ceiling to the runner", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &deadlineCapturingRunner{}
			d := newDeployerWithRunner(runner)
			stack := config.Stack{Name: "svc", Hooks: config.Hooks{
				PreDeploy:      []string{"echo hi"},
				TimeoutSeconds: tc.timeout,
			}}
			if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !runner.sawHook {
				t.Fatal("hook was not run")
			}
			if runner.hookHadDeadline != tc.wantDeadline {
				t.Errorf("hook ctx deadline present = %v, want %v", runner.hookHadDeadline, tc.wantDeadline)
			}
		})
	}
}
