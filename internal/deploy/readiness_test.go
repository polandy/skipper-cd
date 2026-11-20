package deploy

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// readinessEnv is a gated stack whose readiness polls step a fake clock by
// tickPerRead, so a gate timeout runs out after a known number of reads with
// no real waiting.
type readinessEnv struct {
	baseDir string
	runner  *recordingRunner
	cr      *fakeCommitReader
	state   *persistedState
	clock   time.Time
	events  []events.DeployEvent
}

const tickPerRead = 10 * time.Second

func newReadinessEnv(t *testing.T) *readinessEnv {
	t.Helper()
	baseDir := makeBaseWithHealthcheckStack(t)
	composePath := filepath.Join(baseDir, "mystack", "docker-compose.yml")
	return &readinessEnv{
		baseDir: baseDir,
		runner:  &recordingRunner{},
		cr: &fakeCommitReader{files: map[string][]byte{
			"old-sha:" + composePath: []byte(composeWithHealthcheck("nginx:1.25")),
		}},
		state: &persistedState{
			Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
			Images:             map[string]serviceImageByName{},
			LastDeployedCommit: "old-sha",
		},
		clock: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}
}

// health answers every readiness read with the app container's health as the
// given function decides for the n-th read (0-based) of the whole deploy.
func (env *readinessEnv) health(fn func(read int) containerLine) {
	reads := 0
	env.runner.outputFn = func(_ int, args []string) ([]byte, error) {
		if !isReadinessRead(args) {
			return nil, nil
		}
		env.clock = env.clock.Add(tickPerRead)
		line := fn(reads)
		reads++
		return psArray(line), nil
	}
}

func (env *readinessEnv) deploy(t *testing.T, stack config.Stack) error {
	t.Helper()
	d := New(Config{
		Runner:              env.runner,
		Outputter:           env.runner,
		CommitReader:        env.cr,
		RepoDir:             env.baseDir,
		StateDir:            t.TempDir(),
		Now:                 func() time.Time { return env.clock },
		RolloutPollInterval: time.Nanosecond,
		EventSink:           func(e events.DeployEvent) { env.events = append(env.events, e) },
	})
	return d.deployStackIfChanged(context.Background(), stack, env.baseDir, "", nil, env.state)
}

// isReadinessRead tells the readiness wait's `ps --all` from the running-images
// read (`ps --format json --all`).
func isReadinessRead(args []string) bool {
	i := slices.Index(args, "ps")
	return i >= 0 && i+1 < len(args) && args[i+1] == "--all"
}

// readinessReads returns the reads the readiness wait made.
func readinessReads(calls []runCall) []runCall {
	var reads []runCall
	for _, c := range calls {
		if isReadinessRead(c.args) {
			reads = append(reads, c)
		}
	}
	return reads
}

func appContainer(state, health string, exitCode int) containerLine {
	return containerLine{ID: "c1", Name: "mystack-app-1", Service: "app", State: state, Health: health, ExitCode: exitCode}
}

func TestAwaitReady_StartingThenHealthyDeploysWithoutRollback(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(read int) containerLine {
		if read < 3 {
			return appContainer("running", "starting", 0)
		}
		return appContainer("running", "healthy", 0)
	})

	if err := env.deploy(t, config.Stack{Name: "mystack"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(readinessReads(env.runner.outputCalls)); got != 4 {
		t.Errorf("expected 4 readiness reads (3 starting, 1 healthy), got %d", got)
	}
	if got := len(upCalls(env.runner.calls)); got != 1 {
		t.Errorf("expected only the deploy up, got %d up calls", got)
	}
	if len(env.state.Stacks["mystack"]) == 0 || env.state.Stacks["mystack"]["old"] == "oldhash" {
		t.Error("expected the new hashes recorded after a healthy deploy")
	}
}

func TestAwaitReady_UnhealthyRollsBack(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(read int) containerLine {
		if read == 0 {
			return appContainer("running", "unhealthy", 0)
		}
		return appContainer("running", "healthy", 0) // the restored version
	})

	err := env.deploy(t, config.Stack{Name: "mystack"})
	if !errors.Is(err, ErrRolledBack) {
		t.Fatalf("expected ErrRolledBack, got %v", err)
	}
	if !strings.Contains(err.Error(), "container mystack-app-1 is unhealthy") {
		t.Errorf("expected the unhealthy container named, got %v", err)
	}
	if got := len(upCalls(env.runner.calls)); got != 2 {
		t.Errorf("expected deploy + rollback up, got %d", got)
	}
}

func TestAwaitReady_ExitedNonZeroRollsBack(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(read int) containerLine {
		if read == 0 {
			return appContainer("exited", "", 1)
		}
		return appContainer("running", "healthy", 0)
	})

	err := env.deploy(t, config.Stack{Name: "mystack"})
	if !errors.Is(err, ErrRolledBack) {
		t.Fatalf("expected ErrRolledBack, got %v", err)
	}
	if !strings.Contains(err.Error(), "exited with code 1") {
		t.Errorf("expected the exit code in the error, got %v", err)
	}
}

func TestReadinessOf_ExitedZeroIsAFinishedOneShot(t *testing.T) {
	waiting, failure := readinessOf([]containerLine{appContainer("exited", "", 0)})
	if waiting != "" || failure != nil {
		t.Errorf("expected ready, got waiting=%q failure=%v", waiting, failure)
	}
}

func TestAwaitReady_StillStartingAtExpiryHoldsWithoutRollback(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(int) containerLine { return appContainer("running", "starting", 0) })

	err := env.deploy(t, config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 60}})
	if err == nil {
		t.Fatal("expected the deploy to fail")
	}
	if !errors.Is(err, ErrNewVersionFailed) {
		t.Errorf("expected the change held (ErrNewVersionFailed), got %v", err)
	}
	if errors.Is(err, ErrRolledBack) || errors.Is(err, ErrRollbackUnhealthy) {
		t.Errorf("an undecided wait must never roll back, got %v", err)
	}
	for _, want := range []string{"still starting after 1m0s", "not rolled back", "POST /api/stacks/mystack/retry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in the error, got %v", want, err)
		}
	}
	if got := len(upCalls(env.runner.calls)); got != 1 {
		t.Errorf("expected no rollback or roll-forward up, got %d up calls", got)
	}
	// 60s at 10s per read: reads at 10s…60s, the last one at the deadline.
	if got := len(readinessReads(env.runner.outputCalls)); got != 6 {
		t.Errorf("expected 6 readiness reads, got %d", got)
	}
	if _, held := env.state.Held["mystack"]; !held {
		t.Error("expected the change recorded as held")
	}
	if last := env.events[len(env.events)-1]; last.Status != events.StatusFailed {
		t.Errorf("expected a failed event, got %q", last.Status)
	}
}

func TestAwaitReady_UnhealthyRollbackRollsForwardOnce(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(read int) containerLine {
		if read < 2 { // the new version, then the restored one
			return appContainer("running", "unhealthy", 0)
		}
		return appContainer("running", "healthy", 0) // the roll-forward
	})

	if err := env.deploy(t, config.Stack{Name: "mystack"}); err != nil {
		t.Fatalf("expected the roll-forward to settle the deploy, got %v", err)
	}
	if got := len(upCalls(env.runner.calls)); got != 3 {
		t.Errorf("expected deploy + rollback + roll-forward up, got %d", got)
	}
	if env.state.Stacks["mystack"]["old"] == "oldhash" {
		t.Error("expected the new version's hashes recorded")
	}
	if last := env.events[len(env.events)-1]; last.Status != events.StatusSuccess {
		t.Errorf("expected a success event, got %q", last.Status)
	}
}

func TestAwaitReady_FailedRollForwardStaysRolledBackUnhealthy(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(int) containerLine { return appContainer("running", "unhealthy", 0) })

	err := env.deploy(t, config.Stack{Name: "mystack"})
	if !errors.Is(err, ErrRollbackUnhealthy) || !errors.Is(err, ErrNewVersionFailed) {
		t.Fatalf("expected a held rolled_back_unhealthy, got %v", err)
	}
	if !strings.Contains(err.Error(), "roll-forward also failed") {
		t.Errorf("expected the roll-forward failure in the error, got %v", err)
	}
	if got := len(upCalls(env.runner.calls)); got != 3 {
		t.Errorf("expected exactly one roll-forward (3 up calls), got %d", got)
	}
	if _, held := env.state.Held["mystack"]; !held {
		t.Error("expected the change recorded as held")
	}
}

func TestAwaitReady_RollbackStillStartingDoesNotRollForward(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(read int) containerLine {
		if read == 0 {
			return appContainer("running", "unhealthy", 0)
		}
		return appContainer("running", "starting", 0)
	})

	err := env.deploy(t, config.Stack{Name: "mystack"})
	if !errors.Is(err, ErrRollbackUnhealthy) || !errors.Is(err, errStillStarting) {
		t.Fatalf("expected an undecided rolled_back_unhealthy, got %v", err)
	}
	if got := len(upCalls(env.runner.calls)); got != 2 {
		t.Errorf("expected no roll-forward (2 up calls), got %d", got)
	}
}

func TestAwaitReady_RollbackDisabledNeverRollsForward(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(int) containerLine { return appContainer("running", "unhealthy", 0) })

	err := env.deploy(t, config.Stack{Name: "mystack", Rollback: boolPtr(false)})
	if !errors.Is(err, ErrNewVersionFailed) || errors.Is(err, ErrRolledBack) || errors.Is(err, ErrRollbackUnhealthy) {
		t.Fatalf("expected a plain held failure, got %v", err)
	}
	if got := len(upCalls(env.runner.calls)); got != 1 {
		t.Errorf("expected only the deploy up, got %d up calls", got)
	}
	if got := len(readinessReads(env.runner.outputCalls)); got != 1 {
		t.Errorf("expected one readiness read, got %d", got)
	}
}

func TestAwaitReady_UngatedStackReadsNoContainerState(t *testing.T) {
	env := newReadinessEnv(t)
	env.health(func(int) containerLine { return appContainer("running", "unhealthy", 0) })

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{Enabled: boolPtr(false)}}
	if err := env.deploy(t, stack); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(upCalls(env.runner.calls)); got != 1 {
		t.Fatalf("expected the deploy up to run, got %d up calls", got)
	}
	if got := len(readinessReads(env.runner.outputCalls)); got != 0 {
		t.Errorf("expected no readiness read, got %d", got)
	}
}
