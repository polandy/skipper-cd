package deploy

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const stateFileName = "state.yaml"

// stackFileHashes maps each tracked file path to its SHA-256 hash.
type stackFileHashes map[string]string

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

func newEmptyState() persistedState {
	return persistedState{
		Stacks: map[string]stackFileHashes{},
		Images: map[string]serviceImageByName{},
	}
}

func loadPersistedDeployState(stateDir string) (persistedState, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, stateFileName))
	if os.IsNotExist(err) {
		return newEmptyState(), nil
	}
	if err != nil {
		return persistedState{}, err
	}

	state := persistedState{}
	if err := yaml.Unmarshal(data, &state); err != nil {
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

func saveDeployState(stateDir string, state persistedState) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, stateFileName), data, 0o644)
}
