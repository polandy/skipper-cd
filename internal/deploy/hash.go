package deploy

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const deployStateFilePath = "/var/lib/skipper/state.yaml"

// stackFileHashes maps each tracked file path to its SHA-256 hash.
type stackFileHashes map[string]string

func newEmptyState() persistedState {
	return persistedState{
		Stacks: map[string]stackFileHashes{},
		Images: map[string]serviceImageByName{},
	}
}

// persistedState holds the full deploy state written to disk.
type persistedState struct {
	// LastDeployedCommit is the git commit SHA of the last successful deploy run.
	// It is used to compute diffs between the last deploy and the current HEAD.
	LastDeployedCommit string `yaml:"last_deployed_commit,omitempty"`

	// Stacks maps stack names to their per-file hashes from the last deployment.
	Stacks map[string]stackFileHashes `yaml:"stacks"`

	// Images maps stack names to their service→image references from the last deployment.
	// Used to determine whether docker compose pull is necessary.
	Images map[string]serviceImageByName `yaml:"images,omitempty"`
}

// computePerFileHashes returns a SHA-256 hash for each tracked file in the stack.
// Any change to any of these files triggers a new deployment.
// When varsFile is non-empty, it is included in the hash set so that changes
// to global variables also trigger redeployment.
func computePerFileHashes(workDir string, envFiles []string, watchDirs []string, varsFile string) (stackFileHashes, error) {
	filePaths := append([]string{filepath.Join(workDir, "docker-compose.yml")}, envFiles...)
	if varsFile != "" {
		filePaths = append(filePaths, varsFile)
	}
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

func addFileContentsToHash(hasher io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(hasher, file)
	return err
}

func loadPersistedDeployState() (persistedState, error) {
	data, err := os.ReadFile(deployStateFilePath)
	if os.IsNotExist(err) {
		return newEmptyState(), nil
	}
	if err != nil {
		return persistedState{}, err
	}

	state := persistedState{}
	if err := yaml.Unmarshal(data, &state); err != nil {
		// Old format or corrupt file — start fresh and redeploy everything.
		return newEmptyState(), nil
	}
	if state.Stacks == nil {
		state.Stacks = map[string]stackFileHashes{}
	}
	if state.Images == nil {
		state.Images = map[string]serviceImageByName{}
	}
	return state, nil
}

func saveDeployState(state persistedState) error {
	if err := os.MkdirAll(filepath.Dir(deployStateFilePath), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(deployStateFilePath, data, 0o644)
}
