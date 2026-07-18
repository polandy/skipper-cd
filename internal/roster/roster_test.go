package roster

import (
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// lastFrom builds the `last` lookup Build expects from a fixed map.
func lastFrom(recs map[string]audit.Record) func(string) (audit.Record, bool) {
	return func(name string) (audit.Record, bool) {
		r, ok := recs[name]
		return r, ok
	}
}

func stacks(names ...string) []config.Stack {
	out := make([]config.Stack, len(names))
	for i, n := range names {
		out[i] = config.Stack{Name: n}
	}
	return out
}

func TestBuild_MergesLastOutcomePerStack(t *testing.T) {
	ts := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	got := Build(
		stacks("traefik"),
		nil,
		lastFrom(map[string]audit.Record{
			"traefik": {Stack: "traefik", Status: events.StatusSuccess, Timestamp: ts, CommitSHA: "a1b2c3d"},
		}),
	)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	e := got[0]
	if e.Name != "traefik" || e.Disabled {
		t.Errorf("unexpected identity: %+v", e)
	}
	if e.LastStatus != events.StatusSuccess || e.LastCommit != "a1b2c3d" {
		t.Errorf("last outcome not merged: %+v", e)
	}
	if e.LastAt == nil || !e.LastAt.Equal(ts) {
		t.Errorf("last_at not set: %+v", e)
	}
}

func TestBuild_NeverDeployedHasNoOutcome(t *testing.T) {
	got := Build(stacks("grafana"), nil, lastFrom(nil))
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	e := got[0]
	if e.LastStatus != "" || e.LastAt != nil || e.LastCommit != "" {
		t.Errorf("never-deployed stack should carry no outcome: %+v", e)
	}
}

func TestBuild_DisabledStacksAppendedAndParked(t *testing.T) {
	ts := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	// "experiments" is disabled — not in the deploy set — but has a historical
	// record. It must still render as parked, with no live outcome.
	got := Build(
		stacks("traefik"),
		[]string{"experiments"},
		lastFrom(map[string]audit.Record{
			"experiments": {Stack: "experiments", Status: events.StatusFailed, Timestamp: ts, CommitSHA: "dead"},
		}),
	)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	// Enabled sorts before disabled.
	if got[0].Name != "traefik" || got[0].Disabled {
		t.Errorf("enabled stack should sort first: %+v", got[0])
	}
	d := got[1]
	if d.Name != "experiments" || !d.Disabled {
		t.Errorf("disabled stack missing/mislabelled: %+v", d)
	}
	if d.LastStatus != "" || d.LastAt != nil || d.LastCommit != "" {
		t.Errorf("disabled stack should show no live outcome: %+v", d)
	}
}

func TestBuild_OrdersEnabledThenDisabledEachAlphabetical(t *testing.T) {
	got := Build(
		stacks("web", "authelia", "traefik"),
		[]string{"scratch", "experiments"},
		lastFrom(nil),
	)
	want := []struct {
		name     string
		disabled bool
	}{
		{"authelia", false},
		{"traefik", false},
		{"web", false},
		{"experiments", true},
		{"scratch", true},
	}
	if len(got) != len(want) {
		t.Fatalf("want %d entries, got %d", len(want), len(got))
	}
	for i, w := range want {
		if got[i].Name != w.name || got[i].Disabled != w.disabled {
			t.Errorf("entry %d = {%s disabled=%v}, want {%s disabled=%v}", i, got[i].Name, got[i].Disabled, w.name, w.disabled)
		}
	}
}

func TestBuild_EmptySetIsEmptyNotNilPanics(t *testing.T) {
	got := Build(nil, nil, lastFrom(nil))
	if len(got) != 0 {
		t.Errorf("want empty roster, got %d entries", len(got))
	}
}
