package config_test

import (
	"strings"
	"testing"
)

// depends_on validation (ADR-0032): entries must name other configured stacks
// and the graph must be acyclic, so a broken graph fails at load time instead
// of surfacing as runtime behaviour.

func TestLoad_AcceptsDependsOn(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: postgres
  - name: app
    depends_on: [postgres]
`
	cfg := loadFromString(t, content)

	if got := cfg.Stacks[1].DependsOn; len(got) != 1 || got[0] != "postgres" {
		t.Errorf("expected app to depend on postgres, got %v", got)
	}
	if len(cfg.Stacks[0].DependsOn) != 0 {
		t.Errorf("expected postgres to have no dependencies, got %v", cfg.Stacks[0].DependsOn)
	}
}

func TestLoad_RejectsDependsOnUnknownStack(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: app
    depends_on: [postgers]
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Fatal("expected error for depends_on naming an unknown stack, got nil")
	}
	if !strings.Contains(err.Error(), "postgers") {
		t.Errorf("error should name the unknown stack, got %q", err)
	}
}

func TestLoad_RejectsDependsOnSelf(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: app
    depends_on: [app]
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Fatal("expected error for a stack depending on itself, got nil")
	}
	if !strings.Contains(err.Error(), "app") {
		t.Errorf("error should name the stack, got %q", err)
	}
}

func TestLoad_RejectsDependsOnCycle(t *testing.T) {
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: a
    depends_on: [b]
  - name: b
    depends_on: [c]
  - name: c
    depends_on: [a]
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Fatal("expected error for a dependency cycle, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention the cycle, got %q", err)
	}
}

func TestLoad_RejectsDependsOnReservedNixosKey(t *testing.T) {
	// _nixos is implicitly first for every stack (Invariant 4) and cannot be a
	// declared dependency.
	content := `
repo_url: ssh://git@gitea.example.com/user/nixos.git
stacks_base_dir: /var/lib/skipper/repo/modules
stacks:
  - name: app
    depends_on: [_nixos]
`
	_, err := loadStringToConfig(t, content)
	if err == nil {
		t.Fatal("expected error for depends_on referencing _nixos, got nil")
	}
}
