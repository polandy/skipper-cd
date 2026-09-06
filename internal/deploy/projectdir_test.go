package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// fakeProjectDirSyncer records every fast-forward attempt and answers from a
// scripted outcome. The call count is the positive signal the ordering and
// report-once tests assert against.
type fakeProjectDirSyncer struct {
	dir      string
	from, to string
	err      error
	calls    int
	// onCall runs before each answer, so a test can observe what the rest of
	// the run had done by the time the phase ran.
	onCall func()
}

func (f *fakeProjectDirSyncer) Dir() string { return f.dir }

func (f *fakeProjectDirSyncer) FastForward(_ context.Context) (string, string, error) {
	f.calls++
	if f.onCall != nil {
		f.onCall()
	}
	return f.from, f.to, f.err
}

// projectDirEnv is a deployer over one real stack with a project-directory
// syncer wired in.
type projectDirEnv struct {
	d       *Deployer
	syncer  *fakeProjectDirSyncer
	runner  *recordingRunner
	cfg     *config.Config
	emitted *[]events.DeployEvent
}

func newProjectDirEnv(t *testing.T, syncer *fakeProjectDirSyncer) *projectDirEnv {
	t.Helper()
	baseDir := t.TempDir()
	dir := filepath.Join(baseDir, "web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	runner := &recordingRunner{}
	emitted := &[]events.DeployEvent{}
	d := New(Config{
		Runner:           runner,
		CommitReader:     &fakeCommitReader{},
		ProjectDirSyncer: syncer,
		RepoDir:          baseDir,
		StateDir:         t.TempDir(),
		EventSink:        func(e events.DeployEvent) { *emitted = append(*emitted, e) },
	})
	return &projectDirEnv{
		d:      d,
		syncer: syncer,
		runner: runner,
		cfg: &config.Config{
			RepoURL:       "ssh://git@example.com/repo.git",
			StacksBaseDir: baseDir,
			Stacks:        []config.Stack{{Name: "web"}},
		},
		emitted: emitted,
	}
}

func (e *projectDirEnv) run(t *testing.T) {
	t.Helper()
	e.d.DeployAllStacks(context.Background(), e.cfg)
}

func (e *projectDirEnv) eventsFor(stack string) []events.DeployEvent {
	var out []events.DeployEvent
	for _, ev := range *e.emitted {
		if ev.Stack == stack {
			out = append(out, ev)
		}
	}
	return out
}

func TestSyncProjectDirectory_FastForwardsBeforeAnyStackDeploys(t *testing.T) {
	var composeCallsAtSync int
	syncer := &fakeProjectDirSyncer{dir: "/srv/modules", from: "aaa", to: "bbb"}
	env := newProjectDirEnv(t, syncer)
	// The whole point of the phase is its position: a container recreated by
	// this run must read the fast-forwarded file, not the stale one.
	syncer.onCall = func() { composeCallsAtSync = len(env.runner.calls) }

	env.run(t)

	if syncer.calls != 1 {
		t.Fatalf("expected exactly one fast-forward per run, got %d", syncer.calls)
	}
	if composeCallsAtSync != 0 {
		t.Fatalf("expected the fast-forward before the first docker call, but %d had already run", composeCallsAtSync)
	}
	if len(env.runner.calls) == 0 {
		t.Fatal("expected the run to have deployed the stack; with no docker calls the ordering assertion proves nothing")
	}
	if evs := env.eventsFor(ProjectDirStateKey); len(evs) != 0 {
		t.Fatalf("a fast-forward that worked is plumbing, not history; got %+v", evs)
	}
}

func TestSyncProjectDirectory_IsSkippedWhenNotConfigured(t *testing.T) {
	env := newProjectDirEnv(t, nil)
	env.d.projectDirSyncer = nil

	env.run(t)

	if evs := env.eventsFor(ProjectDirStateKey); len(evs) != 0 {
		t.Fatalf("expected no %s events without the phase configured, got %+v", ProjectDirStateKey, evs)
	}
	if len(env.runner.calls) == 0 {
		t.Fatal("expected the run to have deployed the stack")
	}
}

func TestSyncProjectDirectory_RefusalReportsButDoesNotAbortTheRun(t *testing.T) {
	syncer := &fakeProjectDirSyncer{dir: "/srv/modules", err: errors.New("the checkout has uncommitted changes to tracked files in /srv/modules")}
	env := newProjectDirEnv(t, syncer)

	env.run(t)

	evs := env.eventsFor(ProjectDirStateKey)
	if len(evs) != 1 || evs[0].Status != events.StatusFailed {
		t.Fatalf("expected one failed %s event, got %+v", ProjectDirStateKey, evs)
	}
	if !strings.Contains(evs[0].Error, "uncommitted changes") {
		t.Fatalf("expected the refusal's reason on the event, got %q", evs[0].Error)
	}
	// A stale mount is degraded, not wrong: the stacks still converge.
	assertCommandCalled(t, env.runner.calls, "up")
}

func TestSyncProjectDirectory_ReportsAStandingRefusalOnlyOnce(t *testing.T) {
	syncer := &fakeProjectDirSyncer{dir: "/srv/modules", err: errors.New("the checkout has uncommitted changes to tracked files in /srv/modules")}
	env := newProjectDirEnv(t, syncer)

	env.run(t)
	env.run(t)
	env.run(t)

	if syncer.calls != 3 {
		t.Fatalf("expected the phase to run every pass, got %d", syncer.calls)
	}
	if evs := env.eventsFor(ProjectDirStateKey); len(evs) != 1 {
		t.Fatalf("expected the standing refusal reported once across three runs, got %d: %+v", len(evs), evs)
	}
}

func TestSyncProjectDirectory_ReportsAgainWhenTheReasonChanges(t *testing.T) {
	syncer := &fakeProjectDirSyncer{dir: "/srv/modules", err: errors.New("the checkout has uncommitted changes to tracked files in /srv/modules")}
	env := newProjectDirEnv(t, syncer)

	env.run(t)
	syncer.err = errors.New("the checkout has diverged from its upstream branch (/srv/modules)")
	env.run(t)

	evs := env.eventsFor(ProjectDirStateKey)
	if len(evs) != 2 {
		t.Fatalf("expected a new report when the reason changed, got %d: %+v", len(evs), evs)
	}
	if !strings.Contains(evs[1].Error, "diverged") {
		t.Fatalf("expected the second report to carry the new reason, got %q", evs[1].Error)
	}
}

func TestSyncProjectDirectory_AnnouncesTheNextRefusalAfterARecovery(t *testing.T) {
	dirty := errors.New("the checkout has uncommitted changes to tracked files in /srv/modules")
	syncer := &fakeProjectDirSyncer{dir: "/srv/modules", err: dirty}
	env := newProjectDirEnv(t, syncer)

	env.run(t)
	syncer.err = nil // the operator committed their work
	env.run(t)
	syncer.err = dirty // and started editing again
	env.run(t)

	if evs := env.eventsFor(ProjectDirStateKey); len(evs) != 2 {
		t.Fatalf("expected the refusal announced again after it had cleared, got %d: %+v", len(evs), evs)
	}
}

func TestProjectDirStateKey_IsReservedAgainstConfiguredStacks(t *testing.T) {
	if !config.IsReservedStackName(ProjectDirStateKey) {
		t.Fatalf("%s must be reserved, or a real stack could take the key", ProjectDirStateKey)
	}
}
