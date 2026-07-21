package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
)

// fakeDoer is a fake httpDoer returning canned status codes: one per attempt,
// with the last repeating. A non-nil err is returned instead on every attempt.
type fakeDoer struct {
	statuses []int
	err      error
	attempts int
}

func (f *fakeDoer) Do(_ *http.Request) (*http.Response, error) {
	f.attempts++
	if f.err != nil {
		return nil, f.err
	}
	status := f.statuses[min(f.attempts, len(f.statuses))-1]
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d status", status),
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

// --- waitHealthy unit tests ---

func TestWaitHealthy_SucceedsOnFirst2xx(t *testing.T) {
	doer := &fakeDoer{statuses: []int{200}}
	p := &httpHealthProber{doer: doer, interval: time.Millisecond}

	if err := p.waitHealthy(context.Background(), "http://localhost/health", time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", doer.attempts)
	}
}

func TestWaitHealthy_RetriesUntilSuccess(t *testing.T) {
	doer := &fakeDoer{statuses: []int{500, 503, 204}}
	p := &httpHealthProber{doer: doer, interval: time.Millisecond}

	if err := p.waitHealthy(context.Background(), "http://localhost/health", time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", doer.attempts)
	}
}

func TestWaitHealthy_FailsWhenTimeoutElapses(t *testing.T) {
	doer := &fakeDoer{statuses: []int{500}}
	p := &httpHealthProber{doer: doer, interval: time.Millisecond}

	err := p.waitHealthy(context.Background(), "http://localhost/health", 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected error after timeout")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected last probe error (status 500) in message, got: %v", err)
	}
	if doer.attempts < 2 {
		t.Errorf("expected multiple attempts before giving up, got %d", doer.attempts)
	}
}

func TestWaitHealthy_ConnectionErrorRetriesAndFails(t *testing.T) {
	doer := &fakeDoer{err: fmt.Errorf("connection refused")}
	p := &httpHealthProber{doer: doer, interval: time.Millisecond}

	err := p.waitHealthy(context.Background(), "http://localhost/health", 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected error after timeout")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected last probe error in message, got: %v", err)
	}
}

// --- resolveHealthCheck unit tests ---

func TestResolveHealthCheck_ExplicitConfigWins(t *testing.T) {
	explicit := &config.HealthCheck{TimeoutSeconds: 45, URL: "http://localhost:8080/ping"}
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, composeWithHealthcheck("nginx:1.26"))
	cf := mustParseCompose(t, path)

	got := resolveHealthCheck(explicit, cf)
	if got != explicit {
		t.Errorf("expected the explicit deploy_health_check to be returned unchanged, got %+v", got)
	}
}

func TestResolveHealthCheck_AutoDetectsFromComposeHealthcheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, composeWithHealthcheck("nginx:1.26"))
	cf := mustParseCompose(t, path)

	got := resolveHealthCheck(nil, cf)
	if got == nil {
		t.Fatal("expected an automatic deploy_health_check, got nil")
	}
	if got.TimeoutSeconds != config.DefaultHealthCheckTimeoutSeconds {
		t.Errorf("expected default timeout %d, got %d", config.DefaultHealthCheckTimeoutSeconds, got.TimeoutSeconds)
	}
	if got.URL != "" {
		t.Errorf("expected no URL probe on an automatic gate, got %q", got.URL)
	}
}

func TestResolveHealthCheck_ExplicitDisableOverridesAutoDetect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, composeWithHealthcheck("nginx:1.26"))
	cf := mustParseCompose(t, path)

	disabled := &config.HealthCheck{Enabled: boolPtr(false)}
	if got := resolveHealthCheck(disabled, cf); got != nil {
		t.Errorf("expected an explicit deploy_health_check: false to suppress the automatic gate, got %+v", got)
	}
}

func TestResolveHealthCheck_NilWithoutComposeHealthcheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, composeWithImage("nginx:1.26"))
	cf := mustParseCompose(t, path)

	if got := resolveHealthCheck(nil, cf); got != nil {
		t.Errorf("expected no automatic deploy_health_check without a compose healthcheck, got %+v", got)
	}
}

func TestResolveHealthCheck_NilWhenComposeDidNotParse(t *testing.T) {
	if got := resolveHealthCheck(nil, nil); got != nil {
		t.Errorf("expected no automatic deploy_health_check when compose could not be parsed, got %+v", got)
	}
}

// --- deployStackIfChanged integration ---

// makeBaseWithStack creates <base>/mystack/docker-compose.yml and returns base.
func makeBaseWithStack(t *testing.T) string {
	t.Helper()
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithImage("nginx:1.26"))
	return baseDir
}

// makeBaseWithHealthcheckStack is makeBaseWithStack but the compose service
// declares a Docker healthcheck, for tests of the automatic deploy_health_check
// gate (ADR-0046).
func makeBaseWithHealthcheckStack(t *testing.T) string {
	t.Helper()
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), composeWithHealthcheck("nginx:1.26"))
	return baseDir
}

// upCalls returns all recorded calls whose args contain "up".
func upCalls(calls []runCall) []runCall {
	var ups []runCall
	for _, c := range calls {
		if slices.Contains(c.args, "up") {
			ups = append(ups, c)
		}
	}
	return ups
}

func TestDeployStack_UpWaitsForHealthWhenConfigured(t *testing.T) {
	baseDir := makeBaseWithStack(t)

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 45}}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ups := upCalls(runner.calls)
	if len(ups) != 1 {
		t.Fatalf("expected exactly 1 up call, got %d", len(ups))
	}
	for _, want := range []string{"--wait", "--wait-timeout", "45"} {
		if !slices.Contains(ups[0].args, want) {
			t.Errorf("expected up args to contain %q, got %v", want, ups[0].args)
		}
	}
}

func TestDeployStack_NoWaitFlagsWithoutHealthCheck(t *testing.T) {
	baseDir := makeBaseWithStack(t)

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()

	stack := config.Stack{Name: "mystack"}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandNotCalled(t, runner.calls, "--wait")
}

func TestDeployStack_AutoDetectsHealthCheckFromComposeHealthcheck(t *testing.T) {
	baseDir := makeBaseWithHealthcheckStack(t)

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()

	stack := config.Stack{Name: "mystack"} // no deploy_health_check declared
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ups := upCalls(runner.calls)
	if len(ups) != 1 {
		t.Fatalf("expected exactly 1 up call, got %d", len(ups))
	}
	for _, want := range []string{"--wait", "--wait-timeout", "60"} {
		if !slices.Contains(ups[0].args, want) {
			t.Errorf("expected up args to contain %q, got %v", want, ups[0].args)
		}
	}
}

func TestDeployStack_ExplicitDisableSkipsAutoGate(t *testing.T) {
	// The compose file has a healthcheck: (which would auto-gate per ADR-0046),
	// but the stack sets deploy_health_check: false — the on-demand case.
	baseDir := makeBaseWithHealthcheckStack(t)

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{Enabled: boolPtr(false)}}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertCommandNotCalled(t, runner.calls, "--wait")
}

func TestDeployStack_ExplicitHealthCheckOverridesAutoDetect(t *testing.T) {
	baseDir := makeBaseWithHealthcheckStack(t)

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 45}}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ups := upCalls(runner.calls)
	if len(ups) != 1 {
		t.Fatalf("expected exactly 1 up call, got %d", len(ups))
	}
	if !slices.Contains(ups[0].args, "45") {
		t.Errorf("expected the explicit timeout 45 to win over the default, got %v", ups[0].args)
	}
	if slices.Contains(ups[0].args, "60") {
		t.Errorf("did not expect the auto-detected default timeout, got %v", ups[0].args)
	}
}

func TestDeployStack_AutoDetectedHealthCheckFailureTriggersRollback(t *testing.T) {
	baseDir := makeBaseWithHealthcheckStack(t)
	composePath := filepath.Join(baseDir, "mystack", "docker-compose.yml")

	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(composeWithHealthcheck("nginx:1.25")),
		},
	}

	// The first "up" (the deploy under the auto-detected gate) fails; the
	// rollback's own "up" (second call) succeeds.
	failingOnce := &failUpCallsRunner{errs: map[int]error{1: fmt.Errorf("compose up: services never turned healthy")}}
	d := &Deployer{runner: failingOnce, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	stack := config.Stack{Name: "mystack"} // no deploy_health_check declared, auto-detected from compose
	state := &persistedState{
		Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if !errors.Is(err, ErrRolledBack) {
		t.Fatalf("expected ErrRolledBack, got %v", err)
	}
	if failingOnce.upCount != 2 {
		t.Errorf("expected 2 up calls (failed deploy + rollback), got %d", failingOnce.upCount)
	}
}

func TestDeployStack_HealthProbePassKeepsDeploy(t *testing.T) {
	baseDir := makeBaseWithStack(t)

	runner := &recordingRunner{}
	doer := &fakeDoer{statuses: []int{200}}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()
	d.prober = &httpHealthProber{doer: doer, interval: time.Millisecond}

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{
		TimeoutSeconds: 1, URL: "http://localhost:8080/health",
	}}
	state := newEmptyState()

	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doer.attempts != 1 {
		t.Errorf("expected 1 probe attempt, got %d", doer.attempts)
	}
	if len(upCalls(runner.calls)) != 1 {
		t.Errorf("expected no rollback up call, got %d up calls", len(upCalls(runner.calls)))
	}
	if len(state.Stacks["mystack"]) == 0 {
		t.Error("expected state to be recorded after a healthy deploy")
	}
}

func TestDeployStack_NoProbeWithoutURL(t *testing.T) {
	baseDir := makeBaseWithStack(t)

	runner := &recordingRunner{}
	doer := &fakeDoer{statuses: []int{500}}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()
	d.prober = &httpHealthProber{doer: doer, interval: time.Millisecond}

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 1}}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.attempts != 0 {
		t.Errorf("expected no probe attempts without url, got %d", doer.attempts)
	}
}

// rollbackHealsRunner flips the fake doer to healthy when the rollback up
// (the second up call) runs, simulating an old version that comes back
// healthy after the rollback.
type rollbackHealsRunner struct {
	calls []runCall
	doer  *fakeDoer
	ups   int
}

func (r *rollbackHealsRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	r.calls = append(r.calls, runCall{dir: dir, name: name, args: args})
	if slices.Contains(args, "up") {
		r.ups++
		if r.ups == 2 {
			r.doer.statuses = []int{200}
		}
	}
	return nil
}

func TestDeployStack_HealthProbeFailureRollsBack(t *testing.T) {
	t.Parallel() // the probe burns its real 1s timeout budget

	baseDir := makeBaseWithStack(t)
	composePath := filepath.Join(baseDir, "mystack", "docker-compose.yml")

	cr := &fakeCommitReader{
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(composeWithImage("nginx:1.25")),
		},
	}
	// The new version never answers healthy; the restored old version does.
	doer := &fakeDoer{statuses: []int{500}}
	runner := &rollbackHealsRunner{doer: doer}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}
	d.prober = &httpHealthProber{doer: doer, interval: 50 * time.Millisecond}

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{
		TimeoutSeconds: 1, URL: "http://localhost:8080/health",
	}}
	state := &persistedState{
		Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed health check")
	}
	if !errors.Is(err, ErrRolledBack) {
		t.Errorf("expected error wrapping ErrRolledBack, got: %v", err)
	}
	if errors.Is(err, ErrRollbackUnhealthy) {
		t.Errorf("expected error NOT wrapping ErrRollbackUnhealthy when the rollback turns healthy, got: %v", err)
	}
	if !strings.Contains(err.Error(), "health check") {
		t.Errorf("expected 'health check' in error, got: %v", err)
	}

	// The failed deploy up plus the rollback up; the rollback up runs through
	// the same health gate.
	ups := upCalls(runner.calls)
	if len(ups) != 2 {
		t.Fatalf("expected 2 up calls (deploy + rollback), got %d", len(ups))
	}
	for _, want := range []string{"--wait", "--wait-timeout", "1"} {
		if !slices.Contains(ups[1].args, want) {
			t.Errorf("expected rollback up args to contain %q, got %v", want, ups[1].args)
		}
	}
	if state.Stacks["mystack"]["old"] != "oldhash" {
		t.Error("state must not be updated after a rolled-back deploy")
	}
}

func TestDeployStack_RollbackUpStillUnhealthyWrapsErrRollbackUnhealthy(t *testing.T) {
	baseDir := makeBaseWithStack(t)
	composePath := filepath.Join(baseDir, "mystack", "docker-compose.yml")

	cr := &fakeCommitReader{
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(composeWithImage("nginx:1.25")),
		},
	}
	// Every up fails: the deploy up --wait and the rollback up --wait alike.
	runner := &recordingRunner{errOnCommand: "up"}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 45}}
	state := &persistedState{
		Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed deploy")
	}
	if !errors.Is(err, ErrRollbackUnhealthy) {
		t.Errorf("expected error wrapping ErrRollbackUnhealthy, got: %v", err)
	}
	if errors.Is(err, ErrRolledBack) {
		t.Errorf("expected error NOT wrapping ErrRolledBack when the rollback stays unhealthy, got: %v", err)
	}
	if state.Stacks["mystack"]["old"] != "oldhash" {
		t.Error("state must not be updated after an unhealthy rollback")
	}
}

func TestDeployStack_RollbackProbeFailureWrapsErrRollbackUnhealthy(t *testing.T) {
	t.Parallel() // both probes burn their real 1s timeout budget

	baseDir := makeBaseWithStack(t)
	composePath := filepath.Join(baseDir, "mystack", "docker-compose.yml")

	cr := &fakeCommitReader{
		files: map[string][]byte{
			"old-sha:" + composePath: []byte(composeWithImage("nginx:1.25")),
		},
	}
	// Neither the new version nor the restored old one ever answers healthy.
	runner := &recordingRunner{}
	doer := &fakeDoer{statuses: []int{500}}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}
	d.prober = &httpHealthProber{doer: doer, interval: 50 * time.Millisecond}

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{
		TimeoutSeconds: 1, URL: "http://localhost:8080/health",
	}}
	state := &persistedState{
		Stacks:             map[string]stackFileHashes{"mystack": {"old": "oldhash"}},
		Images:             map[string]serviceImageByName{},
		LastDeployedCommit: "old-sha",
	}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state)
	if err == nil {
		t.Fatal("expected error from failed health check")
	}
	if !errors.Is(err, ErrRollbackUnhealthy) {
		t.Errorf("expected error wrapping ErrRollbackUnhealthy, got: %v", err)
	}
	if errors.Is(err, ErrRolledBack) {
		t.Errorf("expected error NOT wrapping ErrRolledBack when the rollback stays unhealthy, got: %v", err)
	}
	// The failed deploy up plus the rollback up (which itself succeeded).
	if got := len(upCalls(runner.calls)); got != 2 {
		t.Errorf("expected 2 up calls (deploy + rollback), got %d", got)
	}
}

func TestDeployStack_HealthProbeFailureRollbackAlsoFails(t *testing.T) {
	t.Parallel() // the probe burns its real 1s timeout budget

	baseDir := makeBaseWithStack(t)

	// No commitReader: the rollback cannot fetch the old compose file.
	runner := &recordingRunner{}
	doer := &fakeDoer{statuses: []int{500}}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()
	d.prober = &httpHealthProber{doer: doer, interval: 50 * time.Millisecond}

	stack := config.Stack{Name: "mystack", DeployHealthCheck: &config.HealthCheck{
		TimeoutSeconds: 1, URL: "http://localhost:8080/health",
	}}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState())
	if err == nil {
		t.Fatal("expected error from failed health check")
	}
	if errors.Is(err, ErrRolledBack) {
		t.Error("expected error NOT wrapping ErrRolledBack when rollback fails")
	}
	// Nothing was restored, so this is a plain failure — not an unhealthy rollback.
	if errors.Is(err, ErrRollbackUnhealthy) {
		t.Error("expected error NOT wrapping ErrRollbackUnhealthy when the rollback never ran")
	}
	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("expected 'rollback also failed' in error, got: %v", err)
	}
}
