// Package config handles loading and validating the skipper.yml configuration file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/ui"
)

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

// HealthCheck configures the optional post-deploy health gate of a stack.
// When present, `docker compose up` runs with --wait so it fails when the
// services' compose healthchecks do not turn healthy in time, and an optional
// HTTP probe additionally verifies the stack from the outside. Either failure
// triggers the regular rollback path (ADR-0004, ADR-0022).
//
// In YAML deploy_health_check accepts either a mapping (the fields below) or a
// boolean scalar (ADR-0049): `false` explicitly disables the gate — overriding
// the automatic compose-`healthcheck:` gate of ADR-0046 — and `true` enables it
// at the defaults (equivalent to an empty mapping `{}`). See UnmarshalYAML.
type HealthCheck struct {
	// Enabled is the off/on switch (ADR-0049). nil means the gate was given as a
	// mapping with no enabled: key (or defaulted on); a non-nil false is an
	// explicit opt-out that suppresses the ADR-0046 automatic gate. Usually set
	// via the boolean-scalar form (see UnmarshalYAML); the yaml tag also lets a
	// mapping set it and, more importantly, carries it into the ConfigHash so
	// toggling the opt-out redeploys the stack. See IsDisabled.
	Enabled *bool `yaml:"enabled,omitempty"`

	// TimeoutSeconds bounds the wait: it is passed as --wait-timeout to
	// docker compose up and is also the deadline of the HTTP probe.
	// Defaults to 60.
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// URL, when set, is HTTP-GET-probed after a successful up until it
	// answers 2xx; anything else within TimeoutSeconds rolls the deploy back.
	URL string `yaml:"url"`
}

// UnmarshalYAML lets deploy_health_check be written as a boolean scalar as well
// as a mapping (ADR-0049): `false` records an explicit opt-out (Enabled=false),
// `true` records an explicit opt-in at the defaults (Enabled=true), and any
// mapping decodes into the fields normally. A non-boolean scalar is an error.
func (hc *HealthCheck) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var enabled bool
		if err := value.Decode(&enabled); err != nil {
			return fmt.Errorf("deploy_health_check must be a mapping or a boolean (true/false), got %q", value.Value)
		}
		hc.Enabled = &enabled
		return nil
	}
	// Decode the mapping without recursing back into this method.
	type rawHealthCheck HealthCheck
	var raw rawHealthCheck
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*hc = HealthCheck(raw)
	return nil
}

// IsDisabled reports whether the gate was explicitly turned off with
// deploy_health_check: false (ADR-0049). A nil receiver (the gate is absent)
// is not "disabled" — absence leaves the automatic compose-`healthcheck:` gate
// of ADR-0046 free to apply.
func (hc *HealthCheck) IsDisabled() bool {
	return hc != nil && hc.Enabled != nil && !*hc.Enabled
}

// Config holds the full skipper-cd configuration.
type Config struct {
	// RepoURL is the Git remote URL. skipper-cd clones it to RepoDir and
	// runs git pull on every incoming webhook.
	RepoURL string `yaml:"repo_url"`

	// RepoDir is the local directory where the repository is cloned.
	// Defaults to /var/lib/skipper/repo when left empty. When set, it must be
	// absolute — checked at Load, since internal/git uses it verbatim.
	RepoDir string `yaml:"repo_dir"`

	// Branch is the Git branch to track. Defaults to "main".
	Branch string `yaml:"branch"`

	// VarsFile is an optional path to a KEY=VALUE env file whose entries are
	// injected into the environment of every stack deployment, enabling
	// ${VAR} substitution in docker-compose.yml (e.g. for domain names). When
	// set, it must exist and be readable — checked at Load, since it is a host
	// path available before any repo clone.
	VarsFile string `yaml:"vars_file"`

	// CommandTimeoutSeconds is the maximum number of seconds a single shell
	// command (docker compose pull/up, git clone/fetch) is allowed to run
	// before being killed. Defaults to 300 (5 minutes).
	CommandTimeoutSeconds int `yaml:"command_timeout_seconds"`

	// LogFormat selects the log output format: "pretty" (colored, icon-led
	// console output, the default), "text" (logfmt) or "json" for structured
	// logs (e.g. for Loki ingestion).
	LogFormat string `yaml:"log_format"`

	// StacksBaseDir is the directory inside the repo clone that holds one
	// subdirectory per stack (<stacks_base_dir>/<name>/docker-compose.yml).
	// Change detection and the compose file always come from here. It is a path
	// relative to repo_dir (the repo clone) — Load resolves it to an absolute
	// path against the effective repo_dir, so downstream consumers join it
	// verbatim. An empty value means the repo root itself. An absolute value is
	// rejected at Load.
	StacksBaseDir string `yaml:"stacks_base_dir"`

	// ProjectDirectoryBase is an optional base directory from which a stack's
	// project_directory is derived as <project_directory_base>/<name> when the
	// stack does not set its own (ADR-0045). Mirrors stacks_base_dir's role for
	// the compose path, but for --project-directory: it avoids repeating a
	// common prefix (e.g. a NixOS modules directory) across every stack. Must
	// be an absolute path when set.
	ProjectDirectoryBase string `yaml:"project_directory_base"`

	// WebhookSecret is the shared HMAC-SHA256 secret push webhooks are signed
	// with (Gitea X-Gitea-Signature / GitHub X-Hub-Signature-256). Optional: the
	// reconcile loop is skipper's convergence baseline and the webhook only
	// accelerates it, so leaving this empty is valid — it disables the /webhook
	// endpoint (which then rejects every request) rather than erroring. When set,
	// every request is signature-verified. Load rejects only the dead
	// combination of an empty secret AND reconcile disabled, where nothing would
	// deploy after startup.
	WebhookSecret string `yaml:"webhook_secret"`

	// Port is the webhook/UI HTTP port. Defaults to 8080.
	Port int `yaml:"port"`

	// MetricsPort is the Prometheus /metrics HTTP port. Defaults to 9120.
	MetricsPort int `yaml:"metrics_port"`

	// Stacks lists the Docker Compose projects to deploy. When stack_discovery
	// is false this list is the stack set; under discovery it is the optional
	// per-stack override list (ADR-0043), matched to discovered directories by name.
	Stacks []Stack `yaml:"stacks"`

	// StackDiscovery discovers the stack set from the deploy repo on every sync
	// (ADR-0034): every direct subdirectory of stacks_base_dir containing a
	// docker-compose.yml is a stack; per-stack overrides come from the Stacks
	// list (ADR-0043). Defaults to true (an omitted key enables discovery);
	// requires stacks_base_dir. Set false to list the stacks in the config
	// yourself. The zero value is false, so a directly-constructed Config is
	// not in discovery mode unless it opts in.
	StackDiscovery bool `yaml:"stack_discovery"`

	// UIEnabled serves the web UI (dashboard, event history, UI API) on the
	// webhook port. nil (omitted) defaults to true; set an explicit false to
	// run headless. Load normalizes it, so it is never nil after Load.
	UIEnabled *bool `yaml:"ui_enabled"`

	// Autosync is the global default for whether detected changes deploy
	// automatically. nil means true (on). A per-stack Autosync overrides it.
	// See docs/autosync.md.
	Autosync *bool `yaml:"autosync"`

	// Rollback is the global default for whether a failed deploy is rolled back
	// to the previous compose version. nil means true (on). A per-stack Rollback
	// overrides it. See RollbackEnabled and ADR-0050.
	Rollback *bool `yaml:"rollback"`

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

	// HostName is this instance's own display label and identity key in the
	// merged multi-host UI (ADR-0048) — the name shown on its host badge, the
	// key its per-host colour is derived from, and its entry in the Hosts
	// filter. Optional; Load defaults it to the OS hostname. Only meaningful
	// when peers are configured (or a peer fans this instance in), but harmless
	// otherwise.
	HostName string `yaml:"host_name"`

	// Peers lists other skipper instances whose read data this instance fans
	// in and renders in one merged UI (ADR-0048). Optional; empty means this
	// instance shows only its own stacks. Only the primary needs a peers list;
	// peers themselves need no reciprocal config. The UI reads the effective
	// set (local + peers, with reachability) via /api/peers.
	Peers []Peer `yaml:"peers"`

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

	// RuntimeHealthPollIntervalSeconds sets how often skipper polls its stacks'
	// runtime health (ADR-0027). nil (omitted) defaults to 30; an explicit 0
	// disables the health view. Only meaningful with ui_enabled; the poll
	// additionally runs only while a UI client is connected. See
	// docs/configuration.md.
	RuntimeHealthPollIntervalSeconds *int `yaml:"runtime_health_poll_interval_seconds"`

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
	// it. Runs headless like reconcile, so it needs runtime_health_poll_interval_seconds
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

	// Warnings lists non-fatal issues found while loading the config — valid
	// but suspicious setups that don't warrant refusing to start. Populated by
	// Load; never read from YAML. The caller is expected to log each one.
	Warnings []string `yaml:"-"`
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

// RollbackEnabled reports whether automatic rollback is effective for the named
// stack: the per-stack override when set, otherwise the global default, which
// is on unless explicitly disabled. An unknown name falls back to the global
// default. See ADR-0050.
func (c *Config) RollbackEnabled(name string) bool {
	return c.EffectiveRollback(c.Stacks, name)
}

// EffectiveRollback is RollbackEnabled over an explicit stack set — the
// discovered stacks in stack-discovery mode, where c.Stacks is empty
// (ADR-0034). The default is on: rollback happens unless a per-stack or the
// global rollback is explicitly false.
func (c *Config) EffectiveRollback(stacks []Stack, name string) bool {
	for _, s := range stacks {
		if s.Name == name {
			if s.Rollback != nil {
				return *s.Rollback
			}
			break
		}
	}
	return c.Rollback == nil || *c.Rollback
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
// the shared health poller's cadence (runtime_health_poll_interval_seconds), the same
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
	// Defaults to /var/lib/skipper/icons. Must be absolute — checked at Load.
	CacheDir string `yaml:"cache_dir"`

	// SourceURL is the icon-set base URL; icons are fetched from
	// SourceURL/<slug>.svg. Defaults to the dashboard-icons CDN.
	SourceURL string `yaml:"source_url"`
}

// NixOSRebuild configures automatic NixOS rebuilds when .nix files or
// flake.lock change in the repository.
type NixOSRebuild struct {
	Enabled *bool  `yaml:"enabled"` // nil = true when section present
	Flake   string `yaml:"flake"`   // e.g. ".#host-a", required when enabled
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

	// Retired keys are checked before decoding so an un-migrated config is told
	// what its setting is called now, rather than getting the generic
	// unknown-key error the strict decode below would produce.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	if err := checkRenamedKeys(&doc); err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:        8080,
		MetricsPort: 9120,
	}
	// KnownFields rejects keys the config has no field for. A typo (env_file
	// for env_files) or a key left over from an older version would otherwise
	// be dropped in silence, and skipper would run with a setting the operator
	// believes is in effect. Nested structs inherit the strictness, except
	// deploy_health_check, which decodes through its own UnmarshalYAML.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		// io.EOF is an empty document — no keys to apply, defaults stand.
		return nil, fmt.Errorf("parse config file: %w — check the key spelling and value types against docs/configuration.md; unknown keys are rejected rather than ignored", err)
	}

	// stack_discovery defaults to true (ADR-0043). The field is a plain bool, so
	// probe for an omitted key (vs an explicit false) to apply the default.
	var probe struct {
		StackDiscovery *bool `yaml:"stack_discovery"`
	}
	if yaml.Unmarshal(data, &probe) == nil && probe.StackDiscovery == nil {
		cfg.StackDiscovery = true
	}

	if cfg.RepoDir == "" {
		// Applied up front (not just downstream in internal/git) so stacks_base_dir
		// can be resolved against the effective clone path below.
		cfg.RepoDir = git.DefaultRepoDir
	}
	if cfg.CommandTimeoutSeconds == 0 {
		cfg.CommandTimeoutSeconds = 300
	}
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	if cfg.LogFormat == "" {
		cfg.LogFormat = LogFormatPretty
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
	if cfg.HostName == "" {
		// Default the self-identity label to the OS hostname so the merged
		// multi-host UI (ADR-0048) has a stable name without extra config; an
		// explicit host_name overrides it.
		if h, err := os.Hostname(); err == nil {
			cfg.HostName = h
		}
	}
	if cfg.UIEnabled == nil {
		d := true
		cfg.UIEnabled = &d
	}
	if cfg.RuntimeHealthPollIntervalSeconds == nil {
		d := defaultRuntimeHealthPollIntervalSeconds
		cfg.RuntimeHealthPollIntervalSeconds = &d
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
		if cfg.Stacks[i].ProjectDirectory == "" && cfg.ProjectDirectoryBase != "" {
			cfg.Stacks[i].ProjectDirectory = filepath.Join(cfg.ProjectDirectoryBase, cfg.Stacks[i].Name)
		}
		if hc := cfg.Stacks[i].DeployHealthCheck; hc != nil && !hc.IsDisabled() && hc.TimeoutSeconds == 0 {
			hc.TimeoutSeconds = DefaultHealthCheckTimeoutSeconds
		}
	}

	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	// stacks_base_dir is relative to the repo clone (repo_dir); resolve it to an
	// absolute path once validation has accepted the raw value, so every
	// downstream consumer keeps joining it verbatim. An empty value resolves to
	// the repo root itself.
	cfg.StacksBaseDir = filepath.Join(cfg.RepoDir, cfg.StacksBaseDir)

	cfg.Warnings = collectWarnings(cfg)
	return cfg, nil
}

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

// DefaultHealthCheckTimeoutSeconds is applied when a deploy_health_check
// section is present without an explicit timeout_seconds, and by
// internal/deploy's automatic gate for a stack that declares no
// deploy_health_check but whose compose file has one (ADR-0046).
const DefaultHealthCheckTimeoutSeconds = 60

// defaultRuntimeHealthPollIntervalSeconds is the UI stack-health poll cadence applied
// when runtime_health_poll_interval_seconds is omitted (ADR-0027). An explicit 0
// disables the health view.
const defaultRuntimeHealthPollIntervalSeconds = 30

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
