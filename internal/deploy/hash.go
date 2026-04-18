package deploy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const stateFile = "/var/lib/orpheus/state.json"

func hashStack(workingDir string, envFiles []string) (string, error) {
	h := sha256.New()

	composePath := filepath.Join(workingDir, "docker-compose.yml")
	if err := hashFile(h, composePath); err != nil {
		return "", fmt.Errorf("hash compose file: %w", err)
	}

	for _, ef := range envFiles {
		if err := hashFile(h, ef); err != nil {
			return "", fmt.Errorf("hash env file %s: %w", ef, err)
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func hashFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

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
