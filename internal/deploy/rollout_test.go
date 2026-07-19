package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// --- test fixtures -----------------------------------------------------------

// rolloutCompose is a stack with a rollable web service (no host ports, has a
// healthcheck) and a non-rollable db (no healthcheck — it is never rolled).
const rolloutCompose = `services:
  web:
    image: nginx:1.25
    healthcheck:
      test: ["CMD", "true"]
  db:
    image: postgres:16
`

// rolloutSetup lays out a one-stack repo with the given compose file and returns
// a Deployer whose runner also serves `docker compose ps`, plus the stackRun.
func rolloutSetup(t *testing.T, composeYAML string) (*Deployer, *recordingRunner, stackRun) {
	t.Helper()
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "app")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeYAML)

	runner := &recordingRunner{}
	d := &Deployer{
		runner:                 runner,
		outputter:              runner,
		stateDir:               t.TempDir(),
		rolloutPollInterval:    time.Millisecond,
		rolloutTimeoutOverride: 50 * time.Millisecond,
	}
	stack := config.Stack{Name: "app", Rollout: &config.Rollout{Services: []string{"web"}}}
	run := newStackRun(stack, baseDir, nil)
	return d, runner, run
}

// cl builds a running compose ps line with the given id/service/health.
func cl(id, service, health string) containerLine {
	return containerLine{ID: id, Name: id, Service: service, State: "running", Health: health}
}

// psArray renders lines as a `docker compose ps --format json` array.
func psArray(lines ...containerLine) []byte {
	if len(lines) == 0 {
		return []byte("[]")
	}
	b, _ := json.Marshal(lines)
	return b
}

// dockerArgv returns the argv of every `docker …` Run call, in order.
func dockerArgv(calls []runCall) [][]string {
	var out [][]string
	for _, c := range calls {
		if c.name == "docker" {
			out = append(out, c.args)
		}
	}
	return out
}

// dockerArgvContains reports whether any docker call's argv equals want.
func dockerArgvContains(calls []runCall, want ...string) bool {
	for _, argv := range dockerArgv(calls) {
		if slices.Equal(argv, want) {
			return true
		}
	}
	return false
}

// dockerArgvMentions reports whether any docker call's argv contains tok.
func dockerArgvMentions(calls []runCall, tok string) bool {
	for _, argv := range dockerArgv(calls) {
		if slices.Contains(argv, tok) {
			return true
		}
	}
	return false
}

// --- rollService: the per-service cutover -----------------------------------

func TestRollService_StartsCanaryThenDrainsOld(t *testing.T) {
	d, runner, run := rolloutSetup(t, rolloutCompose)
	// First ps: only the old container. Later ps: old + a healthy canary.
	runner.outputFn = func(call int, _ []string) ([]byte, error) {
		if call == 0 {
			return psArray(cl("old1", "web", "healthy")), nil
		}
		return psArray(cl("old1", "web", "healthy"), cl("new1", "web", "healthy")), nil
	}

	if err := d.rollService(context.Background(), run, "web", time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Canary started alongside the old container, then the old one drained.
	if !dockerArgvContains(runner.calls, "compose", "up", "-d", "--no-deps", "--no-recreate", "--scale", "web=2", "web") {
		t.Errorf("expected scale-up canary command; docker calls=%v", dockerArgv(runner.calls))
	}
	if !dockerArgvContains(runner.calls, "stop", "old1") || !dockerArgvContains(runner.calls, "rm", "old1") {
		t.Errorf("expected old container old1 to be stopped and removed; docker calls=%v", dockerArgv(runner.calls))
	}
	// The new container must survive.
	if dockerArgvContains(runner.calls, "stop", "new1") || dockerArgvContains(runner.calls, "rm", "new1") {
		t.Errorf("canary new1 must not be removed on success; docker calls=%v", dockerArgv(runner.calls))
	}
}

func TestRollService_WaitsDrainDelayThenDrainsOld(t *testing.T) {
	d, runner, run := rolloutSetup(t, rolloutCompose)
	d.rolloutDrainOverride = 40 * time.Millisecond // stand in for rollout.drain_seconds
	runner.outputFn = func(call int, _ []string) ([]byte, error) {
		if call == 0 {
			return psArray(cl("old1", "web", "healthy")), nil
		}
		return psArray(cl("old1", "web", "healthy"), cl("new1", "web", "healthy")), nil
	}

	start := time.Now()
	if err := d.rollService(context.Background(), run, "web", time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The old container is still drained — the delay only defers it, so the proxy
	// can switch over first.
	if !dockerArgvContains(runner.calls, "stop", "old1") || !dockerArgvContains(runner.calls, "rm", "old1") {
		t.Errorf("old container must still be drained after the delay; docker calls=%v", dockerArgv(runner.calls))
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("expected the drain to wait ~40ms, but rollService returned after %s", elapsed)
	}
}

func TestRollService_FirstDeployPlainUp(t *testing.T) {
	d, runner, run := rolloutSetup(t, rolloutCompose)
	runner.outputFn = func(int, []string) ([]byte, error) { return psArray(), nil } // nothing running yet

	if err := d.rollService(context.Background(), run, "web", time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !dockerArgvContains(runner.calls, "compose", "up", "-d", "--no-deps", "web") {
		t.Errorf("first deploy should be a plain up; docker calls=%v", dockerArgv(runner.calls))
	}
	if dockerArgvMentions(runner.calls, "--scale") {
		t.Errorf("first deploy must not scale (nothing to keep alive); docker calls=%v", dockerArgv(runner.calls))
	}
	if dockerArgvMentions(runner.calls, "stop") || dockerArgvMentions(runner.calls, "rm") {
		t.Errorf("first deploy must not stop/rm anything; docker calls=%v", dockerArgv(runner.calls))
	}
}

func TestRollService_UnhealthyCanaryRemovedOldKept(t *testing.T) {
	d, runner, run := rolloutSetup(t, rolloutCompose)
	// The canary appears but never turns healthy.
	runner.outputFn = func(call int, _ []string) ([]byte, error) {
		if call == 0 {
			return psArray(cl("old1", "web", "healthy")), nil
		}
		return psArray(cl("old1", "web", "healthy"), cl("new1", "web", "starting")), nil
	}

	err := d.rollService(context.Background(), run, "web", 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected error when the canary never becomes healthy")
	}
	if !errors.Is(err, errCanaryUnhealthy) {
		t.Errorf("error must wrap errCanaryUnhealthy; got %v", err)
	}
	// The canary is cleaned up…
	if !dockerArgvContains(runner.calls, "stop", "new1") || !dockerArgvContains(runner.calls, "rm", "new1") {
		t.Errorf("unhealthy canary new1 must be stopped and removed; docker calls=%v", dockerArgv(runner.calls))
	}
	// …and the old version is never touched — zero downtime even on failure.
	if dockerArgvContains(runner.calls, "stop", "old1") || dockerArgvContains(runner.calls, "rm", "old1") {
		t.Errorf("old container old1 must survive a failed rollout; docker calls=%v", dockerArgv(runner.calls))
	}
}

// --- rollout: orchestration --------------------------------------------------

func TestRollout_UpsNonRolledBeforeCuttingOverRolled(t *testing.T) {
	d, runner, run := rolloutSetup(t, rolloutCompose)
	compose, err := parseComposeFile(run.composePath)
	if err != nil {
		t.Fatal(err)
	}
	runner.outputFn = func(call int, _ []string) ([]byte, error) {
		if call == 0 {
			return psArray(cl("old1", "web", "healthy")), nil
		}
		return psArray(cl("old1", "web", "healthy"), cl("new1", "web", "healthy")), nil
	}

	if err := d.rollout(context.Background(), run, compose, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argv := dockerArgv(runner.calls)
	// The non-rolled db comes up in place first…
	if len(argv) == 0 || !slices.Equal(argv[0], []string{"compose", "up", "-d", "--remove-orphans", "db"}) {
		t.Fatalf("first command should be an in-place up of the non-rolled db; got %v", argv)
	}
	// …then the rolled web cuts over.
	if !dockerArgvContains(runner.calls, "compose", "up", "-d", "--no-deps", "--no-recreate", "--scale", "web=2", "web") {
		t.Errorf("expected web to cut over after db; docker calls=%v", argv)
	}
	// The rolled service is never brought up in place.
	if dockerArgvContains(runner.calls, "compose", "up", "-d", "--remove-orphans", "web") {
		t.Errorf("rolled service web must not be recreated in place; docker calls=%v", argv)
	}
}

func TestRollout_CanaryFailureWrapsErrRolledBack(t *testing.T) {
	d, runner, run := rolloutSetup(t, rolloutCompose)
	compose, err := parseComposeFile(run.composePath)
	if err != nil {
		t.Fatal(err)
	}
	runner.outputFn = func(call int, _ []string) ([]byte, error) {
		if call == 0 {
			return psArray(cl("old1", "web", "healthy")), nil
		}
		return psArray(cl("old1", "web", "healthy"), cl("new1", "web", "starting")), nil
	}

	err = d.rollout(context.Background(), run, compose, newEmptyState())
	if err == nil {
		t.Fatal("expected error when the canary never becomes healthy")
	}
	// A failed rollout leaves the stack on the old version — reported as
	// rolled_back, with no git-restore leg (the old container never left).
	if !errors.Is(err, ErrRolledBack) {
		t.Errorf("canary failure must map to ErrRolledBack; got %v", err)
	}
	if dockerArgvMentions(runner.calls, "old1") {
		t.Errorf("old1 must never be stopped/removed on a canary failure; docker calls=%v", dockerArgv(runner.calls))
	}
}

func TestRollout_NilComposeFails(t *testing.T) {
	d, _, run := rolloutSetup(t, rolloutCompose)
	if err := d.rollout(context.Background(), run, nil, newEmptyState()); err == nil {
		t.Fatal("rollout must fail when the compose file could not be parsed")
	}
}

// --- deployStackIfChanged integration ---------------------------------------

func TestDeployStack_RolloutTakesCutoverPathAndSucceeds(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "app")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), rolloutCompose)

	runner := &recordingRunner{}
	runner.outputFn = func(call int, _ []string) ([]byte, error) {
		if call == 0 {
			return psArray(cl("old1", "web", "healthy")), nil
		}
		return psArray(cl("old1", "web", "healthy"), cl("new1", "web", "healthy")), nil
	}
	d := &Deployer{
		runner:                 runner,
		outputter:              runner,
		repoDir:                baseDir,
		stateDir:               t.TempDir(),
		rolloutPollInterval:    time.Millisecond,
		rolloutTimeoutOverride: time.Second,
	}

	var got events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) { got = e })

	stack := config.Stack{Name: "app", Rollout: &config.Rollout{Services: []string{"web"}}}
	state := newEmptyState()
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Status != events.StatusSuccess {
		t.Errorf("event status = %q, want success", got.Status)
	}
	// The rollout path ran — a scale-up canary, not a whole-stack in-place up.
	if !dockerArgvMentions(runner.calls, "--scale") {
		t.Errorf("expected the rollout cutover (scale-up); docker calls=%v", dockerArgv(runner.calls))
	}
	if state.Stacks["app"] == nil {
		t.Error("a successful rollout must record stack state")
	}
}

// --- validation --------------------------------------------------------------

// --- ps parsing --------------------------------------------------------------

func TestParseContainerLines(t *testing.T) {
	arr := `[{"ID":"a","Service":"web","Health":"healthy"},{"ID":"b","Service":"web","Health":"starting"}]`
	nd := `{"ID":"a","Service":"web","Health":"healthy"}
{"ID":"b","Service":"web","Health":"starting"}`

	for _, form := range []struct {
		name string
		in   string
	}{{"array", arr}, {"ndjson", nd}} {
		t.Run(form.name, func(t *testing.T) {
			lines, err := parseContainerLines([]byte(form.in))
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) != 2 || lines[0].ID != "a" || lines[1].Health != "starting" {
				t.Errorf("parsed %+v, want two lines a/healthy, b/starting", lines)
			}
		})
	}

	t.Run("empty", func(t *testing.T) {
		lines, err := parseContainerLines([]byte("  \n"))
		if err != nil || lines != nil {
			t.Errorf("empty output should parse to nil,nil; got %v,%v", lines, err)
		}
	})
}
