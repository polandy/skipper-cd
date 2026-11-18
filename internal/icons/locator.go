package icons

import (
	"path/filepath"

	"github.com/polandy/skipper-cd/internal/config"
)

// NewStackLocator maps a stack name to its icon-resolution inputs from the
// effective stack set. The icon file lives in the stack's directory in the
// clone (stacks_base_dir/<name>), the same directory change detection reads
// from.
func NewStackLocator(cfg *config.Config, stacks func() []config.Stack) StackLocator {
	return func(name string) (Request, bool) {
		// The reserved NixOS pseudo-stack has no directory in the clone and is
		// not in the stack set; resolve its icon by auto-matching the "nixos"
		// slug so it gets a recognizable logo instead of the "_" monogram
		// fallback.
		if name == config.ReservedStackName {
			return Request{Name: "nixos"}, true
		}
		// The reserved stack-config pseudo-stack (ADR-0034) likewise has no
		// directory; its failures are about the repo skipper.yaml, so the git
		// logo is the recognizable stand-in — as they are for the
		// project_directory checkout's fast-forward (ADR-0060).
		if name == config.ReservedConfigStackName || name == config.ReservedProjectDirStackName {
			return Request{Name: "git"}, true
		}
		for _, s := range stacks() {
			if s.Name == name {
				return Request{
					Name: s.Name,
					Slug: s.Icon,
					Dir:  filepath.Join(cfg.StacksBaseDir, s.Name),
				}, true
			}
		}
		return Request{}, false
	}
}
