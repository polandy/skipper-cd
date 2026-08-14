// Removed stacks: reporting that a stack left the deploy set. skipper deploys
// on a git push and visualizes what happened — it never tears a stack down
// (ADR-0036 discarded prune), so a removal is an entry in the history and the
// containers keep running, surfaced by orphan detection.

package deploy

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// announceRemovedStacks emits one removed event per stack skipper still tracks
// that the current set no longer contains, and forgets its recorded hashes so
// the announcement happens exactly once. The project dir is deliberately kept:
// it is what lets orphan detection recognize the still-running project as
// formerly managed (ADR-0036).
//
// cfg is the effective config of this run, so its stack list is already the
// discovered set in discovery mode. Runs before the deploys, so a push that
// removes one stack and changes another reads in that order.
func (d *Deployer) announceRemovedStacks(ctx context.Context, cfg *config.Config, state *persistedState) {
	known := d.knownStackNames(cfg)
	if len(known) == 0 {
		// Every stack gone at once is a broken clone far more often than an
		// operator deleting their whole fleet — and announcing it would drop the
		// hashes of stacks that are about to come back and redeploy them all.
		if len(state.Stacks) > 0 {
			slog.Warn("no stacks discovered: not reporting the recorded stacks as removed", "recorded", len(state.Stacks))
		}
		return
	}

	for _, name := range d.trackedStackNames(state) {
		if known[name] || isReservedStackKey(name) {
			continue
		}
		files := d.vanishedRepoFiles(d.trackedInputs(state, name), cfg.StacksBaseDir)
		d.emit(events.StatusRemoved, name, 0, "", d.collectChange(ctx, files, state.LastDeployedCommit))
		slog.Info("stack removed from the deploy set, its containers are left running", "stack", name)
		// A change still waiting behind paused autosync will never deploy now,
		// and a stuck entry holds the commit base back for every stack.
		d.clearQueued(name)
		state.forgetStack(name)
	}
}

// knownStackNames is the set of stacks that still exist for this run: every
// discovered directory in discovery mode (including the ones parked via
// disabled: true or excluded by an entry-level error — those are broken or
// paused, not gone), else the host config's listed stacks. nil when discovery
// has not produced a set yet, so nothing is judged removed against it.
func (d *Deployer) knownStackNames(cfg *config.Config) map[string]bool {
	var names []string
	if cfg.StackDiscovery {
		repo := d.discoveredStacks.Load()
		if repo == nil {
			return nil
		}
		names = repo.Discovered
	} else {
		for _, s := range cfg.Stacks {
			names = append(names, s.Name)
		}
	}
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
	}
	return known
}

// trackedStackNames returns every stack skipper still holds something for,
// alphabetically so a run that removes several announces them in a stable
// order: the ones recorded in state, plus the ones pending behind paused
// autosync — a change that was deferred and then removed was never recorded,
// yet its pending entry would outlive the stack forever.
func (d *Deployer) trackedStackNames(state *persistedState) []string {
	tracked := make(map[string]bool, len(state.Stacks))
	for name := range state.Stacks {
		tracked[name] = true
	}
	if d.queue != nil {
		for _, item := range d.queue.Snapshot(nil) {
			tracked[item.Stack] = true
		}
	}
	names := make([]string, 0, len(tracked))
	for name := range tracked {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// trackedInputs are the paths skipper last knew as the stack's inputs: its
// recorded hashes, or — for a stack whose only change was deferred, which
// records nothing — the files that change was about.
func (d *Deployer) trackedInputs(state *persistedState, stack string) []string {
	if hashes := state.hashesFor(stack); len(hashes) > 0 {
		paths := make([]string, 0, len(hashes))
		for path := range hashes {
			paths = append(paths, path)
		}
		return paths
	}
	if d.queue == nil {
		return nil
	}
	for _, item := range d.queue.Snapshot(nil) {
		if item.Stack == stack {
			return item.ChangedFiles
		}
	}
	return nil
}

// isReservedStackKey reports whether a state key is one of skipper's own
// pseudo-stacks rather than a real stack — they are never discovered and must
// never read as removed.
func isReservedStackKey(name string) bool {
	return name == config.ReservedStackName || name == config.ReservedConfigStackName
}

// vanishedRepoFiles returns those of the stack's tracked inputs that lived in
// the repo and are now gone from disk — what the removing commit deleted, and
// the paths the event's diffs and commits are collected for. Files outside the
// clone (an env file under /etc) and the synthetic per-stack config key
// (addStackConfigHash — a label, never a file) are left out.
func (d *Deployer) vanishedRepoFiles(paths []string, stacksBaseDir string) []string {
	configKey := filepath.Join(stacksBaseDir, config.RepoConfigFileName)
	var files []string
	for _, path := range paths {
		if path == configKey || !d.insideRepo(path) {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	return files
}
