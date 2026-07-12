// Package nixos handles NixOS rebuild logic. It has no knowledge of Docker,
// state persistence, events, or metrics — those concerns stay in the deploy package.
package nixos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/command"
)

// rebuildUnit is the fixed transient-unit name for the nixos-rebuild. A fixed
// name keeps it easy to inspect (`journalctl -u skipper-nixos-rebuild`); deploys
// serialize on one mutex, so only one ever runs at a time.
const rebuildUnit = "skipper-nixos-rebuild"

// defaultPollInterval is how often Rebuild polls the transient unit for
// completion. The rebuild takes seconds to minutes, so a coarse interval keeps
// the polling overhead negligible.
const defaultPollInterval = 2 * time.Second

// Rebuilder runs nixos-rebuild when nix files change.
type Rebuilder struct {
	runner       command.Runner
	pollInterval time.Duration
}

// New creates a Rebuilder with the given command runner.
func New(runner command.Runner) *Rebuilder {
	return &Rebuilder{runner: runner, pollInterval: defaultPollInterval}
}

// HashFiles walks repoDir and returns SHA-256 hashes for all *.nix files
// and flake.lock. Hidden directories (.git etc.) are skipped.
func HashFiles(repoDir string) (map[string]string, error) {
	hashes := make(map[string]string)

	err := filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".nix") || name == "flake.lock" {
			hash, err := hashFile(path)
			if err != nil {
				return fmt.Errorf("hash %s: %w", path, err)
			}
			hashes[path] = hash
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", repoDir, err)
	}
	return hashes, nil
}

// Rebuild runs `nixos-rebuild switch --flake <flake>` from repoDir inside a
// fire-and-forget transient systemd unit, then polls the unit to completion.
//
// The rebuild must NOT stay a child of the skipper process. The switch it runs
// restarts skipper-cd.service; were skipper still holding a `systemd-run --wait`
// client in its own cgroup, that stop could not complete until the client
// exited, but the client would not exit until the rebuild finished — which is
// blocked on that very stop. That deadlock (broken only by a kill/timeout) left
// the switch half-applied and the host on the old generation (ADR-0014). So
// systemd-run returns immediately (no --wait/--pipe): the unit runs
// independently in system.slice and survives skipper being restarted by its
// own switch. Rebuild then watches the unit via `systemctl is-active` exit
// codes — no client in skipper's cgroup, no deadlock.
func (r *Rebuilder) Rebuild(ctx context.Context, repoDir, flake string) error {
	unit := rebuildUnit + ".service"
	// Clear a prior run's lingering failed unit so the fixed name is free to
	// reuse (a successful run's unit is garbage-collected automatically).
	_ = r.runner.Run(ctx, "", nil, "systemctl", "reset-failed", unit)

	args := []string{
		"--unit=" + rebuildUnit,
		// Transient units start with a minimal environment; nixos-rebuild
		// needs skipper's PATH (git, nix, systemd tooling).
		"--setenv=PATH=" + os.Getenv("PATH"),
		"nixos-rebuild", "switch", "--flake", flake,
	}
	if err := r.runner.Run(ctx, repoDir, nil, "systemd-run", args...); err != nil {
		return fmt.Errorf("start nixos-rebuild unit: %w", err)
	}
	return r.waitForUnit(ctx, unit)
}

// waitForUnit polls the transient rebuild unit until it is no longer active,
// then reports success or failure. It reads state from `systemctl` exit codes
// only, so it needs no output capture. The status probes use a background
// context (not ctx): a probe must still resolve the unit's outcome even as
// shutdown cancels ctx — the abandon decision is made explicitly against ctx.
func (r *Rebuilder) waitForUnit(ctx context.Context, unit string) error {
	for {
		if ctx.Err() != nil {
			// Shutdown (or timeout): the switch is likely restarting skipper.
			// Abandon the wait; the detached unit carries on and completes it.
			return ctx.Err()
		}
		if !r.unitActive(unit) {
			if r.unitFailed(unit) {
				return fmt.Errorf("nixos-rebuild switch failed (see: journalctl -u %s)", unit)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.pollInterval):
		}
	}
}

// unitActive reports whether the unit is still activating or running.
// `systemctl is-active` exits 0 for active/activating, non-zero otherwise.
func (r *Rebuilder) unitActive(unit string) bool {
	return r.runner.Run(context.Background(), "", nil, "systemctl", "is-active", "--quiet", unit) == nil
}

// unitFailed reports whether the unit ended in the failed state.
// `systemctl is-failed` exits 0 when the unit is failed. A successful (or
// already garbage-collected) unit is not failed, so this returns false.
func (r *Rebuilder) unitFailed(unit string) bool {
	return r.runner.Run(context.Background(), "", nil, "systemctl", "is-failed", "--quiet", unit) == nil
}

// DiffHashes returns file paths that are new or changed compared to prev.
func DiffHashes(current, prev map[string]string) []string {
	var changed []string
	for path, hash := range current {
		if prev[path] != hash {
			changed = append(changed, path)
		}
	}
	return changed
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
