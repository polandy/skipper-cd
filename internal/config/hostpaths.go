package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Host paths outside the clone: the global vars_file and the checkout holding
// the stacks' project directories.

// validateHostPaths checks vars_file and project_directory_base/_sync.
func (c *Config) validateHostPaths() error {
	// vars_file is a host path, available before any clone — a typo is caught
	// here rather than aborting every deploy from the first sync.
	if c.VarsFile != "" {
		f, err := os.Open(c.VarsFile)
		if err != nil {
			return fmt.Errorf("vars_file: %w — check the path and that skipper-cd can read it, or remove vars_file if it's no longer needed", err)
		}
		f.Close()
	}
	// A relative base would resolve against skipper's own process cwd, not the
	// repo clone — silently wrong --project-directory.
	if c.ProjectDirectoryBase != "" && !filepath.IsAbs(c.ProjectDirectoryBase) {
		return fmt.Errorf("project_directory_base %q must be an absolute path (start it with \"/\")", c.ProjectDirectoryBase)
	}
	// Without a base there is nothing to fast-forward (ADR-0060); ignoring the
	// key would leave the operator believing stale mounts are kept current.
	if c.ProjectDirectorySync && c.ProjectDirectoryBase == "" {
		return fmt.Errorf("project_directory_sync: true needs project_directory_base — set it to the checkout holding the stacks' project directories, or drop project_directory_sync")
	}
	return nil
}
