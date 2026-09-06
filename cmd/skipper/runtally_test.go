package main

import (
	"testing"

	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
)

func TestRunTally_CountsTerminalStatusesByStack(t *testing.T) {
	tally := newRunTally()
	tally.observe(events.DeployEvent{Stack: "nextcloud", Status: events.StatusSuccess})
	tally.observe(events.DeployEvent{Stack: "arr-stack", Status: events.StatusRolledBack})
	tally.observe(events.DeployEvent{Stack: "monitoring", Status: events.StatusSkipped})
	tally.observe(events.DeployEvent{Stack: "old-blog", Status: events.StatusRemoved})
	tally.observe(events.DeployEvent{Stack: "immich", Status: events.StatusDeploying}) // transient, not counted

	got := tally.flush()
	want := map[events.Status]int{
		events.StatusSuccess:    1,
		events.StatusRolledBack: 1,
		events.StatusSkipped:    1,
		events.StatusRemoved:    1,
	}
	for status, n := range want {
		if got[status] != n {
			t.Errorf("counts[%s] = %d, want %d", status, got[status], n)
		}
	}
	if got[events.StatusDeploying] != 0 {
		t.Errorf("expected the transient StatusDeploying event uncounted, got %d", got[events.StatusDeploying])
	}
}

func TestRunTally_FlushResetsForNextRun(t *testing.T) {
	tally := newRunTally()
	tally.observe(events.DeployEvent{Stack: "nextcloud", Status: events.StatusSuccess})
	tally.flush()

	got := tally.flush()
	if len(got) != 0 {
		t.Errorf("expected an empty tally after flush, got %v", got)
	}
}

func TestRunTally_IgnoresSelfHealEvents(t *testing.T) {
	tally := newRunTally()
	tally.observe(events.DeployEvent{Stack: "arr-stack", Status: events.StatusHealed})
	tally.observe(events.DeployEvent{Stack: "arr-stack", Status: events.StatusHealExhausted})

	if got := tally.flush(); len(got) != 0 {
		t.Errorf("expected self-heal statuses uncounted (they have their own log lines), got %v", got)
	}
}

func TestRunTally_IgnoresConfigStateFailure(t *testing.T) {
	tally := newRunTally()
	tally.observe(events.DeployEvent{Stack: deploy.ConfigStateKey, Status: events.StatusFailed})

	if got := tally.flush(); len(got) != 0 {
		t.Errorf("expected a stack-discovery config failure uncounted (it aborts the run before PostRunHook fires), got %v", got)
	}
}

func TestRunTally_IgnoresAProjectDirRefusal(t *testing.T) {
	tally := newRunTally()
	tally.observe(events.DeployEvent{Stack: deploy.ProjectDirStateKey, Status: events.StatusFailed})
	tally.observe(events.DeployEvent{Stack: "web", Status: events.StatusSuccess})

	got := tally.flush()
	if got[events.StatusFailed] != 0 {
		t.Errorf("expected a refused project_directory fast-forward uncounted (no stack failed), got %d", got[events.StatusFailed])
	}
	if got[events.StatusSuccess] != 1 {
		t.Errorf("expected the run's real stack outcome still counted, got %d", got[events.StatusSuccess])
	}
}

func TestRunTally_IgnoresNixosFailureButCountsOtherNixosOutcomes(t *testing.T) {
	tally := newRunTally()
	tally.observe(events.DeployEvent{Stack: deploy.NixosStateKey, Status: events.StatusFailed})
	tally.observe(events.DeployEvent{Stack: deploy.NixosStateKey, Status: events.StatusSuccess})

	got := tally.flush()
	if got[events.StatusFailed] != 0 {
		t.Errorf("expected a NixOS rebuild failure uncounted (it aborts the run before PostRunHook fires), got %d", got[events.StatusFailed])
	}
	if got[events.StatusSuccess] != 1 {
		t.Errorf("expected a completed NixOS rebuild counted like any stack, got %d", got[events.StatusSuccess])
	}
}
