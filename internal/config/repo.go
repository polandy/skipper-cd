package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/compose"
)

// RepoConfigFileName is the former in-repo per-stack override file. As of
// ADR-0043 it is no longer read (overrides live in the host config); a leftover
// one is rejected (see LoadRepoStacks).
const RepoConfigFileName = "skipper.yaml"

// StackError reports an entry-level failure of a single discovered stack (an
// invalid override, a broken depends_on edge). The stack is excluded from the
// returned set; every other stack deploys normally.
type StackError struct {
	Stack string
	Err   error
}

func (e StackError) Error() string { return fmt.Sprintf("stack %q: %v", e.Stack, e.Err) }

// RepoStacks is the result of stack discovery: the deployable stacks and the
// names parked via disabled: true — excluded from everything skipper does,
// carried only so the UI can show they exist.
type RepoStacks struct {
	Stacks   []Stack
	Disabled []string
}

// LoadRepoStacks discovers the stack set from the deploy-repo clone and applies
// the per-stack overrides from the host config (ADR-0034 discovery + ADR-0043
// single config file): every direct subdirectory of stacksBaseDir containing a
// docker-compose.yml is a stack (name = directory name, alphabetical order).
// overrides are the host config's stacks: entries, matched to discovered stacks
// by name; a discovered stack without a matching entry runs on defaults.
// Relative override paths resolve against stacksBaseDir. projectDirectoryBase,
// when set, derives a discovered stack's project_directory as
// <projectDirectoryBase>/<name> whenever the override does not set its own
// (ADR-0045); empty leaves project_directory unset, same as before
// project_directory_base existed.
//
// The error return is file-level (unreadable base dir, or a leftover in-repo
// skipper.yaml that is no longer read): the caller must not deploy anything.
// StackErrors are entry-level: those stacks are excluded and reported, the
// returned stacks are fine to deploy.
func LoadRepoStacks(stacksBaseDir string, overrides []Stack, projectDirectoryBase string) (RepoStacks, []StackError, error) {
	discovered, err := discoverStackDirs(stacksBaseDir)
	if err != nil {
		return RepoStacks{}, nil, err
	}
	// A leftover in-repo override file is un-migrated config that would otherwise
	// be silently ignored (ADR-0043) — reject it loudly.
	repoFile := filepath.Join(stacksBaseDir, RepoConfigFileName)
	if _, err := os.Stat(repoFile); err == nil {
		return RepoStacks{}, nil, fmt.Errorf("%s is no longer read (ADR-0043): move its per-stack overrides into the host config's stacks: list and delete the file", repoFile)
	}

	known := make(map[string]bool, len(discovered))
	for _, name := range discovered {
		known[name] = true
	}

	overrideByName := make(map[string]Stack, len(overrides))
	for _, s := range overrides {
		overrideByName[s.Name] = s
	}

	var stackErrs []StackError
	fail := func(name string, format string, args ...any) {
		stackErrs = append(stackErrs, StackError{Stack: name, Err: fmt.Errorf(format, args...)})
	}

	// An override for a stack that does not exist is a rename or misspelling that
	// would otherwise silently strip a stack of its config — fail loudly.
	for _, s := range overrides {
		if !known[s.Name] {
			fail(s.Name, "no stack directory %s/%s with a docker-compose.yml", stacksBaseDir, s.Name)
		}
	}

	var stacks []Stack
	var disabled []string
	for _, name := range discovered {
		ov := overrideByName[name]
		if ov.Disabled {
			disabled = append(disabled, name)
			continue
		}
		envFiles, envErr := resolveRepoPaths(stacksBaseDir, ov.EnvFiles)
		watchDirs, watchErr := resolveRepoPaths(stacksBaseDir, ov.WatchDirs)
		stack := Stack{
			Name:               name,
			ProjectDirectory:   ov.ProjectDirectory,
			EnvFiles:           envFiles,
			WatchDirs:          watchDirs,
			OnDemandContainers: ov.OnDemandContainers,
			Icon:               ov.Icon,
			Autosync:           ov.Autosync,
			DeployHealthCheck:  ov.DeployHealthCheck,
			SelfHeal:           ov.SelfHeal,
			DependsOn:          ov.DependsOn,
			Hooks:              ov.Hooks,
			Rollout:            ov.Rollout,
		}
		if stack.ProjectDirectory == "" && projectDirectoryBase != "" {
			stack.ProjectDirectory = filepath.Join(projectDirectoryBase, name)
		}
		if hc := stack.DeployHealthCheck; hc != nil && !hc.IsDisabled() && hc.TimeoutSeconds == 0 {
			hc.TimeoutSeconds = DefaultHealthCheckTimeoutSeconds
		}

		hcErr := validateHealthCheck(stack.DeployHealthCheck)
		hookErr := validateHooks(stack.Hooks)
		rolloutErr := validateRollout(stack.Rollout)
		// Parse the compose every sync so a broken compose or an unrollable
		// rollout service is caught at discovery — rollout is not hash-tracked,
		// so an edit alone would not otherwise trigger a redeploy that reveals it.
		cf, composeErr := compose.Parse(filepath.Join(stacksBaseDir, name, compose.FileName))
		if rolloutErr == nil && composeErr == nil && stack.Rollout != nil {
			rolloutErr = ValidateRolloutServices(stack.Rollout.Services, cf)
		}
		depErr := invalidDependency(stack, known)
		switch {
		case strings.HasPrefix(name, "_"):
			fail(name, "stack names starting with _ are reserved")
		case envErr != nil:
			fail(name, "env_files: %v", envErr)
		case watchErr != nil:
			fail(name, "watch_dirs: %v", watchErr)
		case hcErr != nil:
			fail(name, "deploy_health_check: %v", hcErr)
		case hookErr != nil:
			fail(name, "hooks: %v", hookErr)
		case composeErr != nil:
			fail(name, "%v", composeErr)
		case rolloutErr != nil:
			fail(name, "rollout: %v", rolloutErr)
		case depErr != nil:
			fail(name, "%v", depErr)
		default:
			stacks = append(stacks, stack)
		}
	}

	stacks, cycleErrs := dropDependencyCycles(stacks)
	stackErrs = append(stackErrs, cycleErrs...)

	for i := range stacks {
		stacks[i].ConfigHash = stackConfigHash(stacks[i])
	}

	sort.Slice(stackErrs, func(i, j int) bool { return stackErrs[i].Stack < stackErrs[j].Stack })
	return RepoStacks{Stacks: stacks, Disabled: disabled}, stackErrs, nil
}

// discoverStackDirs returns the names of the direct subdirectories of
// stacksBaseDir that contain a docker-compose.yml, in alphabetical order
// (which seeds the deploy order among unconstrained stacks). Hidden
// directories are skipped; nested directories are not scanned.
func discoverStackDirs(stacksBaseDir string) ([]string, error) {
	entries, err := os.ReadDir(stacksBaseDir)
	if err != nil {
		return nil, fmt.Errorf("read stacks_base_dir: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(stacksBaseDir, entry.Name(), compose.FileName)); err != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// resolveRepoPaths resolves relative paths against the repo clone root, so
// repo-config entries like stacks/web/secrets.env point into the clone.
// Absolute paths stay as-is — a documented escape hatch for host-level
// secrets outside the repo (e.g. a sops-decrypted file), same as host-config
// mode. A relative path is rejected if it escapes stacksBaseDir via "../":
// unlike an absolute path, that's not a documented capability, just an
// unintentional traversal a repo push could otherwise exploit.
//
// A relative (in-repo) path must also exist — a missing one is a typo, caught
// here rather than at the deploy. Absolute paths are not stat-ed (they may be
// produced out-of-band on the host — the secrets escape hatch).
func resolveRepoPaths(stacksBaseDir string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		if filepath.IsAbs(p) {
			out[i] = p
			continue
		}
		resolved := filepath.Join(stacksBaseDir, p)
		rel, err := filepath.Rel(stacksBaseDir, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("path %q escapes stacks_base_dir", p)
		}
		if _, err := os.Stat(resolved); err != nil {
			return nil, fmt.Errorf("path %q does not exist in the repo", p)
		}
		out[i] = resolved
	}
	return out, nil
}

// invalidDependency reports the first broken depends_on edge of a stack: a
// self-reference or a name with no stack directory. A reference to a disabled
// stack is fine — the dependency is hands-off, the runtime gate treats it as
// satisfied.
func invalidDependency(stack Stack, known map[string]bool) error {
	for _, dep := range stack.DependsOn {
		if dep == stack.Name {
			return fmt.Errorf("depends_on must not reference the stack itself")
		}
		if !known[dep] {
			return fmt.Errorf("depends_on references unknown stack %q", dep)
		}
	}
	return nil
}

// dropDependencyCycles removes the members of depends_on cycles from the stack
// set, reporting each as a StackError. Edges to stacks outside the set
// (disabled or already errored) count as satisfied — the runtime gate handles
// those. Kahn's algorithm, mirroring validateStackDependencies.
func dropDependencyCycles(stacks []Stack) ([]Stack, []StackError) {
	inSet := make(map[string]bool, len(stacks))
	for _, s := range stacks {
		inSet[s.Name] = true
	}

	resolved := make(map[string]bool, len(stacks))
	for remaining := len(stacks); remaining > 0; {
		progressed := false
		for _, s := range stacks {
			if resolved[s.Name] {
				continue
			}
			allResolved := true
			for _, dep := range s.DependsOn {
				if inSet[dep] && !resolved[dep] {
					allResolved = false
					break
				}
			}
			if allResolved {
				resolved[s.Name] = true
				remaining--
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}

	var kept []Stack
	var errs []StackError
	var stuck []string
	for _, s := range stacks {
		if !resolved[s.Name] {
			stuck = append(stuck, s.Name)
		}
	}
	for _, s := range stacks {
		if resolved[s.Name] {
			kept = append(kept, s)
			continue
		}
		errs = append(errs, StackError{Stack: s.Name, Err: fmt.Errorf("depends_on cycle involving stacks: %s", strings.Join(stuck, ", "))})
	}
	return kept, errs
}

// stackDeployInputs are the fields of a stack's effective config that shape
// what a deploy produces; they feed the stack's ConfigHash so a config edit
// redeploys exactly the affected stack. Display-only (icon), runtime-only
// (self_heal), ordering-only (depends_on), side-effect (hooks), and
// deploy-mechanism (rollout) fields are deliberately excluded — editing them
// must never redeploy.
type stackDeployInputs struct {
	ProjectDirectory   string       `yaml:"project_directory"`
	EnvFiles           []string     `yaml:"env_files"`
	WatchDirs          []string     `yaml:"watch_dirs"`
	OnDemandContainers []string     `yaml:"on_demand_containers"`
	DeployHealthCheck  *HealthCheck `yaml:"deploy_health_check"`
}

// stackConfigHash returns the SHA-256 over the stack's deploy-shaping config,
// canonically marshaled (struct field order is fixed, so the hash is stable).
func stackConfigHash(s Stack) string {
	data, err := yaml.Marshal(stackDeployInputs{
		ProjectDirectory:   s.ProjectDirectory,
		EnvFiles:           s.EnvFiles,
		WatchDirs:          s.WatchDirs,
		OnDemandContainers: s.OnDemandContainers,
		DeployHealthCheck:  s.DeployHealthCheck,
	})
	if err != nil {
		// Marshaling a plain struct cannot fail; guard anyway.
		return fmt.Sprintf("marshal-error:%v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
