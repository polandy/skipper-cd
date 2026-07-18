package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RepoConfigFileName is the optional per-stack override file at the root of
// the deploy repo in stack-discovery mode (ADR-0034).
const RepoConfigFileName = "skipper.yaml"

// StackError reports an entry-level failure of a single discovered stack (an
// invalid override, a broken depends_on edge). The stack is excluded from the
// returned set; every other stack deploys normally.
type StackError struct {
	Stack string
	Err   error
}

func (e StackError) Error() string { return fmt.Sprintf("stack %q: %v", e.Stack, e.Err) }

// repoStackOverride is one stack's entry in the repo-root skipper.yaml. Every
// field is optional — a discovered stack without an entry runs on defaults.
// It mirrors the per-stack fields of the host config; autosync is deliberately
// absent for now (the autosync controller's config baseline is fixed at
// startup; global autosync and UI overrides work as usual).
type repoStackOverride struct {
	WorkingDir         string       `yaml:"working_dir"`
	EnvFiles           []string     `yaml:"env_files"`
	WatchDirs          []string     `yaml:"watch_dirs"`
	OnDemandContainers []string     `yaml:"on_demand_containers"`
	Icon               string       `yaml:"icon"`
	HealthCheck        *HealthCheck `yaml:"health_check"`
	SelfHeal           *bool        `yaml:"self_heal"`
	DependsOn          []string     `yaml:"depends_on"`

	// Disabled excludes the stack entirely: not deployed, not health-polled.
	// A running stack that becomes disabled keeps running — skipper hands it
	// off, it does not tear it down.
	Disabled bool `yaml:"disabled"`
}

// repoConfig is the shape of the repo-root skipper.yaml.
type repoConfig struct {
	Stacks map[string]repoStackOverride `yaml:"stacks"`
}

// RepoStacks is the result of stack discovery: the deployable stacks and the
// names parked via disabled: true — excluded from everything skipper does,
// carried only so the UI can show they exist.
type RepoStacks struct {
	Stacks   []Stack
	Disabled []string
}

// LoadRepoStacks discovers the stack set from the deploy-repo clone
// (ADR-0034): every direct subdirectory of stacksBaseDir containing a
// docker-compose.yml is a stack (name = directory name, alphabetical order),
// with optional per-stack overrides from <repoDir>/skipper.yaml.
//
// The error return is file-level (unreadable base dir, unparseable or
// unknown-field skipper.yaml): nothing can be trusted, the caller must not
// deploy anything. StackErrors are entry-level: those stacks are excluded and
// reported, the returned stacks are fine to deploy.
func LoadRepoStacks(repoDir, stacksBaseDir string) (RepoStacks, []StackError, error) {
	discovered, err := discoverStackDirs(stacksBaseDir)
	if err != nil {
		return RepoStacks{}, nil, err
	}
	ovf, err := loadRepoOverrides(filepath.Join(repoDir, RepoConfigFileName))
	if err != nil {
		return RepoStacks{}, nil, err
	}

	known := make(map[string]bool, len(discovered))
	for _, name := range discovered {
		known[name] = true
	}

	// failAt records an entry-level error, appending the marked skipper.yaml
	// excerpt of the stack's entry (or one of its fields) when the location is
	// known — so the failed row shows the offending config, not just its name.
	var stackErrs []StackError
	failAt := func(name, field string, format string, args ...any) {
		stackErrs = append(stackErrs, StackError{Stack: name, Err: ovf.withSnippet(fmt.Errorf(format, args...), name, field)})
	}

	// A typo'd entry (no matching stack directory) must fail loudly — it is
	// most likely a rename or misspelling that would otherwise silently strip
	// a stack of its config.
	for name := range ovf.stacks {
		if !known[name] {
			failAt(name, "", "no stack directory %s/%s with a docker-compose.yml", stacksBaseDir, name)
		}
	}

	var stacks []Stack
	var disabled []string
	for _, name := range discovered {
		ov := ovf.stacks[name]
		if ov.Disabled {
			disabled = append(disabled, name)
			continue
		}
		stack := Stack{
			Name:               name,
			WorkingDir:         ov.WorkingDir,
			EnvFiles:           resolveRepoPaths(repoDir, ov.EnvFiles),
			WatchDirs:          resolveRepoPaths(repoDir, ov.WatchDirs),
			OnDemandContainers: ov.OnDemandContainers,
			Icon:               ov.Icon,
			HealthCheck:        ov.HealthCheck,
			SelfHeal:           ov.SelfHeal,
			DependsOn:          ov.DependsOn,
		}
		if hc := stack.HealthCheck; hc != nil && hc.TimeoutSeconds == 0 {
			hc.TimeoutSeconds = defaultHealthCheckTimeoutSeconds
		}

		hcErr := validateHealthCheck(stack.HealthCheck)
		depErr := invalidDependency(stack, known)
		switch {
		case strings.HasPrefix(name, "_"):
			failAt(name, "", "stack names starting with _ are reserved")
		case hcErr != nil:
			failAt(name, "health_check", "health_check: %v", hcErr)
		case depErr != nil:
			failAt(name, "depends_on", "%v", depErr)
		default:
			stacks = append(stacks, stack)
		}
	}

	stacks, cycleErrs := dropDependencyCycles(stacks)
	for i := range cycleErrs {
		cycleErrs[i].Err = ovf.withSnippet(cycleErrs[i].Err, cycleErrs[i].Stack, "depends_on")
	}
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
		if _, err := os.Stat(filepath.Join(stacksBaseDir, entry.Name(), "docker-compose.yml")); err != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// repoOverridesFile is the parsed repo-root skipper.yaml plus the raw source
// and a stack/field → line index, kept so error messages can show the marked
// excerpt of the offending entry. The zero value (missing file) yields no
// overrides and no snippets.
type repoOverridesFile struct {
	stacks map[string]repoStackOverride
	src    []byte
	lines  map[string]map[string]int
}

// withSnippet appends the marked skipper.yaml excerpt for the stack entry (or
// one of its fields, falling back to the entry) to err; err is returned
// unchanged when no location is known.
func (f repoOverridesFile) withSnippet(err error, stack, field string) error {
	fields := f.lines[stack]
	if fields == nil {
		return err
	}
	line := fields[field]
	if line == 0 {
		line = fields[""]
	}
	if snip := yamlSnippet(f.src, line); snip != "" {
		return fmt.Errorf("%w\n\n%s", err, snip)
	}
	return err
}

// loadRepoOverrides parses the optional repo-root skipper.yaml. A missing file
// means no overrides. Decoding is strict: an unknown field is a file-level
// error, so a misspelled field fails loudly instead of silently deploying
// without it. Parse errors carry the marked excerpt of the failing line.
func loadRepoOverrides(path string) (repoOverridesFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return repoOverridesFile{}, nil
	}
	if err != nil {
		return repoOverridesFile{}, fmt.Errorf("read %s: %w", RepoConfigFileName, err)
	}

	var rc repoConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&rc); err != nil && !errors.Is(err, io.EOF) {
		parseErr := fmt.Errorf("parse %s: %w", RepoConfigFileName, err)
		if snip := yamlSnippet(data, yamlErrorLine(err)); snip != "" {
			parseErr = fmt.Errorf("%w\n\n%s", parseErr, snip)
		}
		return repoOverridesFile{}, parseErr
	}
	return repoOverridesFile{stacks: rc.Stacks, src: data, lines: indexOverrideLines(data)}, nil
}

// resolveRepoPaths resolves relative paths against the repo clone root, so
// repo-config entries like stacks/web/secrets.env point into the clone.
// Absolute paths stay as-is.
func resolveRepoPaths(repoDir string, paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		if filepath.IsAbs(p) {
			out[i] = p
		} else {
			out[i] = filepath.Join(repoDir, p)
		}
	}
	return out
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
// (self_heal), and ordering-only (depends_on) fields are deliberately
// excluded — editing them must never redeploy.
type stackDeployInputs struct {
	WorkingDir         string       `yaml:"working_dir"`
	EnvFiles           []string     `yaml:"env_files"`
	WatchDirs          []string     `yaml:"watch_dirs"`
	OnDemandContainers []string     `yaml:"on_demand_containers"`
	HealthCheck        *HealthCheck `yaml:"health_check"`
}

// stackConfigHash returns the SHA-256 over the stack's deploy-shaping config,
// canonically marshaled (struct field order is fixed, so the hash is stable).
func stackConfigHash(s Stack) string {
	data, err := yaml.Marshal(stackDeployInputs{
		WorkingDir:         s.WorkingDir,
		EnvFiles:           s.EnvFiles,
		WatchDirs:          s.WatchDirs,
		OnDemandContainers: s.OnDemandContainers,
		HealthCheck:        s.HealthCheck,
	})
	if err != nil {
		// Marshaling a plain struct cannot fail; guard anyway.
		return fmt.Sprintf("marshal-error:%v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
