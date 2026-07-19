package config_test

import (
	"strings"
	"testing"
)

func TestLoad_HooksParsed(t *testing.T) {
	content := minimalConfig + `
stacks:
  - name: paperless
    hooks:
      pre_deploy:
        - "pg_dump paperless > /backup/pre.sql"
      post_deploy:
        - "curl -fsS http://localhost:8000/health"
        - "echo done"
      timeout_seconds: 120
`
	cfg := loadFromString(t, content)

	s, ok := cfg.StackByName("paperless")
	if !ok {
		t.Fatal("stack paperless not found")
	}
	if len(s.Hooks.PreDeploy) != 1 || s.Hooks.PreDeploy[0] != "pg_dump paperless > /backup/pre.sql" {
		t.Errorf("pre_deploy = %v", s.Hooks.PreDeploy)
	}
	if len(s.Hooks.PostDeploy) != 2 {
		t.Errorf("post_deploy = %v, want 2 entries", s.Hooks.PostDeploy)
	}
	if s.Hooks.TimeoutSeconds != 120 {
		t.Errorf("timeout_seconds = %d, want 120", s.Hooks.TimeoutSeconds)
	}
}

func TestLoad_HooksOptional(t *testing.T) {
	// A stack without a hooks section is valid and carries empty hook lists.
	cfg := loadFromString(t, minimalConfig+`
stacks:
  - name: web
`)
	s, _ := cfg.StackByName("web")
	if len(s.Hooks.PreDeploy) != 0 || len(s.Hooks.PostDeploy) != 0 {
		t.Errorf("expected no hooks, got %+v", s.Hooks)
	}
}

func TestLoad_RejectsEmptyHookCommand(t *testing.T) {
	for _, phase := range []string{"pre_deploy", "post_deploy"} {
		content := minimalConfig + `
stacks:
  - name: web
    hooks:
      ` + phase + `:
        - "   "
`
		_, err := loadStringToConfig(t, content)
		if err == nil {
			t.Errorf("%s: expected error for a blank hook command, got nil", phase)
			continue
		}
		if !strings.Contains(err.Error(), "hooks") {
			t.Errorf("%s: error should mention hooks, got: %v", phase, err)
		}
	}
}

func TestLoad_RejectsNegativeHookTimeout(t *testing.T) {
	content := minimalConfig + `
stacks:
  - name: web
    hooks:
      pre_deploy:
        - "echo hi"
      timeout_seconds: -5
`
	if _, err := loadStringToConfig(t, content); err == nil {
		t.Fatal("expected error for negative hook timeout_seconds, got nil")
	}
}
