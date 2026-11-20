package deploy

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func TestDeployAllStacks_RunEndHookFollowsPostRunHook(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"alpha"}, nil)
	var calls []string
	d := New(Config{
		Runner:      &recordingRunner{},
		RepoDir:     repoDir,
		StateDir:    t.TempDir(),
		PostRunHook: func() { calls = append(calls, "post") },
		RunEndHook:  func() { calls = append(calls, "end") },
	})

	d.DeployAllStacks(context.Background(), cfg)

	if want := []string{"post", "end"}; !slices.Equal(calls, want) {
		t.Errorf("hook calls = %v, want %v", calls, want)
	}
}

func TestDeployAllStacks_RunEndHookFiresOnAbortedRun(t *testing.T) {
	tests := []struct {
		name string
		cfg  func(t *testing.T) (string, *config.Config)
	}{
		{"unreadable vars_file", func(t *testing.T) (string, *config.Config) {
			repoDir, cfg := discoveryRepo(t, []string{"alpha"}, nil)
			cfg.VarsFile = filepath.Join(t.TempDir(), "missing.env")
			return repoDir, cfg
		}},
		{"stack discovery failure", func(t *testing.T) (string, *config.Config) {
			repoDir, cfg := discoveryRepo(t, []string{"alpha"}, nil)
			writeFile(t, filepath.Join(repoDir, "stacks", "skipper.yaml"), "stacks: {}\n")
			return repoDir, cfg
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir, cfg := tt.cfg(t)
			var post, end int
			d := New(Config{
				Runner:      &recordingRunner{},
				RepoDir:     repoDir,
				StateDir:    t.TempDir(),
				PostRunHook: func() { post++ },
				RunEndHook:  func() { end++ },
			})

			d.DeployAllStacks(context.Background(), cfg)

			if post != 0 {
				t.Fatalf("PostRunHook ran %d times; the case no longer aborts the run", post)
			}
			if end != 1 {
				t.Errorf("RunEndHook ran %d times, want 1", end)
			}
		})
	}
}
