package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// gaugeValue reads the current value of one labelled gauge. A label that was
// deleted reads as 0, which is what a cleared config error must look like.
func gaugeValue(t *testing.T, stack string) float64 {
	t.Helper()
	var m dto.Metric
	if err := metrics.StackConfigError.WithLabelValues(stack).Write(&m); err != nil {
		t.Fatalf("read gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

func failedEvents(recorded []events.DeployEvent, stack string) []events.DeployEvent {
	var out []events.DeployEvent
	for _, e := range recorded {
		if e.Stack == stack && e.Status == events.StatusFailed {
			out = append(out, e)
		}
	}
	return out
}

func TestConfigErrorLog_RecordsFirstSightingAndChangesOnly(t *testing.T) {
	log := newConfigErrorLog()

	if !log.record("alpha", "boom") {
		t.Error("first sighting must be reported")
	}
	if log.record("alpha", "boom") {
		t.Error("unchanged error must not be reported again")
	}
	if !log.record("alpha", "different boom") {
		t.Error("changed error must be reported")
	}
	if log.record("alpha", "different boom") {
		t.Error("changed error must be reported only once")
	}
}

func TestConfigErrorLog_ForgetOthersClearsGoneErrors(t *testing.T) {
	log := newConfigErrorLog()
	log.record("alpha", "boom")
	log.record("beta", "boom")
	log.record("gamma", "boom")

	if got := log.forgetOthers(map[string]bool{"beta": true}); len(got) != 2 || got[0] != "alpha" || got[1] != "gamma" {
		t.Fatalf("forgetOthers = %v, want [alpha gamma]", got)
	}
	if log.record("beta", "boom") {
		t.Error("beta is still broken: its error must stay remembered")
	}
	if !log.record("alpha", "boom") {
		t.Error("alpha's error was cleared: the same error must be reported again")
	}
}

func TestDeployAllStacks_ReportsStandingConfigErrorOnce(t *testing.T) {
	// An override without a stack directory is a standing config error: it reads
	// identically on every reconcile, so it is announced once (ADR-0055).
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, []config.Stack{{Name: "ghost-once"}})
	d, _, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)
	d.DeployAllStacks(context.Background(), cfg)

	if got := failedEvents(*recorded, "ghost-once"); len(got) != 1 {
		t.Errorf("failed events for ghost-once = %d, want 1", len(got))
	}
	// Positive signal that the second run did reach the stack phase and read the
	// error again, rather than not running at all.
	if got := eventsWithStatus(*recorded, events.StatusSkipped); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("skipped events = %v, want [alpha] from the second run", got)
	}
	if got := gaugeValue(t, "ghost-once"); got != 1 {
		t.Errorf("skipper_stack_config_error{stack=ghost-once} = %v, want 1 while the error stands", got)
	}
}

func TestDeployAllStacks_ReportsConfigErrorAgainWhenMessageChanges(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, []config.Stack{{Name: "ghost-changed"}})
	d, _, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)

	// The directory appears, but its compose file does not parse: same stack,
	// a different reason to exclude it — the operator must hear about it.
	dir := filepath.Join(repoDir, "stacks", "ghost-changed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), "services: [not, a, mapping\n")
	d.DeployAllStacks(context.Background(), cfg)

	got := failedEvents(*recorded, "ghost-changed")
	if len(got) != 2 {
		t.Fatalf("failed events for ghost-changed = %d, want 2 (missing dir, then broken compose)", len(got))
	}
	if !strings.Contains(got[0].Error, "no stack directory") {
		t.Errorf("first failure = %q, want the missing-directory error", got[0].Error)
	}
	if got[1].Error == got[0].Error {
		t.Errorf("second failure repeats the first (%q); it must carry the new reason", got[1].Error)
	}
}

func TestDeployAllStacks_ReportsConfigErrorAgainAfterItCleared(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, []config.Stack{{Name: "ghost-cleared"}})
	d, _, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)

	// The stack directory is added: the error is gone and the stack deploys.
	dir := filepath.Join(repoDir, "stacks", "ghost-cleared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	d.DeployAllStacks(context.Background(), cfg)

	if got := gaugeValue(t, "ghost-cleared"); got != 0 {
		t.Errorf("skipper_stack_config_error{stack=ghost-cleared} = %v, want 0 once the error cleared", got)
	}

	// And it breaks again the same way: a cleared error is not remembered, so
	// the return must be announced.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	d.DeployAllStacks(context.Background(), cfg)

	if got := failedEvents(*recorded, "ghost-cleared"); len(got) != 2 {
		t.Errorf("failed events for ghost-cleared = %d, want 2 (before the fix and after the regression)", len(got))
	}
}

func TestDeployAllStacks_ReportsStandingDiscoveryFailureOnce(t *testing.T) {
	// A leftover in-repo skipper.yaml fails the whole stack phase (ADR-0043).
	// It too stands until someone migrates it, so it is announced once.
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, nil)
	writeFile(t, filepath.Join(repoDir, "stacks", config.RepoConfigFileName), "stacks:\n  alpha:\n    icon: nginx\n")
	d, _, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)
	d.DeployAllStacks(context.Background(), cfg)

	got := failedEvents(*recorded, ConfigStateKey)
	if len(got) != 1 {
		t.Fatalf("failed events for %s = %d, want 1", ConfigStateKey, len(got))
	}
	if !strings.Contains(got[0].Error, "no stacks deploy this run") {
		t.Errorf("message = %q, want the consequence of the abort spelled out", got[0].Error)
	}
	// Positive signal that the second run reached the same decision point: the
	// gauge is (re)set there on every run, event or no event.
	if got := gaugeValue(t, ConfigStateKey); got != 1 {
		t.Errorf("skipper_stack_config_error{stack=%s} = %v, want 1 while discovery keeps failing", ConfigStateKey, got)
	}
}
