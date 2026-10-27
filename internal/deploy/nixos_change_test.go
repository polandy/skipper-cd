package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/nixos"
)

// --- derivation-based change detection (ADR-0065) ---

const (
	testDrvA = "/nix/store/aaaa-nixos-system-host-a-26.05.drv"
	testDrvB = "/nix/store/bbbb-nixos-system-host-a-26.05.drv"
)

// derivationPhase wires a Deployer whose recordingRunner also serves the
// evaluation through Output, plus an event recorder for the _nixos stack.
type derivationPhase struct {
	t        *testing.T
	repoDir  string
	nixFile  string
	stateDir string
	runner   *recordingRunner
	reader   *fakeCommitReader
	d        *Deployer
	events   []events.DeployEvent

	evalOut string // stdout of the next evaluation
	evalErr error  // error of the next evaluation
}

func newDerivationPhase(t *testing.T) *derivationPhase {
	t.Helper()
	p := &derivationPhase{t: t, repoDir: t.TempDir(), stateDir: t.TempDir(), reader: &fakeCommitReader{head: "c1"}}
	p.nixFile = filepath.Join(p.repoDir, "flake.nix")
	writeFile(t, p.nixFile, "{ }")
	p.runner = &recordingRunner{outputFn: func(_ int, _ []string) ([]byte, error) {
		return []byte(p.evalOut), p.evalErr
	}}
	p.d = p.newDeployer(p.runner)
	return p
}

func (p *derivationPhase) newDeployer(runner Runner) *Deployer {
	return New(Config{
		Runner:       runner,
		Outputter:    p.runner,
		RepoDir:      p.repoDir,
		StateDir:     p.stateDir,
		CommitReader: p.reader,
		EventSink: func(e events.DeployEvent) {
			if e.Stack == NixosStateKey {
				p.events = append(p.events, e)
			}
		},
	})
}

func derivationCfg() *config.Config {
	enabled := true
	return &config.Config{NixOSRebuild: &config.NixOSRebuild{
		Enabled:         &enabled,
		Flake:           ".#host-a",
		ChangeDetection: config.NixOSChangeDetectionDerivation,
	}}
}

// currentHashes returns the file hashes of the phase's repo as they are now.
func (p *derivationPhase) currentHashes() stackFileHashes {
	p.t.Helper()
	h, err := nixos.HashFiles(p.repoDir)
	if err != nil {
		p.t.Fatal(err)
	}
	return h
}

// stateWith returns a state whose _nixos entry holds the given file hashes
// and, unless drv is "", the given derivation.
func stateWith(files stackFileHashes, drv string) *persistedState {
	entry := stackFileHashes{}
	for k, v := range files {
		entry[k] = v
	}
	if drv != "" {
		entry[nixos.SystemDrvKey] = drv
	}
	s := newEmptyState()
	s.recordStack(NixosStateKey, entry)
	return s
}

func (p *derivationPhase) evalCalls() []runCall {
	var out []runCall
	for _, c := range p.runner.outputCalls {
		if c.name == "nix" {
			out = append(out, c)
		}
	}
	return out
}

func (p *derivationPhase) statuses() []events.Status {
	var out []events.Status
	for _, e := range p.events {
		out = append(out, e.Status)
	}
	return out
}

func (p *derivationPhase) lastEvent() events.DeployEvent {
	p.t.Helper()
	if len(p.events) == 0 {
		p.t.Fatal("expected a _nixos event, got none")
	}
	return p.events[len(p.events)-1]
}

func rebuildAttempts(calls []runCall) int {
	n := 0
	for _, c := range calls {
		if c.name == "systemd-run" && slices.Contains(c.args, "nixos-rebuild") {
			n++
		}
	}
	return n
}

func TestNixOSPhase_Derivation_EvaluatesWithExactArgv(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvA + "\n"

	p.d.rebuildNixOSIfChanged(context.Background(), derivationCfg(), newEmptyState())

	calls := p.evalCalls()
	if len(calls) != 1 {
		t.Fatalf("expected one evaluation, got %d", len(calls))
	}
	want := []string{
		"--extra-experimental-features", "nix-command flakes",
		"eval", "--raw", `.#nixosConfigurations."host-a"`,
		"--apply", nixos.SystemDrvApplyExpr,
	}
	if !slices.Equal(calls[0].args, want) {
		t.Errorf("eval argv = %q, want %q", calls[0].args, want)
	}
	if calls[0].dir != p.repoDir {
		t.Errorf("eval dir = %q, want the repo dir %q", calls[0].dir, p.repoDir)
	}
}

func TestNixOSPhase_Derivation_SkipsWhenDrvUnchanged(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvA
	state := stateWith(stackFileHashes{p.nixFile: "stale"}, testDrvA)

	if ok := p.d.rebuildNixOSIfChanged(context.Background(), derivationCfg(), state); !ok {
		t.Fatal("an unchanged derivation must not abort the run")
	}

	if len(p.evalCalls()) != 1 {
		t.Fatalf("expected the derivation to be evaluated once, got %d", len(p.evalCalls()))
	}
	if nixosRebuildCalled(p.runner.calls) {
		t.Error("an unchanged derivation must not rebuild, even though a nix file changed")
	}
	if got := p.statuses(); !slices.Equal(got, []events.Status{events.StatusSkipped}) {
		t.Errorf("_nixos events = %v, want [skipped]", got)
	}
	entry := state.hashesFor(NixosStateKey)
	if entry[p.nixFile] != p.currentHashes()[p.nixFile] {
		t.Error("the skip must record the current file hashes so the next rebuild lists only newer changes")
	}
	if entry[nixos.SystemDrvKey] != testDrvA {
		t.Errorf("recorded drv = %q, want %q", entry[nixos.SystemDrvKey], testDrvA)
	}
}

func TestNixOSPhase_Derivation_RebuildsWhenDrvChanged(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvB
	state := stateWith(stackFileHashes{p.nixFile: "stale"}, testDrvA)

	if ok := p.d.rebuildNixOSIfChanged(context.Background(), derivationCfg(), state); !ok {
		t.Fatal("a successful rebuild must not abort the run")
	}

	if rebuildAttempts(p.runner.calls) != 1 {
		t.Fatalf("expected one rebuild, got %d", rebuildAttempts(p.runner.calls))
	}
	ev := p.lastEvent()
	if ev.Status != events.StatusSuccess {
		t.Errorf("status = %q, want success", ev.Status)
	}
	if !slices.Equal(ev.ChangedFiles, []string{"flake.nix"}) {
		t.Errorf("changed files = %v, want [flake.nix]", ev.ChangedFiles)
	}
	if got := state.hashesFor(NixosStateKey)[nixos.SystemDrvKey]; got != testDrvB {
		t.Errorf("recorded drv = %q, want %q", got, testDrvB)
	}
}

func TestNixOSPhase_Derivation_RebuildsWhenOnlyNonNixInputChanged(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvB
	state := stateWith(p.currentHashes(), testDrvA)

	// Read the persisted marker when the rebuild starts: that is what a
	// self-restart would leave behind (ADR-0025).
	var markerAtRebuild []string
	p.runner.onRunStart = func(args []string) {
		if slices.Contains(args, "nixos-rebuild") {
			persisted, err := loadPersistedDeployState(p.stateDir)
			if err != nil {
				t.Errorf("load state: %v", err)
				return
			}
			markerAtRebuild = persisted.NixOSRebuildInFlight
		}
	}

	if ok := p.d.rebuildNixOSIfChanged(context.Background(), derivationCfg(), state); !ok {
		t.Fatal("a successful rebuild must not abort the run")
	}

	if rebuildAttempts(p.runner.calls) != 1 {
		t.Fatalf("a changed derivation must rebuild without any nix-file change, got %d rebuilds", rebuildAttempts(p.runner.calls))
	}
	ev := p.lastEvent()
	if ev.Status != events.StatusSuccess || len(ev.ChangedFiles) != 0 {
		t.Errorf("event = %q with files %v, want success with no files", ev.Status, ev.ChangedFiles)
	}
	if !slices.Equal(markerAtRebuild, []string{nixos.SystemDrvKey}) {
		t.Errorf("in-flight marker = %v, want [%s] so an empty change list still reads as in flight", markerAtRebuild, nixos.SystemDrvKey)
	}
}

func TestNixOSPhase_Derivation_ReconcilesDrvOnlyMarkerWithoutFiles(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvB
	state := stateWith(p.currentHashes(), testDrvB)
	state.markNixOSRebuildInFlight([]string{nixos.SystemDrvKey})

	if ok := p.d.rebuildNixOSIfChanged(context.Background(), derivationCfg(), state); !ok {
		t.Fatal("reconciliation must not abort the run")
	}

	if got := p.statuses(); !slices.Equal(got, []events.Status{events.StatusSuccess}) {
		t.Fatalf("_nixos events = %v, want one reconciled success", got)
	}
	if files := p.events[0].ChangedFiles; len(files) != 0 {
		t.Errorf("reconciled success must not list the reserved key as a file, got %v", files)
	}
	if nixosRebuildCalled(p.runner.calls) {
		t.Error("reconciliation must not rebuild again when the derivation is unchanged")
	}
	if len(state.NixOSRebuildInFlight) != 0 {
		t.Error("expected the in-flight marker to be cleared")
	}
}

func TestNixOSPhase_Derivation_EvaluatesOncePerCommit(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvA
	state := newEmptyState()
	cfg := derivationCfg()

	p.d.rebuildNixOSIfChanged(context.Background(), cfg, state)
	p.d.rebuildNixOSIfChanged(context.Background(), cfg, state)
	if n := len(p.evalCalls()); n != 1 {
		t.Fatalf("two runs at the same HEAD: %d evaluations, want 1", n)
	}

	p.reader.head = "c2"
	p.d.rebuildNixOSIfChanged(context.Background(), cfg, state)
	if n := len(p.evalCalls()); n != 2 {
		t.Fatalf("a new HEAD must evaluate again: %d evaluations, want 2", n)
	}
}

func TestNixOSPhase_Derivation_EvalFailureAbortsStacks(t *testing.T) {
	p := newDerivationPhase(t)
	stackDir := filepath.Join(p.repoDir, "modules", "gitea")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	p.evalErr = errors.New("simulated eval failure")
	cfg := derivationCfg()
	cfg.RepoURL = "ssh://git@example.com/repo.git"
	cfg.StacksBaseDir = filepath.Join(p.repoDir, "modules")
	cfg.Stacks = []config.Stack{{Name: "gitea"}}

	p.d.DeployAllStacks(context.Background(), cfg)

	if len(p.evalCalls()) != 1 {
		t.Fatalf("expected one evaluation, got %d", len(p.evalCalls()))
	}
	ev := p.lastEvent()
	if ev.Status != events.StatusFailed || !strings.Contains(ev.Error, "simulated eval failure") {
		t.Errorf("event = %q %q, want failed naming the eval error", ev.Status, ev.Error)
	}
	for _, c := range p.runner.calls {
		if c.name == "docker" && slices.Contains(c.args, "compose") {
			t.Fatalf("stacks must not deploy after an evaluation failure; got %v", c.args)
		}
	}
	if nixosRebuildCalled(p.runner.calls) {
		t.Error("no rebuild must run after an evaluation failure")
	}
	persisted, err := loadPersistedDeployState(p.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.hashesFor(NixosStateKey); len(got) != 0 {
		t.Errorf("nothing may be persisted after an evaluation failure, got %v", got)
	}

	// The failure must not have filled the per-commit cache: the same HEAD
	// evaluates again.
	p.evalErr = nil
	p.evalOut = testDrvA
	p.d.DeployAllStacks(context.Background(), cfg)
	if len(p.evalCalls()) != 2 {
		t.Fatalf("a failed evaluation must not be cached: %d evaluations, want 2", len(p.evalCalls()))
	}
	if rebuildAttempts(p.runner.calls) != 1 {
		t.Errorf("expected the retry to rebuild once, got %d", rebuildAttempts(p.runner.calls))
	}
}

func TestNixOSPhase_Derivation_RejectsMalformedEvalOutput(t *testing.T) {
	for name, out := range map[string]string{
		"empty":       "",
		"whitespace":  " \n",
		"not a drv":   "/nix/store/aaaa-nixos-system-host-a-26.05",
		"relative":    "aaaa-nixos-system-host-a-26.05.drv",
		"two lines":   testDrvA + "\n" + testDrvB,
		"stray words": "warning: " + testDrvA,
	} {
		t.Run(name, func(t *testing.T) {
			p := newDerivationPhase(t)
			p.evalOut = out
			state := stateWith(p.currentHashes(), testDrvA)

			if ok := p.d.rebuildNixOSIfChanged(context.Background(), derivationCfg(), state); ok {
				t.Error("malformed evaluation output must abort the run")
			}
			if len(p.evalCalls()) != 1 {
				t.Fatalf("expected one evaluation, got %d", len(p.evalCalls()))
			}
			if got := p.statuses(); !slices.Equal(got, []events.Status{events.StatusFailed}) {
				t.Errorf("_nixos events = %v, want [failed]", got)
			}
			if nixosRebuildCalled(p.runner.calls) {
				t.Error("malformed output must not rebuild")
			}
			if got := state.hashesFor(NixosStateKey)[nixos.SystemDrvKey]; got != testDrvA {
				t.Errorf("recorded drv = %q, want it untouched (%q)", got, testDrvA)
			}
		})
	}
}

func TestNixOSPhase_Derivation_RevertsDrvOnSurvivingFailure(t *testing.T) {
	p := newDerivationPhase(t)
	p.runner.errOnCommand = "switch"
	p.evalOut = testDrvB
	state := stateWith(p.currentHashes(), testDrvA)
	cfg := derivationCfg()

	if ok := p.d.rebuildNixOSIfChanged(context.Background(), cfg, state); ok {
		t.Fatal("a failed rebuild must abort the run")
	}
	if got := state.hashesFor(NixosStateKey)[nixos.SystemDrvKey]; got != testDrvA {
		t.Fatalf("recorded drv = %q, want it reverted to %q", got, testDrvA)
	}
	if p.lastEvent().Status != events.StatusFailed {
		t.Errorf("status = %q, want failed", p.lastEvent().Status)
	}

	p.d.rebuildNixOSIfChanged(context.Background(), cfg, state)
	if n := rebuildAttempts(p.runner.calls); n != 2 {
		t.Fatalf("expected the rebuild to be retried (2 attempts), got %d", n)
	}
}

func TestNixOSPhase_Derivation_KeepsDrvAndMarkerOnShutdown(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvB
	blocking := &shutdownAwareRunner{rebuildStarted: make(chan struct{})}
	shutdownCtx, requestShutdown := context.WithCancel(context.Background())
	defer requestShutdown()
	d := New(Config{
		Runner:       blocking,
		Outputter:    p.runner,
		RepoDir:      p.repoDir,
		StateDir:     p.stateDir,
		CommitReader: p.reader,
		ShutdownCtx:  shutdownCtx,
	})
	state := stateWith(p.currentHashes(), testDrvA)

	done := make(chan bool)
	go func() { done <- d.rebuildNixOSIfChanged(context.Background(), derivationCfg(), state) }()
	select {
	case <-blocking.rebuildStarted:
	case ok := <-done:
		t.Fatalf("the phase returned %v without starting a rebuild", ok)
	}
	requestShutdown()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("a shutdown-abandoned rebuild must abort the stack deploys")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rebuild wait was not abandoned on shutdown")
	}

	persisted, err := loadPersistedDeployState(p.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.hashesFor(NixosStateKey)[nixos.SystemDrvKey]; got != testDrvB {
		t.Errorf("persisted drv = %q, want the pre-saved %q", got, testDrvB)
	}
	if !slices.Equal(persisted.NixOSRebuildInFlight, []string{nixos.SystemDrvKey}) {
		t.Errorf("persisted marker = %v, want [%s]", persisted.NixOSRebuildInFlight, nixos.SystemDrvKey)
	}
}

func TestNixOSPhase_Derivation_MissingDrvKeyRebuilds(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvA
	state := stateWith(p.currentHashes(), "") // written in files mode

	p.d.rebuildNixOSIfChanged(context.Background(), derivationCfg(), state)

	if len(p.evalCalls()) != 1 {
		t.Fatalf("expected one evaluation, got %d", len(p.evalCalls()))
	}
	if rebuildAttempts(p.runner.calls) != 1 {
		t.Fatalf("a missing derivation key must count as changed, got %d rebuilds", rebuildAttempts(p.runner.calls))
	}
	if got := state.hashesFor(NixosStateKey)[nixos.SystemDrvKey]; got != testDrvA {
		t.Errorf("recorded drv = %q, want %q", got, testDrvA)
	}
}

func TestNixOSPhase_DefaultEvaluates(t *testing.T) {
	p := newDerivationPhase(t)
	p.evalOut = testDrvA
	cfg := derivationCfg()
	cfg.NixOSRebuild.ChangeDetection = ""

	p.d.rebuildNixOSIfChanged(context.Background(), cfg, newEmptyState())

	if n := len(p.evalCalls()); n != 1 {
		t.Errorf("an unset change_detection must evaluate the derivation, got %d evaluations", n)
	}
}

func TestNixOSPhase_Files_NeverEvaluates(t *testing.T) {
	p := newDerivationPhase(t)
	cfg := derivationCfg()
	cfg.NixOSRebuild.ChangeDetection = config.NixOSChangeDetectionFiles

	p.d.rebuildNixOSIfChanged(context.Background(), cfg, newEmptyState())

	if rebuildAttempts(p.runner.calls) != 1 {
		t.Fatalf("files mode must rebuild on a nix-file change, got %d rebuilds", rebuildAttempts(p.runner.calls))
	}
	if n := len(p.evalCalls()); n != 0 {
		t.Errorf("files mode must never evaluate, got %d evaluations", n)
	}
}
