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

// bootstrapEnv is a deployer over two real stack dirs with a fresh state dir,
// so the first run is always a bootstrap run (nothing recorded).
type bootstrapEnv struct {
	d        *Deployer
	runner   *recordingRunner
	stateDir string
	cfg      *config.Config
	emitted  *[]events.DeployEvent
}

func newBootstrapEnv(t *testing.T) *bootstrapEnv {
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
	emitted := &[]events.DeployEvent{}
	d := New(Config{
		Runner:       runner,
		CommitReader: &fakeCommitReader{},
		RepoDir:      baseDir,
		StateDir:     stateDir,
		EventSink:    func(e events.DeployEvent) { *emitted = append(*emitted, e) },
	})

	return &bootstrapEnv{
		d:        d,
		runner:   runner,
		stateDir: stateDir,
		emitted:  emitted,
		cfg: &config.Config{
			RepoURL:       "ssh://git@example.com/repo.git",
			StacksBaseDir: baseDir,
			Stacks:        []config.Stack{{Name: "api"}, {Name: "web"}},
		},
	}
}

func (e *bootstrapEnv) countCalls(subcommand string) int {
	n := 0
	for _, c := range e.runner.calls {
		if slices.Contains(c.args, subcommand) {
			n++
		}
	}
	return n
}

func (e *bootstrapEnv) statusesFor(stack string) []events.Status {
	var out []events.Status
	for _, ev := range *e.emitted {
		if ev.Stack == stack {
			out = append(out, ev.Status)
		}
	}
	return out
}

// The host is still converged: every stack is deployed, so a stack that is not
// actually running (or no longer matches the repo) is brought up by `up`.
func TestDeployAllStacks_BootstrapStillDeploysEveryStack(t *testing.T) {
	env := newBootstrapEnv(t)

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if got := env.countCalls("up"); got != 2 {
		t.Errorf("up calls = %d, want 2 — a bootstrap run must still converge every stack", got)
	}
	for _, stack := range []string{"api", "web"} {
		if got := env.statusesFor(stack); !slices.Contains(got, events.StatusSuccess) {
			t.Errorf("%s statuses = %v, want a successful deploy", stack, got)
		}
	}
}

// …but it does not force-refresh images. With nothing recorded every image
// reads as changed, so an unsuppressed pull would move every floating tag on
// the host at once — unattended, and never asked for.
func TestDeployAllStacks_BootstrapDoesNotPull(t *testing.T) {
	env := newBootstrapEnv(t)

	env.d.DeployAllStacks(context.Background(), env.cfg)

	if got := env.countCalls("pull"); got != 0 {
		t.Errorf("pull calls = %d, want 0 on a bootstrap run", got)
	}
}

// Suppression lasts exactly one run: once state exists, a changed image tag
// pulls normally.
func TestDeployAllStacks_PullsAgainAfterTheBootstrapRun(t *testing.T) {
	env := newBootstrapEnv(t)
	env.d.DeployAllStacks(context.Background(), env.cfg)
	if got := env.countCalls("pull"); got != 0 {
		t.Fatalf("bootstrap run: pull calls = %d, want 0", got)
	}

	// An unchanged second run pulls nothing either — for the ordinary reason.
	env.d.DeployAllStacks(context.Background(), env.cfg)
	if got := env.countCalls("pull"); got != 0 {
		t.Errorf("unchanged run: pull calls = %d, want 0", got)
	}

	writeFile(t, filepath.Join(env.cfg.StacksBaseDir, "api", "docker-compose.yml"), composeWithImage("nginx:1.26"))
	env.d.DeployAllStacks(context.Background(), env.cfg)
	if got := env.countCalls("pull"); got != 1 {
		t.Errorf("after an image change: pull calls = %d, want 1 — suppression must not outlive the bootstrap run", got)
	}
}

// A build: service's --pull refreshes its Dockerfile base image, which is the
// same unasked-for jump on a bootstrap run.
func TestDeployAllStacks_BootstrapBuildsWithoutPull(t *testing.T) {
	baseDir := t.TempDir()
	dir := filepath.Join(baseDir, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM alpine:3.20\n")
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), "services:\n  app:\n    build: .\n")

	runner := &recordingRunner{}
	d := New(Config{Runner: runner, CommitReader: &fakeCommitReader{}, RepoDir: baseDir, StateDir: t.TempDir()})
	cfg := &config.Config{
		RepoURL:       "ssh://git@example.com/repo.git",
		StacksBaseDir: baseDir,
		Stacks:        []config.Stack{{Name: "app"}},
	}

	d.DeployAllStacks(context.Background(), cfg)

	var buildArgs []string
	for _, c := range runner.calls {
		if slices.Contains(c.args, "build") {
			buildArgs = c.args
		}
	}
	if buildArgs == nil {
		t.Fatal("expected a docker compose build call")
	}
	if slices.Contains(buildArgs, "--pull") {
		t.Errorf("build args = %v, want no --pull on a bootstrap run", buildArgs)
	}
}
