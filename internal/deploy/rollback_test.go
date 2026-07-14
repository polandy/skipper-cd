package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// --- rollback tests ---

// selectiveErrRunner fails only on specific compose subcommands.
type selectiveErrRunner struct {
	calls       []runCall
	errCommands map[string]bool // compose subcommands that should fail (e.g. "up")
}

func (r *selectiveErrRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	for _, a := range args {
		if r.errCommands[a] {
			return fmt.Errorf("simulated error for command: %s", a)
		}
	}
	return nil
}

func TestDeployStack_RollbackOnUpFailure(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	oldCompose := composeWithImage("nginx:1.25")

	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(oldCompose),
		},
	}

	// Fail on "up" but succeed on everything else (including rollback up).
	runner := &selectiveErrRunner{errCommands: map[string]bool{"up": true}}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	stack := config.Stack{Name: "mystack"}
	state := &persistedState{
		Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed deploy")
	}

	// The rollback also calls "up", so it will also fail. Let's test the
	// case where rollback succeeds instead — need a smarter runner.
	// This test verifies the error message format when rollback also fails.
	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("expected 'rollback also failed' in error, got: %s", err.Error())
	}
}

func TestDeployStack_RollbackSucceeds(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	oldCompose := composeWithImage("nginx:1.25")

	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(oldCompose),
		},
	}

	// Use a runner that fails only the first "up" call (the deploy), not the rollback "up".
	upCallCount := 0
	runner := &countingErrRunner{failOnNthUp: 1, upCount: &upCallCount}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	stack := config.Stack{Name: "mystack"}
	state := &persistedState{
		Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed deploy")
	}
	if !errors.Is(err, ErrRolledBack) {
		t.Errorf("expected error wrapping ErrRolledBack, got: %s", err.Error())
	}

	// Without health_check the rollback up must stay a plain up -d.
	for _, c := range runner.calls {
		if strings.Contains(strings.Join(c.args, " "), "--wait") {
			t.Errorf("expected no --wait on any call without health_check, got %v", c.args)
		}
	}

	// State should NOT be updated (the deploy failed).
	if state.Stacks["mystack"] != nil && state.Stacks["mystack"]["old"] != "oldhash" {
		t.Error("state should not be updated after a rolled-back deploy")
	}
}

func TestDeployStack_RollbackSkippedWithoutPreviousCommit(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	runner := &recordingRunner{errOnCommand: "up"}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "mystack"}
	state := newEmptyState()

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed deploy")
	}
	// Without commitReader or LastDeployedCommit, rollback should fail with "no previous commit".
	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("expected 'rollback also failed' in error, got: %s", err.Error())
	}
}

func TestDeployAllStacks_EmitsRolledBackEvent(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	oldCompose := composeWithImage("nginx:1.25")

	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(oldCompose),
		},
	}

	upCallCount := 0
	runner := &countingErrRunner{failOnNthUp: 1, upCount: &upCallCount}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	var emitted []events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) {
		emitted = append(emitted, e)
	})

	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "mystack"}},
	}

	// Pre-seed state with old hashes and commit.
	state, _ := loadPersistedDeployState(d.stateDir)
	state.Stacks["mystack"] = stackFileHashes{"old": "oldhash"}
	state.LastDeployedCommit = "old-sha"
	_ = saveDeployState(d.stateDir, state)

	d.DeployAllStacks(context.Background(), cfg)

	var rolledBack *events.DeployEvent
	for i := range emitted {
		if emitted[i].Status == events.StatusRolledBack {
			rolledBack = &emitted[i]
			break
		}
	}
	if rolledBack == nil {
		t.Fatal("expected a rolled_back event")
	}
	if rolledBack.Stack != "mystack" {
		t.Errorf("expected stack 'mystack', got %q", rolledBack.Stack)
	}
}

func TestDeployAllStacks_EmitsRolledBackUnhealthyEvent(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(composeWithImage("nginx:1.25")),
		},
	}

	// Every up fails: the health-gated deploy up and the rollback up alike.
	runner := &selectiveErrRunner{errCommands: map[string]bool{"up": true}}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	var emitted []events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) {
		emitted = append(emitted, e)
	})

	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "mystack", HealthCheck: &config.HealthCheck{TimeoutSeconds: 45}}},
	}

	state, _ := loadPersistedDeployState(d.stateDir)
	state.Stacks["mystack"] = stackFileHashes{"old": "oldhash"}
	state.LastDeployedCommit = "old-sha"
	_ = saveDeployState(d.stateDir, state)

	d.DeployAllStacks(context.Background(), cfg)

	var unhealthy *events.DeployEvent
	for i := range emitted {
		if emitted[i].Status == events.StatusRolledBackUnhealthy {
			unhealthy = &emitted[i]
			break
		}
		if emitted[i].Status == events.StatusRolledBack {
			t.Fatalf("expected rolled_back_unhealthy, got plain rolled_back: %+v", emitted[i])
		}
	}
	if unhealthy == nil {
		t.Fatal("expected a rolled_back_unhealthy event")
	}
	if unhealthy.Stack != "mystack" {
		t.Errorf("expected stack 'mystack', got %q", unhealthy.Stack)
	}
}

// countingErrRunner fails only the Nth "up" call (1-indexed).
type countingErrRunner struct {
	calls       []runCall
	failOnNthUp int
	upCount     *int
}

func (r *countingErrRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	for _, a := range args {
		if a == "up" {
			*r.upCount++
			if *r.upCount == r.failOnNthUp {
				return fmt.Errorf("simulated error for command: up")
			}
		}
	}
	return nil
}
