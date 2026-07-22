package ui

import (
	"encoding/base64"
	"slices"
)

// Built-in UI theme names, accepted by the ui_theme config field and by
// IndexHandler/ManifestHandler. See internal/ui/UI_SPEC.md and
// docs/configuration.md.
const (
	ThemeCatppuccin = "catppuccin"
	ThemeNord       = "nord"
	ThemeSolarized  = "solarized"
	ThemeGruvbox    = "gruvbox"
	ThemeRosePine   = "rose-pine"
)

// ValidThemes lists every built-in palette, in the order shown in docs.
var ValidThemes = []string{ThemeCatppuccin, ThemeNord, ThemeSolarized, ThemeGruvbox, ThemeRosePine}

// HostColorCount is how many distinct per-host identity colours the merged
// multi-host UI provides (ADR-0048): each host's name is hashed deterministically
// onto one of these slots, so beyond this many hosts two will share a colour.
// Must stay in sync with HOST_COLOR_COUNT in static/app-helpers.js (the JS that
// assigns the slot) and the per-theme `[data-host-color="0..N-1"]` rules in
// static/app.css.
const HostColorCount = 6

// IsValidTheme reports whether name is a recognised built-in palette.
func IsValidTheme(name string) bool {
	return slices.Contains(ValidThemes, name)
}

// themeIdentity carries the handful of raw colours needed *outside* the
// page's own CSS custom properties: the favicon (an inlined SVG with no
// access to the page's CSS) and the PWA meta/manifest colours (read by the
// OS/browser chrome before the page's stylesheet is even parsed). The full
// per-component palette lives only in index.html; this is intentionally a
// much smaller, duplicated slice of it.
type themeIdentity struct {
	darkMantle, darkBase, darkText, darkAccent, darkSuccess string
	lightBase, lightText, lightAccent, lightSuccess         string
}

var themeIdentities = map[string]themeIdentity{
	ThemeCatppuccin: {
		darkMantle: "#181825", darkBase: "#1e1e2e", darkText: "#cdd6f4", darkAccent: "#fab387", darkSuccess: "#94e2d5",
		lightBase: "#eff1f5", lightText: "#4c4f69", lightAccent: "#fe640b", lightSuccess: "#179299",
	},
	ThemeNord: {
		darkMantle: "#2e3440", darkBase: "#3b4252", darkText: "#eceff4", darkAccent: "#88c0d0", darkSuccess: "#a3be8c",
		lightBase: "#eceff4", lightText: "#2e3440", lightAccent: "#4c6f96", lightSuccess: "#4c8f6b",
	},
	ThemeSolarized: {
		darkMantle: "#002b36", darkBase: "#073642", darkText: "#93a1a1", darkAccent: "#268bd2", darkSuccess: "#2aa198",
		lightBase: "#fdf6e3", lightText: "#586e75", lightAccent: "#268bd2", lightSuccess: "#2aa198",
	},
	ThemeGruvbox: {
		darkMantle: "#282828", darkBase: "#32302f", darkText: "#ebdbb2", darkAccent: "#fe8019", darkSuccess: "#8ec07c",
		lightBase: "#fbf1c7", lightText: "#3c3836", lightAccent: "#af3a03", lightSuccess: "#427b58",
	},
	ThemeRosePine: {
		darkMantle: "#191724", darkBase: "#1f1d2e", darkText: "#e0def4", darkAccent: "#c4a7e7", darkSuccess: "#9ccfd8",
		lightBase: "#fffaf3", lightText: "#575279", lightAccent: "#907aa9", lightSuccess: "#56949f",
	},
}

// themeIdentityFor returns the identity for name, falling back to Catppuccin
// for an unrecognised value (config.Load rejects unknown names, so this only
// guards handler construction against a caller that skipped validation).
func themeIdentityFor(name string) themeIdentity {
	if id, ok := themeIdentities[name]; ok {
		return id
	}
	return themeIdentities[ThemeCatppuccin]
}

// faviconSVG renders the ship-logo favicon for a theme: a light variant (used
// by default) and a dark variant swapped in via a prefers-color-scheme media
// query — this follows the OS preference, not the in-page toggle, the same
// platform limitation the PWA theme-color metas have.
func faviconSVG(id themeIdentity) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" fill="none">` +
		`<style>svg{color:` + id.lightText + `}.a{fill:` + id.lightAccent + `}.b{fill:` + id.lightSuccess + `}` +
		`@media(prefers-color-scheme:dark){svg{color:` + id.darkText + `}.a{fill:` + id.darkAccent + `}.b{fill:` + id.darkSuccess + `}}</style>` +
		`<path d="M9 41 H55 L45 53 H19 Z" stroke="currentColor" stroke-width="3.5" stroke-linejoin="round"/>` +
		`<rect class="a" x="19" y="30" width="12" height="9" rx="2"/>` +
		`<rect x="33" y="30" width="12" height="9" rx="2" stroke="currentColor" stroke-width="3"/>` +
		`<rect class="b" x="26" y="18" width="12" height="9" rx="2"/>` +
		`<path d="M14 60 q5 -5 10 0 t10 0 t10 0 t10 0" stroke="currentColor" stroke-width="3" stroke-linecap="round" opacity=".5"/>` +
		`</svg>`
}

// faviconDataURI returns the base64-encoded data: URI for the theme's
// favicon, ready to substitute into the __FAVICON_URI__ placeholder.
func faviconDataURI(name string) string {
	svg := faviconSVG(themeIdentityFor(name))
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}
