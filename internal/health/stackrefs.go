package health

import (
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/config"
)

// StackRefs maps each effective stack to the compose identity the poller
// probes: the compose file from the repo clone plus project_directory (if any)
// as --project-directory — the same identity the deploy path uses.
func StackRefs(cfg *config.Config, stacks []config.Stack) []StackRef {
	refs := make([]StackRef, 0, len(stacks))
	for _, s := range stacks {
		refs = append(refs, StackRef{
			Name:        s.Name,
			ComposePath: filepath.Join(cfg.StacksBaseDir, s.Name, compose.FileName),
			ProjectDir:  s.ProjectDirectory,
			OnDemand:    s.OnDemandContainers,
		})
	}
	return refs
}
