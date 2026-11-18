package deploy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/regularfile"
)

// hashStackInputs hashes a stack's tracked inputs (Invariant 2). It is their
// single definition: the deploy path and the run-plan look-ahead both resolve
// here, so neither can track an input the other misses. cf is nil for an
// unparseable compose file — no Dockerfile is tracked then. The Dockerfile
// paths come back because the deploy path needs them again to decide on a build.
func (d *Deployer) hashStackInputs(ctx context.Context, stack config.Stack, baseDir, repoDir, varsFile string, cf *composeFile) (stackFileHashes, []string, error) {
	var dockerfilePaths []string
	if cf != nil {
		dockerfilePaths = cf.dockerfilePaths(repoDir)
	}
	hashes, err := computePerFileHashes(ctx, repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		return nil, nil, err
	}
	d.addStackConfigHash(hashes, stack, baseDir)
	return hashes, dockerfilePaths, nil
}

// maxHashedFileBytes caps one hashed input — far above any plausible compose
// file, env file, Dockerfile or watched config, low enough that hashing
// cannot stall the deploy mutex (Invariant 7) on a giant file.
const maxHashedFileBytes = 64 << 20

// computePerFileHashes returns a SHA-256 hash for each tracked file in the stack.
// Any change to any of these files triggers a new deployment.
// When varsFile is non-empty, it is included in the hash set so that changes
// to global variables also trigger redeployment.
func computePerFileHashes(ctx context.Context, workDir string, envFiles []string, watchDirs []string, varsFile string, extraFiles []string) (stackFileHashes, error) {
	filePaths := append([]string{filepath.Join(workDir, compose.FileName)}, envFiles...)
	if varsFile != "" {
		filePaths = append(filePaths, varsFile)
	}
	filePaths = append(filePaths, extraFiles...)
	hashes := make(stackFileHashes)

	for _, path := range filePaths {
		hash, err := hashFile(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("hash file %s: %w", path, err)
		}
		hashes[path] = hash
	}

	for _, dir := range watchDirs {
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			hash, err := hashFile(ctx, path)
			if err != nil {
				return fmt.Errorf("hash file %s: %w", path, err)
			}
			hashes[path] = hash
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk watch_dir %s: %w", dir, err)
		}
	}

	return hashes, nil
}

// changedFiles returns the paths of files whose hash differs between current and last.
func changedFiles(current, last stackFileHashes) []string {
	var changed []string
	for path, hash := range current {
		if last[path] != hash {
			changed = append(changed, path)
		}
	}
	return changed
}

// hashFile hashes a regular file of at most maxHashedFileBytes. A symlink is
// followed (an absolute env file may be a host secret behind one), but its
// target must be regular too: a FIFO or a device would block or never end.
func hashFile(ctx context.Context, path string) (string, error) {
	file, err := regularfile.Open(path, regularfile.FollowLinks)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	n, err := io.Copy(hasher, ctxReader{ctx, io.LimitReader(file, maxHashedFileBytes+1)})
	if err != nil {
		return "", err
	}
	if n > maxHashedFileBytes {
		return "", regularfile.TooLarge(path, maxHashedFileBytes)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// ctxReader stops a read between chunks once ctx is done, so a shutdown is
// not held up by a large hash.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
