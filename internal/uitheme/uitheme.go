// Package uitheme holds the UI theme names and palette constants shared by
// internal/config (validation, defaults) and internal/ui (rendering). It is a
// leaf package so validating a config field does not drag in the embedded-asset
// UI package.
package uitheme

import "slices"

// Built-in UI theme names, accepted by the ui_theme config field and by
// ui.IndexHandler/ui.ManifestHandler. See internal/ui/UI_SPEC.md and
// docs/configuration.md.
const (
	ThemeCatppuccin = "catppuccin"
	ThemeNord       = "nord"
	ThemeSolarized  = "solarized"
	ThemeGruvbox    = "gruvbox"
	ThemeRosePine   = "rose-pine"
	ThemeFlake      = "flake"
)

// ValidThemes lists every built-in palette, in the order shown in docs.
var ValidThemes = []string{ThemeCatppuccin, ThemeNord, ThemeSolarized, ThemeGruvbox, ThemeRosePine, ThemeFlake}

// HostColorCount is how many distinct per-host identity colours the merged
// multi-host UI provides (ADR-0048): each host's name is hashed deterministically
// onto one of these slots, so beyond this many hosts two will share a colour.
// Must stay in sync with HOST_COLOR_COUNT in internal/ui/static/app-helpers.js
// (the JS that assigns the slot) and the per-theme `[data-host-color="0..N-1"]`
// rules in internal/ui/static/app.css.
const HostColorCount = 6

// IsValidTheme reports whether name is a recognised built-in palette.
func IsValidTheme(name string) bool {
	return slices.Contains(ValidThemes, name)
}
