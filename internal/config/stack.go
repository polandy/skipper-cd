package config

import (
	"fmt"
	"strings"

	"github.com/polandy/skipper-cd/internal/compose"
)

// The per-stack shape and its validation: what one stack declares, and the
// rules that make a declaration usable. Stack *behaviour* lives in
// internal/deploy; this file is only the contract the config expresses.

// Stack represents a single Docker Compose project to be deployed.
type Stack struct {
	// Name is a unique identifier for the stack. When project_directory is
	// omitted, the compose file is still read from stacks_base_dir/<name>
	// (Invariant 1).
	Name string `yaml:"name"`

	// ProjectDirectory is an optional absolute path passed as
	// --project-directory to docker compose. It controls Docker Compose
	// project identity (container labels) and .env file loading. Change
	// detection and the compose file always come from stacks_base_dir/<name>
	// — never conflate the two. When empty, it defaults to
	// project_directory_base/<name> if the config sets project_directory_base.
	ProjectDirectory string `yaml:"project_directory"`

	// EnvFiles lists KEY=VALUE files injected into the environment when docker-compose
	// is invoked, enabling ${VAR} substitution inside docker-compose.yml. Under
	// stack discovery, a relative entry resolves against stacks_base_dir
	// (LoadRepoStacks); in host-list mode (stack_discovery: false) every entry
	// must be absolute — checked at Load, since there is no repo-relative base
	// to resolve against.
	EnvFiles []string `yaml:"env_files"`

	// WatchDirs lists additional directories whose contents are hashed alongside
	// docker-compose.yml. Any change inside these directories triggers a
	// redeployment. Same relative/absolute rule as EnvFiles above.
	WatchDirs []string `yaml:"watch_dirs"`

	// OnDemandContainers lists container names to stop after a successful deployment.
	// Use this for containers managed by an on-demand scheduler:
	// the scheduler will start them again on the next incoming request.
	OnDemandContainers []string `yaml:"on_demand_containers,omitempty"`

	// Icon optionally overrides the icon-set slug used for this stack's UI
	// icon (e.g. "jellyfin" for a stack named "media"). When empty, the icon
	// is auto-matched from the stack name. A repo icon.svg/icon.png in the
	// stack directory takes precedence over both. Purely visual, never hashed.
	Icon string `yaml:"icon,omitempty"`

	// Autosync overrides the global autosync for this stack. nil means inherit
	// the global setting. When autosync is not effective, a detected change is
	// queued instead of deployed. See docs/autosync.md.
	Autosync *bool `yaml:"autosync"`

	// DeployHealthCheck optionally gates a deploy of this stack on a post-deploy
	// health check; on failure the deploy is rolled back. nil disables the
	// gate. See ADR-0022.
	DeployHealthCheck *HealthCheck `yaml:"deploy_health_check,omitempty"`

	// SelfHeal overrides the global self_heal for this stack. nil means inherit
	// the global setting. When effective, a stack found degraded by the health
	// poller is automatically restored to its deployed running state by a
	// corrective redeploy. See ADR-0029.
	SelfHeal *bool `yaml:"self_heal"`

	// Rollback overrides the global rollback for this stack. nil means inherit
	// the global setting (which defaults to on). When off, a failed deploy is
	// not restored to the previous compose version: it is marked failed, the
	// failed containers are left running for inspection, and the change stays
	// pending. Use it for stateful stacks whose forward migrations make
	// restoring the old image over migrated data unsafe. See ADR-0050. Never
	// hashed — a runtime failure policy, so toggling it does not itself redeploy.
	Rollback *bool `yaml:"rollback"`

	// UpdateCheck overrides the global registry update check for this stack.
	// nil means inherit (on whenever the check itself is enabled); false opts
	// the stack out — for a stack whose floating tag is meant to lag, or whose
	// registry is unreachable. See ADR-0054. Never hashed — a display policy,
	// so toggling it does not itself redeploy.
	UpdateCheck *bool `yaml:"update_check"`

	// DependsOn lists stacks that must deploy before this one. Within a run,
	// a failed dependency blocks this stack (it stays dirty and retries on the
	// next sync) and a queued dependency queues it. Entries must name other
	// configured stacks and the graph must be acyclic. See ADR-0032.
	DependsOn []string `yaml:"depends_on,omitempty"`

	// Hooks optionally runs shell commands around this stack's deploy: a backup
	// before it updates (pre_deploy) and a smoke test after (post_deploy). See
	// ADR-0038. Purely a deploy-time side effect — never hashed, so editing a
	// hook does not itself redeploy.
	Hooks Hooks `yaml:"hooks,omitempty"`

	// Rollout optionally deploys named services with a zero-downtime cutover
	// instead of an in-place recreate (ADR-0040); nil disables it. Never hashed —
	// toggling it does not itself redeploy.
	Rollout *Rollout `yaml:"rollout,omitempty"`

	// Disabled excludes a discovered stack entirely (stack-discovery mode): not
	// deployed, not health-polled. A running stack that becomes disabled keeps
	// running — skipper hands it off, it does not tear it down. Ignored when the
	// stacks are listed explicitly (stack_discovery: false), where the list is
	// the membership.
	Disabled bool `yaml:"disabled,omitempty"`

	// ConfigHash is the hash of the stack's deploy-shaping config, set only by
	// LoadRepoStacks in stack-discovery mode (ADR-0034). It participates in
	// change detection so a per-stack config edit redeploys exactly the affected
	// stack. Empty when the stacks are listed explicitly. Never read from YAML.
	ConfigHash string `yaml:"-"`
}

// Hooks configures optional shell commands run around a stack's deploy
// (ADR-0038). Both lists are optional; the zero value runs nothing. Each entry
// is one `sh -c` command line, run sequentially in list order.
type Hooks struct {
	// PreDeploy runs before any container is touched — the point at which the
	// stack's previous version is still running, so a backup command can dump
	// it. A failing pre_deploy hook aborts the deploy before pull/up, with no
	// rollback (nothing changed yet).
	PreDeploy []string `yaml:"pre_deploy"`

	// PostDeploy runs after a successful up and health gate, before on-demand
	// containers are stopped. A failing post_deploy hook triggers the same
	// rollback path as a health-check failure (ADR-0022, ADR-0038), even when no
	// deploy_health_check is configured.
	PostDeploy []string `yaml:"post_deploy"`

	// TimeoutSeconds bounds each individual hook command. 0 (the default) leaves
	// each hook bounded only by the global command_timeout_seconds, which is
	// also the hard ceiling: a larger value here cannot exceed it (raise
	// command_timeout_seconds for a backup slower than that) — a value that
	// does exceed it logs a startup warning, since it would otherwise silently
	// never have any effect.
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

// Rollout configures zero-downtime deployment for a stack's services (ADR-0040).
// Only the listed services roll (needs a reverse proxy in front; only Traefik is
// tested); every other service recreates in place. Per-service eligibility is
// verified against the compose file at deploy time.
type Rollout struct {
	// Services is the allowlist of compose service names to roll (required, non-empty).
	Services []string `yaml:"services"`

	// HealthTimeoutSeconds is the canary health-wait deadline. 0 (the default)
	// falls back to the stack's deploy_health_check timeout, else 60.
	HealthTimeoutSeconds int `yaml:"health_timeout_seconds"`

	// DrainSeconds holds the old container this long after the canary is healthy,
	// so the proxy can switch over before it is removed. 0 (default) drains at once.
	DrainSeconds int `yaml:"drain_seconds"`
}

// validateStackDependencies checks every depends_on edge (ADR-0032): entries
// must name other configured stacks (never the stack itself or the reserved
// _nixos key, which is implicitly first for everyone) and the resulting graph
// must be acyclic. A broken graph is a config bug, caught at load time.
func validateStackDependencies(stacks []Stack) error {
	names := make(map[string]struct{}, len(stacks))
	for _, s := range stacks {
		names[s.Name] = struct{}{}
	}
	for _, s := range stacks {
		for _, dep := range s.DependsOn {
			if dep == s.Name {
				return fmt.Errorf("stack %q: depends_on must not reference the stack itself", s.Name)
			}
			if _, ok := names[dep]; !ok {
				return fmt.Errorf("stack %q: depends_on references unknown stack %q", s.Name, dep)
			}
		}
	}

	if stuck := cyclicStacks(stacks); len(stuck) > 0 {
		return cycleError(stuck)
	}
	return nil
}

// cyclicStacks returns the names that depends_on cannot order, in config
// order: Kahn's algorithm peels every stack whose dependencies are resolved,
// and whatever is left is part of — or downstream of — a cycle. A dependency
// outside the given set is ignored, so a stack the caller already rejected on
// its own does not make every dependent look cyclic.
func cyclicStacks(stacks []Stack) []string {
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

	var stuck []string
	for _, s := range stacks {
		if !resolved[s.Name] {
			stuck = append(stuck, s.Name)
		}
	}
	return stuck
}

// cycleError is the message both modes report for a depends_on cycle: the
// host config rejects the whole file, stack discovery fails the stacks named.
func cycleError(stuck []string) error {
	return fmt.Errorf("depends_on cycle involving stacks: %s", strings.Join(stuck, ", "))
}

// validateHooks checks a stack's optional hooks section: no blank command
// lines (a whitespace-only entry is almost certainly a mistake) and a
// non-negative timeout.
func validateHooks(h Hooks) error {
	if h.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must not be negative, got %d", h.TimeoutSeconds)
	}
	for _, phase := range []struct {
		name string
		cmds []string
	}{{"pre_deploy", h.PreDeploy}, {"post_deploy", h.PostDeploy}} {
		for i, cmd := range phase.cmds {
			if strings.TrimSpace(cmd) == "" {
				return fmt.Errorf("%s[%d] is empty", phase.name, i)
			}
		}
	}
	return nil
}

// ValidateRolloutServices checks that every rolled service is present in the
// compose file and can be cut over: no published host ports, no container_name,
// and a healthcheck (ADR-0040). It is the single source of the rollout
// eligibility rules — called at discovery (LoadRepoStacks) and at deploy time
// (internal/deploy), the latter being the only check in host-config mode.
func ValidateRolloutServices(services []string, cf *compose.File) error {
	for _, name := range services {
		svc, ok := cf.Services[name]
		if !ok {
			return fmt.Errorf("service %q is not defined in %s", name, compose.FileName)
		}
		if svc.PublishesPorts() {
			return fmt.Errorf("service %q publishes host ports; cannot run two replicas — route it via the proxy instead", name)
		}
		if svc.HasContainerName() {
			return fmt.Errorf("service %q sets container_name; compose cannot scale a named container — remove container_name to roll it", name)
		}
		if !svc.HasHealthcheck() {
			return fmt.Errorf("service %q has no healthcheck; rollout needs a readiness signal", name)
		}
	}
	return nil
}

// validateRollout checks what the config alone can decide; compose-dependent
// checks (service exists, ports, healthcheck, container_name) run via
// ValidateRolloutServices at discovery and at deploy time.
func validateRollout(r *Rollout) error {
	if r == nil {
		return nil
	}
	if len(r.Services) == 0 {
		return fmt.Errorf("services must list at least one service")
	}
	for i, svc := range r.Services {
		if strings.TrimSpace(svc) == "" {
			return fmt.Errorf("services[%d] is empty", i)
		}
	}
	if r.HealthTimeoutSeconds < 0 {
		return fmt.Errorf("health_timeout_seconds must not be negative, got %d", r.HealthTimeoutSeconds)
	}
	if r.DrainSeconds < 0 {
		return fmt.Errorf("drain_seconds must not be negative, got %d", r.DrainSeconds)
	}
	return nil
}
