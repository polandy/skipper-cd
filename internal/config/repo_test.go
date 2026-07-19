package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepo lays out a fake deploy-repo clone: keys are repo-relative paths,
// values file contents. Returns the repo root.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repoDir := t.TempDir()
	for path, content := range files {
		full := filepath.Join(repoDir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return repoDir
}

const minimalCompose = "services:\n  app:\n    image: nginx:1.25\n"

func stackNames(stacks []Stack) []string {
	names := make([]string, len(stacks))
	for i, s := range stacks {
		names[i] = s.Name
	}
	return names
}

func errorStacks(errs []StackError) []string {
	names := make([]string, len(errs))
	for i, e := range errs {
		names[i] = e.Stack
	}
	return names
}

func TestLoadRepoStacks_DiscoversDirsWithComposeFile(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/beta/docker-compose.yml":      minimalCompose,
		"stacks/alpha/docker-compose.yml":     minimalCompose,
		"stacks/no-compose/readme.md":         "not a stack",
		"stacks/plainfile":                    "not a dir",
		"stacks/.hidden/docker-compose.yml":   minimalCompose,
		"stacks/alpha/sub/docker-compose.yml": minimalCompose, // nested: not scanned
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if len(stackErrs) != 0 {
		t.Fatalf("unexpected stack errors: %v", stackErrs)
	}
	got := stackNames(repo.Stacks)
	want := []string{"alpha", "beta"} // alphabetical = deterministic deploy seed order
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("discovered stacks = %v, want %v", got, want)
	}
}

func TestLoadRepoStacks_MissingRepoConfigYieldsDefaults(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})

	repo, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if len(repo.Stacks) != 1 {
		t.Fatalf("got %d stacks, want 1", len(repo.Stacks))
	}
	s := repo.Stacks[0]
	if s.Name != "web" || s.WorkingDir != "" || len(s.EnvFiles) != 0 || s.HealthCheck != nil || len(s.DependsOn) != 0 {
		t.Errorf("stack not built with defaults: %+v", s)
	}
	if s.ConfigHash == "" {
		t.Error("ConfigHash must be set for discovered stacks")
	}
}

func TestLoadRepoStacks_AppliesOverrides(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml": `
stacks:
  web:
    working_dir: /opt/web
    env_files:
      - web/secrets.env
      - /etc/global.env
    watch_dirs:
      - web/conf
    on_demand_containers: [web-app]
    icon: nginx
    self_heal: true
    health_check:
      url: http://localhost:8080/health
`,
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if len(stackErrs) != 0 {
		t.Fatalf("unexpected stack errors: %v", stackErrs)
	}
	s := repo.Stacks[0]
	if s.WorkingDir != "/opt/web" {
		t.Errorf("WorkingDir = %q", s.WorkingDir)
	}
	// Relative paths resolve against the stacks base dir (where skipper.yaml
	// lives); absolute ones stay as-is.
	if want := filepath.Join(repoDir, "stacks/web/secrets.env"); s.EnvFiles[0] != want {
		t.Errorf("EnvFiles[0] = %q, want %q", s.EnvFiles[0], want)
	}
	if s.EnvFiles[1] != "/etc/global.env" {
		t.Errorf("EnvFiles[1] = %q, want /etc/global.env", s.EnvFiles[1])
	}
	if want := filepath.Join(repoDir, "stacks/web/conf"); s.WatchDirs[0] != want {
		t.Errorf("WatchDirs[0] = %q, want %q", s.WatchDirs[0], want)
	}
	if s.Icon != "nginx" || s.SelfHeal == nil || !*s.SelfHeal || len(s.OnDemandContainers) != 1 {
		t.Errorf("overrides not applied: %+v", s)
	}
	if s.HealthCheck == nil || s.HealthCheck.TimeoutSeconds != 60 {
		t.Errorf("health_check timeout not defaulted: %+v", s.HealthCheck)
	}
}

func TestLoadRepoStacks_DisabledStackExcluded(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/wip/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  wip:\n    disabled: true\n",
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil || len(stackErrs) != 0 {
		t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v", err, stackErrs)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "web" {
		t.Errorf("stacks = %v, want [web]", got)
	}
	// The parked name is carried for the UI's disabled line.
	if strings.Join(repo.Disabled, ",") != "wip" {
		t.Errorf("Disabled = %v, want [wip]", repo.Disabled)
	}
}

func TestLoadRepoStacks_UnknownOverrideEntryReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  ghost:\n    icon: casper\n",
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	// The typo'd entry fails alone; the discovered stack still deploys.
	if got := errorStacks(stackErrs); strings.Join(got, ",") != "ghost" {
		t.Fatalf("error stacks = %v, want [ghost]", got)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "web" {
		t.Errorf("stacks = %v, want [web]", got)
	}
}

func TestLoadRepoStacks_ParseErrorIsFileLevel(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks: [not: {a map\n",
	})

	if _, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks")); err == nil {
		t.Fatal("expected a file-level error for unparseable skipper.yaml")
	}
}

func TestLoadRepoStacks_UnknownFieldIsFileLevel(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		// A misspelled field must fail loudly, not silently deploy without it.
		"stacks/skipper.yaml": "stacks:\n  web:\n    depends_onn: [db]\n",
	})

	if _, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks")); err == nil {
		t.Fatal("expected a file-level error for an unknown field")
	}
}

func TestLoadRepoStacks_MissingBaseDirIsFileLevel(t *testing.T) {
	repoDir := t.TempDir()
	if _, _, err := LoadRepoStacks(filepath.Join(repoDir, "missing")); err == nil {
		t.Fatal("expected a file-level error for a missing stacks_base_dir")
	}
}

func TestLoadRepoStacks_ReservedDirNameReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/_nixos/docker-compose.yml": minimalCompose,
		"stacks/web/docker-compose.yml":    minimalCompose,
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if got := errorStacks(stackErrs); strings.Join(got, ",") != "_nixos" {
		t.Fatalf("error stacks = %v, want [_nixos]", got)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "web" {
		t.Errorf("stacks = %v, want [web]", got)
	}
}

func TestLoadRepoStacks_EnvFileEscapingStacksBaseDirReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/bad/docker-compose.yml": minimalCompose,
		"stacks/ok/docker-compose.yml":  minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  bad:\n    env_files:\n      - ../../etc/passwd\n",
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if got := errorStacks(stackErrs); strings.Join(got, ",") != "bad" {
		t.Fatalf("error stacks = %v, want [bad]", got)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "ok" {
		t.Errorf("stacks = %v, want [ok]", got)
	}
}

func TestLoadRepoStacks_WatchDirEscapingStacksBaseDirReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/bad/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  bad:\n    watch_dirs:\n      - ../outside\n",
	})

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if got := errorStacks(stackErrs); strings.Join(got, ",") != "bad" {
		t.Fatalf("error stacks = %v, want [bad]", got)
	}
}

func TestLoadRepoStacks_InvalidHealthCheckReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/bad/docker-compose.yml": minimalCompose,
		"stacks/ok/docker-compose.yml":  minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  bad:\n    health_check:\n      url: notaurl\n",
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if got := errorStacks(stackErrs); strings.Join(got, ",") != "bad" {
		t.Fatalf("error stacks = %v, want [bad]", got)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "ok" {
		t.Errorf("stacks = %v, want [ok]", got)
	}
}

func TestLoadRepoStacks_InvalidDependencyReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/selfref/docker-compose.yml":  minimalCompose,
		"stacks/dangling/docker-compose.yml": minimalCompose,
		"stacks/ok/docker-compose.yml":       minimalCompose,
		"stacks/skipper.yaml": `
stacks:
  selfref:
    depends_on: [selfref]
  dangling:
    depends_on: [missing]
`,
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if got := errorStacks(stackErrs); strings.Join(got, ",") != "dangling,selfref" {
		t.Fatalf("error stacks = %v, want [dangling selfref]", got)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "ok" {
		t.Errorf("stacks = %v, want [ok]", got)
	}
}

func TestLoadRepoStacks_DependencyCycleReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/a/docker-compose.yml":  minimalCompose,
		"stacks/b/docker-compose.yml":  minimalCompose,
		"stacks/ok/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml": `
stacks:
  a:
    depends_on: [b]
  b:
    depends_on: [a]
`,
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if got := errorStacks(stackErrs); strings.Join(got, ",") != "a,b" {
		t.Fatalf("error stacks = %v, want [a b]", got)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "ok" {
		t.Errorf("stacks = %v, want [ok]", got)
	}
}

func TestLoadRepoStacks_DependencyOnDisabledStackAllowed(t *testing.T) {
	// A disabled dependency is hands-off, not broken: the dependent stays
	// valid and the runtime gate treats the absent dependency as satisfied.
	repoDir := writeRepo(t, map[string]string{
		"stacks/app/docker-compose.yml": minimalCompose,
		"stacks/db/docker-compose.yml":  minimalCompose,
		"stacks/skipper.yaml": `
stacks:
  app:
    depends_on: [db]
  db:
    disabled: true
`,
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil || len(stackErrs) != 0 {
		t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v", err, stackErrs)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "app" {
		t.Errorf("stacks = %v, want [app]", got)
	}
}

func TestLoadRepoStacks_ConfigHashTracksDeployInputsOnly(t *testing.T) {
	hashFor := func(t *testing.T, repoConfig string) string {
		t.Helper()
		f := map[string]string{"stacks/web/docker-compose.yml": minimalCompose}
		if repoConfig != "" {
			f["stacks/skipper.yaml"] = repoConfig
		}
		repoDir := writeRepo(t, f)
		repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
		if err != nil || len(stackErrs) != 0 || len(repo.Stacks) != 1 {
			t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v stacks=%v", err, stackErrs, repo.Stacks)
		}
		return repo.Stacks[0].ConfigHash
	}

	base := hashFor(t, "")
	if again := hashFor(t, ""); again != base {
		t.Error("ConfigHash must be stable across loads of the same config")
	}
	// env_files shape the deploy → hash changes. The path resolves against the
	// stacks base dir, which is a temp dir per load, so compare via absolute
	// path to keep the input identical across loads.
	if withEnv := hashFor(t, "stacks:\n  web:\n    env_files: [/etc/web.env]\n"); withEnv == base {
		t.Error("ConfigHash must change when env_files change")
	}
	// icon is display-only ("never hashed") and self_heal/depends_on are
	// runtime/ordering behaviour → the hash must not move.
	if withIcon := hashFor(t, "stacks:\n  web:\n    icon: nginx\n    self_heal: true\n"); withIcon != base {
		t.Error("ConfigHash must ignore icon and self_heal")
	}
	// hooks must not redeploy (ADR-0038): editing a backup command changes no
	// hashed input, so the ConfigHash stays put.
	if withHooks := hashFor(t, "stacks:\n  web:\n    hooks:\n      pre_deploy: [\"pg_dump > /backup/x.sql\"]\n"); withHooks != base {
		t.Error("ConfigHash must ignore hooks")
	}
}

func TestLoadRepoStacks_AppliesHookOverrides(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml": `stacks:
  web:
    hooks:
      pre_deploy:
        - "pg_dump > /backup/web.sql"
      post_deploy:
        - "curl -fsS http://localhost/health"
      timeout_seconds: 90
`,
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil || len(stackErrs) != 0 || len(repo.Stacks) != 1 {
		t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v stacks=%v", err, stackErrs, repo.Stacks)
	}
	h := repo.Stacks[0].Hooks
	if len(h.PreDeploy) != 1 || h.PreDeploy[0] != "pg_dump > /backup/web.sql" {
		t.Errorf("pre_deploy = %v", h.PreDeploy)
	}
	if len(h.PostDeploy) != 1 || h.PostDeploy[0] != "curl -fsS http://localhost/health" {
		t.Errorf("post_deploy = %v", h.PostDeploy)
	}
	if h.TimeoutSeconds != 90 {
		t.Errorf("timeout_seconds = %d, want 90", h.TimeoutSeconds)
	}
}

func TestLoadRepoStacks_InvalidHookReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml": `stacks:
  web:
    hooks:
      pre_deploy:
        - "  "
`,
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if len(repo.Stacks) != 0 {
		t.Errorf("stack with an invalid hook must be excluded, got %v", stackNames(repo.Stacks))
	}
	if errorStacks(stackErrs) == nil || stackErrs[0].Stack != "web" {
		t.Fatalf("expected an entry-level error for web, got %v", stackErrs)
	}
}
