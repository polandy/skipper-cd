// Config validation: what makes a loaded config invalid (refuse to start) or
// merely suspicious (start, but warn). config.go holds the shape and the
// loading; everything that rejects or flags a value lives here.

package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/ui"
)

// collectWarnings checks for valid-but-suspicious configs that don't warrant
// refusing to start. Runs only once validateConfig has already accepted cfg.
func collectWarnings(cfg *Config) []string {
	var warnings []string
	// Discovery off, no explicit stacks, and no nixos_rebuild: skipper has
	// nothing to deploy or manage. Under discovery this is not suspicious —
	// the repo may simply not have been synced yet.
	if !cfg.StackDiscovery && len(cfg.Stacks) == 0 && !cfg.NixOSRebuild.IsEnabled() {
		warnings = append(warnings, "stack_discovery is off, no stacks are configured, and nixos_rebuild is disabled — skipper-cd has nothing to deploy; set stack_discovery: true, add entries under stacks:, or configure nixos_rebuild")
	}

	// Under discovery, SelfHealActive follows the global self_heal flag alone —
	// the stack set is unknown at startup, so a per-stack override cannot
	// activate the poller by itself. A stacks: override that sets self_heal:
	// true while the global flag is off therefore never takes effect.
	if cfg.StackDiscovery && (cfg.SelfHeal == nil || !*cfg.SelfHeal) {
		for _, s := range cfg.Stacks {
			if s.SelfHeal != nil && *s.SelfHeal {
				warnings = append(warnings, fmt.Sprintf("stack %q sets self_heal: true, but stack_discovery is on and the global self_heal is off — under discovery only the global flag activates self-heal, so this override never takes effect; set the top-level self_heal: true instead", s.Name))
			}
		}
	}

	// hooks.timeout_seconds is capped by command_timeout_seconds at deploy time
	// (the hard ceiling), so a larger value here silently never has any effect.
	for _, s := range cfg.Stacks {
		if s.Hooks.TimeoutSeconds > cfg.CommandTimeoutSeconds {
			warnings = append(warnings, fmt.Sprintf("stack %q: hooks.timeout_seconds (%d) exceeds command_timeout_seconds (%d) — the hook is capped to the lower value; raise command_timeout_seconds or lower hooks.timeout_seconds", s.Name, s.Hooks.TimeoutSeconds, cfg.CommandTimeoutSeconds))
		}
	}

	// Per-host identity colours are drawn from a fixed palette (ui.HostColorCount
	// slots); the merged multi-host UI (ADR-0048) counts this host plus every
	// peer. Beyond the palette size two hosts hash to the same colour, so the
	// colour stops uniquely identifying a host.
	if totalHosts := len(cfg.Peers) + 1; totalHosts > ui.HostColorCount {
		warnings = append(warnings, fmt.Sprintf("%d hosts are configured (this host plus %d peers) but only %d distinct host colours are available — some hosts will share a colour in the merged UI; reduce the number of peers to keep colours unique", totalHosts, len(cfg.Peers), ui.HostColorCount))
	}

	return warnings
}

// Valid values for the log_format config field.
const (
	LogFormatPretty = "pretty"
	LogFormatText   = "text"
	LogFormatJSON   = "json"
)

// Valid values for the initial_deploy config field (ADR-0051).
const (
	// InitialDeployFull deploys every stack when no state is recorded — the
	// default, and the only safe choice for a host where nothing runs yet.
	InitialDeployFull = "full"
	// InitialDeployAdopt records the current inputs as deployed instead, for a
	// host whose stacks are already running the repo's version.
	InitialDeployAdopt = "adopt"
)

// AdoptsInitialState reports whether a run that finds no recorded state should
// adopt the running stacks instead of deploying them all.
func (c *Config) AdoptsInitialState() bool {
	return c.InitialDeploy == InitialDeployAdopt
}

func validateConfig(cfg *Config) error {
	if cfg.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}
	// webhook_secret is optional: reconcile is skipper's convergence baseline and
	// the webhook only accelerates it. An empty secret disables the /webhook
	// endpoint (it rejects with 403) rather than erroring — as long as reconcile
	// can still converge the host. With both the webhook off AND reconcile
	// disabled, nothing would deploy past the startup sync, so reject that dead
	// combination. ReconcileIntervalSeconds is defaulted before validateConfig
	// runs, so a nil here means it was never defaulted (on) — treat it as on.
	if cfg.WebhookSecret == "" && cfg.ReconcileIntervalSeconds != nil && *cfg.ReconcileIntervalSeconds == 0 {
		return fmt.Errorf("webhook_secret is empty (which disables the /webhook endpoint) and reconcile_interval_seconds is 0, so nothing would deploy after startup — set webhook_secret to enable push-triggered deploys, or set reconcile_interval_seconds > 0 (default 300) to converge on a timer")
	}

	// vars_file is a host path, available before any repo clone — check it now
	// rather than letting every deploy abort on a typo (internal/deploy would
	// otherwise only discover a missing/unreadable file at the first sync).
	if cfg.VarsFile != "" {
		f, err := os.Open(cfg.VarsFile)
		if err != nil {
			return fmt.Errorf("vars_file: %w — check the path and that skipper-cd can read it, or remove vars_file if it's no longer needed", err)
		}
		f.Close()
	}

	// repo_dir is used verbatim for git clone/pull; a relative value would
	// resolve against skipper's own process cwd, not the intended location.
	if cfg.RepoDir != "" && !filepath.IsAbs(cfg.RepoDir) {
		return fmt.Errorf("repo_dir %q must be an absolute path (start it with \"/\"), or leave it empty to use the default %s", cfg.RepoDir, git.DefaultRepoDir)
	}

	// A negative command_timeout_seconds would build an already-expired context,
	// failing every git/docker/nixos command from the first sync with an opaque
	// "context deadline exceeded". An omitted or 0 value took the default above.
	if cfg.CommandTimeoutSeconds < 0 {
		return fmt.Errorf("command_timeout_seconds must be >= 0, got %d", cfg.CommandTimeoutSeconds)
	}

	if cfg.Port < minPort || cfg.Port > maxPort {
		return fmt.Errorf("port must be between %d and %d, got %d", minPort, maxPort, cfg.Port)
	}
	if cfg.MetricsPort < minPort || cfg.MetricsPort > maxPort {
		return fmt.Errorf("metrics_port must be between %d and %d, got %d", minPort, maxPort, cfg.MetricsPort)
	}
	if cfg.Port == cfg.MetricsPort {
		return fmt.Errorf("port and metrics_port must differ, both are %d", cfg.Port)
	}

	if cfg.ProjectDirectoryBase != "" && !filepath.IsAbs(cfg.ProjectDirectoryBase) {
		// A relative project_directory_base would resolve against skipper's own
		// process cwd, not the repo clone — silently wrong --project-directory.
		return fmt.Errorf("project_directory_base %q must be an absolute path (start it with \"/\")", cfg.ProjectDirectoryBase)
	}
	// stacks_base_dir is relative to repo_dir (Load resolves it afterwards). An
	// absolute value would be silently mangled by the resolution join and, by
	// definition, could point outside the clone — which breaks invariant #1
	// (change detection and the compose file always come from the repo clone).
	if filepath.IsAbs(cfg.StacksBaseDir) {
		return fmt.Errorf("stacks_base_dir %q must be relative to repo_dir (the repo clone), not absolute — drop the leading \"/\" and the repo_dir prefix (e.g. \"stacks\" for <repo_dir>/stacks)", cfg.StacksBaseDir)
	}
	// A "../" escape would resolve outside the clone, same invariant #1 breach.
	if rel := filepath.Clean(cfg.StacksBaseDir); rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("stacks_base_dir %q must stay inside repo_dir (the repo clone) — it must not escape via \"../\"", cfg.StacksBaseDir)
	}

	// icons.cache_dir is always non-empty here (Load applies the default before
	// validateConfig runs) — an explicit relative override would still resolve
	// against skipper's own process cwd instead of the intended cache location.
	if !filepath.IsAbs(cfg.Icons.CacheDir) {
		return fmt.Errorf("icons.cache_dir %q must be an absolute path (start it with \"/\")", cfg.Icons.CacheDir)
	}

	seen := make(map[string]struct{}, len(cfg.Stacks))
	for i, s := range cfg.Stacks {
		if s.Name == "" {
			return fmt.Errorf("stacks[%d] has no name — add a name: field to this entry", i)
		}
		if s.Name == ReservedStackName || s.Name == ReservedConfigStackName {
			return fmt.Errorf("stack name %q is reserved for skipper's internal use (%s, %s) — rename this stack", s.Name, ReservedStackName, ReservedConfigStackName)
		}
		if _, dup := seen[s.Name]; dup {
			return fmt.Errorf("duplicate stack name %q — stack names must be unique, rename one of the two entries", s.Name)
		}
		seen[s.Name] = struct{}{}

		// project_directory is optional: when unset the compose file is located
		// at <stacks_base_dir>/<name>, and stacks_base_dir always resolves to an
		// absolute path (empty = repo root), so there is nothing left to require.
		if s.ProjectDirectory != "" && !filepath.IsAbs(s.ProjectDirectory) {
			// A relative project_directory would resolve against skipper's own
			// process cwd, not the repo clone — silently wrong --project-directory.
			return fmt.Errorf("stack %q: project_directory %q must be an absolute path (start it with \"/\")", s.Name, s.ProjectDirectory)
		}

		if !cfg.StackDiscovery {
			// Under stack_discovery, LoadRepoStacks resolves relative env_files/
			// watch_dirs against stacks_base_dir (resolveRepoPaths). Host-list mode
			// has no such resolution step, so a relative entry here would silently
			// resolve against skipper's own process cwd instead.
			for j, f := range s.EnvFiles {
				if !filepath.IsAbs(f) {
					return fmt.Errorf("stack %q: env_files[%d] %q must be an absolute path (start it with \"/\")", s.Name, j, f)
				}
			}
			for j, d := range s.WatchDirs {
				if !filepath.IsAbs(d) {
					return fmt.Errorf("stack %q: watch_dirs[%d] %q must be an absolute path (start it with \"/\")", s.Name, j, d)
				}
			}
		}

		if err := validateHealthCheck(s.DeployHealthCheck); err != nil {
			return fmt.Errorf("stack %q: deploy_health_check: %w", s.Name, err)
		}

		if err := validateHooks(s.Hooks); err != nil {
			return fmt.Errorf("stack %q: hooks: %w", s.Name, err)
		}

		if err := validateRollout(s.Rollout); err != nil {
			return fmt.Errorf("stack %q: rollout: %w", s.Name, err)
		}
	}

	// Under discovery, cfg.Stacks is only the override subset; depends_on is
	// validated against the full set at discovery (LoadRepoStacks), not here.
	if !cfg.StackDiscovery {
		if err := validateStackDependencies(cfg.Stacks); err != nil {
			return err
		}
	}

	if cfg.NixOSRebuild.IsEnabled() && cfg.NixOSRebuild.Flake == "" {
		return fmt.Errorf("nixos_rebuild.flake is required when nixos_rebuild is enabled")
	}

	if !ui.IsValidTheme(cfg.UITheme) {
		return fmt.Errorf("ui_theme must be one of %s, got %q", strings.Join(ui.ValidThemes, ", "), cfg.UITheme)
	}

	if cfg.LogFormat != LogFormatPretty && cfg.LogFormat != LogFormatText && cfg.LogFormat != LogFormatJSON {
		return fmt.Errorf("log_format must be %q, %q or %q, got %q", LogFormatPretty, LogFormatText, LogFormatJSON, cfg.LogFormat)
	}

	if cfg.InitialDeploy != InitialDeployFull && cfg.InitialDeploy != InitialDeployAdopt {
		return fmt.Errorf("initial_deploy must be %q or %q, got %q — use %q on a host whose stacks already run the repo's version, otherwise leave it unset", InitialDeployFull, InitialDeployAdopt, cfg.InitialDeploy, InitialDeployAdopt)
	}

	if cfg.RuntimeHealthPollIntervalSeconds != nil && *cfg.RuntimeHealthPollIntervalSeconds < 0 {
		return fmt.Errorf("runtime_health_poll_interval_seconds must be >= 0, got %d", *cfg.RuntimeHealthPollIntervalSeconds)
	}

	if cfg.ReconcileIntervalSeconds != nil && *cfg.ReconcileIntervalSeconds < 0 {
		return fmt.Errorf("reconcile_interval_seconds must be >= 0, got %d", *cfg.ReconcileIntervalSeconds)
	}

	if cfg.SelfHealMinUnhealthyPolls < 1 {
		return fmt.Errorf("self_heal_min_unhealthy_polls must be >= 1, got %d", cfg.SelfHealMinUnhealthyPolls)
	}
	if cfg.SelfHealMaxAttempts < 1 {
		return fmt.Errorf("self_heal_max_attempts must be >= 1, got %d", cfg.SelfHealMaxAttempts)
	}
	if *cfg.SelfHealCooldownSeconds < 0 {
		return fmt.Errorf("self_heal_cooldown_seconds must be >= 0, got %d", *cfg.SelfHealCooldownSeconds)
	}
	// Self-heal rides the health poll cadence and runs headless, so it needs a
	// positive poll interval even with the UI off (ADR-0029).
	if cfg.SelfHealActive() && (cfg.RuntimeHealthPollIntervalSeconds == nil || *cfg.RuntimeHealthPollIntervalSeconds <= 0) {
		return fmt.Errorf("self_heal requires runtime_health_poll_interval_seconds > 0 (it drives the detection cadence)")
	}

	for i, t := range cfg.Notifications {
		if err := validateNotificationTarget(t); err != nil {
			return fmt.Errorf("notifications[%d]: %w", i, err)
		}
	}

	if err := validatePeers(cfg.Peers); err != nil {
		return err
	}

	if err := validateHealthWatch(cfg.HealthWatch); err != nil {
		return fmt.Errorf("health_watch: %w", err)
	}
	// Like self-heal, the watchdog rides the health poll cadence and runs
	// headless, so it needs a positive poll interval even with the UI off
	// (ADR-0031).
	if cfg.HealthWatch != nil && (cfg.RuntimeHealthPollIntervalSeconds == nil || *cfg.RuntimeHealthPollIntervalSeconds <= 0) {
		return fmt.Errorf("health_watch requires runtime_health_poll_interval_seconds > 0 (it drives the watch cadence)")
	}

	return nil
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

	// Kahn's algorithm: peel stacks whose dependencies are all resolved; any
	// leftover is part of (or downstream of) a cycle.
	resolved := make(map[string]bool, len(stacks))
	for remaining := len(stacks); remaining > 0; {
		progressed := false
		for _, s := range stacks {
			if resolved[s.Name] {
				continue
			}
			allResolved := true
			for _, dep := range s.DependsOn {
				if !resolved[dep] {
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
			var stuck []string
			for _, s := range stacks {
				if !resolved[s.Name] {
					stuck = append(stuck, s.Name)
				}
			}
			return fmt.Errorf("depends_on cycle involving stacks: %s", strings.Join(stuck, ", "))
		}
	}
	return nil
}

// validateHealthWatch checks the optional health_watch section. Defaults have
// already been applied in Load.
func validateHealthWatch(hw *HealthWatch) error {
	if hw == nil {
		return nil
	}
	if hw.DebouncePolls < 1 {
		return fmt.Errorf("debounce_polls must be >= 1, got %d", hw.DebouncePolls)
	}
	if hw.AttributionWindowSeconds < 0 {
		return fmt.Errorf("attribution_window_seconds must be >= 0, got %d", hw.AttributionWindowSeconds)
	}
	if hw.AlertCooldownSeconds != nil && *hw.AlertCooldownSeconds < 0 {
		return fmt.Errorf("alert_cooldown_seconds must be >= 0, got %d", *hw.AlertCooldownSeconds)
	}
	for i, t := range hw.Targets {
		// Health targets carry no `on:` — they receive every alert-worthy
		// transition; the field belongs to deploy notifications only.
		if len(t.On) > 0 {
			return fmt.Errorf("targets[%d]: on is not valid for health_watch targets", i)
		}
		if err := validateNotificationTarget(t); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
	}
	return nil
}

// validateHealthCheck checks a stack's optional deploy_health_check section.
// TimeoutSeconds has already been defaulted in Load.
func validateHealthCheck(hc *HealthCheck) error {
	if hc == nil {
		return nil
	}
	if hc.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must not be negative, got %d", hc.TimeoutSeconds)
	}
	if hc.URL != "" {
		if u, err := url.ParseRequestURI(hc.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("url %q must be a valid http(s) URL", hc.URL)
		}
	}
	return nil
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

// validateNotificationTarget checks a single notification target. Format and On
// have already been defaulted in Load.
// Peer is one other skipper instance this instance federates in (ADR-0048):
// the primary reads the peer's read data over HTTP and renders it, tagged by
// host, in one merged UI.
type Peer struct {
	// Name is the display label and identity key — it appears on the host
	// badge/filter and drives the peer's per-host colour. Must be unique.
	Name string `yaml:"name"`

	// URL is the peer's skipper base URL, reachable from this instance (its
	// LAN address, e.g. http://host-b:8001).
	URL string `yaml:"url"`
}

// validatePeers checks the peers list: each entry needs a unique name and a
// valid http(s) URL. Names must be unique because the name is the identity key
// the merged UI groups and colours by.
func validatePeers(peers []Peer) error {
	seen := make(map[string]bool, len(peers))
	for i, p := range peers {
		if p.Name == "" {
			return fmt.Errorf("peers[%d]: name is required (the host label shown in the UI)", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("peers[%d]: duplicate peer name %q — peer names must be unique", i, p.Name)
		}
		seen[p.Name] = true
		if p.URL == "" {
			return fmt.Errorf("peers[%d] (%s): url is required (the peer's skipper base URL, e.g. http://host-b:8001)", i, p.Name)
		}
		if u, err := url.ParseRequestURI(p.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("peers[%d] (%s): url %q must be a valid http(s) URL", i, p.Name, p.URL)
		}
	}
	return nil
}

func validateNotificationTarget(t NotificationTarget) error {
	switch t.Format {
	case NotifyFormatSignal, NotifyFormatGeneric:
	default:
		return fmt.Errorf("unknown format %q, must be %q or %q", t.Format, NotifyFormatSignal, NotifyFormatGeneric)
	}

	if t.URL == "" {
		return fmt.Errorf("url is required (the endpoint the notification is POSTed to)")
	}
	if u, err := url.ParseRequestURI(t.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("url %q must be a valid http(s) URL", t.URL)
	}

	for _, s := range t.On {
		switch s {
		case NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy, NotifyOnHealExhausted:
		default:
			return fmt.Errorf("unknown on value %q, must be one of %q, %q, %q, %q, %q",
				s, NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy, NotifyOnHealExhausted)
		}
	}

	// Signal identity fields are required for and exclusive to the signal format.
	if t.Format == NotifyFormatSignal {
		if t.Number == "" {
			return fmt.Errorf("signal format requires number")
		}
		if len(t.Recipients) == 0 {
			return fmt.Errorf("signal format requires at least one recipient")
		}
	} else if t.Number != "" || len(t.Recipients) > 0 {
		return fmt.Errorf("number/recipients are only valid for the signal format")
	}

	return nil
}
