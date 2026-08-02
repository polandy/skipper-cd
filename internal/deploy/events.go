// Deploy events: turning what a run did into the events the UI, the audit log
// and the notifications read. Emission, the change context each event carries
// (diffs and commits since the last deployed commit), and the repo-relative
// path shortening those events need — the UI has no notion of the clone dir.

package deploy

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// changeSet carries what a deploy is applying: the changed tracked files and
// their git context (diffs and commits since the last deployed commit). The
// zero value means "no change context" (e.g. a skipped stack).
type changeSet struct {
	files        []string
	diffs        map[string]string
	commits      []events.CommitInfo
	imageChanges []events.ServiceImageChange
	// healthGated records that the deploy ran under an effective
	// deploy_health_check (explicit or inferred from a compose healthcheck), so a
	// success event says the stack was verified healthy, not just applied.
	healthGated bool
}

// collectChange gathers the full change context for the given changed files:
// their diffs and the commits that produced them, both against
// lastDeployedCommit. Diffs and commits are nil when no CommitReader is
// configured or no previous commit is known.
func (d *Deployer) collectChange(ctx context.Context, changedFiles []string, lastDeployedCommit string) changeSet {
	return changeSet{
		files:   changedFiles,
		diffs:   d.collectDiffs(ctx, changedFiles, lastDeployedCommit),
		commits: d.collectCommits(ctx, changedFiles, lastDeployedCommit),
	}
}

// emit sends a deploy event to the sink and returns its ID (0 when no
// sink is configured). The ID lets log lines reference the event, e.g.
// for diff lookups via /api/events/{id}/diffs.
func (d *Deployer) emit(status events.Status, stack string, duration time.Duration, errMsg string, cs changeSet) int64 {
	if d.eventSink == nil {
		return 0
	}
	id := d.nextEventID.Add(1)
	d.eventSink(events.DeployEvent{
		ID:           id,
		Timestamp:    time.Now(),
		Stack:        stack,
		Status:       status,
		DurationMs:   duration.Milliseconds(),
		Error:        errMsg,
		ChangedFiles: d.repoRelativePaths(cs.files),
		Diffs:        d.repoRelativeDiffs(cs.diffs),
		Commits:      cs.commits,
		ImageChanges: cs.imageChanges,
		HealthGated:  cs.healthGated,
	})
	return id
}

// emitDeployFailure counts the error and emits the terminal event that matches
// how the deploy ended: rolled_back_unhealthy when the restored version also
// failed its health gate, rolled_back when the rollback recovered, else a plain
// failed. It carries the same change set a success does, so the UI can show
// what the failed deploy was applying and render its diff — not just the file
// paths left over from the deploying row.
func (d *Deployer) emitDeployFailure(stack string, duration time.Duration, err error, cs changeSet) {
	metrics.DeployErrors.WithLabelValues(stack).Inc()
	switch {
	case errors.Is(err, ErrRollbackUnhealthy):
		slog.Error("deploy failed, rollback ran but stack is still unhealthy", "stack", stack, "err", err)
		d.emit(events.StatusRolledBackUnhealthy, stack, duration, err.Error(), cs)
	case errors.Is(err, ErrRolledBack):
		slog.Warn("deploy failed but rolled back", "stack", stack, "err", err)
		d.emit(events.StatusRolledBack, stack, duration, err.Error(), cs)
	default:
		slog.Error("deploy failed", "stack", stack, "err", err)
		d.emit(events.StatusFailed, stack, duration, err.Error(), cs)
	}
}

// relToRepo returns path relative to repoDir and whether it lies inside it.
// An empty repoDir never matches.
func relToRepo(repoDir, path string) (string, bool) {
	if repoDir == "" {
		return "", false
	}
	rel, err := filepath.Rel(repoDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// insideRepo reports whether path lies inside the repo clone, for excluding
// out-of-repo files (e.g. env files under /etc) from git diff/log lookups.
// With no repo dir configured every path counts as inside — there is nothing
// to exclude against.
func (d *Deployer) insideRepo(path string) bool {
	if d.repoDir == "" {
		return true
	}
	_, inside := relToRepo(d.repoDir, path)
	return inside
}

// repoRelative shortens an absolute path under the repo clone to a repo-relative
// path for display: the hashing and diff layers work in absolute filesystem
// paths, but the UI has no notion of the repo dir. Paths outside the repo (or
// when the repo dir is unknown) are returned unchanged.
func (d *Deployer) repoRelative(path string) string {
	if rel, inside := relToRepo(d.repoDir, path); inside {
		return rel
	}
	return path
}

// repoRelativePaths returns a copy of files with each path shortened to
// repo-relative for display; nil in, nil out (leaves the caller's slice intact).
func (d *Deployer) repoRelativePaths(files []string) []string {
	if files == nil {
		return nil
	}
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = d.repoRelative(f)
	}
	return out
}

// repoRelativeDiffs returns a copy of the diff map re-keyed to repo-relative
// paths for display; nil in, nil out.
func (d *Deployer) repoRelativeDiffs(diffs map[string]string) map[string]string {
	if diffs == nil {
		return nil
	}
	out := make(map[string]string, len(diffs))
	for path, diff := range diffs {
		out[d.repoRelative(path)] = diff
	}
	return out
}

// emitHealed emits the self-heal corrective-redeploy event. A heal has no
// changed files, diffs, or commits (the desired version is unchanged), so it
// carries only the drift that triggered it — the services the UI shows the heal
// reacted to. Its own path rather than emit's, whose diff/commit params never
// apply here.
func (d *Deployer) emitHealed(stack string, duration time.Duration, drift []events.DriftedService) {
	if d.eventSink == nil {
		return
	}
	d.eventSink(events.DeployEvent{
		ID:         d.nextEventID.Add(1),
		Timestamp:  time.Now(),
		Stack:      stack,
		Status:     events.StatusHealed,
		DurationMs: duration.Milliseconds(),
		HealDrift:  drift,
	})
}

// EmitHealExhausted records that self-heal gave up on a stack it could not
// restore after repeated corrective redeploys (ADR-0029). It is routed through
// the deploy event pipeline so it lands in history, the SSE stream, and
// notifications like any other terminal outcome, and counts a deploy error for
// metrics/alerting.
func (d *Deployer) EmitHealExhausted(stack string) {
	metrics.DeployErrors.WithLabelValues(stack).Inc()
	d.emit(events.StatusHealExhausted, stack, 0, "self-heal exhausted: still unhealthy after repeated corrective redeploys", changeSet{})
}

const (
	maxDiffPerFile = 10 * 1024 // 10 KB per file
	maxDiffTotal   = 50 * 1024 // 50 KB total per event
)

// collectDiffs collects git diffs for each changed file inside the repo and
// returns them as a map of file path to diff content. Large diffs are
// truncated. Returns nil when no CommitReader is configured or no previous
// commit is known.
func (d *Deployer) collectDiffs(ctx context.Context, changedFilePaths []string, lastDeployedCommit string) map[string]string {
	if d.commitReader == nil || lastDeployedCommit == "" {
		return nil
	}
	result := make(map[string]string)
	totalSize := 0
	for _, filePath := range changedFilePaths {
		if !d.insideRepo(filePath) {
			continue
		}
		diff, err := d.commitReader.DiffSinceCommit(ctx, lastDeployedCommit, filePath)
		if err != nil {
			slog.Warn("could not compute diff", "file", filePath, "err", err)
			continue
		}
		if diff == "" {
			continue
		}
		if len(diff) > maxDiffPerFile {
			diff = diff[:maxDiffPerFile] + "\n... (truncated)"
		}
		// The diff rides along on the log record: the console renders it under
		// the file name (internal/prettylog), because it is the one surface
		// that cannot fetch it — the web UI opens the same content from the
		// deploy event on demand, and the in-memory log ring summarises the
		// attr rather than carrying the payload (internal/logbuf).
		slog.Info("file changed", "file", d.repoRelative(filePath), "diff", diff)
		if totalSize+len(diff) > maxDiffTotal {
			remaining := maxDiffTotal - totalSize
			if remaining > 0 {
				diff = diff[:remaining] + "\n... (truncated)"
			} else {
				break
			}
		}
		result[filePath] = diff
		totalSize += len(diff)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// collectCommits returns the commits in the range lastDeployedCommit..HEAD that
// touched the event's changed files (repo-relative story of what shipped), for
// the diff panel's commit header. Returns nil when no CommitReader is configured,
// no previous commit is known, or no changed file lives inside the repo clone —
// mirroring collectDiffs so the header and the diffs stay in lockstep.
func (d *Deployer) collectCommits(ctx context.Context, changedFilePaths []string, lastDeployedCommit string) []events.CommitInfo {
	if d.commitReader == nil || lastDeployedCommit == "" {
		return nil
	}
	repoFiles := make([]string, 0, len(changedFilePaths))
	for _, filePath := range changedFilePaths {
		if !d.insideRepo(filePath) {
			continue
		}
		repoFiles = append(repoFiles, filePath)
	}
	if len(repoFiles) == 0 {
		return nil
	}
	commits, err := d.commitReader.CommitsSinceCommit(ctx, lastDeployedCommit, repoFiles)
	if err != nil {
		slog.Warn("could not read commit metadata", "err", err)
		return nil
	}
	return commits
}
