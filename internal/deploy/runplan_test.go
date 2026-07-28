package deploy

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/config"
)

// makeStacks creates a stack directory with a compose file for each name under
// baseDir, so every stack looks changed against an empty state.
func makeStacks(t *testing.T, baseDir string, names ...string) {
	t.Helper()
	for _, name := range names {
		dir := filepath.Join(baseDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	}
}

func TestComputeRunPlan_ChangedUnpausedStacksInDeployOrder(t *testing.T) {
	baseDir := t.TempDir()
	makeStacks(t, baseDir, "alpha", "beta", "gamma")

	d := newDeployerWithRunner(&recordingRunner{})
	cfg := &config.Config{
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "alpha"}, {Name: "beta"}, {Name: "gamma"}},
	}

	// Record beta as already deployed so it is unchanged and must be excluded;
	// alpha and gamma stay changed and keep their config order.
	betaHashes, err := computePerFileHashes(filepath.Join(baseDir, "beta"), nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	state := &persistedState{
		Stacks: map[string]stackFileHashes{"beta": betaHashes},
		Images: map[string]serviceImageByName{},
	}

	plan := d.computeRunPlan(cfg, state)
	if want := []string{"alpha", "gamma"}; !slices.Equal(plan, want) {
		t.Fatalf("plan = %v, want %v", plan, want)
	}
}

func TestComputeRunPlan_ExcludesPausedStacks(t *testing.T) {
	baseDir := t.TempDir()
	makeStacks(t, baseDir, "alpha", "beta")

	// Global autosync on (nil), beta paused via its config value.
	d := New(Config{
		Runner:   &recordingRunner{},
		Autosync: autosync.NewController(nil, map[string]*bool{"beta": off()}),
		Queue:    autosync.NewQueue(),
	})

	cfg := &config.Config{
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "alpha"}, {Name: "beta"}},
	}

	plan := d.computeRunPlan(cfg, newEmptyState())
	if want := []string{"alpha"}; !slices.Equal(plan, want) {
		t.Fatalf("plan = %v, want %v (paused beta must be excluded)", plan, want)
	}
}

func TestDeployAllStacks_PublishesShrinkingUpcoming(t *testing.T) {
	baseDir := t.TempDir()
	makeStacks(t, baseDir, "alpha", "beta", "gamma")

	var got [][]string
	d := New(Config{Runner: &recordingRunner{}, StateDir: t.TempDir(), RunPlanSink: func(p RunPlan) {
		got = append(got, append([]string{}, p.Upcoming...))
	}})

	cfg := &config.Config{
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "alpha"}, {Name: "beta"}, {Name: "gamma"}},
	}
	d.DeployAllStacks(context.Background(), cfg)

	// alpha deploying -> [beta gamma]; beta -> [gamma]; gamma -> []; run end -> [].
	want := [][]string{{"beta", "gamma"}, {"gamma"}, {}, {}}
	if len(got) != len(want) {
		t.Fatalf("published %d run-plan snapshots %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("snapshot %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestDeployAllStacks_ClearsRunPlanAtEnd(t *testing.T) {
	baseDir := t.TempDir()
	makeStacks(t, baseDir, "alpha")

	d := New(Config{Runner: &recordingRunner{}, StateDir: t.TempDir(), RunPlanSink: func(RunPlan) {}})

	cfg := &config.Config{
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "alpha"}},
	}
	d.DeployAllStacks(context.Background(), cfg)

	if up := d.CurrentRunPlan().Upcoming; len(up) != 0 {
		t.Fatalf("CurrentRunPlan().Upcoming = %v after run, want empty", up)
	}
}

func TestComputeRunPlan_SkippedWhenNoSink(t *testing.T) {
	// With no run-plan sink installed (UI off), a deploy run must still work and
	// leave an empty current plan — the upfront hashing pass is skipped entirely.
	baseDir := t.TempDir()
	makeStacks(t, baseDir, "alpha")

	d := New(Config{Runner: &recordingRunner{}, StateDir: t.TempDir()})
	cfg := &config.Config{
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "alpha"}},
	}
	d.DeployAllStacks(context.Background(), cfg)

	if up := d.CurrentRunPlan().Upcoming; len(up) != 0 {
		t.Fatalf("CurrentRunPlan().Upcoming = %v, want empty", up)
	}
}

func TestCurrentRunPlan_ReflectsLastPublished(t *testing.T) {
	d := New(Config{Runner: &recordingRunner{}, StateDir: t.TempDir(), RunPlanSink: func(RunPlan) {}})

	d.publishRunPlan([]string{"x", "y"})
	if up := d.CurrentRunPlan().Upcoming; !slices.Equal(up, []string{"x", "y"}) {
		t.Fatalf("CurrentRunPlan().Upcoming = %v, want [x y]", up)
	}
}
