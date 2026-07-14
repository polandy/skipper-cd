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

	stack := config.Stack{Name: "mystack", HealthCheck: &config.HealthCheck{TimeoutSeconds: 45}}
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

func TestDeployStack_HealthProbePassKeepsDeploy(t *testing.T) {
	baseDir := makeBaseWithStack(t)

	runner := &recordingRunner{}
	doer := &fakeDoer{statuses: []int{200}}
	d := newDeployerWithRunner(runner)
	d.stateDir = t.TempDir()
	d.prober = &httpHealthProber{doer: doer, interval: time.Millisecond}

	stack := config.Stack{Name: "mystack", HealthCheck: &config.HealthCheck{
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

	stack := config.Stack{Name: "mystack", HealthCheck: &config.HealthCheck{TimeoutSeconds: 1}}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.attempts != 0 {
		t.Errorf("expected no probe attempts without url, got %d", doer.attempts)
	}
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
	runner := &recordingRunner{}
	doer := &fakeDoer{statuses: []int{500}}
	d := &Deployer{runner: runner, commitReader: cr, repoDir: baseDir, stateDir: t.TempDir()}
	d.prober = &httpHealthProber{doer: doer, interval: 50 * time.Millisecond}

	stack := config.Stack{Name: "mystack", HealthCheck: &config.HealthCheck{
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
	if !strings.Contains(err.Error(), "health check") {
		t.Errorf("expected 'health check' in error, got: %v", err)
	}

	// The failed deploy up plus the rollback up.
	if got := len(upCalls(runner.calls)); got != 2 {
		t.Errorf("expected 2 up calls (deploy + rollback), got %d", got)
	}
	if state.Stacks["mystack"]["old"] != "oldhash" {
		t.Error("state must not be updated after a rolled-back deploy")
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

	stack := config.Stack{Name: "mystack", HealthCheck: &config.HealthCheck{
		TimeoutSeconds: 1, URL: "http://localhost:8080/health",
	}}

	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState())
	if err == nil {
		t.Fatal("expected error from failed health check")
	}
	if errors.Is(err, ErrRolledBack) {
		t.Error("expected error NOT wrapping ErrRolledBack when rollback fails")
	}
	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("expected 'rollback also failed' in error, got: %v", err)
	}
}
