package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestLogComposeInvocation_WithProjectDirectory(t *testing.T) {
	d := New(Config{StateDir: t.TempDir()})
	cfg := &config.Config{
		StacksBaseDir: "/repo",
		Stacks:        []config.Stack{{Name: "web", ProjectDirectory: "/srv/web"}},
	}
	dir, _, args, ok, err := d.LogComposeInvocation(cfg, "web")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v, want ok,nil", ok, err)
	}
	if dir != "/srv/web" {
		t.Errorf("dir = %q, want /srv/web", dir)
	}
	want := "compose -f /repo/web/docker-compose.yml --project-directory /srv/web"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
}

func TestLogComposeInvocation_NoProjectDirectory(t *testing.T) {
	d := New(Config{StateDir: t.TempDir()})
	cfg := &config.Config{
		StacksBaseDir: "/repo",
		Stacks:        []config.Stack{{Name: "api"}},
	}
	dir, _, args, ok, err := d.LogComposeInvocation(cfg, "api")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	// Without project_directory compose runs from the compose file's dir with no -f.
	if dir != "/repo/api" {
		t.Errorf("dir = %q, want /repo/api", dir)
	}
	if got := strings.Join(args, " "); got != "compose" {
		t.Errorf("args = %q, want %q", got, "compose")
	}
}

func TestLogComposeInvocation_UnknownStack(t *testing.T) {
	d := New(Config{StateDir: t.TempDir()})
	cfg := &config.Config{StacksBaseDir: "/repo", Stacks: []config.Stack{{Name: "web"}}}
	_, _, _, ok, err := d.LogComposeInvocation(cfg, "ghost")
	if ok || err != nil {
		t.Fatalf("ok=%v err=%v, want false,nil", ok, err)
	}
}

func TestLogComposeInvocation_EnvFilesMerged(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "web.env")
	if err := os.WriteFile(envPath, []byte("COMPOSE_PROJECT_NAME=web_prod\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := New(Config{StateDir: t.TempDir()})
	cfg := &config.Config{
		StacksBaseDir: "/repo",
		Stacks:        []config.Stack{{Name: "web", EnvFiles: []string{envPath}}},
	}
	_, env, _, ok, err := d.LogComposeInvocation(cfg, "web")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !containsEnv(env, "COMPOSE_PROJECT_NAME=web_prod") {
		t.Errorf("env missing env_file entry; got tail %v", env[len(env)-1:])
	}
}

func TestLogComposeInvocation_EnvFileReadError(t *testing.T) {
	d := New(Config{StateDir: t.TempDir()})
	cfg := &config.Config{
		StacksBaseDir: "/repo",
		Stacks:        []config.Stack{{Name: "web", EnvFiles: []string{"/nonexistent/does-not-exist.env"}}},
	}
	_, _, _, ok, err := d.LogComposeInvocation(cfg, "web")
	if err == nil {
		t.Fatal("want error for unreadable env file")
	}
	if ok {
		t.Error("ok must be false on env-file error")
	}
}

func containsEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}
