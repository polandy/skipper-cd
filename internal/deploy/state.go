package deploy

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/fsatomic"
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

	// ProjectDirs maps each stack to its last successful deploy's compose project
	// directory (the working_dir label). Lets orphan detection (ADR-0036)
	// recognize a removed stack's project even when its dir lay outside
	// stacks_base_dir.
	ProjectDirs map[string]string `yaml:"project_dirs,omitempty"`

	// NixOSRebuildInFlight holds the changed nix files of a rebuild that was
	// started but whose outcome was not yet recorded. It is set (and persisted)
	// just before a rebuild whose switch may restart skipper-cd, and cleared once
	// the rebuild's outcome is observed. If skipper is restarted mid-rebuild, the
	// marker survives and the next startup reconciles it into a _nixos success
	// event — the rebuild kept running in its transient unit and applied
	// (ADR-0025). Empty means no rebuild is in flight.
	NixOSRebuildInFlight []string `yaml:"nixos_rebuild_in_flight,omitempty"`
}

func newEmptyState() *persistedState {
	return &persistedState{
		Stacks:      map[string]stackFileHashes{},
		Images:      map[string]serviceImageByName{},
		ProjectDirs: map[string]string{},
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

// revertStack restores a stack's hashes to an earlier snapshot, removing the
// entry entirely when the snapshot is nil (the stack had no recorded state).
// It undoes a recordStack whose deploy then failed, so the stack is retried.
func (s *persistedState) revertStack(stack string, previous stackFileHashes) {
	if previous == nil {
		delete(s.Stacks, stack)
		return
	}
	s.Stacks[stack] = previous
}

// markNixOSRebuildInFlight records that a nixos-rebuild is about to run for the
// given changed files, so a self-restart that interrupts it can be reconciled
// on the next startup.
func (s *persistedState) markNixOSRebuildInFlight(changed []string) {
	s.NixOSRebuildInFlight = changed
}

// clearNixOSRebuildInFlight clears the in-flight marker once the rebuild's
// outcome has been observed (success or genuine failure).
func (s *persistedState) clearNixOSRebuildInFlight() {
	s.NixOSRebuildInFlight = nil
}

// imagesFor returns the service→image map recorded for a stack (nil when unknown).
func (s *persistedState) imagesFor(stack string) serviceImageByName {
	return s.Images[stack]
}

// recordImages stores the service→image map of a successfully deployed stack.
func (s *persistedState) recordImages(stack string, images serviceImageByName) {
	s.Images[stack] = images
}

// recordProjectDir stores the compose project directory of a successfully
// deployed stack, so orphan detection can match its running project by
// working_dir even after the stack is removed from the repo.
func (s *persistedState) recordProjectDir(stack, projectDir string) {
	if s.ProjectDirs == nil {
		s.ProjectDirs = map[string]string{}
	}
	s.ProjectDirs[stack] = projectDir
}

// projectDirs returns a copy of the recorded stack→project-dir map, for
// out-of-run consumers (orphan detection). Never nil.
func (s *persistedState) projectDirs() map[string]string {
	out := make(map[string]string, len(s.ProjectDirs))
	for k, v := range s.ProjectDirs {
		out[k] = v
	}
	return out
}

// trackedFiles returns a copy of the recorded stack→hashed-path map, each
// stack's paths sorted, for out-of-run consumers (the roster's change-detection
// panel). These are exactly the inputs whose hashes decide whether a stack
// redeploys, so the UI can answer "what does skipper watch here" from the same
// source the decision is made from. Never nil.
func (s *persistedState) trackedFiles() map[string][]string {
	out := make(map[string][]string, len(s.Stacks))
	for stack, hashes := range s.Stacks {
		paths := make([]string, 0, len(hashes))
		for p := range hashes {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		out[stack] = paths
	}
	return out
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
	if state.ProjectDirs == nil {
		state.ProjectDirs = map[string]string{}
	}
	return state, nil
}

func saveDeployState(stateDir string, state *persistedState) error {
	if err := os.MkdirAll(stateDir, fsatomic.PrivateDirMode); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return fsatomic.WriteFile(filepath.Join(stateDir, stateFileName), data, fsatomic.PrivateFileMode)
}
