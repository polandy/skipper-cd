package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// --- orderStacks: stable topological sort (ADR-0032) ---

func stackNames(stacks []config.Stack) []string {
	names := make([]string, len(stacks))
	for i, s := range stacks {
		names[i] = s.Name
	}
	return names
}

func TestOrderStacks_KeepsConfigOrderWithoutDependencies(t *testing.T) {
	stacks := []config.Stack{{Name: "c"}, {Name: "a"}, {Name: "b"}}

	got := stackNames(orderStacks(stacks))

	want := []string{"c", "a", "b"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("order = %v, want config order %v", got, want)
	}
}

func TestOrderStacks_DeploysDependencyBeforeDependent(t *testing.T) {
	// The dependent is listed first — the sort must move its dependency ahead.
	stacks := []config.Stack{
		{Name: "app", DependsOn: []string{"db"}},
		{Name: "db"},
	}

	got := stackNames(orderStacks(stacks))

	want := []string{"db", "app"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestOrderStacks_StableAmongIndependentStacks(t *testing.T) {
	// Ties keep config order: only the app→db edge reorders; the independent
	// stacks stay where the config put them.
	stacks := []config.Stack{
		{Name: "monitoring"},
		{Name: "app", DependsOn: []string{"db"}},
		{Name: "whoami"},
		{Name: "db"},
	}

	got := stackNames(orderStacks(stacks))

	want := []string{"monitoring", "whoami", "db", "app"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// --- depGate: edge decision in isolation ---

func TestDepGate_ReadyWhenNoDependencies(t *testing.T) {
	g := newDepGate()
	if d := g.decide(nil); d.outcome != depReady {
		t.Errorf("no dependencies should be ready, got %+v", d)
	}
}

func TestDepGate_ReadyWhenAllDependenciesReady(t *testing.T) {
	g := newDepGate()
	g.record("db", depReady)
	if d := g.decide([]string{"db"}); d.outcome != depReady {
		t.Errorf("a ready dependency should not constrain, got %+v", d)
	}
}

func TestDepGate_BlocksOnFailedDependency(t *testing.T) {
	g := newDepGate()
	g.record("db", depBlocked)
	d := g.decide([]string{"db"})
	if d.outcome != depBlocked || d.depName != "db" {
		t.Errorf("expected blocked by db, got %+v", d)
	}
}

func TestDepGate_QueuesOnQueuedDependency(t *testing.T) {
	g := newDepGate()
	g.record("db", depQueued)
	d := g.decide([]string{"db"})
	if d.outcome != depQueued || d.depName != "db" {
		t.Errorf("expected queued behind db, got %+v", d)
	}
}

func TestDepGate_BlockWinsOverQueue(t *testing.T) {
	// A stack depending on both a queued and a failed dependency must block:
	// it cannot deploy regardless, and blocking is the safe leave-dirty outcome.
	g := newDepGate()
	g.record("cache", depQueued)
	g.record("db", depBlocked)
	d := g.decide([]string{"cache", "db"})
	if d.outcome != depBlocked || d.depName != "db" {
		t.Errorf("block must win over queue, got %+v", d)
	}
}

// --- deploy-run edges (blocked / queued / satisfied) ---

// orderingEnv is a DeployAllStacks test fixture: one compose dir per stack, a
// recording runner, autosync on for every stack, and captured events.
type orderingEnv struct {
	d       *Deployer
	runner  *recordingRunner
	cfg     *config.Config
	baseDir string
	queue   *autosync.Queue
	emitted *[]events.DeployEvent
}

// newOrderingEnv builds the fixture with autosync on for every stack; a test
// needing a different policy passes its own controller as ctrl.
func newOrderingEnv(t *testing.T, stacks []config.Stack, ctrl ...*autosync.Controller) *orderingEnv {
	t.Helper()
	baseDir := t.TempDir()
	for _, s := range stacks {
		stackDir := filepath.Join(baseDir, s.Name)
		if err := os.MkdirAll(stackDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	}

	controller := autosync.NewController(nil, nil)
	if len(ctrl) > 0 {
		controller = ctrl[0]
	}
	runner := &recordingRunner{}
	q := autosync.NewQueue()
	emitted := &[]events.DeployEvent{}
	d := New(Config{
		Runner:       runner,
		CommitReader: &fakeCommitReader{},
		RepoDir:      baseDir,
		StateDir:     t.TempDir(),
		Autosync:     controller,
		Queue:        q,
		EventSink:    func(e events.DeployEvent) { *emitted = append(*emitted, e) },
	})

	return &orderingEnv{
		d:       d,
		runner:  runner,
		baseDir: baseDir,
		queue:   q,
		emitted: emitted,
		cfg: &config.Config{
			RepoURL:       "ssh://git@example.com/repo.git",
			StacksBaseDir: baseDir,
			Stacks:        stacks,
		},
	}
}

// failUpIn makes every `docker compose up` under the named stack's directory fail.
func (env *orderingEnv) failUpIn(stack string) {
	dir := filepath.Join(env.baseDir, stack)
	env.runner.failFn = func(callDir string, args []string) error {
		if callDir == dir && slices.Contains(args, "up") {
			return fmt.Errorf("simulated up failure in %s", stack)
		}
		return nil
	}
}

// eventFor returns the first captured event of the given stack and status.
func (env *orderingEnv) eventFor(stack string, status events.Status) *events.DeployEvent {
	for i := range *env.emitted {
		e := (*env.emitted)[i]
		if e.Stack == stack && e.Status == status {
			return &e
		}
	}
	return nil
}

// upDirs returns the working directories of all `up` invocations, in order.
func (env *orderingEnv) upDirs() []string {
	var dirs []string
	for _, c := range env.runner.calls {
		if slices.Contains(c.args, "up") {
			dirs = append(dirs, c.dir)
		}
	}
	return dirs
}

func TestDeployAllStacks_DeploysInDependencyOrder(t *testing.T) {
	env := newOrderingEnv(t, []config.Stack{
		{Name: "app", DependsOn: []string{"db"}},
		{Name: "db"},
	})

	env.d.DeployAllStacks(context.Background(), env.cfg)

	ups := env.upDirs()
	if len(ups) != 2 {
		t.Fatalf("expected 2 up calls, got %d (%v)", len(ups), ups)
	}
	if !strings.HasSuffix(ups[0], "/db") || !strings.HasSuffix(ups[1], "/app") {
		t.Errorf("expected db to deploy before app, got up order %v", ups)
	}
}

func TestDeployAllStacks_BlocksDependentWhenDependencyFails(t *testing.T) {
	env := newOrderingEnv(t, []config.Stack{
		{Name: "db"},
		{Name: "app", DependsOn: []string{"db"}},
	})
	env.failUpIn("db")

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if env.eventFor("db", events.StatusFailed) == nil {
		t.Fatalf("expected db to fail, events: %+v", *env.emitted)
	}

	blocked := env.eventFor("app", events.StatusBlocked)
	if blocked == nil {
		t.Fatalf("expected a blocked event for app, events: %+v", *env.emitted)
	}
	if len(blocked.ChangedFiles) == 0 {
		t.Error("blocked event should carry the changed files")
	}
	if !strings.Contains(blocked.Error, "db") {
		t.Errorf("blocked event error should name the failed dependency, got %q", blocked.Error)
	}

	// app must not deploy and must stay dirty for the next sync.
	for _, dir := range env.upDirs() {
		if strings.HasSuffix(dir, "/app") {
			t.Error("app must not run docker compose up when its dependency failed")
		}
	}
	state, err := loadPersistedDeployState(env.d.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.hashesFor("app")) != 0 {
		t.Error("blocked stack must not have its hashes recorded")
	}

	// The pending registry carries the blocked stack, which also pins the
	// diff/rollback base at the last fully-deployed commit.
	items := env.queue.Snapshot([]string{"db", "app"})
	if len(items) != 1 || items[0].Stack != "app" {
		t.Fatalf("expected app in the pending registry, got %+v", items)
	}
	if !strings.Contains(items[0].Reason, "db") {
		t.Errorf("pending reason should name the dependency, got %q", items[0].Reason)
	}
	if state.LastDeployedCommit != "" {
		t.Errorf("LastDeployedCommit must not advance past a blocked change, got %q", state.LastDeployedCommit)
	}
}

func TestDeployAllStacks_BlockPropagatesTransitively(t *testing.T) {
	env := newOrderingEnv(t, []config.Stack{
		{Name: "a"},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "c", DependsOn: []string{"b"}},
	})
	env.failUpIn("a")

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if env.eventFor("b", events.StatusBlocked) == nil {
		t.Error("expected b to be blocked by a's failure")
	}
	if env.eventFor("c", events.StatusBlocked) == nil {
		t.Error("expected c to be blocked transitively")
	}
	if len(env.upDirs()) != 1 {
		t.Errorf("only a should have attempted an up, got %v", env.upDirs())
	}
}

func TestDeployAllStacks_UnchangedDependencySatisfies(t *testing.T) {
	env := newOrderingEnv(t, []config.Stack{
		{Name: "db"},
		{Name: "app", DependsOn: []string{"db"}},
	})

	// First run deploys both; the second run changes only app.
	env.d.DeployAllStacks(context.Background(), env.cfg)
	writeFile(t, filepath.Join(env.baseDir, "app", "docker-compose.yml"), composeWithImage("nginx:1.26"))
	*env.emitted = nil
	env.runner.calls = nil

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if env.eventFor("db", events.StatusSkipped) == nil {
		t.Fatalf("expected db to be skipped, events: %+v", *env.emitted)
	}
	if env.eventFor("app", events.StatusSuccess) == nil {
		t.Fatalf("a skipped dependency must satisfy the edge; events: %+v", *env.emitted)
	}
}

func TestDeployAllStacks_QueuedDependencyQueuesDependent(t *testing.T) {
	// Pause autosync for db only; app remains on.
	paused := false
	env := newOrderingEnv(t, []config.Stack{
		{Name: "db"},
		{Name: "app", DependsOn: []string{"db"}},
	}, autosync.NewController(nil, map[string]*bool{"db": &paused}))

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if env.eventFor("db", events.StatusQueued) == nil {
		t.Fatalf("expected db to be queued, events: %+v", *env.emitted)
	}
	appQueued := env.eventFor("app", events.StatusQueued)
	if appQueued == nil {
		t.Fatalf("expected app to queue behind its queued dependency, events: %+v", *env.emitted)
	}
	if len(env.upDirs()) != 0 {
		t.Errorf("nothing should deploy, got up calls in %v", env.upDirs())
	}

	items := env.queue.Snapshot([]string{"db", "app"})
	if len(items) != 2 {
		t.Fatalf("expected both stacks pending, got %+v", items)
	}
	if !strings.Contains(items[1].Reason, "db") {
		t.Errorf("app's pending reason should name the dependency it waits for, got %q", items[1].Reason)
	}
}

func TestDeployAllStacks_BlockedStackDeploysWhenDependencyRecovers(t *testing.T) {
	env := newOrderingEnv(t, []config.Stack{
		{Name: "db"},
		{Name: "app", DependsOn: []string{"db"}},
	})
	env.failUpIn("db")
	env.d.DeployAllStacks(context.Background(), env.cfg)
	if env.eventFor("app", events.StatusBlocked) == nil {
		t.Fatal("precondition: app should be blocked in the first run")
	}

	// The dependency recovers; both stacks are still dirty and deploy in order.
	env.runner.failFn = nil
	*env.emitted = nil
	env.runner.calls = nil

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if env.eventFor("db", events.StatusSuccess) == nil || env.eventFor("app", events.StatusSuccess) == nil {
		t.Fatalf("expected both stacks to deploy after recovery, events: %+v", *env.emitted)
	}
	ups := env.upDirs()
	if len(ups) != 2 || !strings.HasSuffix(ups[0], "/db") || !strings.HasSuffix(ups[1], "/app") {
		t.Errorf("expected db then app, got %v", ups)
	}
	// The pending registry is clean again and the base may advance.
	if env.queue.Count() != 0 {
		t.Errorf("pending registry should be empty, got %d", env.queue.Count())
	}
	state, err := loadPersistedDeployState(env.d.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastDeployedCommit == "" {
		t.Error("LastDeployedCommit should advance once nothing is pending")
	}
}
