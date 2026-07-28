package health

import (
	"reflect"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestStackRefs_MapsComposeIdentityPerStack(t *testing.T) {
	cfg := &config.Config{StacksBaseDir: "/srv/stacks"}
	stacks := []config.Stack{
		{Name: "gitea"},
		{Name: "paperless", ProjectDirectory: "/srv/data/paperless", OnDemandContainers: []string{"worker"}},
	}

	got := StackRefs(cfg, stacks)

	want := []StackRef{
		{Name: "gitea", ComposePath: "/srv/stacks/gitea/docker-compose.yml"},
		{Name: "paperless", ComposePath: "/srv/stacks/paperless/docker-compose.yml", ProjectDir: "/srv/data/paperless", OnDemand: []string{"worker"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStackRefs_EmptyStackSetYieldsEmptySlice(t *testing.T) {
	if got := StackRefs(&config.Config{}, nil); len(got) != 0 {
		t.Errorf("expected no refs, got %+v", got)
	}
}
