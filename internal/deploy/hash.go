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

// deployState maps stack names to their per-file hashes.
type deployState map[string]stackFileHashes

// computePerFileHashes returns a SHA-256 hash for each tracked file in the stack.
// Any change to any of these files triggers a new deployment.
func computePerFileHashes(workDir string, envFiles []string) (stackFileHashes, error) {
	filePaths := append([]string{filepath.Join(workDir, "docker-compose.yml")}, envFiles...)
	hashes := make(stackFileHashes, len(filePaths))

	for _, path := range filePaths {
		hasher := sha256.New()
		if err := addFileContentsToHash(hasher, path); err != nil {
			return nil, fmt.Errorf("hash file %s: %w", path, err)
		}
		hashes[path] = fmt.Sprintf("%x", hasher.Sum(nil))
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

func loadPersistedDeployState() (deployState, error) {
	data, err := os.ReadFile(deployStateFilePath)
	if os.IsNotExist(err) {
		return deployState{}, nil
	}
	if err != nil {
		return nil, err
	}

	state := deployState{}
	if err := yaml.Unmarshal(data, &state); err != nil {
		// Old format (JSON) or corrupt file — start fresh and redeploy everything.
		return deployState{}, nil
	}
	return state, nil
}

func persistDeployState(state deployState) error {
	if err := os.MkdirAll(filepath.Dir(deployStateFilePath), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(deployStateFilePath, data, 0o644)
}
