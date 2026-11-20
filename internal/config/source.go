package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// The deploy repo and how skipper follows it: where it comes from, where the
// clone and its stacks live, and what triggers a sync.

// defaultBranch is the branch tracked when branch is omitted.
const defaultBranch = "main"

// defaultReconcileIntervalSeconds is the git sync + deploy cadence applied when
// reconcile_interval_seconds is omitted (ADR-0028). On by default so skipper
// self-corrects a missed webhook out of the box; an explicit 0 disables it.
const defaultReconcileIntervalSeconds = 300

// applySourceDefaults fills the omitted repo and sync fields.
func (c *Config) applySourceDefaults() {
	if c.RepoDir == "" {
		// Applied here (not just downstream in internal/git) so stacks_base_dir
		// resolves against the effective clone path.
		c.RepoDir = DefaultRepoDir
	}
	if c.Branch == "" {
		c.Branch = defaultBranch
	}
	if c.ReconcileIntervalSeconds == nil {
		d := defaultReconcileIntervalSeconds
		c.ReconcileIntervalSeconds = &d
	}
}

// validateSource checks the repo and sync fields; defaults are applied first.
func (c *Config) validateSource() error {
	if c.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}
	// An empty webhook_secret only disables /webhook (reconcile is the
	// convergence baseline), but with reconcile off too nothing would deploy
	// past the startup sync.
	if c.WebhookSecret == "" && c.ReconcileIntervalSeconds != nil && *c.ReconcileIntervalSeconds == 0 {
		return fmt.Errorf("webhook_secret is empty (which disables the /webhook endpoint) and reconcile_interval_seconds is 0, so nothing would deploy after startup — set webhook_secret to enable push-triggered deploys, or set reconcile_interval_seconds > 0 (default 300) to converge on a timer")
	}
	if c.ReconcileIntervalSeconds != nil && *c.ReconcileIntervalSeconds < 0 {
		return fmt.Errorf("reconcile_interval_seconds must be >= 0, got %d", *c.ReconcileIntervalSeconds)
	}
	// repo_dir is used verbatim for git clone/pull; a relative value would
	// resolve against skipper's own process cwd, not the intended location.
	if !filepath.IsAbs(c.RepoDir) {
		return fmt.Errorf("repo_dir %q must be an absolute path (start it with \"/\"), or leave it empty to use the default %s", c.RepoDir, DefaultRepoDir)
	}
	// repo_web_url becomes the href of every commit link in the UI, so a value
	// the browser would not follow as a web page (or would follow into a
	// script URL) must not reach the page.
	if c.RepoWebURL != "" {
		if u, err := url.Parse(c.RepoWebURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("repo_web_url %q must be an http(s) URL of the repository's page on the forge, e.g. \"https://forge.example.com/owner/repo\" — or leave it empty to derive it from repo_url", c.RepoWebURL)
		}
	}
	// stacks_base_dir is relative to repo_dir; an absolute value or a "../"
	// escape could point outside the clone, breaking invariant #1.
	if filepath.IsAbs(c.StacksBaseDir) {
		return fmt.Errorf("stacks_base_dir %q must be relative to repo_dir (the repo clone), not absolute — drop the leading \"/\" and the repo_dir prefix (e.g. \"stacks\" for <repo_dir>/stacks)", c.StacksBaseDir)
	}
	if rel := filepath.Clean(c.StacksBaseDir); rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("stacks_base_dir %q must stay inside repo_dir (the repo clone) — it must not escape via \"../\"", c.StacksBaseDir)
	}
	return nil
}

// resolveStacksBaseDir makes stacks_base_dir absolute once validation has
// accepted the raw value, so every downstream consumer joins it verbatim. An
// empty value resolves to the repo root itself.
func (c *Config) resolveStacksBaseDir() {
	c.StacksBaseDir = filepath.Join(c.RepoDir, c.StacksBaseDir)
}
