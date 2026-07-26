package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/polandy/skipper-cd/internal/config"
)

// Exit codes returned by validateConfigFile, so `skipper -validate` can gate a
// deploy pipeline or a pre-commit hook.
const (
	validateOK      = 0
	validateInvalid = 1
)

// report collects the lines of a validation run so the whole report is emitted
// in a single write at the end — the check has several early exits, and a
// half-written report on a failing writer would be worse than none.
type report struct {
	lines []string
}

func (r *report) linef(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func (r *report) String() string {
	if len(r.lines) == 0 {
		return ""
	}
	return strings.Join(r.lines, "\n") + "\n"
}

// validateConfigFile loads the config at path, reports what skipper would run
// with, and returns the process exit code — without starting any server,
// touching the repo clone, or deploying anything.
//
// When the deploy-repo clone is already on disk it also resolves the stack set
// the way a sync would (stack discovery plus the host overrides), so a broken
// compose file or an invalid per-stack override is caught here rather than on
// the next push. A clone that does not exist yet is not an error: discovery
// runs on the first sync, and validating the host config before the first
// clone is a legitimate thing to do.
func validateConfigFile(path string, out io.Writer) int {
	var r report
	code := checkConfigFile(path, &r)
	if _, err := io.WriteString(out, r.String()); err != nil {
		// The report is the entire output of this mode; if it cannot be
		// written, reporting success would tell the operator nothing.
		return validateInvalid
	}
	return code
}

// checkConfigFile performs the validation, appending its findings to r.
func checkConfigFile(path string, r *report) int {
	cfg, err := config.Load(path)
	if err != nil {
		r.linef("config invalid: %v", err)
		return validateInvalid
	}

	for _, w := range cfg.Warnings {
		r.linef("warning: %s", w)
	}

	r.linef("config OK: %s", path)
	r.linef("  repo:            %s (branch %s)", cfg.RepoURL, cfg.Branch)
	r.linef("  stacks_base_dir: %s", cfg.StacksBaseDir)
	r.linef("  stack_discovery: %t", cfg.StackDiscovery)

	if !cfg.StackDiscovery {
		r.linef("  stacks:          %d listed in the host config", len(cfg.Stacks))
		return validateOK
	}

	r.linef("  overrides:       %d stack entries in the host config", len(cfg.Stacks))
	if _, err := os.Stat(cfg.StacksBaseDir); err != nil {
		r.linef("  stacks:          not checked — no repo clone at %s yet; discovery runs on the first sync", cfg.StacksBaseDir)
		return validateOK
	}

	repoStacks, stackErrs, err := config.LoadRepoStacks(cfg.StacksBaseDir, cfg.Stacks, cfg.ProjectDirectoryBase)
	if err != nil {
		r.linef("stack discovery failed: %v", err)
		return validateInvalid
	}
	discovered := fmt.Sprintf("  stacks:          %d discovered", len(repoStacks.Stacks))
	if len(repoStacks.Disabled) > 0 {
		discovered += fmt.Sprintf(", %d disabled", len(repoStacks.Disabled))
	}
	r.linef("%s", discovered)
	for _, s := range repoStacks.Stacks {
		r.linef("    - %s", s.Name)
	}

	if len(stackErrs) > 0 {
		// Entry-level errors fail only their own stack at runtime (and block
		// its dependents), but for a pre-flight check they are the whole
		// point: report them and exit non-zero so a pipeline stops here.
		for _, se := range stackErrs {
			r.linef("stack invalid: %v", se)
		}
		return validateInvalid
	}
	return validateOK
}
