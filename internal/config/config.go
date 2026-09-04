// Package config handles loading and validating the skipper.yml configuration file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/polandy/skipper-cd/internal/git"
	"github.com/polandy/skipper-cd/internal/uitheme"
	"gopkg.in/yaml.v3"
)

// Config holds the full skipper-cd configuration.
type Config struct {
	// RepoURL is the Git remote URL. skipper-cd clones it to RepoDir and
	// runs git pull on every incoming webhook.
	RepoURL string `yaml:"repo_url"`

	// RepoDir is the local directory where the repository is cloned.
	// Defaults to /var/lib/skipper/repo when left empty. When set, it must be
	// absolute — checked at Load, since internal/git uses it verbatim.
	RepoDir string `yaml:"repo_dir"`

	// RepoWebURL is the repository's page on the forge, e.g.
	// https://forge.example.com/owner/repo — the UI links every commit SHA it
	// shows to <RepoWebURL>/commit/<sha>. Optional: when empty it is derived
	// from RepoURL, which covers the usual case where the clone URL and the web
	// UI share a host. Set it when they do not (a mirror, an SSH host that is
	// not the web host, a clone from a local path). Must be http(s) — checked
	// at Load, since the value ends up in a link the browser follows.
	RepoWebURL string `yaml:"repo_web_url"`

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

	// UpdateCheck configures the read-only registry update check (ADR-0054).
	// Optional — omitting the section runs the check at the defaults (on,
	// every 6h); update_check.interval_seconds: 0 disables it.
	UpdateCheck *UpdateCheck `yaml:"update_check"`

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

	// UITheme selects the web UI's colour palette (see uitheme.ValidThemes).
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

// EffectiveRepoWebURL returns the forge browse URL the UI links commit SHAs
// through: the explicit repo_web_url when set, else one derived from repo_url.
// Empty when neither yields one — the UI then shows SHAs as plain text. Any
// trailing slash is trimmed so callers append a path verbatim.
func (c *Config) EffectiveRepoWebURL() string {
	if c.RepoWebURL != "" {
		return strings.TrimRight(c.RepoWebURL, "/")
	}
	return git.WebURL(c.RepoURL)
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

// stackFlag resolves a per-stack boolean policy: the stack's own override when
// it sets one, otherwise def. A name that is not in stacks falls back to def
// too, so a removed or parked stack answers with the global default rather
// than a stale override.
func stackFlag(stacks []Stack, name string, override func(Stack) *bool, def bool) bool {
	for _, s := range stacks {
		if s.Name == name {
			if v := override(s); v != nil {
				return *v
			}
			break
		}
	}
	return def
}

// SelfHealEnabled reports whether self-heal is effective for the named stack:
// the per-stack override when set, otherwise the global default (off when
// unset). An unknown name falls back to the global default.
func (c *Config) SelfHealEnabled(name string) bool {
	return c.EffectiveSelfHeal(c.Stacks, name)
}

// EffectiveSelfHeal is SelfHealEnabled over an explicit stack set. Callers in
// stack-discovery mode pass the discovered stacks, which carry the host
// config's per-stack overrides merged in (ADR-0043).
func (c *Config) EffectiveSelfHeal(stacks []Stack, name string) bool {
	return stackFlag(stacks, name, func(s Stack) *bool { return s.SelfHeal }, c.SelfHeal != nil && *c.SelfHeal)
}

// RollbackEnabled reports whether automatic rollback is effective for the named
// stack: the per-stack override when set, otherwise the global default, which
// is on unless explicitly disabled. An unknown name falls back to the global
// default. See ADR-0050.
func (c *Config) RollbackEnabled(name string) bool {
	return c.EffectiveRollback(c.Stacks, name)
}

// EffectiveRollback is RollbackEnabled over an explicit stack set (see
// EffectiveSelfHeal). The default is on: rollback happens unless a per-stack
// or the global rollback is explicitly false.
func (c *Config) EffectiveRollback(stacks []Stack, name string) bool {
	return stackFlag(stacks, name, func(s Stack) *bool { return s.Rollback }, c.Rollback == nil || *c.Rollback)
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
		cfg.UITheme = uitheme.ThemeCatppuccin
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

// TCP port bounds for the webhook/UI and metrics listeners.
const (
	minPort = 1
	maxPort = 65535
)
