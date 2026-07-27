package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/audit"
)

func TestBuildStacksState_CarriesRepoWebURLForCommitLinks(t *testing.T) {
	const webURL = "https://forge.example.com/owner/repo"

	state := buildStacksState(nil, nil, audit.NewLog(t.TempDir()), nil, repoRef{dir: "/var/lib/skipper/repo", webURL: webURL})

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

func TestBuildStacksState_OmitsRepoWebURLWhenNoneDerivable(t *testing.T) {
	// A repo_url the forge URL cannot be derived from (a local path, say) leaves
	// the key out entirely, so the UI renders plain SHAs instead of dead links.
	state := buildStacksState(nil, nil, audit.NewLog(t.TempDir()), nil, repoRef{dir: "/var/lib/skipper/repo"})

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
