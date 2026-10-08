package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// heldEnv is one stack ("signal", optionally a dependent "app") wired for full
// runs whose new version can be made to fail after it started.
type heldEnv struct {
	t           *testing.T
	d           *Deployer
	cfg         *config.Config
	runner      *recordingRunner
	stateDir    string
	composePath string
	emitted     []events.DeployEvent
	// failStart fails the deploy's own `up` (the one carrying --remove-orphans);
	// the rollback's `up` still succeeds.
	failStart bool
	// failPull fails `pull`, a failure before any container is touched.
	failPull bool
	// clock is what the deployer reads as now; advance moves it.
	clock time.Time
}

func newHeldEnv(t *testing.T, stacks ...config.Stack) *heldEnv {
	t.Helper()
	if len(stacks) == 0 {
		stacks = []config.Stack{{Name: "signal"}}
	}
	baseDir := t.TempDir()
	env := &heldEnv{t: t, stateDir: t.TempDir(), clock: time.Date(2026, 10, 7, 2, 6, 0, 0, time.UTC)}
	seed := newEmptyState()
	seed.LastDeployedCommit = "old-sha"
	files := map[string][]byte{}
	for _, s := range stacks {
		dir := filepath.Join(baseDir, s.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "docker-compose.yml")
		writeFile(t, path, composeWithImage(s.Name+":2"))
		seed.Stacks[s.Name] = stackFileHashes{path: "hash-of-v1"}
		files["old-sha:"+path] = []byte(composeWithImage(s.Name + ":1"))
		if s.Name == "signal" {
			env.composePath = path
		}
	}
	if err := saveDeployState(env.stateDir, seed); err != nil {
		t.Fatal(err)
	}
	env.runner = &recordingRunner{failFn: func(_ string, args []string) error {
		if env.failStart && slices.Contains(args, "--remove-orphans") {
			return errors.New("new version failed to start")
		}
		if env.failPull && slices.Contains(args, "pull") {
			return errors.New("registry unreachable")
		}
		return nil
	}}
	env.d = env.newDeployer(files, baseDir)
	env.cfg = &config.Config{StacksBaseDir: baseDir, Stacks: stacks}
	return env
}

// newDeployer builds a deployer over the env's state dir, runner and clock —
// called again to stand in for a restart.
func (env *heldEnv) newDeployer(files map[string][]byte, repoDir string) *Deployer {
	return New(Config{
		Runner:       env.runner,
		CommitReader: &fakeCommitReader{files: files},
		RepoDir:      repoDir,
		StateDir:     env.stateDir,
		EventSink:    func(e events.DeployEvent) { env.emitted = append(env.emitted, e) },
		Now:          func() time.Time { return env.clock },
	})
}

// advance moves the deployer's clock forward.
func (env *heldEnv) advance(d time.Duration) { env.clock = env.clock.Add(d) }

// pulls counts the `pull` calls the runner has seen for a stack: the attempt
// signal for a change that fails before it starts.
func (env *heldEnv) pulls(stack string) int {
	n := 0
	for _, c := range env.runner.calls {
		if slices.Contains(c.args, "pull") && filepath.Base(c.dir) == stack {
			n++
		}
	}
	return n
}

// run performs one full run and returns the stack's statuses in emit order,
// plus how many `up` calls it made for the stack's own deploy.
func (env *heldEnv) run(stack string) (statuses []events.Status, deployUps int) {
	env.t.Helper()
	env.emitted = nil
	before := len(env.runner.calls)
	env.d.DeployAllStacks(context.Background(), env.cfg)
	for _, e := range env.emitted {
		if e.Stack == stack {
			statuses = append(statuses, e.Status)
		}
	}
	for _, c := range env.runner.calls[before:] {
		if slices.Contains(c.args, "--remove-orphans") && filepath.Base(c.dir) == stack {
			deployUps++
		}
	}
	return statuses, deployUps
}

func (env *heldEnv) state() *persistedState {
	env.t.Helper()
	s, err := loadPersistedDeployState(env.stateDir)
	if err != nil {
		env.t.Fatal(err)
	}
	return s
}

// A change whose new version failed after it started is not retried on the
// next tick: the run reports it held and never touches the containers.
func TestDeployAllStacks_HoldsChangeThatRolledBack(t *testing.T) {
	env := newHeldEnv(t)
	env.failStart = true

	statuses, ups := env.run("signal")
	if !slices.Contains(statuses, events.StatusRolledBack) || ups != 1 {
		t.Fatalf("run 1: statuses %v, deploy ups %d; want a rolled_back after one up", statuses, ups)
	}

	statuses, ups = env.run("signal")
	if ups != 0 {
		t.Errorf("run 2: %d deploy ups, want none while the change is held", ups)
	}
	if !slices.Equal(statuses, []events.Status{events.StatusHeld}) {
		t.Errorf("run 2: statuses %v, want exactly [held]", statuses)
	}

	held := env.d.HeldStacks()
	h, ok := held["signal"]
	if !ok {
		t.Fatalf("HeldStacks() = %v, want signal held", held)
	}
	if h.Status != events.StatusRolledBack || h.Since.IsZero() {
		t.Errorf("held view = %+v, want status rolled_back and a since time", h)
	}
}

// A new commit that changes the stack's inputs releases the hold.
func TestDeployAllStacks_NewCommitReleasesHold(t *testing.T) {
	env := newHeldEnv(t)
	env.failStart = true
	env.run("signal")

	env.failStart = false
	writeFile(t, env.composePath, composeWithImage("signal:3"))
	statuses, ups := env.run("signal")
	if ups != 1 || !slices.Contains(statuses, events.StatusSuccess) {
		t.Fatalf("statuses %v, deploy ups %d; want the new commit deployed", statuses, ups)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("a successful deploy must clear the hold")
	}
	if _, ok := env.d.HeldStacks()["signal"]; ok {
		t.Error("HeldStacks() must no longer list the stack")
	}
}

// A retry request deploys the held change once more; failing again holds it
// again, so one click is one attempt.
func TestDeployAllStacks_RetryRequestDeploysHeldChangeOnce(t *testing.T) {
	env := newHeldEnv(t)
	env.failStart = true
	env.run("signal")

	if !env.d.RequestRetry("signal") {
		t.Fatal("RequestRetry must accept a held stack")
	}
	statuses, ups := env.run("signal")
	if ups != 1 || !slices.Contains(statuses, events.StatusRolledBack) {
		t.Fatalf("retry run: statuses %v, deploy ups %d; want one more attempt", statuses, ups)
	}

	statuses, ups = env.run("signal")
	if ups != 0 || !slices.Equal(statuses, []events.Status{events.StatusHeld}) {
		t.Errorf("after the retry: statuses %v, deploy ups %d; want held again", statuses, ups)
	}
}

// Only a held stack accepts a retry; anything else has nothing to retry.
func TestRequestRetry_RejectsStackThatIsNotHeld(t *testing.T) {
	env := newHeldEnv(t)
	env.run("signal") // deploys fine

	if env.d.RequestRetry("signal") {
		t.Error("RequestRetry must refuse a stack that is not held")
	}
	if env.d.RequestRetry("unknown") {
		t.Error("RequestRetry must refuse an unknown stack")
	}
}

// A retry that succeeds clears the hold.
func TestDeployAllStacks_SuccessfulRetryClearsHold(t *testing.T) {
	env := newHeldEnv(t)
	env.failStart = true
	env.run("signal")

	env.failStart = false
	env.d.RequestRetry("signal")
	statuses, _ := env.run("signal")
	if !slices.Contains(statuses, events.StatusSuccess) {
		t.Fatalf("statuses %v, want the retry to succeed", statuses)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("a successful retry must clear the hold")
	}
}

// A failure before any container was touched (here: the pull) is usually
// transient: the change is retried once its backoff has passed, and a retry
// that succeeds leaves nothing held (ADR-0064).
func TestDeployAllStacks_TransientFailureBeforeStartHealsOnRetry(t *testing.T) {
	env := newHeldEnv(t)
	env.failPull = true
	statuses, _ := env.run("signal")
	if !slices.Contains(statuses, events.StatusFailed) {
		t.Fatalf("run 1: statuses %v, want failed", statuses)
	}

	env.failPull = false
	env.advance(preStartBackoffBase)
	statuses, ups := env.run("signal")
	if ups != 1 || !slices.Contains(statuses, events.StatusSuccess) {
		t.Errorf("run 2: statuses %v, deploy ups %d; want the change retried", statuses, ups)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("a successful retry must leave nothing held")
	}
	if _, ok := env.d.HeldStacks()["signal"]; ok {
		t.Error("HeldStacks() must not list a stack that deployed")
	}
}

// The incident: a deterministic build failure was rebuilt on every reconcile
// tick for five hours. Now each attempt waits twice as long as the last, and
// the third failure holds the change like ADR-0062 does.
func TestDeployAllStacks_BacksOffThenHoldsFailureBeforeStart(t *testing.T) {
	env := newHeldEnv(t)
	env.failPull = true

	// tick is one reconcile run after advancing the clock by by; it returns the
	// stack's statuses and how many pull attempts the run made.
	tick := func(by time.Duration) ([]events.Status, int) {
		env.advance(by)
		before := env.pulls("signal")
		statuses, ups := env.run("signal")
		if ups != 0 {
			t.Fatalf("a failure before start must never reach up, got %d", ups)
		}
		return statuses, env.pulls("signal") - before
	}
	attempted := func(step string, statuses []events.Status, pulls int) {
		t.Helper()
		if pulls != 1 || !slices.Contains(statuses, events.StatusFailed) {
			t.Fatalf("%s: statuses %v, %d pulls; want one failed attempt", step, statuses, pulls)
		}
	}
	waited := func(step string, statuses []events.Status, pulls int) {
		t.Helper()
		if pulls != 0 || !slices.Equal(statuses, []events.Status{events.StatusHeld}) {
			t.Fatalf("%s: statuses %v, %d pulls; want exactly [held] and no attempt", step, statuses, pulls)
		}
	}

	s, p := tick(0)
	attempted("first attempt", s, p)
	s, p = tick(time.Minute)
	waited("1 min after the first failure", s, p)
	s, p = tick(preStartBackoffBase - time.Minute)
	attempted("5 min after the first failure", s, p)
	s, p = tick(preStartBackoffBase)
	waited("5 min after the second failure", s, p)
	s, p = tick(preStartBackoffBase)
	attempted("10 min after the second failure", s, p)

	h, ok := env.d.HeldStacks()["signal"]
	if !ok || !h.RetryAt.IsZero() || h.Attempts != preStartHoldAfter || h.Status != events.StatusFailed {
		t.Fatalf("after the third failure HeldStacks()[signal] = %+v (ok=%v); want held for good after %d attempts", h, ok, preStartHoldAfter)
	}
	s, p = tick(24 * time.Hour)
	waited("a day after the hold", s, p)
}

// A backoff is a hold in waiting: the alerting gauge is raised only once the
// change is held for good.
// Its own stack name: the gauge is process-global and other tests hold signal.
func TestDeployAllStacks_HeldGaugeOnlyForAHoldThatNeedsAnOperator(t *testing.T) {
	const stack = "gauge-backoff"
	env := newHeldEnv(t, config.Stack{Name: stack})
	env.failPull = true
	env.run(stack)
	if _, ok := env.d.HeldStacks()[stack]; !ok {
		t.Fatal("the failed attempt must be recorded as a backoff")
	}
	if heldGauge(t, stack) != 0 {
		t.Error("skipper_stack_held must not be raised while the change still retries on its own")
	}
	for range preStartHoldAfter - 1 {
		env.advance(time.Hour)
		env.run(stack)
	}
	if heldGauge(t, stack) != 1 {
		t.Error("skipper_stack_held must be raised once the change is held")
	}
}

// heldGauge reads skipper_stack_held for a stack; a deleted series reads 0.
func heldGauge(t *testing.T, stack string) float64 {
	t.Helper()
	var m dto.Metric
	if err := metrics.StackHeld.WithLabelValues(stack).Write(&m); err != nil {
		t.Fatalf("read gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

// A new push resets the count: the new inputs get their own attempts.
func TestDeployAllStacks_NewCommitReleasesBackoff(t *testing.T) {
	env := newHeldEnv(t)
	env.failPull = true
	env.run("signal")

	env.failPull = false
	writeFile(t, env.composePath, composeWithImage("signal:3"))
	env.advance(time.Minute)
	statuses, ups := env.run("signal")
	if ups != 1 || !slices.Contains(statuses, events.StatusSuccess) {
		t.Fatalf("statuses %v, deploy ups %d; want the new commit deployed despite the backoff", statuses, ups)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("a successful deploy must clear the backoff")
	}
}

// The retry button attempts a backing-off change now, and is one attempt.
func TestDeployAllStacks_RetryRequestSkipsTheBackoff(t *testing.T) {
	env := newHeldEnv(t)
	env.failPull = true
	env.run("signal")

	if !env.d.RequestRetry("signal") {
		t.Fatal("RequestRetry must accept a stack that is backing off")
	}
	env.advance(time.Minute)
	before := env.pulls("signal")
	statuses, _ := env.run("signal")
	if env.pulls("signal") != before+1 || !slices.Contains(statuses, events.StatusFailed) {
		t.Fatalf("retry run: statuses %v; want one more attempt inside the backoff", statuses)
	}
	if h := env.state().Held["signal"]; h.Attempts != 2 {
		t.Errorf("attempts = %d after the retry failed, want 2", h.Attempts)
	}

	env.advance(time.Minute)
	statuses, _ = env.run("signal")
	if !slices.Equal(statuses, []events.Status{events.StatusHeld}) {
		t.Errorf("after the retry: statuses %v, want held again", statuses)
	}
}

// A change held after its attempts were used up is released by a retry like
// any hold, and a retry that fails again holds it again.
func TestDeployAllStacks_RetryOfHoldAfterFailuresBeforeStart(t *testing.T) {
	env := newHeldEnv(t)
	env.failPull = true
	for range preStartHoldAfter {
		env.run("signal")
		env.advance(time.Hour)
	}

	env.d.RequestRetry("signal")
	before := env.pulls("signal")
	env.run("signal")
	if env.pulls("signal") != before+1 {
		t.Fatal("the retry must attempt the held change once more")
	}
	if h, ok := env.d.HeldStacks()["signal"]; !ok || !h.RetryAt.IsZero() {
		t.Errorf("after a failed retry HeldStacks()[signal] = %+v (ok=%v); want held again, no backoff", h, ok)
	}

	env.failPull = false
	env.d.RequestRetry("signal")
	statuses, _ := env.run("signal")
	if !slices.Contains(statuses, events.StatusSuccess) {
		t.Fatalf("statuses %v, want the second retry to deploy", statuses)
	}
	if _, ok := env.d.HeldStacks()["signal"]; ok {
		t.Error("a successful retry must release the hold")
	}
}

// A revert to the deployed inputs ends the backoff: nothing is pending.
func TestDeployAllStacks_RevertReleasesBackoff(t *testing.T) {
	env := newHeldEnv(t)
	env.run("signal") // deploys signal:2, recording its inputs

	writeFile(t, env.composePath, composeWithImage("signal:3"))
	env.failPull = true
	env.run("signal")
	if _, ok := env.state().Held["signal"]; !ok {
		t.Fatal("the failed push must be backing off")
	}

	writeFile(t, env.composePath, composeWithImage("signal:2"))
	statuses, _ := env.run("signal")
	if !slices.Equal(statuses, []events.Status{events.StatusSkipped}) {
		t.Fatalf("statuses %v, want the reverted stack skipped as unchanged", statuses)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("a revert must release the backoff")
	}
}

// The backoff survives a restart: the attempt count and the next attempt are
// in state.yaml, so a restarted skipper neither retries early nor forgets
// how many attempts were made.
func TestDeployAllStacks_BackoffSurvivesRestart(t *testing.T) {
	env := newHeldEnv(t)
	env.failPull = true
	env.run("signal")

	env.d = env.newDeployer(nil, filepath.Dir(filepath.Dir(env.composePath)))
	if h, ok := env.d.HeldStacks()["signal"]; !ok || h.RetryAt.IsZero() {
		t.Fatalf("restarted HeldStacks()[signal] = %+v (ok=%v), want the backoff seeded from state", h, ok)
	}
	env.advance(time.Minute)
	before := env.pulls("signal")
	statuses, _ := env.run("signal")
	if env.pulls("signal") != before || !slices.Equal(statuses, []events.Status{events.StatusHeld}) {
		t.Fatalf("restarted run inside the backoff: statuses %v; want [held] and no attempt", statuses)
	}
	env.advance(preStartBackoffBase)
	env.run("signal")
	if h := env.state().Held["signal"]; h.Attempts != 2 {
		t.Errorf("attempts = %d after the first attempt following the restart, want 2", h.Attempts)
	}
}

// A build cut short by shutdown says nothing about the change: it is not
// counted as an attempt.
func TestDeployAllStacks_ShutdownDuringDeployIsNotAnAttempt(t *testing.T) {
	env := newHeldEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	env.runner.failFn = func(_ string, args []string) error {
		if slices.Contains(args, "pull") {
			cancel()
			return context.Canceled
		}
		return nil
	}
	env.d.DeployAllStacks(ctx, env.cfg)
	if h, ok := env.state().Held["signal"]; ok {
		t.Errorf("a deploy cut short by shutdown must not be counted, got %+v", h)
	}
}

// With rollback disabled the failed version is left running and the event is
// a plain failed — but the new version did fail after it started, so the change
// is held all the same.
func TestDeployAllStacks_HoldsFailedStartWithRollbackDisabled(t *testing.T) {
	env := newHeldEnv(t, config.Stack{Name: "signal", Rollback: off()})
	env.failStart = true
	statuses, _ := env.run("signal")
	if !slices.Contains(statuses, events.StatusFailed) {
		t.Fatalf("run 1: statuses %v, want failed", statuses)
	}

	statuses, ups := env.run("signal")
	if ups != 0 || !slices.Equal(statuses, []events.Status{events.StatusHeld}) {
		t.Errorf("run 2: statuses %v, deploy ups %d; want held", statuses, ups)
	}
}

// A held stack's changed dependents stay blocked: its change never deployed.
func TestDeployAllStacks_HeldStackBlocksChangedDependents(t *testing.T) {
	env := newHeldEnv(t, config.Stack{Name: "signal"}, config.Stack{Name: "app", DependsOn: []string{"signal"}})
	env.failStart = true
	env.run("signal")

	env.failStart = false
	statuses, ups := env.run("app")
	if ups != 0 || !slices.Contains(statuses, events.StatusBlocked) {
		t.Errorf("app: statuses %v, deploy ups %d; want blocked behind the held dependency", statuses, ups)
	}
}

// The hold survives a restart: it lives in state.yaml, and a new Deployer
// seeds its held view from it before any run.
func TestNew_SeedsHeldViewFromState(t *testing.T) {
	env := newHeldEnv(t)
	env.failStart = true
	env.run("signal")

	fresh := New(Config{Runner: env.runner, StateDir: env.stateDir})
	if _, ok := fresh.HeldStacks()["signal"]; !ok {
		t.Error("a restarted deployer must report the held stack before its first run")
	}
}
