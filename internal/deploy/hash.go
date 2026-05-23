package deploy

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// computePerFileHashes returns a SHA-256 hash for each tracked file in the stack.
// Any change to any of these files triggers a new deployment.
// When varsFile is non-empty, it is included in the hash set so that changes
// to global variables also trigger redeployment.
func computePerFileHashes(workDir string, envFiles []string, watchDirs []string, varsFile string, extraFiles []string) (stackFileHashes, error) {
	filePaths := append([]string{filepath.Join(workDir, "docker-compose.yml")}, envFiles...)
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
