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

// healConfig writes a stack's compose file under a temp base dir and returns a
// config pointing at it.
func healConfig(t *testing.T, stack string) *config.Config {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, stack)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), composeWithImage("nginx:1.25"))
	return &config.Config{StacksBaseDir: base, Stacks: []config.Stack{{Name: stack}}}
}

func TestHealStack_RunsCorrectiveUpAndEmitsHealed(t *testing.T) {
	r := &recordingRunner{}
	d := newDeployerWithRunner(r)
	var got []events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) { got = append(got, e) })

	cfg := healConfig(t, "web")
	drift := []events.DriftedService{{Name: "web", Status: "unhealthy"}}
	ran, err := d.HealStack(context.Background(), cfg, "web", drift)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Fatal("expected ran=true when the mutex was free")
	}

	// Exactly one corrective up, and never a --wait health gate or a rollback.
	if len(r.calls) != 1 {
		t.Fatalf("expected exactly one docker call, got %d: %+v", len(r.calls), r.calls)
	}
	want := []string{"compose", "up", "-d", "--remove-orphans"}
	if r.calls[0].name != "docker" || !slices.Equal(r.calls[0].args, want) {
		t.Fatalf("unexpected argv: %s %v", r.calls[0].name, r.calls[0].args)
	}
	assertCommandNotCalled(t, r.calls, "--wait")

	if len(got) != 1 || got[0].Status != events.StatusHealed || got[0].Stack != "web" {
		t.Fatalf("expected one healed event for web, got %+v", got)
	}
	// The drift that triggered the heal rides the event (no changed files/diffs).
	if !slices.Equal(got[0].HealDrift, drift) {
		t.Fatalf("expected healed event to carry drift %+v, got %+v", drift, got[0].HealDrift)
	}
	if got[0].ChangedFiles != nil || got[0].Diffs != nil {
		t.Fatalf("a heal carries no changed files or diffs, got files=%v diffs=%v", got[0].ChangedFiles, got[0].Diffs)
	}
}

func TestHealStack_SkipsWhenDeployInProgress(t *testing.T) {
	r := &recordingRunner{}
	d := newDeployerWithRunner(r)
	cfg := healConfig(t, "web")

	// Simulate a deploy already holding the lock.
	d.mu.Lock()
	defer d.mu.Unlock()

	ran, err := d.HealStack(context.Background(), cfg, "web", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Fatal("expected ran=false while a deploy holds the mutex")
	}
	if len(r.calls) != 0 {
		t.Fatalf("no docker call expected when skipped, got %+v", r.calls)
	}
}

func TestHealStack_ReturnsErrorWithoutHealedEventOnUpFailure(t *testing.T) {
	r := &recordingRunner{errOnCommand: "up"}
	d := newDeployerWithRunner(r)
	var got []events.DeployEvent
	d.SetEventSink(func(e events.DeployEvent) { got = append(got, e) })

	cfg := healConfig(t, "web")
	ran, err := d.HealStack(context.Background(), cfg, "web", nil)
	if !ran {
		t.Fatal("expected ran=true: the up was attempted")
	}
	if err == nil {
		t.Fatal("expected an error when the corrective up fails")
	}
	// A single failed up must not emit a misleading healed (or failed) event —
	// the engine counts the attempt and only heal_exhausted is terminal.
	for _, e := range got {
		if e.Status == events.StatusHealed {
			t.Fatalf("no healed event expected on a failed up, got %+v", got)
		}
	}
}

func TestHealStack_UnknownStack(t *testing.T) {
	r := &recordingRunner{}
	d := newDeployerWithRunner(r)
	cfg := healConfig(t, "web")

	ran, err := d.HealStack(context.Background(), cfg, "nope", nil)
	if !ran || err == nil {
		t.Fatalf("expected ran=true and an error for an unknown stack, got ran=%v err=%v", ran, err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("no docker call expected for an unknown stack, got %+v", r.calls)
	}
}
