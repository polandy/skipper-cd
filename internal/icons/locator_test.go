package icons

import (
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

// staticStacks adapts a fixed stack list to the stack-source func the locator
// takes (main wires stackViews.effective here).
func staticStacks(stacks []config.Stack) func() []config.Stack {
	return func() []config.Stack { return stacks }
}

func TestNewStackLocator_ResolvesNixosPseudoStackToNixosSlug(t *testing.T) {
	locate := NewStackLocator(&config.Config{}, staticStacks(nil))

	req, ok := locate(config.ReservedStackName)
	if !ok {
		t.Fatalf("expected the reserved %q stack to resolve an icon", config.ReservedStackName)
	}
	if req.Name != "nixos" {
		t.Errorf("expected icon request name %q, got %q", "nixos", req.Name)
	}
}

func TestNewStackLocator_ResolvesConfigPseudoStackToGitSlug(t *testing.T) {
	locate := NewStackLocator(&config.Config{}, staticStacks(nil))

	req, ok := locate(config.ReservedConfigStackName)
	if !ok {
		t.Fatalf("expected the reserved %q stack to resolve an icon", config.ReservedConfigStackName)
	}
	if req.Name != "git" {
		t.Errorf("expected icon request name %q, got %q", "git", req.Name)
	}
}

func TestNewStackLocator_ResolvesConfiguredStack(t *testing.T) {
	cfg := &config.Config{
		StacksBaseDir: "/srv/stacks",
		Stacks:        []config.Stack{{Name: "gitea", Icon: "forgejo"}},
	}
	locate := NewStackLocator(cfg, staticStacks(cfg.Stacks))

	req, ok := locate("gitea")
	if !ok {
		t.Fatal("expected configured stack to resolve")
	}
	want := Request{Name: "gitea", Slug: "forgejo", Dir: "/srv/stacks/gitea"}
	if req != want {
		t.Errorf("got %+v, want %+v", req, want)
	}
}

func TestNewStackLocator_UnknownStackIsNotFound(t *testing.T) {
	locate := NewStackLocator(&config.Config{}, staticStacks([]config.Stack{{Name: "gitea"}}))

	if _, ok := locate("nope"); ok {
		t.Error("expected unknown stack to be not found")
	}
}
