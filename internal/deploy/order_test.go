package deploy

import (
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestRunOrder_NixosFirstWhenRebuildEnabled(t *testing.T) {
	cfg := &config.Config{NixOSRebuild: &config.NixOSRebuild{}} // section present = enabled
	stacks := []config.Stack{{Name: "gitea"}, {Name: "web"}}

	got := RunOrder(cfg, stacks)

	want := []string{NixosStateKey, "gitea", "web"}
	if !slices.Equal(got, want) {
		t.Errorf("RunOrder = %v, want %v", got, want)
	}
}

func TestRunOrder_StacksOnlyWhenRebuildDisabled(t *testing.T) {
	got := RunOrder(&config.Config{}, []config.Stack{{Name: "gitea"}})

	want := []string{"gitea"}
	if !slices.Equal(got, want) {
		t.Errorf("RunOrder = %v, want %v", got, want)
	}
}
