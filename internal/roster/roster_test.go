package roster

import (
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// historyFrom builds the `history` lookup Build expects from a fixed map of
// newest-first record slices.
func historyFrom(recs map[string][]audit.Record) func(string) []audit.Record {
	return func(name string) []audit.Record { return recs[name] }
}

func stacks(names ...string) []config.Stack {
	out := make([]config.Stack, len(names))
	for i, n := range names {
		out[i] = config.Stack{Name: n}
	}
	return out
}

func TestBuild_CarriesHookCommandsWhenDeclared(t *testing.T) {
	withHooks := config.Stack{Name: "paperless", Hooks: config.Hooks{
		PreDeploy:      []string{"docker exec paperless-restic backup"},
		PostDeploy:     []string{"curl -fsS http://localhost/health"},
		TimeoutSeconds: 120, // deploy-time only — must not leak into the roster view
	}}
	got := Build([]config.Stack{withHooks, {Name: "traefik"}}, nil, historyFrom(nil), nil)

	byName := map[string]Entry{}
	for _, e := range got {
		byName[e.Name] = e
	}
	if h := byName["paperless"].Hooks; h == nil {
		t.Fatal("paperless should carry its hooks")
	} else if len(h.PreDeploy) != 1 || h.PreDeploy[0] != "docker exec paperless-restic backup" || len(h.PostDeploy) != 1 {
		t.Errorf("hooks view wrong: %+v", h)
	}
	if byName["traefik"].Hooks != nil {
		t.Error("a stack with no hooks must carry nil Hooks (omitted from JSON)")
	}
}

func TestBuild_MergesLastOutcomePerStack(t *testing.T) {
	ts := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	got := Build(
		stacks("traefik"),
		nil,
		historyFrom(map[string][]audit.Record{
			"traefik": {{Stack: "traefik", Status: events.StatusSuccess, Timestamp: ts, CommitSHA: "a1b2c3d"}},
		}),
		nil,
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
	got := Build(stacks("grafana"), nil, historyFrom(nil), nil)
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
		historyFrom(map[string][]audit.Record{
			"experiments": {{Stack: "experiments", Status: events.StatusFailed, Timestamp: ts, CommitSHA: "dead"}},
		}),
		nil,
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
		historyFrom(nil),
		nil,
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
	got := Build(nil, nil, historyFrom(nil), nil)
	if len(got) != 0 {
		t.Errorf("want empty roster, got %d entries", len(got))
	}
}

// watchedFrom builds the `watched` lookup Build expects from a fixed map.
func watchedFrom(paths map[string][]string) func(string) ([]string, bool) {
	return func(name string) ([]string, bool) { return paths[name], false }
}

func TestBuild_CarriesWatchedFilesPerStack(t *testing.T) {
	got := Build(
		stacks("traefik", "grafana"),
		nil,
		historyFrom(nil),
		watchedFrom(map[string][]string{
			"traefik": {"modules/traefik/docker-compose.yml", "/run/secrets/compose.env"},
		}),
	)

	byName := map[string]Entry{}
	for _, e := range got {
		byName[e.Name] = e
	}
	if got, want := len(byName["traefik"].Watched), 2; got != want {
		t.Errorf("traefik watched = %d paths, want %d", got, want)
	}
	// A stack with nothing recorded (never deployed) carries no list, rather
	// than an empty one the UI would have to special-case.
	if w := byName["grafana"].Watched; len(w) != 0 {
		t.Errorf("never-deployed stack should carry no watched files, got %v", w)
	}
}

// A parked stack is not watched, whatever its last deploy recorded — showing
// its old input list would claim skipper is still tracking it.
func TestBuild_DisabledStacksCarryNoWatchedFiles(t *testing.T) {
	got := Build(
		stacks("traefik"),
		[]string{"experiments"},
		historyFrom(nil),
		watchedFrom(map[string][]string{
			"experiments": {"modules/experiments/docker-compose.yml"},
		}),
	)
	for _, e := range got {
		if e.Disabled && len(e.Watched) != 0 {
			t.Errorf("disabled stack %q should carry no watched files, got %v", e.Name, e.Watched)
		}
	}
}

// records builds a newest-first slice of n records, one hour apart, cycling
// through the given statuses (newest gets statuses[0]).
func records(n int, newest time.Time, statuses ...events.Status) []audit.Record {
	out := make([]audit.Record, n)
	for i := range out {
		out[i] = audit.Record{
			Status:    statuses[i%len(statuses)],
			Timestamp: newest.Add(-time.Duration(i) * time.Hour),
			CommitSHA: "sha",
		}
	}
	return out
}

func TestBuild_RecentOutcomesCappedNewestFirst(t *testing.T) {
	ts := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	got := Build(
		stacks("web"),
		nil,
		historyFrom(map[string][]audit.Record{"web": records(15, ts, events.StatusSuccess)}),
		nil,
	)
	recent := got[0].Recent
	if len(recent) != 10 {
		t.Fatalf("recent = %d records, want the cap of 10", len(recent))
	}
	if !recent[0].At.Equal(ts) {
		t.Errorf("recent[0].At = %v, want the newest record %v", recent[0].At, ts)
	}
	if recent[0].Commit != "sha" || recent[0].Status != events.StatusSuccess {
		t.Errorf("recent[0] = %+v, want status+commit carried", recent[0])
	}
	if recent[0].Stack != "" {
		t.Errorf("per-stack refs must not repeat the stack name: %+v", recent[0])
	}
}

func TestBuild_LastIncidentSurvivesLaterSuccesses(t *testing.T) {
	// The 2026-08-05 shape: a rollback, then a successful retry that takes the
	// badge. The incident must stay named on the entry.
	ts := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	rolledBackAt := ts.Add(-time.Hour)
	got := Build(
		stacks("nextcloud"),
		nil,
		historyFrom(map[string][]audit.Record{"nextcloud": {
			{Status: events.StatusSuccess, Timestamp: ts},
			{Status: events.StatusRolledBack, Timestamp: rolledBackAt},
			{Status: events.StatusSuccess, Timestamp: ts.Add(-2 * time.Hour)},
		}}),
		nil,
	)
	li := got[0].LastIncident
	if li == nil {
		t.Fatal("a rollback papered over by a later success must surface as last_incident")
	}
	if li.Status != events.StatusRolledBack || !li.At.Equal(rolledBackAt) {
		t.Errorf("last_incident = %+v, want the rollback at %v", li, rolledBackAt)
	}
}

func TestBuild_LastIncidentScansPastTheStripWindow(t *testing.T) {
	// recentCap bounds the strip, not incident visibility: a rollback 12
	// records back (beyond the 10-dot strip) must still surface.
	ts := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	recs := records(12, ts, events.StatusSuccess)
	recs = append(recs, audit.Record{Status: events.StatusRolledBack, Timestamp: ts.Add(-13 * time.Hour)})
	got := Build(stacks("web"), nil, historyFrom(map[string][]audit.Record{"web": recs}), nil)
	if got[0].LastIncident == nil {
		t.Fatal("an incident beyond the strip window must still surface as last_incident")
	}
}

func TestBuild_NoLastIncidentWhenBadgeAlreadySaysIt(t *testing.T) {
	ts := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		recs []audit.Record
	}{
		{"newest record is itself bad", []audit.Record{
			{Status: events.StatusFailed, Timestamp: ts},
			{Status: events.StatusSuccess, Timestamp: ts.Add(-time.Hour)},
		}},
		{"no bad record at all", records(3, ts, events.StatusSuccess)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Build(stacks("web"), nil, historyFrom(map[string][]audit.Record{"web": tc.recs}), nil)
			if got[0].LastIncident != nil {
				t.Errorf("last_incident = %+v, want nil", got[0].LastIncident)
			}
		})
	}
}

// The stack's deploy-shaping config is hashed under a synthetic key, not a
// file — it travels as a flag so the UI never renders it as a path.
func TestBuild_ConfigHashTravelsAsAFlagNotAPath(t *testing.T) {
	got := Build(
		stacks("traefik"),
		nil,
		historyFrom(nil),
		func(string) ([]string, bool) { return []string{"modules/traefik/docker-compose.yml"}, true },
	)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if !got[0].WatchedConfig {
		t.Error("watched_config should be set when the config hash is tracked")
	}
	for _, f := range got[0].Watched {
		if f == "skipper.yaml" || f == "modules/skipper.yaml" {
			t.Errorf("the synthetic config key leaked into the file list: %v", got[0].Watched)
		}
	}
}
