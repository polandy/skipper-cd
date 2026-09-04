package deploy

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/config"
)

// hashStackInputs hashes every tracked input of a stack (Invariant 2): the
// compose file, env_files, the global vars_file and the watch_dirs contents
// under repoDir, the Dockerfiles of its build: services, and the stack's
// deploy-shaping config hash. It is the single definition of what change
// detection reads — the deploy path and the run-plan look-ahead both resolve
// their inputs here, so neither can start tracking one the other misses.
// cf is nil for an unparseable compose file: no Dockerfile is tracked then.
// The Dockerfile paths are returned because the deploy path needs them again
// to decide whether to build.
func (d *Deployer) hashStackInputs(stack config.Stack, baseDir, repoDir, varsFile string, cf *composeFile) (stackFileHashes, []string, error) {
	var dockerfilePaths []string
	if cf != nil {
		dockerfilePaths = cf.dockerfilePaths(repoDir)
	}
	hashes, err := computePerFileHashes(repoDir, stack.EnvFiles, stack.WatchDirs, varsFile, dockerfilePaths)
	if err != nil {
		return nil, nil, err
	}
	d.addStackConfigHash(hashes, stack, baseDir)
	return hashes, dockerfilePaths, nil
}

// computePerFileHashes returns a SHA-256 hash for each tracked file in the stack.
// Any change to any of these files triggers a new deployment.
// When varsFile is non-empty, it is included in the hash set so that changes
// to global variables also trigger redeployment.
func computePerFileHashes(workDir string, envFiles []string, watchDirs []string, varsFile string, extraFiles []string) (stackFileHashes, error) {
	filePaths := append([]string{filepath.Join(workDir, compose.FileName)}, envFiles...)
	if varsFile != "" {
		filePaths = append(filePaths, varsFile)
	}
	filePaths = append(filePaths, extraFiles...)
	hashes := make(stackFileHashes)

	for _, path := range filePaths {
		hasher := sha256.New()
		if err := addFileContentsToHash(hasher, path); err != nil {
			return nil, fmt.Errorf("hash file %s: %w", path, err)
		}
		hashes[path] = fmt.Sprintf("%x", hasher.Sum(nil))
	}

	for _, dir := range watchDirs {
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			hasher := sha256.New()
			if err := addFileContentsToHash(hasher, path); err != nil {
				return fmt.Errorf("hash file %s: %w", path, err)
			}
			hashes[path] = fmt.Sprintf("%x", hasher.Sum(nil))
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

func addFileContentsToHash(hasher io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(hasher, file)
	return err
}
