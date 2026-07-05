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

	"github.com/polandy/skipper-cd/internal/command"
)

// Rebuilder runs nixos-rebuild when nix files change.
type Rebuilder struct {
	runner command.Runner
}

// New creates a Rebuilder with the given command runner.
func New(runner command.Runner) *Rebuilder {
	return &Rebuilder{runner: runner}
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

// RebuildIfChanged compares currentHashes with prevHashes. If any file changed,
// it runs `nixos-rebuild switch --flake <flake>` from repoDir.
// Returns the list of changed files and any error.
// Returns nil, nil when nothing changed.
func (r *Rebuilder) RebuildIfChanged(ctx context.Context, repoDir, flake string, prevHashes map[string]string) ([]string, error) {
	currentHashes, err := HashFiles(repoDir)
	if err != nil {
		return nil, err
	}

	changed := DiffHashes(currentHashes, prevHashes)
	if len(changed) == 0 {
		return nil, nil
	}

	if err := r.runner.Run(ctx, repoDir, nil, "nixos-rebuild", "switch", "--flake", flake); err != nil {
		return changed, fmt.Errorf("nixos-rebuild switch --flake %s: %w", flake, err)
	}
	return changed, nil
}

// Rebuild runs `nixos-rebuild switch --flake <flake>` from repoDir.
func (r *Rebuilder) Rebuild(ctx context.Context, repoDir, flake string) error {
	if err := r.runner.Run(ctx, repoDir, nil, "nixos-rebuild", "switch", "--flake", flake); err != nil {
		return fmt.Errorf("nixos-rebuild switch --flake %s: %w", flake, err)
	}
	return nil
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
