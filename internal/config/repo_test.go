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

// rolloutReadyCompose defines service "app" with a healthcheck and no host
// ports / container_name, so it passes the discovery-time rollout eligibility
// check (ADR-0040).
const rolloutReadyCompose = "services:\n  app:\n    image: nginx:1.25\n    healthcheck:\n      test: [\"CMD\", \"true\"]\n"

func boolPtr(b bool) *bool { return &b }

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

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), nil, "")
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

func TestLoadRepoStacks_NoOverridesYieldsDefaults(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})

	repo, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), nil, "")
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

func TestLoadRepoStacks_WorkingDirBaseAppliesWithoutOverride(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})

	repo, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), nil, "/etc/nixos/modules")
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if len(repo.Stacks) != 1 {
		t.Fatalf("got %d stacks, want 1", len(repo.Stacks))
	}
	if got, want := repo.Stacks[0].WorkingDir, "/etc/nixos/modules/web"; got != want {
		t.Errorf("expected working_dir %q, got %q", want, got)
	}
}

func TestLoadRepoStacks_OverrideWorkingDirWinsOverBase(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "web", WorkingDir: "/srv/custom/web"}}

	repo, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "/etc/nixos/modules")
	if err != nil {
		t.Fatalf("LoadRepoStacks: %v", err)
	}
	if got, want := repo.Stacks[0].WorkingDir, "/srv/custom/web"; got != want {
		t.Errorf("expected override working_dir to win, got %q, want %q", got, want)
	}
}

func TestLoadRepoStacks_LeftoverRepoFileIsFileLevel(t *testing.T) {
	// ADR-0043: the in-repo override file is no longer read. A leftover one is
	// un-migrated config that would otherwise be silently ignored — it must fail
	// loudly (file-level), so nothing deploys until the operator migrates it.
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  web:\n    icon: nginx\n",
	})

	_, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), nil, "")
	if err == nil {
		t.Fatal("expected a file-level error for a leftover repo skipper.yaml")
	}
	if !strings.Contains(err.Error(), "no longer read") {
		t.Errorf("error should explain the file is no longer read, got: %v", err)
	}
}

func TestLoadRepoStacks_AppliesOverrides(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/web/secrets.env":        "SECRET=1\n", // relative in-repo paths must exist
		"stacks/web/conf/app.conf":      "x\n",
	})
	overrides := []Stack{{
		Name:               "web",
		WorkingDir:         "/opt/web",
		EnvFiles:           []string{"web/secrets.env", "/etc/global.env"},
		WatchDirs:          []string{"web/conf"},
		OnDemandContainers: []string{"web-app"},
		Icon:               "nginx",
		SelfHeal:           boolPtr(true),
		HealthCheck:        &HealthCheck{URL: "http://localhost:8080/health"},
	}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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
	// Relative paths resolve against the stacks base dir; absolute ones stay as-is.
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

func TestLoadRepoStacks_AppliesAutosyncOverride(t *testing.T) {
	// ADR-0043: with overrides in the startup host config, a per-stack autosync
	// override is available (default on; autosync: false opts a stack out).
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "web", Autosync: boolPtr(false)}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil || len(stackErrs) != 0 || len(repo.Stacks) != 1 {
		t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v stacks=%v", err, stackErrs, repo.Stacks)
	}
	if a := repo.Stacks[0].Autosync; a == nil || *a {
		t.Errorf("Autosync override not applied: %+v", a)
	}
}

func TestLoadRepoStacks_DisabledStackExcluded(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/wip/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "wip", Disabled: true}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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
	})
	overrides := []Stack{{Name: "ghost", Icon: "casper"}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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

func TestLoadRepoStacks_MissingBaseDirIsFileLevel(t *testing.T) {
	repoDir := t.TempDir()
	if _, _, err := LoadRepoStacks(filepath.Join(repoDir, "missing"), nil, ""); err == nil {
		t.Fatal("expected a file-level error for a missing stacks_base_dir")
	}
}

func TestLoadRepoStacks_ReservedDirNameReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/_nixos/docker-compose.yml": minimalCompose,
		"stacks/web/docker-compose.yml":    minimalCompose,
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), nil, "")
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
	})
	overrides := []Stack{{Name: "bad", EnvFiles: []string{"../../etc/passwd"}}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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
	})
	overrides := []Stack{{Name: "bad", WatchDirs: []string{"../outside"}}}

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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
	})
	overrides := []Stack{{Name: "bad", HealthCheck: &HealthCheck{URL: "notaurl"}}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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
	})
	overrides := []Stack{
		{Name: "selfref", DependsOn: []string{"selfref"}},
		{Name: "dangling", DependsOn: []string{"missing"}},
	}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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
	})
	overrides := []Stack{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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
	// A disabled dependency is hands-off, not broken: the dependent stays valid
	// and the runtime gate treats the absent dependency as satisfied.
	repoDir := writeRepo(t, map[string]string{
		"stacks/app/docker-compose.yml": minimalCompose,
		"stacks/db/docker-compose.yml":  minimalCompose,
	})
	overrides := []Stack{
		{Name: "app", DependsOn: []string{"db"}},
		{Name: "db", Disabled: true},
	}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil || len(stackErrs) != 0 {
		t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v", err, stackErrs)
	}
	if got := stackNames(repo.Stacks); strings.Join(got, ",") != "app" {
		t.Errorf("stacks = %v, want [app]", got)
	}
}

func TestLoadRepoStacks_ConfigHashTracksDeployInputsOnly(t *testing.T) {
	hashFor := func(t *testing.T, override *Stack) string {
		t.Helper()
		repoDir := writeRepo(t, map[string]string{"stacks/web/docker-compose.yml": rolloutReadyCompose})
		var overrides []Stack
		if override != nil {
			override.Name = "web"
			overrides = []Stack{*override}
		}
		repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
		if err != nil || len(stackErrs) != 0 || len(repo.Stacks) != 1 {
			t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v stacks=%v", err, stackErrs, repo.Stacks)
		}
		return repo.Stacks[0].ConfigHash
	}

	base := hashFor(t, nil)
	if again := hashFor(t, nil); again != base {
		t.Error("ConfigHash must be stable across loads of the same config")
	}
	// env_files shape the deploy → hash changes. Absolute path so the input is
	// identical across loads (the base dir is a fresh temp dir each time).
	if withEnv := hashFor(t, &Stack{EnvFiles: []string{"/etc/web.env"}}); withEnv == base {
		t.Error("ConfigHash must change when env_files change")
	}
	// icon is display-only ("never hashed") and self_heal is runtime behaviour →
	// the hash must not move.
	if withIcon := hashFor(t, &Stack{Icon: "nginx", SelfHeal: boolPtr(true)}); withIcon != base {
		t.Error("ConfigHash must ignore icon and self_heal")
	}
	// hooks must not redeploy (ADR-0038).
	if withHooks := hashFor(t, &Stack{Hooks: Hooks{PreDeploy: []string{"pg_dump > /backup/x.sql"}}}); withHooks != base {
		t.Error("ConfigHash must ignore hooks")
	}
	// rollout is a deploy-mechanism knob (ADR-0040): switching a service to/from
	// rollout must not by itself redeploy an unchanged stack.
	if withRollout := hashFor(t, &Stack{Rollout: &Rollout{Services: []string{"app"}}}); withRollout != base {
		t.Error("ConfigHash must ignore rollout")
	}
	// autosync is a runtime toggle (ADR-0043) → never hashed.
	if withAutosync := hashFor(t, &Stack{Autosync: boolPtr(false)}); withAutosync != base {
		t.Error("ConfigHash must ignore autosync")
	}
}

func TestLoadRepoStacks_AppliesHookOverrides(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "web", Hooks: Hooks{
		PreDeploy:      []string{"pg_dump > /backup/web.sql"},
		PostDeploy:     []string{"curl -fsS http://localhost/health"},
		TimeoutSeconds: 90,
	}}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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

func TestLoadRepoStacks_AppliesRolloutOverride(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": rolloutReadyCompose, // eligible service "app"
	})
	overrides := []Stack{{Name: "web", Rollout: &Rollout{Services: []string{"app"}, HealthTimeoutSeconds: 30}}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil || len(stackErrs) != 0 || len(repo.Stacks) != 1 {
		t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v stacks=%v", err, stackErrs, repo.Stacks)
	}
	r := repo.Stacks[0].Rollout
	if r == nil || len(r.Services) != 1 || r.Services[0] != "app" || r.HealthTimeoutSeconds != 30 {
		t.Errorf("rollout override = %+v", r)
	}
}

func TestLoadRepoStacks_InvalidRolloutReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "web", Rollout: &Rollout{Services: []string{}}}}

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(stackErrs) != 1 || !strings.Contains(stackErrs[0].Err.Error(), "rollout") {
		t.Fatalf("expected a rollout stack error, got %v", stackErrs)
	}
}

func TestLoadRepoStacks_RolloutUnknownServiceReported(t *testing.T) {
	// A rollout naming a service not in the compose file is a typo we catch at
	// discovery: rollout config is excluded from change detection (ADR-0040), so
	// it would otherwise stay latent until an unrelated redeploy.
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose, // defines service "app"
	})
	overrides := []Stack{{Name: "web", Rollout: &Rollout{Services: []string{"nope"}}}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(repo.Stacks) != 0 {
		t.Errorf("stack with an unknown rollout service must be excluded, got %v", stackNames(repo.Stacks))
	}
	if len(stackErrs) != 1 || stackErrs[0].Stack != "web" {
		t.Fatalf("expected one entry-level error for web, got %v", stackErrs)
	}
	msg := stackErrs[0].Err.Error()
	if !strings.Contains(msg, "rollout") || !strings.Contains(msg, "nope") {
		t.Errorf("error should name the rollout and the missing service, got: %v", msg)
	}
}

func TestLoadRepoStacks_RolloutOneOfManyServicesUnknownReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": rolloutReadyCompose, // eligible service "app"
	})
	overrides := []Stack{{Name: "web", Rollout: &Rollout{Services: []string{"app", "ghost"}}}}

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(stackErrs) != 1 || !strings.Contains(stackErrs[0].Err.Error(), "ghost") {
		t.Fatalf("expected an error naming the missing service ghost, got %v", stackErrs)
	}
}

func TestLoadRepoStacks_RolloutKnownServiceAccepted(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": rolloutReadyCompose, // eligible service "app"
	})
	overrides := []Stack{{Name: "web", Rollout: &Rollout{Services: []string{"app"}}}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil || len(stackErrs) != 0 || len(repo.Stacks) != 1 {
		t.Fatalf("LoadRepoStacks: err=%v stackErrs=%v stacks=%v", err, stackErrs, repo.Stacks)
	}
}

func TestLoadRepoStacks_RolloutServiceMissingHealthcheckReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose, // service "app", no healthcheck
	})
	overrides := []Stack{{Name: "web", Rollout: &Rollout{Services: []string{"app"}}}}

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(stackErrs) != 1 || !strings.Contains(stackErrs[0].Err.Error(), "no healthcheck") {
		t.Fatalf("expected a no-healthcheck rollout error, got %v", stackErrs)
	}
}

func TestLoadRepoStacks_RolloutServicePublishesPortsReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": "services:\n  app:\n    image: nginx:1.25\n    ports: [\"8080:80\"]\n    healthcheck:\n      test: [\"CMD\", \"true\"]\n",
	})
	overrides := []Stack{{Name: "web", Rollout: &Rollout{Services: []string{"app"}}}}

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(stackErrs) != 1 || !strings.Contains(stackErrs[0].Err.Error(), "publishes host ports") {
		t.Fatalf("expected a published-ports rollout error, got %v", stackErrs)
	}
}

func TestLoadRepoStacks_RolloutServiceContainerNameReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": "services:\n  app:\n    image: nginx:1.25\n    container_name: app\n    healthcheck:\n      test: [\"CMD\", \"true\"]\n",
	})
	overrides := []Stack{{Name: "web", Rollout: &Rollout{Services: []string{"app"}}}}

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(stackErrs) != 1 || !strings.Contains(stackErrs[0].Err.Error(), "container_name") {
		t.Fatalf("expected a container_name rollout error, got %v", stackErrs)
	}
}

func TestLoadRepoStacks_UnparseableComposeReported(t *testing.T) {
	// A broken docker-compose.yml is caught at discovery (parsed every sync),
	// not only when the stack next deploys. The stack is excluded.
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": "services: [not valid",
	})

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), nil, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(repo.Stacks) != 0 {
		t.Errorf("stack with a broken compose must be excluded, got %v", stackNames(repo.Stacks))
	}
	if len(stackErrs) != 1 || stackErrs[0].Stack != "web" {
		t.Fatalf("expected one entry-level error for web, got %v", stackErrs)
	}
}

func TestLoadRepoStacks_MissingRelativeEnvFileReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "web", EnvFiles: []string{"web/missing.env"}}}

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(stackErrs) != 1 || !strings.Contains(stackErrs[0].Err.Error(), "does not exist") {
		t.Fatalf("expected a missing env_file error, got %v", stackErrs)
	}
}

func TestLoadRepoStacks_MissingRelativeWatchDirReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "web", WatchDirs: []string{"web/missing"}}}

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil {
		t.Fatalf("file-level error unexpected: %v", err)
	}
	if len(stackErrs) != 1 || !strings.Contains(stackErrs[0].Err.Error(), "does not exist") {
		t.Fatalf("expected a missing watch_dir error, got %v", stackErrs)
	}
}

func TestLoadRepoStacks_AbsoluteEnvFileNotRequiredToExist(t *testing.T) {
	// Absolute paths are the host-secret escape hatch (may be produced
	// out-of-band), so they are not existence-checked at discovery.
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "web", EnvFiles: []string{"/etc/does-not-exist.env"}}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
	if err != nil || len(stackErrs) != 0 || len(repo.Stacks) != 1 {
		t.Fatalf("absolute env_file must not be existence-checked: err=%v stackErrs=%v stacks=%v", err, stackErrs, repo.Stacks)
	}
}

func TestLoadRepoStacks_InvalidHookReported(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
	})
	overrides := []Stack{{Name: "web", Hooks: Hooks{PreDeploy: []string{"  "}}}}

	repo, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"), overrides, "")
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
