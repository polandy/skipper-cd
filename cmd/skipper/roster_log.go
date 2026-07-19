package main

import (
	"log/slog"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/prettylog"
)

// logStackRoster logs the effective stack set once, so an operator can see
// what skipper is watching — and each stack's hooks/watch dirs — without
// waiting for a deploy event. Callers guard it with a sync.Once: a static
// host `stacks:` list is known immediately at startup, but in stack-discovery
// mode (ADR-0034, Invariant 8) the set isn't known until the first sync
// resolves it, so it is logged from the first PostRunHook instead.
//
// disabled carries the names parked via `disabled: true` (stack-discovery
// mode only — a static host `stacks:` list has no such concept, so callers
// pass nil there): still discovered in the repo, but deliberately excluded
// from stacks, matching the Stacks view's roster (dev-docs/stack-roster-spec.md).
func logStackRoster(stacks []config.Stack, disabled []string) {
	slog.Info(prettylog.MsgStacksResolved, "stacks", len(stacks))
	for _, s := range stacks {
		slog.Info(prettylog.MsgStackDiscovered,
			"stack", s.Name,
			"pre_deploy_hooks", len(s.Hooks.PreDeploy),
			"post_deploy_hooks", len(s.Hooks.PostDeploy),
			"watch_dirs", s.WatchDirs,
		)
	}
	if len(disabled) > 0 {
		slog.Info(prettylog.MsgStacksDisabled, "stacks", disabled)
	}
}
