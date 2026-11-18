package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes a skipper.yml into dir and returns its path.
func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "skipper.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// writeStack creates <repoDir>/modules/<name>/docker-compose.yml.
func writeStack(t *testing.T, repoDir, name, compose string) {
	t.Helper()
	dir := filepath.Join(repoDir, "modules", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir stack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
}

const validCompose = `services:
  app:
    image: nginx:1.27
`

func TestValidateConfigFile_RedactsRepoURLCredentials(t *testing.T) {
	tests := []struct {
		name, repoURL, wantIn string
	}{
		{
			name:    "password in userinfo",
			repoURL: "https://user:sekrit-token@example.com/user/deploy.git",
			wantIn:  "https://user:xxxxx@example.com/user/deploy.git",
		},
		{
			name:    "token as sole userinfo",
			repoURL: "https://sekrit-token@example.com/user/deploy.git",
			wantIn:  "https://xxxxx@example.com/user/deploy.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeConfig(t, dir, `
repo_url: `+tt.repoURL+`
repo_dir: `+filepath.Join(dir, "repo")+`
stacks_base_dir: modules
webhook_secret: test-secret
`)

			var out strings.Builder
			if code := validateConfigFile(path, &out); code != validateOK {
				t.Fatalf("expected exit code %d, got %d — output:\n%s", validateOK, code, out.String())
			}
			if strings.Contains(out.String(), "sekrit-token") {
				t.Errorf("report leaked the credential, got:\n%s", out.String())
			}
			if !strings.Contains(out.String(), tt.wantIn) {
				t.Errorf("report should show the redacted URL %q, got:\n%s", tt.wantIn, out.String())
			}
		})
	}
}

func TestValidateConfigFile_ReportsWhereCommitLinksWillPoint(t *testing.T) {
	tests := []struct {
		name, repoLines, wantIn string
	}{
		{
			// Derived: the operator never wrote this URL, so the report is the
			// only place to catch a wrong guess before the links go live.
			name:      "derived from repo_url",
			repoLines: "repo_url: ssh://git@forge.example.com:2222/owner/deploy.git\n",
			wantIn:    "commit links:    https://forge.example.com/owner/deploy/commit/<sha>",
		},
		{
			name:      "explicit repo_web_url wins",
			repoLines: "repo_url: ssh://git@forge.example.com/owner/deploy.git\nrepo_web_url: https://code.example.com/ops/deploy\n",
			wantIn:    "commit links:    https://code.example.com/ops/deploy/commit/<sha>",
		},
		{
			// A clone from a local path has no forge — say so, and name the key
			// that fixes it, rather than printing a half-built URL.
			name:      "none derivable",
			repoLines: "repo_url: /srv/git/deploy.git\n",
			wantIn:    "commit links:    none",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeConfig(t, dir, "\n"+tt.repoLines+`repo_dir: `+filepath.Join(dir, "repo")+`
stacks_base_dir: modules
webhook_secret: test-secret
`)

			var out strings.Builder
			if code := validateConfigFile(path, &out); code != validateOK {
				t.Fatalf("expected exit code %d, got %d — output:\n%s", validateOK, code, out.String())
			}
			if !strings.Contains(out.String(), tt.wantIn) {
				t.Errorf("report should contain %q, got:\n%s", tt.wantIn, out.String())
			}
		})
	}
}

func TestValidateConfigFile_CommitLinkLineNeverLeaksCredentials(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
repo_url: https://user:sekrit-token@forge.example.com/owner/deploy.git
repo_dir: `+filepath.Join(dir, "repo")+`
stacks_base_dir: modules
webhook_secret: test-secret
`)

	var out strings.Builder
	if code := validateConfigFile(path, &out); code != validateOK {
		t.Fatalf("expected exit code %d, got %d — output:\n%s", validateOK, code, out.String())
	}
	if strings.Contains(out.String(), "sekrit-token") {
		t.Errorf("the commit-link line leaked the credential, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "commit links:    https://forge.example.com/owner/deploy/commit/<sha>") {
		t.Errorf("expected a credential-free commit-link URL, got:\n%s", out.String())
	}
}

func TestValidateConfigFile_ValidConfigWithCloneReportsStacks(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	writeStack(t, repoDir, "gitea", validCompose)
	writeStack(t, repoDir, "traefik", validCompose)

	path := writeConfig(t, dir, `
repo_url: ssh://git@example.com/user/deploy.git
repo_dir: `+repoDir+`
stacks_base_dir: modules
webhook_secret: test-secret
`)

	var out strings.Builder
	if code := validateConfigFile(path, &out); code != validateOK {
		t.Fatalf("expected exit code %d, got %d — output:\n%s", validateOK, code, out.String())
	}
	for _, want := range []string{"config OK", "2 discovered", "gitea", "traefik"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output should mention %q, got:\n%s", want, out.String())
		}
	}
}

func TestValidateConfigFile_UnknownKeyIsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
repo_url: ssh://git@example.com/user/deploy.git
webhook_secrets: typo
`)

	var out strings.Builder
	if code := validateConfigFile(path, &out); code != validateInvalid {
		t.Fatalf("expected exit code %d, got %d — output:\n%s", validateInvalid, code, out.String())
	}
	if !strings.Contains(out.String(), "config invalid") {
		t.Errorf("output should report the config as invalid, got:\n%s", out.String())
	}
}

func TestValidateConfigFile_MissingFileIsInvalid(t *testing.T) {
	var out strings.Builder
	if code := validateConfigFile(filepath.Join(t.TempDir(), "absent.yml"), &out); code != validateInvalid {
		t.Fatalf("expected exit code %d for a missing config, got %d", validateInvalid, code)
	}
}

// Validating before the first clone is legitimate — the host config can be
// correct while the repo has never been fetched.
func TestValidateConfigFile_MissingCloneIsNotAFailure(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
repo_url: ssh://git@example.com/user/deploy.git
repo_dir: `+filepath.Join(dir, "never-cloned")+`
stacks_base_dir: modules
webhook_secret: test-secret
`)

	var out strings.Builder
	if code := validateConfigFile(path, &out); code != validateOK {
		t.Fatalf("expected exit code %d, got %d — output:\n%s", validateOK, code, out.String())
	}
	if !strings.Contains(out.String(), "no repo clone") {
		t.Errorf("output should explain that discovery was not checked, got:\n%s", out.String())
	}
}

// A stack whose compose file cannot be parsed fails only that stack at
// runtime, but a pre-flight check exists to surface exactly this.
func TestValidateConfigFile_BrokenStackIsInvalid(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	writeStack(t, repoDir, "gitea", validCompose)
	writeStack(t, repoDir, "broken", "services: [this is not a mapping")

	path := writeConfig(t, dir, `
repo_url: ssh://git@example.com/user/deploy.git
repo_dir: `+repoDir+`
stacks_base_dir: modules
webhook_secret: test-secret
`)

	var out strings.Builder
	if code := validateConfigFile(path, &out); code != validateInvalid {
		t.Fatalf("expected exit code %d, got %d — output:\n%s", validateInvalid, code, out.String())
	}
	if !strings.Contains(out.String(), "stack invalid") {
		t.Errorf("output should report the broken stack, got:\n%s", out.String())
	}
}

func TestValidateConfigFile_ReportsWarnings(t *testing.T) {
	dir := t.TempDir()
	// Discovery off, no stacks and no nixos_rebuild: valid, but skipper would
	// have nothing to do — a warning, not an error.
	path := writeConfig(t, dir, `
repo_url: ssh://git@example.com/user/deploy.git
repo_dir: `+filepath.Join(dir, "repo")+`
stack_discovery: false
webhook_secret: test-secret
`)

	var out strings.Builder
	if code := validateConfigFile(path, &out); code != validateOK {
		t.Fatalf("expected exit code %d, got %d — output:\n%s", validateOK, code, out.String())
	}
	if !strings.Contains(out.String(), "warning:") {
		t.Errorf("output should surface the config warning, got:\n%s", out.String())
	}
}
