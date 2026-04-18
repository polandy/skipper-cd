package deploy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const deployStateFilePath = "/var/lib/orpheus/state.json"

// computeStackConfigHash returns a SHA-256 hash of docker-compose.yml and all
// env files. Any change to these files produces a different hash, triggering
// a new deployment.
func computeStackConfigHash(workDir string, envFiles []string) (string, error) {
	h := sha256.New()

	if err := writeFileToHash(h, filepath.Join(workDir, "docker-compose.yml")); err != nil {
		return "", fmt.Errorf("hash docker-compose.yml: %w", err)
	}

	for _, envFile := range envFiles {
		if err := writeFileToHash(h, envFile); err != nil {
			return "", fmt.Errorf("hash env file %s: %w", envFile, err)
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func writeFileToHash(dst io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(dst, f)
	return err
}

func loadPersistedDeployState() (map[string]string, error) {
	data, err := os.ReadFile(deployStateFilePath)
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

func persistDeployState(state map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(deployStateFilePath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(deployStateFilePath, data, 0o644)
}
