package deploy

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/config"
)

// parseOverrideContexts reads back the generated override as service → context.
func parseOverrideContexts(override []byte) (map[string]string, error) {
	var file buildContextOverrideFile
	if err := yaml.Unmarshal(override, &file); err != nil {
		return nil, err
	}
	contexts := make(map[string]string, len(file.Services))
	for name, svc := range file.Services {
		contexts[name] = svc.Build.Context
	}
	return contexts, nil
}

func TestBuildContextOverride_PinsRelativeContextsToClone(t *testing.T) {
	repoDir := t.TempDir()
	cf := parseComposeString(t, `services:
  app:
    build: "."
  worker:
    build:
      context: ./worker
      dockerfile: Dockerfile.worker
  tool:
    build:
      dockerfile: Dockerfile.tool
  db:
    image: postgres:18-alpine
`)

	got, err := buildContextOverride(cf, repoDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// tool: an omitted context defaults to "." — it must be pinned exactly like
	// an explicit one, or it would keep resolving against the project directory
	// while the hashed Dockerfile comes from the clone.
	want := map[string]string{
		"app":    repoDir,
		"worker": filepath.Join(repoDir, "worker"),
		"tool":   repoDir,
	}
	assertOverrideContexts(t, got, want)
}

func TestBuildContextOverride_EmptyWhenNothingToPin(t *testing.T) {
	repoDir := t.TempDir()
	tests := []struct {
		name    string
		compose string
	}{
		{"no build section", `services:
  app:
    image: nginx:1.25
`},
		{"absolute context stays as written", `services:
  app:
    build:
      context: /srv/build-context
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildContextOverride(parseComposeString(t, tt.compose), repoDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != nil {
				t.Errorf("expected no override, got:\n%s", got)
			}
		})
	}
}

// The regression this whole file exists for: with project_directory pointing at
// a tree that is not the clone, compose resolves a relative build context there
// — so the build reads a Dockerfile skipper never hashed, produces the very same
// image, and the run reports a success that changed nothing (ADR-0057).
func TestDeployStack_BuildsFromCloneNotProjectDirectory(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "myapp")
	projectDir := t.TempDir()
	for _, dir := range []string{stackDir, projectDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), `services:
  app:
    build: "."
    image: myapp:latest
`)
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM nginx:1.27\n")
	// The stale copy compose would otherwise read.
	writeFile(t, filepath.Join(projectDir, "Dockerfile"), "FROM nginx:1.25\n")

	runner := &recordingRunner{}
	// The override file is removed once the build returns, so capture it while
	// the build call is in flight.
	var override []byte
	runner.failFn = func(_ string, args []string) error {
		if slices.Contains(args, "build") {
			override = readOverrideFile(t, args)
		}
		return nil
	}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "myapp", ProjectDirectory: projectDir}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertOverrideContexts(t, override, map[string]string{"app": stackDir})

	// The override is a temp file for one build call; it must not pile up.
	overridePath := overrideFilePath(t, findCall(t, runner.calls, "build").args)
	if _, err := os.Stat(overridePath); !os.IsNotExist(err) {
		t.Errorf("override file %s not cleaned up after the deploy (stat err: %v)", overridePath, err)
	}

	// Project identity is untouched: the build still runs against the same
	// --project-directory, so the image it tags is the one `up` then starts.
	build := findCall(t, runner.calls, "build")
	if !slices.Contains(build.args, "--project-directory") || !slices.Contains(build.args, projectDir) {
		t.Errorf("build lost the project directory: %v", build.args)
	}
}

// The same divergence one step later: a service with `pull_policy: build` makes
// `docker compose up` build again. Unpinned, that second build resolves the
// context against the project directory and re-tags the stale image over the one
// the pinned build just produced — a green deploy that changed nothing, exactly
// the ADR-0057 no-op the build-only override left open.
func TestDeployStack_UpBuildsFromCloneToo(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "myapp")
	projectDir := t.TempDir()
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), `services:
  app:
    build: "."
    image: myapp:latest
    pull_policy: build
`)
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM nginx:1.27\n")
	writeFile(t, filepath.Join(projectDir, "Dockerfile"), "FROM nginx:1.25\n")

	runner := &recordingRunner{}
	// The override is a temp file removed when the apply returns, so read it
	// while the up call is in flight.
	var override []byte
	runner.failFn = func(_ string, args []string) error {
		if slices.Contains(args, "up") {
			override = readOverrideFile(t, args)
		}
		return nil
	}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "myapp", ProjectDirectory: projectDir}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertOverrideContexts(t, override, map[string]string{"app": stackDir})

	// The project directory still governs identity, .env and bind mounts on the
	// very call that now reads the clone for its build inputs.
	up := findCall(t, runner.calls, "up")
	if !slices.Contains(up.args, "--project-directory") || !slices.Contains(up.args, projectDir) {
		t.Errorf("up lost the project directory: %v", up.args)
	}
}

// A rollback restores the previous compose file but not the previous Dockerfile.
// Carrying the override there would point the restored version's build at the
// clone — the failed version — and can name services the old file does not have.
func TestRollback_DropsTheBuildContextOverride(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "myapp")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	writeFile(t, composePath, `services:
  app:
    build: "."
    image: myapp:latest
`)
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM nginx:1.27\n")

	cr := &fakeCommitReader{
		diffs: map[string]string{},
		files: map[string][]byte{"old-sha:" + composePath: []byte("services:\n  app:\n    image: myapp:previous\n")},
	}
	runner := &recordingRunner{errOnCommand: "up"}
	d := New(Config{Runner: runner, CommitReader: cr, RepoDir: baseDir, StateDir: t.TempDir()})

	state := newEmptyState()
	state.LastDeployedCommit = "old-sha"
	stack := config.Stack{Name: "myapp", ProjectDirectory: t.TempDir()}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, state); err == nil {
		t.Fatal("expected the failed up to surface as an error")
	}

	// Two ups: the failed deploy (pinned) and the rollback (not).
	var ups []runCall
	for _, c := range runner.calls {
		if slices.Contains(c.args, "up") {
			ups = append(ups, c)
		}
	}
	if len(ups) != 2 {
		t.Fatalf("expected a deploy up and a rollback up, got %d: %v", len(ups), runner.calls)
	}
	if got := countArg(ups[0].args, "-f"); got != 2 {
		t.Errorf("expected the deploy up to carry the override, got %d -f: %v", got, ups[0].args)
	}
	if got := countArg(ups[1].args, "-f"); got != 1 {
		t.Errorf("expected the rollback up to pass only the restored compose file, got %d -f: %v", got, ups[1].args)
	}
}

// When the override cannot be written, the deploy must fail — building anyway
// would read the project directory's tree, the exact silent divergence the
// override exists to prevent (ADR-0057).
func TestDeployStack_FailsWhenBuildOverrideCannotBeWritten(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "myapp")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), `services:
  app:
    build: "."
    image: myapp:latest
`)
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM nginx:1.27\n")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "myapp", ProjectDirectory: t.TempDir()}
	err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState())
	if err == nil || !strings.Contains(err.Error(), "create build context override") {
		t.Fatalf("expected a create-override error, got: %v", err)
	}
	for _, c := range runner.calls {
		if slices.Contains(c.args, "build") {
			t.Errorf("build ran despite the failed override: %v", c.args)
		}
	}
}

func TestDeployStack_NoBuildOverrideWithoutProjectDirectory(t *testing.T) {
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "myapp")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stackDir, "docker-compose.yml"), `services:
  app:
    build: "."
    image: myapp:latest
`)
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM nginx:1.27\n")

	runner := &recordingRunner{}
	d := newDeployerWithRunner(runner)

	stack := config.Stack{Name: "myapp"}
	if err := d.deployStackIfChanged(context.Background(), stack, baseDir, "", nil, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without a project directory compose already resolves the context against
	// the compose file's own directory in the clone — nothing to pin.
	build := findCall(t, runner.calls, "build")
	if countArg(build.args, "-f") != 0 {
		t.Errorf("expected no compose file selection, got: %v", build.args)
	}
	if build.dir != stackDir {
		t.Errorf("expected build to run in %s, got %s", stackDir, build.dir)
	}
}

// parseComposeString parses compose YAML written to a temp file, the same path
// the deploy code takes.
func parseComposeString(t *testing.T, content string) *composeFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-compose.yml")
	writeFile(t, path, content)
	cf, err := parseComposeFile(path)
	if err != nil {
		t.Fatalf("parse compose: %v", err)
	}
	return cf
}

// assertOverrideContexts checks that the override YAML pins exactly the given
// services to the given absolute build contexts.
func assertOverrideContexts(t *testing.T, override []byte, want map[string]string) {
	t.Helper()
	if override == nil {
		t.Fatal("expected a build context override, got none")
	}
	got, err := parseOverrideContexts(override)
	if err != nil {
		t.Fatalf("parse override %q: %v", override, err)
	}
	if len(got) != len(want) {
		t.Fatalf("override pins %d service(s), want %d:\n%s", len(got), len(want), override)
	}
	for service, ctx := range want {
		if got[service] != ctx {
			t.Errorf("service %q context = %q, want %q", service, got[service], ctx)
		}
	}
}

// overrideFilePath finds the override compose file in a recorded build argv:
// it is the second -f, the one that is not the stack's compose file.
func overrideFilePath(t *testing.T, args []string) string {
	t.Helper()
	var path string
	for i, a := range args {
		if a == "-f" && i+1 < len(args) && !strings.HasSuffix(args[i+1], "docker-compose.yml") {
			path = args[i+1]
		}
	}
	if path == "" {
		t.Fatalf("no override compose file in build argv: %v", args)
	}
	return path
}

// readOverrideFile reads the override compose file back from a recorded build
// argv — only possible while the build call is in flight, before cleanup.
func readOverrideFile(t *testing.T, args []string) []byte {
	t.Helper()
	path := overrideFilePath(t, args)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read override %s: %v", path, err)
	}
	return data
}

func findCall(t *testing.T, calls []runCall, command string) runCall {
	t.Helper()
	for _, c := range calls {
		if slices.Contains(c.args, command) {
			return c
		}
	}
	t.Fatalf("no %q call recorded, got %d call(s)", command, len(calls))
	return runCall{}
}

func countArg(args []string, arg string) int {
	n := 0
	for _, a := range args {
		if a == arg {
			n++
		}
	}
	return n
}
