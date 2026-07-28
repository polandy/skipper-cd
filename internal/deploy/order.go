package deploy

import "github.com/polandy/skipper-cd/internal/config"

// RunOrder returns stack names in the order DeployAllStacks processes them:
// _nixos first (when the rebuild is enabled), then the effective stacks.
func RunOrder(cfg *config.Config, stacks []config.Stack) []string {
	order := make([]string, 0, len(stacks)+1)
	if cfg.NixOSRebuild.IsEnabled() {
		order = append(order, NixosStateKey)
	}
	for _, s := range stacks {
		order = append(order, s.Name)
	}
	return order
}
