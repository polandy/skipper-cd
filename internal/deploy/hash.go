package deploy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// stateFile is where the deploy state (stack name -> last deployed hash) is persisted.
const stateFile = "/var/lib/orpheus/state.json"

// hashStack computes a SHA-256 hash over docker-compose.yml and all env files
// for a stack. If any of these files change, the hash changes and a new
// deployment is triggered.
func hashStack(workDir string, envFiles []string) (string, error) {
	h := sha256.New()

	composePath := filepath.Join(workDir, "docker-compose.yml")
	if err := hashFile(h, composePath); err != nil {
		return "", fmt.Errorf("hash docker-compose.yml: %w", err)
	}

	for _, envFile := range envFiles {
		if err := hashFile(h, envFile); err != nil {
			return "", fmt.Errorf("hash env file %s: %w", envFile, err)
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// hashFile writes the contents of the file at path into the given writer.
// It is used to feed files into a running SHA-256 hash.
func hashFile(dst io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(dst, f)
	return err
}

// loadState reads the persisted deploy state from disk.
// Returns an empty map if the state file does not exist yet (first run).
func loadState() (map[string]string, error) {
	data, err := os.ReadFile(stateFile)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}

	state := map[string]string{}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

// saveState writes the deploy state to disk so it survives restarts.
func saveState(state map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile, data, 0o644)
}
