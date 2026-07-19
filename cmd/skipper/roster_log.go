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
func logStackRoster(stacks []config.Stack) {
	slog.Info(prettylog.MsgStacksResolved, "stacks", len(stacks))
	for _, s := range stacks {
		slog.Info(prettylog.MsgStackDiscovered,
			"stack", s.Name,
			"pre_deploy_hooks", len(s.Hooks.PreDeploy),
			"post_deploy_hooks", len(s.Hooks.PostDeploy),
			"watch_dirs", s.WatchDirs,
		)
	}
}
