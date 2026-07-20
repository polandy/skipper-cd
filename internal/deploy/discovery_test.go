package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// discoveryRepo lays out a fake deploy-repo clone with the given stacks under
// <repo>/stacks and returns the config pointing at it in stack-discovery mode.
// overrides are the host config's per-stack override entries (ADR-0043),
// matched to discovered directories by name.
func discoveryRepo(t *testing.T, stackNames []string, overrides []config.Stack) (repoDir string, cfg *config.Config) {
	t.Helper()
	repoDir = t.TempDir()
	for _, name := range stackNames {
		dir := filepath.Join(repoDir, "stacks", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	}
	return repoDir, &config.Config{
		StacksBaseDir:  filepath.Join(repoDir, "stacks"),
		StackDiscovery: true,
		Stacks:         overrides,
	}
}

// discoveryDeployer builds a deployer wired for discovery tests: fake runner,
// repo dir, temp state dir, and an event recorder.
func discoveryDeployer(t *testing.T, repoDir string) (*Deployer, *recordingRunner, *[]events.DeployEvent) {
	t.Helper()
	runner := &recordingRunner{}
	var recorded []events.DeployEvent
	d := New(Config{
		Runner:   runner,
		RepoDir:  repoDir,
		StateDir: t.TempDir(),
	})
	d.SetEventSink(func(e events.DeployEvent) { recorded = append(recorded, e) })
	return d, runner, &recorded
}

func eventsWithStatus(recorded []events.DeployEvent, status events.Status) []string {
	var stacks []string
	for _, e := range recorded {
		if e.Status == status {
			stacks = append(stacks, e.Stack)
		}
	}
	return stacks
}

func TestDeployAllStacks_DiscoveryDeploysDiscoveredStacks(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "beta"}, nil)
	d, runner, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)

	var upDirs []string
	for _, c := range runner.calls {
		if len(c.args) > 1 && c.args[1] == "up" {
			upDirs = append(upDirs, c.dir)
		}
	}
	if len(upDirs) != 2 {
		t.Fatalf("expected 2 up calls, got %d (%v)", len(upDirs), runner.calls)
	}
	if got := eventsWithStatus(*recorded, events.StatusSuccess); len(got) != 2 {
		t.Errorf("success events = %v, want [alpha beta]", got)
	}
	if got := d.CurrentStacks(); len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Errorf("CurrentStacks = %v, want alpha,beta", got)
	}
}

func TestDeployAllStacks_DiscoveryConfigEditRedeploysOnlyThatStack(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "beta"}, nil)
	stateDir := t.TempDir()
	d := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir})
	d.DeployAllStacks(context.Background(), cfg)

	// Edit alpha's config in the host config: a new watch_dirs entry changes its
	// effective config (ConfigHash), so exactly alpha must redeploy.
	if err := os.MkdirAll(filepath.Join(repoDir, "stacks", "alpha", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	edited := &config.Config{
		StacksBaseDir:  cfg.StacksBaseDir,
		StackDiscovery: true,
		Stacks:         []config.Stack{{Name: "alpha", WatchDirs: []string{"alpha/conf"}}},
	}

	secondRunner := &recordingRunner{}
	var recorded []events.DeployEvent
	d2 := New(Config{Runner: secondRunner, RepoDir: repoDir, StateDir: stateDir})
	d2.SetEventSink(func(e events.DeployEvent) { recorded = append(recorded, e) })
	d2.DeployAllStacks(context.Background(), edited)

	if got := eventsWithStatus(recorded, events.StatusSuccess); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("success events = %v, want [alpha]", got)
	}
	if got := eventsWithStatus(recorded, events.StatusSkipped); len(got) != 1 || got[0] != "beta" {
		t.Errorf("skipped events = %v, want [beta]", got)
	}
}

func TestDeployAllStacks_DiscoveryLeftoverRepoFileEmitsConfigFailed(t *testing.T) {
	// ADR-0043: a leftover in-repo skipper.yaml is no longer read and fails the
	// whole stack phase loudly (file-level), so nothing deploys until migrated.
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, nil)
	writeFile(t, filepath.Join(repoDir, "stacks", "skipper.yaml"), "stacks:\n  alpha:\n    icon: nginx\n")
	d, runner, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)

	if len(runner.calls) != 0 {
		t.Errorf("no docker command may run when a leftover repo config is present, got %v", runner.calls)
	}
	if got := eventsWithStatus(*recorded, events.StatusFailed); len(got) != 1 || got[0] != ConfigStateKey {
		t.Errorf("failed events = %v, want [%s]", got, ConfigStateKey)
	}
}

func TestDeployAllStacks_DiscoveryEntryErrorFailsOnlyThatStack(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, []config.Stack{{Name: "ghost", Icon: "casper"}})
	d, _, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)

	if got := eventsWithStatus(*recorded, events.StatusFailed); len(got) != 1 || got[0] != "ghost" {
		t.Errorf("failed events = %v, want [ghost]", got)
	}
	if got := eventsWithStatus(*recorded, events.StatusSuccess); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("success events = %v, want [alpha]", got)
	}
}

func TestDeployAllStacks_DiscoveryInvalidStackBlocksDependents(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"app", "bad"}, []config.Stack{
		{Name: "bad", HealthCheck: &config.HealthCheck{URL: "notaurl"}},
		{Name: "app", DependsOn: []string{"bad"}},
	})
	d, runner, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)

	assertCommandNotCalled(t, runner.calls, "up")
	if got := eventsWithStatus(*recorded, events.StatusFailed); len(got) != 1 || got[0] != "bad" {
		t.Errorf("failed events = %v, want [bad]", got)
	}
	if got := eventsWithStatus(*recorded, events.StatusBlocked); len(got) != 1 || got[0] != "app" {
		t.Errorf("blocked events = %v, want [app]", got)
	}
}

func TestDeployAllStacks_DiscoveryDisabledStackNotDeployed(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "wip"}, []config.Stack{{Name: "wip", Disabled: true}})
	d, _, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)

	if got := eventsWithStatus(*recorded, events.StatusSuccess); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("success events = %v, want [alpha]", got)
	}
	for _, e := range *recorded {
		if e.Stack == "wip" {
			t.Errorf("disabled stack must emit no events, got %v", e)
		}
	}
	// The parked name is published so the UI's disabled line can show it.
	if got := d.CurrentDisabledStacks(); len(got) != 1 || got[0] != "wip" {
		t.Errorf("CurrentDisabledStacks = %v, want [wip]", got)
	}
}

func TestHealStack_DiscoveryUsesDiscoveredStacks(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, nil)
	d, runner, _ := discoveryDeployer(t, repoDir)
	d.DeployAllStacks(context.Background(), cfg) // publishes the discovered set

	runner.calls = nil
	ran, err := d.HealStack(context.Background(), cfg, "alpha", nil)
	if !ran || err != nil {
		t.Fatalf("HealStack(alpha) = ran=%v err=%v, want ran with no error", ran, err)
	}
	assertCommandCalled(t, runner.calls, "up")

	if _, err := d.HealStack(context.Background(), cfg, "ghost", nil); err == nil {
		t.Error("HealStack(ghost) should fail for an undiscovered stack")
	}
}
