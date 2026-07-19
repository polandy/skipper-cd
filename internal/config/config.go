// Package config handles loading and validating the skipper.yml configuration file.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/ui"
)

// Stack represents a single Docker Compose project to be deployed.
type Stack struct {
	// Name is a unique identifier for the stack. When working_dir is omitted,
	// the working directory is derived as stacks_base_dir/<name>.
	Name string `yaml:"name"`

	// WorkingDir is an optional absolute path passed as --project-directory to
	// docker compose. It controls Docker Compose project identity (container
	// labels) and .env file loading. Change detection and the compose file
	// always come from stacks_base_dir/<name>.
	WorkingDir string `yaml:"working_dir"`

	// EnvFiles lists KEY=VALUE files injected into the environment when docker-compose
	// is invoked, enabling ${VAR} substitution inside docker-compose.yml.
	EnvFiles []string `yaml:"env_files"`

	// WatchDirs lists additional directories (relative to the repo root or absolute)
	// whose contents are hashed alongside docker-compose.yml. Any change inside
	// these directories triggers a redeployment.
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

	// HealthCheck optionally gates a deploy of this stack on a post-deploy
	// health check; on failure the deploy is rolled back. nil disables the
	// gate. See ADR-0022.
	HealthCheck *HealthCheck `yaml:"health_check,omitempty"`

	// SelfHeal overrides the global self_heal for this stack. nil means inherit
	// the global setting. When effective, a stack found degraded by the health
	// poller is automatically restored to its deployed running state by a
	// corrective redeploy. See ADR-0029.
	SelfHeal *bool `yaml:"self_heal"`

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

	// ConfigHash is the hash of the stack's deploy-shaping config, set only by
	// LoadRepoStacks in stack-discovery mode (ADR-0034). It participates in
	// change detection so a repo skipper.yaml edit redeploys exactly the
	// affected stack. Empty in legacy (host stacks list) mode. Never read from
	// YAML.
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
	// health_check is configured.
	PostDeploy []string `yaml:"post_deploy"`

	// TimeoutSeconds bounds each individual hook command. 0 (the default) leaves
	// each hook bounded only by the global command_timeout_seconds, which is
	// also the hard ceiling: a larger value here cannot exceed it (raise
	// command_timeout_seconds for a backup slower than that).
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

// HealthCheck configures the optional post-deploy health gate of a stack.
// When present, `docker compose up` runs with --wait so it fails when the
// services' compose healthchecks do not turn healthy in time, and an optional
// HTTP probe additionally verifies the stack from the outside. Either failure
// triggers the regular rollback path (ADR-0004, ADR-0022).
type HealthCheck struct {
	// TimeoutSeconds bounds the wait: it is passed as --wait-timeout to
	// docker compose up and is also the deadline of the HTTP probe.
	// Defaults to 60.
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// URL, when set, is HTTP-GET-probed after a successful up until it
	// answers 2xx; anything else within TimeoutSeconds rolls the deploy back.
	URL string `yaml:"url"`
}

// Config holds the full skipper-cd configuration.
type Config struct {
	// RepoURL is the Git remote URL. skipper-cd clones it to RepoDir and
	// runs git pull on every incoming webhook.
	RepoURL string `yaml:"repo_url"`

	// RepoDir is the local directory where the repository is cloned.
	// Defaults to /var/lib/skipper/repo when left empty.
	RepoDir string `yaml:"repo_dir"`

	// Branch is the Git branch to track. Defaults to "main".
	Branch string `yaml:"branch"`

	// VarsFile is an optional path to a KEY=VALUE env file whose entries are
	// injected into the environment of every stack deployment, enabling
	// ${VAR} substitution in docker-compose.yml (e.g. for domain names).
	VarsFile string `yaml:"vars_file"`

	// CommandTimeoutSeconds is the maximum number of seconds a single shell
	// command (docker compose pull/up, git clone/fetch) is allowed to run
	// before being killed. Defaults to 300 (5 minutes).
	CommandTimeoutSeconds int `yaml:"command_timeout_seconds"`

	// LogFormat selects the log output format: "text" (logfmt, the default)
	// or "json" for structured logs (e.g. for Loki ingestion).
	LogFormat string `yaml:"log_format"`

	// StacksBaseDir is the directory inside the repo clone that holds one
	// subdirectory per stack (<stacks_base_dir>/<name>/docker-compose.yml).
	// Change detection and the compose file always come from here.
	StacksBaseDir string `yaml:"stacks_base_dir"`

	// WebhookSecret is the shared HMAC-SHA256 secret push webhooks are signed
	// with (Gitea X-Gitea-Signature / GitHub X-Hub-Signature-256). Empty
	// disables signature verification.
	WebhookSecret string `yaml:"webhook_secret"`

	// Port is the webhook/UI HTTP port. Defaults to 8080.
	Port int `yaml:"port"`

	// MetricsPort is the Prometheus /metrics HTTP port. Defaults to 9120.
	MetricsPort int `yaml:"metrics_port"`

	// Stacks lists the Docker Compose projects to deploy. Mutually exclusive
	// with StackDiscovery.
	Stacks []Stack `yaml:"stacks"`

	// StackDiscovery discovers the stack set from the deploy repo on every
	// sync instead of this file (ADR-0034): every direct subdirectory of
	// stacks_base_dir containing a docker-compose.yml is a stack, and the
	// optional repo-root skipper.yaml holds per-stack overrides. Mutually
	// exclusive with a non-empty Stacks list; requires stacks_base_dir.
	StackDiscovery bool `yaml:"stack_discovery"`

	// UIEnabled serves the web UI (dashboard, event history, UI API) on the
	// webhook port. nil (omitted) defaults to true; set an explicit false to
	// run headless. Load normalizes it, so it is never nil after Load.
	UIEnabled *bool `yaml:"ui_enabled"`

	// Autosync is the global default for whether detected changes deploy
	// automatically. nil means true (on). A per-stack Autosync overrides it.
	// See docs/autosync.md.
	Autosync *bool `yaml:"autosync"`

	// NixOSRebuild configures automatic nixos-rebuild when nix files change.
	// Omit the section entirely to disable. When present without an explicit
	// "enabled" key, it defaults to enabled.
	NixOSRebuild *NixOSRebuild `yaml:"nixos_rebuild"`

	// Icons configures service-icon resolution for the web UI. The section is
	// optional; omitting it uses the defaults applied in Load.
	Icons IconsConfig `yaml:"icons"`

	// Notifications lists outbound notification targets that receive a message
	// on every terminal deploy outcome they subscribe to. An empty section
	// disables notifications entirely. See ADR-0020.
	Notifications []NotificationTarget `yaml:"notifications"`

	// UITheme selects the web UI's colour palette (see ui.ValidThemes).
	// Optional; defaults to "catppuccin". Each theme has its own dark/light
	// variant, toggled independently in the browser — this only picks which
	// palette that toggle switches within. Distinguishes multiple skipper-cd
	// instances (e.g. one per host) at a glance. See docs/configuration.md.
	UITheme string `yaml:"ui_theme"`

	// UIThemeSwitcher enables the in-UI theme picker: a per-browser override
	// for trying palettes out, applied without a reload. Optional; defaults to
	// false, so the deployed UITheme is fixed and cannot be switched from the
	// browser (keeping the per-instance colour a reliable at-a-glance marker).
	// See docs/configuration.md.
	UIThemeSwitcher bool `yaml:"ui_theme_switcher"`

	// HealthPollIntervalSeconds sets how often the UI polls its stacks' runtime
	// health (ADR-0027). nil (omitted) defaults to 30; an explicit 0 disables the
	// health view. Only meaningful with ui_enabled; the poll additionally runs
	// only while a UI client is connected. See docs/configuration.md.
	HealthPollIntervalSeconds *int `yaml:"health_poll_interval_seconds"`

	// ReconcileIntervalSeconds sets how often skipper re-runs its git sync +
	// deploy on a timer, so a missed or lost webhook cannot leave the host
	// drifted from the deploy repo indefinitely (ADR-0028). nil (omitted)
	// defaults to 300; an explicit 0 disables the loop (pure webhook + startup
	// behaviour). Unlike the health poll it is not UI-gated — it runs headless.
	// See docs/configuration.md.
	ReconcileIntervalSeconds *int `yaml:"reconcile_interval_seconds"`

	// SelfHeal is the global default for whether a stack the health poller finds
	// degraded (a stopped/removed container, an unhealthy service) is
	// automatically restored to its deployed running state by a corrective
	// redeploy (ADR-0029). nil means false (off). A per-stack SelfHeal overrides
	// it. Runs headless like reconcile, so it needs health_poll_interval_seconds
	// > 0 — which drives the detection cadence — even with the UI off.
	SelfHeal *bool `yaml:"self_heal"`

	// SelfHealMinUnhealthyPolls is how many consecutive degraded health polls a
	// stack must show before self-heal acts. The debounce keeps skipper from
	// racing Docker's own restart policy on a transient blip. Defaults to 3;
	// must be >= 1 (ADR-0029).
	SelfHealMinUnhealthyPolls int `yaml:"self_heal_min_unhealthy_polls"`

	// SelfHealMaxAttempts caps consecutive corrective redeploys per outage
	// before self-heal gives up and reports heal_exhausted, so an app-level
	// fault an `up` cannot fix does not become a hot loop. Defaults to 3; must
	// be >= 1 (ADR-0029).
	SelfHealMaxAttempts int `yaml:"self_heal_max_attempts"`

	// SelfHealCooldownSeconds is the minimum gap between corrective redeploys of
	// the same stack (ADR-0029). nil (omitted) defaults to 60; an explicit 0
	// disables the cooldown; must be >= 0 — the same omitted-vs-explicit-0
	// convention as the other interval fields.
	SelfHealCooldownSeconds *int `yaml:"self_heal_cooldown_seconds"`

	// HealthWatch configures the own-stack health watchdog: it detects
	// per-service health transitions and alerts on newly-failed and recovered
	// services (ADR-0031). Omit the section to disable. Like self-heal it runs
	// headless — not UI-gated.
	HealthWatch *HealthWatch `yaml:"health_watch"`
}

// StackByName returns the configured stack with the given name.
func (c *Config) StackByName(name string) (Stack, bool) {
	for _, s := range c.Stacks {
		if s.Name == name {
			return s, true
		}
	}
	return Stack{}, false
}

// SelfHealEnabled reports whether self-heal is effective for the named stack:
// the per-stack override when set, otherwise the global default (off when
// unset). An unknown name falls back to the global default.
func (c *Config) SelfHealEnabled(name string) bool {
	return c.EffectiveSelfHeal(c.Stacks, name)
}

// EffectiveSelfHeal is SelfHealEnabled over an explicit stack set — the
// discovered stacks in stack-discovery mode, where c.Stacks is empty
// (ADR-0034).
func (c *Config) EffectiveSelfHeal(stacks []Stack, name string) bool {
	for _, s := range stacks {
		if s.Name == name {
			if s.SelfHeal != nil {
				return *s.SelfHeal
			}
			break
		}
	}
	return c.SelfHeal != nil && *c.SelfHeal
}

// SelfHealActive reports whether self-heal is effective for at least one stack.
// When true the health poller must run headless (not UI/subscriber-gated) so
// self-heal sees degradation on an unattended host (ADR-0029). In
// stack-discovery mode the stack set is unknown at startup, so activation
// follows the global flag alone; a per-stack self_heal: true with the global
// off is not supported there (documented in docs/configuration.md).
func (c *Config) SelfHealActive() bool {
	if c.StackDiscovery {
		return c.SelfHeal != nil && *c.SelfHeal
	}
	for _, s := range c.Stacks {
		if c.SelfHealEnabled(s.Name) {
			return true
		}
	}
	return false
}

// HealthWatch configures the own-stack health watchdog (ADR-0031). It rides
// the shared health poller's cadence (health_poll_interval_seconds), the same
// way self-heal does — it has no poll interval of its own.
type HealthWatch struct {
	// DebouncePolls is how many consecutive health polls a new status must
	// persist before it is accepted (and may alert). Defaults to 2.
	DebouncePolls int `yaml:"debounce_polls"`

	// AttributionWindowSeconds is how long after a stack's deploy a health
	// transition still counts as deploy-correlated. Defaults to 300.
	AttributionWindowSeconds int `yaml:"attribution_window_seconds"`

	// AlertCooldownSeconds is the minimum gap in seconds between delivered
	// alerts of the same service and direction (unhealthy / recovered) — the
	// rate limit against a slow flapper paging on every cycle. Suppressed
	// transitions are still journaled and persisted, and once the cooldown
	// expires a still-diverged service gets the owed alert late (catch-up).
	// Defaults to 1800 when omitted; an explicit 0 disables the cooldown;
	// must be >= 0.
	AlertCooldownSeconds *int `yaml:"alert_cooldown_seconds"`

	// Targets lists the outbound sinks health alerts are delivered to, in the
	// same shape as the notifications targets but without `on:` (a health
	// target receives all alert-worthy transitions). Optional: with no targets
	// the watchdog still logs transitions and persists the history.
	Targets []NotificationTarget `yaml:"targets"`
}

// NotificationTarget configures a single outbound notification sink: where to
// POST, in which provider shape, and which deploy outcomes to report. See
// ADR-0020.
type NotificationTarget struct {
	// Format selects the provider shape of the request body. One of
	// "signal", "generic". Defaults to "generic".
	Format string `yaml:"format"`

	// URL is the endpoint the notification is POSTed to. For "signal" it is the
	// signal-cli-rest-api base (e.g. http://localhost:8020); "/v2/send" is
	// appended by the formatter.
	URL string `yaml:"url"`

	// On lists the terminal deploy statuses that trigger this target. Any
	// subset of "failed", "success", "rolled_back", "rolled_back_unhealthy".
	// Empty means all four.
	On []string `yaml:"on"`

	// Prefix is prepended (as "[<prefix>] ") to the human-readable message of
	// the "signal" format, e.g. to label which host/instance sent it. Optional;
	// empty adds no prefix. Ignored by the "generic" format, whose structured
	// payload already carries the event.
	Prefix string `yaml:"prefix"`

	// Headers are static HTTP headers added to the request. Only meaningful for
	// the "generic" format (e.g. an Authorization bearer token).
	Headers map[string]string `yaml:"headers"`

	// Number is the Signal sender number ("signal" format only, required).
	Number string `yaml:"number"`

	// Recipients are the Signal recipient numbers ("signal" format only,
	// non-empty).
	Recipients []string `yaml:"recipients"`
}

// Notification format values for NotificationTarget.Format.
const (
	NotifyFormatSignal  = "signal"
	NotifyFormatGeneric = "generic"
)

// Terminal deploy statuses accepted in NotificationTarget.On. They mirror the
// terminal events.Status values without importing that package (config is the
// lowest layer).
const (
	NotifyOnFailed              = "failed"
	NotifyOnSuccess             = "success"
	NotifyOnRolledBack          = "rolled_back"
	NotifyOnRolledBackUnhealthy = "rolled_back_unhealthy"
	// NotifyOnHealExhausted fires when self-heal gave up on a stack it could not
	// restore — the high-signal "a stack is down and I couldn't fix it" alarm
	// (ADR-0029). Part of the default set, so a target with no explicit `on`
	// reports it.
	NotifyOnHealExhausted = "heal_exhausted"
)

// IconsConfig configures how the web UI resolves and caches stack icons.
type IconsConfig struct {
	// CacheDir is the on-disk directory where fetched icons are cached.
	// Defaults to /var/lib/skipper/icons.
	CacheDir string `yaml:"cache_dir"`

	// SourceURL is the icon-set base URL; icons are fetched from
	// SourceURL/<slug>.svg. Defaults to the dashboard-icons CDN.
	SourceURL string `yaml:"source_url"`
}

// NixOSRebuild configures automatic NixOS rebuilds when .nix files or
// flake.lock change in the repository.
type NixOSRebuild struct {
	Enabled *bool  `yaml:"enabled"` // nil = true when section present
	Flake   string `yaml:"flake"`   // e.g. ".#nuc", required when enabled
}

// IsEnabled returns whether NixOS rebuild is enabled.
// Section omitted → disabled. Section present without enabled key → enabled.
// Explicit enabled value → that value.
func (n *NixOSRebuild) IsEnabled() bool {
	if n == nil {
		return false
	}
	if n.Enabled == nil {
		return true
	}
	return *n.Enabled
}

// Load reads and validates the configuration file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{
		Port:        8080,
		MetricsPort: 9120,
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	if cfg.CommandTimeoutSeconds == 0 {
		cfg.CommandTimeoutSeconds = 300
	}
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	if cfg.LogFormat == "" {
		cfg.LogFormat = LogFormatText
	}
	if cfg.Icons.CacheDir == "" {
		cfg.Icons.CacheDir = defaultIconCacheDir
	}
	if cfg.Icons.SourceURL == "" {
		cfg.Icons.SourceURL = defaultIconSourceURL
	}
	for i := range cfg.Notifications {
		t := &cfg.Notifications[i]
		if t.Format == "" {
			t.Format = NotifyFormatGeneric
		}
		if len(t.On) == 0 {
			t.On = []string{NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy, NotifyOnHealExhausted}
		}
	}
	if cfg.UITheme == "" {
		cfg.UITheme = ui.ThemeCatppuccin
	}
	if cfg.UIEnabled == nil {
		d := true
		cfg.UIEnabled = &d
	}
	if cfg.HealthPollIntervalSeconds == nil {
		d := defaultHealthPollIntervalSeconds
		cfg.HealthPollIntervalSeconds = &d
	}
	if cfg.ReconcileIntervalSeconds == nil {
		d := defaultReconcileIntervalSeconds
		cfg.ReconcileIntervalSeconds = &d
	}
	if cfg.SelfHealMinUnhealthyPolls == 0 {
		cfg.SelfHealMinUnhealthyPolls = defaultSelfHealMinUnhealthyPolls
	}
	if cfg.SelfHealMaxAttempts == 0 {
		cfg.SelfHealMaxAttempts = defaultSelfHealMaxAttempts
	}
	if cfg.SelfHealCooldownSeconds == nil {
		// Only an omitted field takes the default — an explicit 0 disables
		// the cooldown.
		d := defaultSelfHealCooldownSeconds
		cfg.SelfHealCooldownSeconds = &d
	}
	if hw := cfg.HealthWatch; hw != nil {
		if hw.DebouncePolls == 0 {
			hw.DebouncePolls = defaultHealthWatchDebouncePolls
		}
		if hw.AttributionWindowSeconds == 0 {
			hw.AttributionWindowSeconds = defaultHealthWatchAttributionWindowSeconds
		}
		if hw.AlertCooldownSeconds == nil {
			// Only an omitted field takes the default — an explicit 0 is the
			// off switch for the cooldown.
			d := defaultHealthWatchAlertCooldownSeconds
			hw.AlertCooldownSeconds = &d
		}
		for i := range hw.Targets {
			if hw.Targets[i].Format == "" {
				hw.Targets[i].Format = NotifyFormatGeneric
			}
		}
	}
	for i := range cfg.Stacks {
		if hc := cfg.Stacks[i].HealthCheck; hc != nil && hc.TimeoutSeconds == 0 {
			hc.TimeoutSeconds = defaultHealthCheckTimeoutSeconds
		}
	}

	return cfg, validateConfig(cfg)
}

// Valid values for the log_format config field.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// Defaults for the icons section. SourceURL is the icon-set root; icons are
// fetched from <source_url>/<format>/<slug>.<format> (svg, then png, webp).
const (
	defaultIconCacheDir  = "/var/lib/skipper/icons"
	defaultIconSourceURL = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"
)

// ReservedStackName is used as the state/event key for NixOS rebuild hashes
// (mirrored as deploy.NixosStateKey) and must not collide with a configured
// stack.
const ReservedStackName = "_nixos"

// ReservedConfigStackName is the event key for repo stack-config failures in
// stack-discovery mode (ADR-0034, mirrored as deploy.ConfigStateKey) and must
// not collide with a configured stack.
const ReservedConfigStackName = "_config"

// defaultHealthCheckTimeoutSeconds is applied when a health_check section is
// present without an explicit timeout_seconds.
const defaultHealthCheckTimeoutSeconds = 60

// defaultHealthPollIntervalSeconds is the UI stack-health poll cadence applied
// when health_poll_interval_seconds is omitted (ADR-0027). An explicit 0
// disables the health view.
const defaultHealthPollIntervalSeconds = 30

// defaultReconcileIntervalSeconds is the git sync + deploy cadence applied when
// reconcile_interval_seconds is omitted (ADR-0028). On by default so skipper
// self-corrects a missed webhook out of the box; an explicit 0 disables it.
const defaultReconcileIntervalSeconds = 300

// Defaults for the self-heal pacing constants, applied when the corresponding
// field is omitted (ADR-0029). Self-heal itself is off unless self_heal is
// set true globally or per stack.
const (
	defaultSelfHealMinUnhealthyPolls = 3
	defaultSelfHealMaxAttempts       = 3
	defaultSelfHealCooldownSeconds   = 60
)

// Defaults for the health_watch section (ADR-0031), applied when the section
// is present. The section itself is opt-in: omitting it disables the watchdog.
const (
	defaultHealthWatchDebouncePolls            = 2
	defaultHealthWatchAttributionWindowSeconds = 300
	defaultHealthWatchAlertCooldownSeconds     = 1800
)

// TCP port bounds for the webhook/UI and metrics listeners.
const (
	minPort = 1
	maxPort = 65535
)

func validateConfig(cfg *Config) error {
	if cfg.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
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

	if cfg.StackDiscovery {
		if len(cfg.Stacks) > 0 {
			return fmt.Errorf("stacks must be empty when stack_discovery is enabled — per-stack config moves to the repo-root %s (ADR-0034)", RepoConfigFileName)
		}
		if cfg.StacksBaseDir == "" {
			return fmt.Errorf("stacks_base_dir is required when stack_discovery is enabled")
		}
	}

	seen := make(map[string]struct{}, len(cfg.Stacks))
	for _, s := range cfg.Stacks {
		if s.Name == "" {
			return fmt.Errorf("every stack needs a name")
		}
		if s.Name == ReservedStackName || s.Name == ReservedConfigStackName {
			return fmt.Errorf("stack name %q is reserved", s.Name)
		}
		if _, dup := seen[s.Name]; dup {
			return fmt.Errorf("duplicate stack name %q", s.Name)
		}
		seen[s.Name] = struct{}{}

		if s.WorkingDir == "" && cfg.StacksBaseDir == "" {
			return fmt.Errorf("stack %q: working_dir is required when stacks_base_dir is not set", s.Name)
		}

		if err := validateHealthCheck(s.HealthCheck); err != nil {
			return fmt.Errorf("stack %q: health_check: %w", s.Name, err)
		}

		if err := validateHooks(s.Hooks); err != nil {
			return fmt.Errorf("stack %q: hooks: %w", s.Name, err)
		}
	}

	if err := validateStackDependencies(cfg.Stacks); err != nil {
		return err
	}

	if cfg.NixOSRebuild.IsEnabled() && cfg.NixOSRebuild.Flake == "" {
		return fmt.Errorf("nixos_rebuild.flake is required when nixos_rebuild is enabled")
	}

	if !ui.IsValidTheme(cfg.UITheme) {
		return fmt.Errorf("ui_theme must be one of %s, got %q", strings.Join(ui.ValidThemes, ", "), cfg.UITheme)
	}

	if cfg.LogFormat != LogFormatText && cfg.LogFormat != LogFormatJSON {
		return fmt.Errorf("log_format must be %q or %q, got %q", LogFormatText, LogFormatJSON, cfg.LogFormat)
	}

	if cfg.HealthPollIntervalSeconds != nil && *cfg.HealthPollIntervalSeconds < 0 {
		return fmt.Errorf("health_poll_interval_seconds must be >= 0, got %d", *cfg.HealthPollIntervalSeconds)
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
	if cfg.SelfHealActive() && (cfg.HealthPollIntervalSeconds == nil || *cfg.HealthPollIntervalSeconds <= 0) {
		return fmt.Errorf("self_heal requires health_poll_interval_seconds > 0 (it drives the detection cadence)")
	}

	for i, t := range cfg.Notifications {
		if err := validateNotificationTarget(t); err != nil {
			return fmt.Errorf("notifications[%d]: %w", i, err)
		}
	}

	if err := validateHealthWatch(cfg.HealthWatch); err != nil {
		return fmt.Errorf("health_watch: %w", err)
	}
	// Like self-heal, the watchdog rides the health poll cadence and runs
	// headless, so it needs a positive poll interval even with the UI off
	// (ADR-0031).
	if cfg.HealthWatch != nil && (cfg.HealthPollIntervalSeconds == nil || *cfg.HealthPollIntervalSeconds <= 0) {
		return fmt.Errorf("health_watch requires health_poll_interval_seconds > 0 (it drives the watch cadence)")
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

// validateHealthCheck checks a stack's optional health_check section.
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

// validateNotificationTarget checks a single notification target. Format and On
// have already been defaulted in Load.
func validateNotificationTarget(t NotificationTarget) error {
	switch t.Format {
	case NotifyFormatSignal, NotifyFormatGeneric:
	default:
		return fmt.Errorf("unknown format %q", t.Format)
	}

	if t.URL == "" {
		return fmt.Errorf("url is required")
	}
	if u, err := url.ParseRequestURI(t.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("url %q must be a valid http(s) URL", t.URL)
	}

	for _, s := range t.On {
		switch s {
		case NotifyOnFailed, NotifyOnSuccess, NotifyOnRolledBack, NotifyOnRolledBackUnhealthy, NotifyOnHealExhausted:
		default:
			return fmt.Errorf("unknown on value %q", s)
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
