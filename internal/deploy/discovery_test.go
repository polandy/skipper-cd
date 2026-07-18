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
// <repo>/stacks and an optional skipper.yaml at the stacks base dir, and
// returns the config pointing at it in stack-discovery mode.
func discoveryRepo(t *testing.T, stackNames []string, repoConfig string) (repoDir string, cfg *config.Config) {
	t.Helper()
	repoDir = t.TempDir()
	for _, name := range stackNames {
		dir := filepath.Join(repoDir, "stacks", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	}
	if repoConfig != "" {
		writeFile(t, filepath.Join(repoDir, "stacks", "skipper.yaml"), repoConfig)
	}
	return repoDir, &config.Config{
		StacksBaseDir:  filepath.Join(repoDir, "stacks"),
		StackDiscovery: true,
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
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "beta"}, "")
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
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "beta"}, "")
	stateDir := t.TempDir()
	d := New(Config{Runner: &recordingRunner{}, RepoDir: repoDir, StateDir: stateDir})
	d.DeployAllStacks(context.Background(), cfg)

	// Edit alpha's config: a new watch_dirs entry changes its effective
	// config, so exactly alpha must redeploy.
	if err := os.MkdirAll(filepath.Join(repoDir, "stacks", "alpha", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repoDir, "stacks", "skipper.yaml"), "stacks:\n  alpha:\n    watch_dirs: [alpha/conf]\n")

	secondRunner := &recordingRunner{}
	var recorded []events.DeployEvent
	d2 := New(Config{Runner: secondRunner, RepoDir: repoDir, StateDir: stateDir})
	d2.SetEventSink(func(e events.DeployEvent) { recorded = append(recorded, e) })
	d2.DeployAllStacks(context.Background(), cfg)

	if got := eventsWithStatus(recorded, events.StatusSuccess); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("success events = %v, want [alpha]", got)
	}
	if got := eventsWithStatus(recorded, events.StatusSkipped); len(got) != 1 || got[0] != "beta" {
		t.Errorf("skipped events = %v, want [beta]", got)
	}
	// The change is attributed to the repo config file, so the UI shows
	// skipper.yaml as the changed file.
	configPath := filepath.Join(repoDir, "stacks", "skipper.yaml")
	var deployingChanged []string
	for _, e := range recorded {
		if e.Status == events.StatusDeploying && e.Stack == "alpha" {
			deployingChanged = e.ChangedFiles
		}
	}
	found := false
	for _, f := range deployingChanged {
		if f == "stacks/skipper.yaml" || f == configPath {
			found = true
		}
	}
	if !found {
		t.Errorf("changed files %v should include the repo skipper.yaml", deployingChanged)
	}
}

func TestDeployAllStacks_DiscoveryParseErrorEmitsConfigFailed(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, "stacks: [broken: {\n")
	d, runner, recorded := discoveryDeployer(t, repoDir)

	d.DeployAllStacks(context.Background(), cfg)

	if len(runner.calls) != 0 {
		t.Errorf("no docker command may run on a config parse error, got %v", runner.calls)
	}
	if got := eventsWithStatus(*recorded, events.StatusFailed); len(got) != 1 || got[0] != ConfigStateKey {
		t.Errorf("failed events = %v, want [%s]", got, ConfigStateKey)
	}
}

func TestDeployAllStacks_DiscoveryEntryErrorFailsOnlyThatStack(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, "stacks:\n  ghost:\n    icon: casper\n")
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
	repoDir, cfg := discoveryRepo(t, []string{"app", "bad"}, `
stacks:
  bad:
    health_check:
      url: notaurl
  app:
    depends_on: [bad]
`)
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
	repoDir, cfg := discoveryRepo(t, []string{"alpha", "wip"}, "stacks:\n  wip:\n    disabled: true\n")
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
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, "")
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
