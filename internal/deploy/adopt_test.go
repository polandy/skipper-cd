package deploy

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// adoptEnv is a deployer over two real stack dirs with a fresh state dir, so
// every run starts from "nothing recorded" unless a previous run wrote state.
type adoptEnv struct {
	d        *Deployer
	runner   *recordingRunner
	stateDir string
	cfg      *config.Config
	emitted  *[]events.DeployEvent
}

func newAdoptEnv(t *testing.T, initialDeploy string) *adoptEnv {
	t.Helper()
	baseDir := t.TempDir()
	for _, name := range []string{"api", "web"} {
		dir := filepath.Join(baseDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	}

	runner := &recordingRunner{}
	stateDir := t.TempDir()
	d := &Deployer{
		runner:       runner,
		commitReader: &fakeCommitReader{},
		repoDir:      baseDir,
		stateDir:     stateDir,
	}
	emitted := &[]events.DeployEvent{}
	d.SetEventSink(func(e events.DeployEvent) { *emitted = append(*emitted, e) })

	return &adoptEnv{
		d:        d,
		runner:   runner,
		stateDir: stateDir,
		emitted:  emitted,
		cfg: &config.Config{
			RepoURL:       "ssh://git@example.com/repo.git",
			StacksBaseDir: baseDir,
			InitialDeploy: initialDeploy,
			Stacks: []config.Stack{
				{Name: "api"},
				{Name: "web"},
			},
		},
	}
}

func (e *adoptEnv) upCalls() int {
	n := 0
	for _, c := range e.runner.calls {
		if slices.Contains(c.args, "up") {
			n++
		}
	}
	return n
}

func (e *adoptEnv) statusesFor(stack string) []events.Status {
	var out []events.Status
	for _, ev := range *e.emitted {
		if ev.Stack == stack {
			out = append(out, ev.Status)
		}
	}
	return out
}

// The default is unchanged: with nothing recorded, every stack deploys.
func TestDeployAllStacks_FullInitialDeployDeploysEverything(t *testing.T) {
	env := newAdoptEnv(t, config.InitialDeployFull)

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if got := env.upCalls(); got != 2 {
		t.Errorf("up calls = %d, want 2 — a first run with no state must deploy every stack", got)
	}
}

// initial_deploy: adopt takes the running stacks to be the repo's version:
// nothing is deployed, but the state is recorded as if it had been.
func TestDeployAllStacks_AdoptRecordsStateWithoutDeploying(t *testing.T) {
	env := newAdoptEnv(t, config.InitialDeployAdopt)

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if got := env.upCalls(); got != 0 {
		t.Errorf("up calls = %d, want 0 — adopting must not run docker compose", got)
	}
	for _, stack := range []string{"api", "web"} {
		if got := env.statusesFor(stack); !slices.Contains(got, events.StatusSkipped) {
			t.Errorf("%s statuses = %v, want a skipped event", stack, got)
		}
		if slices.Contains(env.statusesFor(stack), events.StatusDeploying) {
			t.Errorf("%s must not report a deploy it never ran", stack)
		}
	}

	// The hashes are on disk, so the stacks are now genuinely up to date.
	state, err := loadPersistedDeployState(env.stateDir)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	for _, stack := range []string{"api", "web"} {
		if len(state.hashesFor(stack)) == 0 {
			t.Errorf("%s: adopting must record its input hashes", stack)
		}
		if state.ProjectDirs[stack] == "" {
			t.Errorf("%s: adopting must record its project dir, or orphan detection loses it", stack)
		}
	}
}

// Adopting is a one-off bootstrap, not a mode: once state exists, a real change
// deploys normally.
func TestDeployAllStacks_AdoptOnlyAppliesWhileNothingIsRecorded(t *testing.T) {
	env := newAdoptEnv(t, config.InitialDeployAdopt)
	env.d.DeployAllStacks(context.Background(), env.cfg)
	if got := env.upCalls(); got != 0 {
		t.Fatalf("first run: up calls = %d, want 0", got)
	}

	// A second run with nothing changed stays quiet…
	env.d.DeployAllStacks(context.Background(), env.cfg)
	if got := env.upCalls(); got != 0 {
		t.Errorf("unchanged second run: up calls = %d, want 0", got)
	}

	// …but an edited compose file now deploys for real.
	writeFile(t, filepath.Join(env.cfg.StacksBaseDir, "api", "docker-compose.yml"), composeWithImage("nginx:1.26"))
	env.d.DeployAllStacks(context.Background(), env.cfg)
	if got := env.upCalls(); got != 1 {
		t.Errorf("after a change: up calls = %d, want 1 — adopt must not survive the bootstrap run", got)
	}
	if got := env.statusesFor("api"); !slices.Contains(got, events.StatusSuccess) {
		t.Errorf("api statuses = %v, want a real deploy after the change", got)
	}
}

// A stack added to the repo later is genuinely new, not something already
// running — it must deploy even though the config still says adopt.
func TestDeployAllStacks_AdoptDoesNotSwallowAStackAddedLater(t *testing.T) {
	env := newAdoptEnv(t, config.InitialDeployAdopt)
	env.d.DeployAllStacks(context.Background(), env.cfg)

	dir := filepath.Join(env.cfg.StacksBaseDir, "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("redis:7"))
	env.cfg.Stacks = append(env.cfg.Stacks, config.Stack{Name: "cache"})

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if got := env.upCalls(); got != 1 {
		t.Errorf("up calls = %d, want 1 — a newly added stack must deploy", got)
	}
	if got := env.statusesFor("cache"); !slices.Contains(got, events.StatusSuccess) {
		t.Errorf("cache statuses = %v, want a real deploy", got)
	}
}
