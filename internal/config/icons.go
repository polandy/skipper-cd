package config

import (
	"fmt"
	"path/filepath"
)

// The icons section: how the web UI resolves and caches stack icons.

// IconsConfig configures how the web UI resolves and caches stack icons.
type IconsConfig struct {
	// CacheDir is the on-disk directory where fetched icons are cached.
	// Defaults to /var/lib/skipper/icons. Must be absolute — checked at Load.
	CacheDir string `yaml:"cache_dir"`

	// SourceURL is the icon-set base URL; icons are fetched from
	// SourceURL/<slug>.svg. Defaults to the dashboard-icons CDN.
	SourceURL string `yaml:"source_url"`
}

// Defaults for the icons section. SourceURL is the icon-set root; icons are
// fetched from <source_url>/<format>/<slug>.<format> (svg, then png, webp).
const (
	defaultIconCacheDir  = "/var/lib/skipper/icons"
	defaultIconSourceURL = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"
)

// applyDefaults fills the omitted icons fields.
func (ic *IconsConfig) applyDefaults() {
	if ic.CacheDir == "" {
		ic.CacheDir = defaultIconCacheDir
	}
	if ic.SourceURL == "" {
		ic.SourceURL = defaultIconSourceURL
	}
}

// validate checks the icons section; defaults are applied first.
func (ic *IconsConfig) validate() error {
	// A relative override would resolve against skipper's own process cwd
	// instead of the intended cache location.
	if !filepath.IsAbs(ic.CacheDir) {
		return fmt.Errorf("icons.cache_dir %q must be an absolute path (start it with \"/\")", ic.CacheDir)
	}
	return nil
}
