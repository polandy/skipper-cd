package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
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
}

func newHeldEnv(t *testing.T, stacks ...config.Stack) *heldEnv {
	t.Helper()
	if len(stacks) == 0 {
		stacks = []config.Stack{{Name: "signal"}}
	}
	baseDir := t.TempDir()
	env := &heldEnv{t: t, stateDir: t.TempDir()}
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
	env.d = New(Config{
		Runner:       env.runner,
		CommitReader: &fakeCommitReader{files: files},
		RepoDir:      baseDir,
		StateDir:     env.stateDir,
		EventSink:    func(e events.DeployEvent) { env.emitted = append(env.emitted, e) },
	})
	env.cfg = &config.Config{StacksBaseDir: baseDir, Stacks: stacks}
	return env
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

// A failure before any container was touched (here: the pull) is not held —
// it is usually transient, so the next tick tries again.
func TestDeployAllStacks_DoesNotHoldFailureBeforeStart(t *testing.T) {
	env := newHeldEnv(t)
	env.failPull = true
	statuses, _ := env.run("signal")
	if !slices.Contains(statuses, events.StatusFailed) {
		t.Fatalf("run 1: statuses %v, want failed", statuses)
	}

	env.failPull = false
	statuses, ups := env.run("signal")
	if ups != 1 || !slices.Contains(statuses, events.StatusSuccess) {
		t.Errorf("run 2: statuses %v, deploy ups %d; want the change retried", statuses, ups)
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
