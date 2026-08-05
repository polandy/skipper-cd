package roster

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/updatecheck"
)

func TestBuildState_CarriesRepoWebURLForCommitLinks(t *testing.T) {
	const webURL = "https://forge.example.com/owner/repo"

	state := BuildState(nil, nil, audit.NewLog(t.TempDir()), nil, RepoRef{Dir: "/var/lib/skipper/repo", WebURL: webURL}, nil)

	if state.RepoWebURL != webURL {
		t.Errorf("RepoWebURL = %q, want %q", state.RepoWebURL, webURL)
	}
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"repo_web_url":"`+webURL+`"`) {
		t.Errorf("snapshot JSON missing repo_web_url: %s", body)
	}
}

func TestBuildState_MergesNewestAuditRecordAndTrackedPaths(t *testing.T) {
	const repoDir = "/var/lib/skipper/repo"
	ts := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)

	auditLog := audit.NewLog(t.TempDir())
	auditLog.Record(events.DeployEvent{Stack: "web", Status: events.StatusFailed, Timestamp: ts.Add(-time.Hour), Commits: []events.CommitInfo{{SHA: "0000000"}}})
	auditLog.Record(events.DeployEvent{Stack: "web", Status: events.StatusSuccess, Timestamp: ts, Commits: []events.CommitInfo{{SHA: "a1b2c3d"}}})

	state := BuildState(
		// "idle" has never deployed: no audit record, no tracked paths.
		[]config.Stack{{Name: "web"}, {Name: "idle"}},
		[]string{"experiments"},
		auditLog,
		map[string][]string{"web": {repoDir + "/modules/web/docker-compose.yml", repoDir + "/modules/skipper.yaml"}},
		RepoRef{Dir: repoDir},
		nil,
	)

	byName := map[string]Entry{}
	for _, e := range state.Roster {
		byName[e.Name] = e
	}
	if len(state.Roster) != 3 {
		t.Fatalf("roster = %+v, want both enabled stacks and the disabled one", state.Roster)
	}
	if idle := byName["idle"]; idle.LastStatus != "" || idle.LastAt != nil || len(idle.Watched) != 0 {
		t.Errorf("idle entry = %+v, want no outcome and no watched paths", idle)
	}
	web := byName["web"]
	if web.LastStatus != events.StatusSuccess || web.LastCommit != "a1b2c3d" {
		t.Errorf("web entry = %+v, want the newest record (success/a1b2c3d)", web)
	}
	if !slices.Equal(web.Watched, []string{"modules/web/docker-compose.yml"}) || !web.WatchedConfig {
		t.Errorf("web watched = %v config=%t, want the repo-relative compose file and the config flag", web.Watched, web.WatchedConfig)
	}
	if !slices.Equal(state.Disabled, []string{"experiments"}) {
		t.Errorf("Disabled = %v, want [experiments]", state.Disabled)
	}
}

func TestBuildState_OmitsRepoWebURLWhenNoneDerivable(t *testing.T) {
	// A repo_url the forge URL cannot be derived from (a local path, say) leaves
	// the key out entirely, so the UI renders plain SHAs instead of dead links.
	state := BuildState(nil, nil, audit.NewLog(t.TempDir()), nil, RepoRef{Dir: "/var/lib/skipper/repo"}, nil)

	body, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "repo_web_url") {
		t.Errorf("snapshot JSON should omit repo_web_url, got: %s", body)
	}
}

func TestSplitTrackedPaths(t *testing.T) {
	const repoDir = "/var/lib/skipper/repo"

	tests := []struct {
		name       string
		paths      []string
		want       []string
		wantConfig bool
	}{
		{
			name:  "repo files show repo-relative",
			paths: []string{repoDir + "/modules/gitea/docker-compose.yml"},
			want:  []string{"modules/gitea/docker-compose.yml"},
		},
		{
			name:  "host paths stay absolute",
			paths: []string{"/run/secrets/rendered/skipper/compose.env", "/etc/skipper/vars.env"},
			want:  []string{"/run/secrets/rendered/skipper/compose.env", "/etc/skipper/vars.env"},
		},
		{
			name:  "a mixed list keeps its order",
			paths: []string{repoDir + "/modules/web/docker-compose.yml", "/run/secrets/compose.env"},
			want:  []string{"modules/web/docker-compose.yml", "/run/secrets/compose.env"},
		},
		{
			// A sibling of the clone must not be rendered as "../something" —
			// that reads as a repo path when it is a host path.
			name:  "a sibling of the clone stays absolute",
			paths: []string{"/var/lib/skipper/state.yaml"},
			want:  []string{"/var/lib/skipper/state.yaml"},
		},
		{
			name:  "nothing recorded yields nothing",
			paths: nil,
			want:  nil,
		},
		{
			// The config hash rides a synthetic key, not a file anyone can
			// open — it must be reported as a flag, never as a path.
			name:       "the synthetic config key is split out",
			paths:      []string{repoDir + "/modules/web/docker-compose.yml", repoDir + "/modules/skipper.yaml"},
			want:       []string{"modules/web/docker-compose.yml"},
			wantConfig: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotConfig := splitTrackedPaths(tc.paths, repoDir)
			if !slices.Equal(got, tc.want) {
				t.Errorf("splitTrackedPaths(%v) files = %v, want %v", tc.paths, got, tc.want)
			}
			if gotConfig != tc.wantConfig {
				t.Errorf("splitTrackedPaths(%v) config = %t, want %t", tc.paths, gotConfig, tc.wantConfig)
			}
		})
	}
}

func TestBuildState_Incidents24hFiltersWindowAndStatuses(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	auditLog := audit.NewLog(t.TempDir())
	// Inside the window: one rollback, one failure, one success (not counted).
	auditLog.Record(events.DeployEvent{Stack: "nextcloud", Status: events.StatusRolledBack, Timestamp: now.Add(-time.Hour)})
	auditLog.Record(events.DeployEvent{Stack: "web", Status: events.StatusFailed, Timestamp: now.Add(-2 * time.Hour)})
	auditLog.Record(events.DeployEvent{Stack: "nextcloud", Status: events.StatusSuccess, Timestamp: now.Add(-30 * time.Minute)})
	// Outside the window: an old failure that must not count.
	auditLog.Record(events.DeployEvent{Stack: "web", Status: events.StatusFailed, Timestamp: now.Add(-25 * time.Hour)})

	state := buildState([]config.Stack{{Name: "web"}, {Name: "nextcloud"}}, nil, auditLog, nil, RepoRef{}, nil, now)

	if len(state.Incidents24h) != 2 {
		t.Fatalf("incidents_24h = %+v, want the rollback and the fresh failure only", state.Incidents24h)
	}
	// Newest first, each naming its stack (the cross-stack list needs it).
	if state.Incidents24h[0].Stack != "nextcloud" || state.Incidents24h[0].Status != events.StatusRolledBack {
		t.Errorf("incidents_24h[0] = %+v, want the nextcloud rollback first", state.Incidents24h[0])
	}
	if state.Incidents24h[1].Stack != "web" || state.Incidents24h[1].Status != events.StatusFailed {
		t.Errorf("incidents_24h[1] = %+v, want the web failure", state.Incidents24h[1])
	}
}

func TestBuildState_Incidents24hOmittedWhenClean(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	auditLog := audit.NewLog(t.TempDir())
	auditLog.Record(events.DeployEvent{Stack: "web", Status: events.StatusSuccess, Timestamp: now.Add(-time.Hour)})

	state := buildState(nil, nil, auditLog, nil, RepoRef{}, nil, now)
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "incidents_24h") {
		t.Errorf("a clean day must omit incidents_24h from the JSON: %s", body)
	}
}

func TestBuildState_Incidents24hCapped(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	auditLog := audit.NewLog(t.TempDir())
	for i := range 60 {
		auditLog.Record(events.DeployEvent{Stack: "web", Status: events.StatusFailed, Timestamp: now.Add(-time.Duration(i) * time.Minute)})
	}
	state := buildState(nil, nil, auditLog, nil, RepoRef{}, nil, now)
	if len(state.Incidents24h) != incidentsCap {
		t.Errorf("incidents_24h = %d records, want the cap of %d", len(state.Incidents24h), incidentsCap)
	}
}

func TestBuildState_CarriesUpdateCheckSnapshot(t *testing.T) {
	updates := &updatecheck.Snapshot{
		Stacks: map[string]map[string]updatecheck.ServiceUpdate{
			"gitea": {"server": {Running: "1.22.3", Latest: "1.22.6"}},
		},
		CheckedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	}

	state := BuildState(nil, nil, audit.NewLog(t.TempDir()), nil, RepoRef{}, updates)

	if state.Updates != updates {
		t.Fatal("Updates not carried on the stacks state")
	}
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"updates"`, `"latest":"1.22.6"`, `"checked_at"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("snapshot JSON missing %s: %s", want, body)
		}
	}

	// Absent entirely while no check has run, so the UI and peers see no field.
	none := BuildState(nil, nil, audit.NewLog(t.TempDir()), nil, RepoRef{}, nil)
	if body, _ := json.Marshal(none); strings.Contains(string(body), `"updates"`) {
		t.Errorf("nil updates must be omitted from the JSON: %s", body)
	}
}
