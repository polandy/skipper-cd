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
  db:
    image: postgres:18-alpine
`)

	got, err := buildContextOverride(cf, repoDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"app":    repoDir,
		"worker": filepath.Join(repoDir, "worker"),
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

	// Project identity is untouched: the build still runs against the same
	// --project-directory, so the image it tags is the one `up` then starts.
	build := findCall(t, runner.calls, "build")
	if !slices.Contains(build.args, "--project-directory") || !slices.Contains(build.args, projectDir) {
		t.Errorf("build lost the project directory: %v", build.args)
	}
	// Only the build reads the override; every other call stays as it was.
	up := findCall(t, runner.calls, "up")
	if got := countArg(up.args, "-f"); got != 1 {
		t.Errorf("expected up to pass exactly one -f, got %d: %v", got, up.args)
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

// readOverrideFile reads the override compose file from a recorded build argv:
// it is the second -f, the one that is not the stack's compose file.
func readOverrideFile(t *testing.T, args []string) []byte {
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
