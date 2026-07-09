package deploy

import (
	"log/slog"
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

func newEmptyState() *persistedState {
	return &persistedState{
		Stacks: map[string]stackFileHashes{},
		Images: map[string]serviceImageByName{},
	}
}

// hashesFor returns the per-file hashes recorded for a stack (nil when unknown).
func (s *persistedState) hashesFor(stack string) stackFileHashes {
	return s.Stacks[stack]
}

// recordStack stores the per-file hashes of a successfully deployed stack.
func (s *persistedState) recordStack(stack string, hashes stackFileHashes) {
	s.Stacks[stack] = hashes
}

// imagesFor returns the service→image map recorded for a stack (nil when unknown).
func (s *persistedState) imagesFor(stack string) serviceImageByName {
	return s.Images[stack]
}

// recordImages stores the service→image map of a successfully deployed stack.
func (s *persistedState) recordImages(stack string, images serviceImageByName) {
	s.Images[stack] = images
}

func loadPersistedDeployState(stateDir string) (*persistedState, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, stateFileName))
	if os.IsNotExist(err) {
		return newEmptyState(), nil
	}
	if err != nil {
		return nil, err
	}

	state := &persistedState{}
	if err := yaml.Unmarshal(data, state); err != nil {
		// A corrupt state file means all stacks redeploy — by design.
		slog.Warn("state file corrupt, all stacks will redeploy", "err", err)
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

func saveDeployState(stateDir string, state *persistedState) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(stateDir, stateFileName), data, 0o644)
}

// writeFileAtomic writes data via a temp file + rename so that a crash
// mid-write (e.g. nixos-rebuild restarting the service) can never leave a
// truncated state file behind.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
