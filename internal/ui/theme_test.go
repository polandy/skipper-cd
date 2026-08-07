package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/uitheme"
)

// A theme is only complete when three places agree: uitheme.ValidThemes (what
// the config accepts), themeIdentities (the favicon + PWA colours) and the
// stylesheet's palette blocks (everything the page itself renders). The config
// accepting a name whose palette is missing would serve an unstyled UI, so each
// side is asserted against ValidThemes rather than against a hand-kept list.

func TestThemeIdentityFor_CoversEveryBuiltInTheme(t *testing.T) {
	for _, theme := range uitheme.ValidThemes {
		t.Run(theme, func(t *testing.T) {
			id, ok := themeIdentities[theme]
			if !ok {
				t.Fatalf("no themeIdentity for built-in theme %q — themeIdentityFor would silently fall back to Catppuccin", theme)
			}
			v := reflect.ValueOf(id)
			for i := range v.NumField() {
				if v.Field(i).String() == "" {
					t.Errorf("themeIdentity[%q].%s is empty", theme, v.Type().Field(i).Name)
				}
			}
		})
	}
}

func TestThemeIdentityFor_FallsBackForUnknownTheme(t *testing.T) {
	if got := themeIdentityFor("monokai"); got != themeIdentities[uitheme.ThemeCatppuccin] {
		t.Errorf("themeIdentityFor(unknown) = %+v, want the Catppuccin identity", got)
	}
}

func TestStylesheet_DefinesAPaletteForEveryBuiltInTheme(t *testing.T) {
	sheet := embeddedAsset(t, "static/app.css")

	for _, theme := range uitheme.ValidThemes {
		t.Run(theme, func(t *testing.T) {
			// Dark is each theme's stylesheet default, .light the toggle's
			// variant; both must exist or the toggle lands on an unstyled page.
			for _, variant := range []string{"", ".light"} {
				if !declaresToken(sheet, theme, variant, "--raw-accent:") {
					t.Errorf("app.css has no palette block declaring --raw-accent for %q%s", theme, variant)
				}
				// Per-host identity slots (ADR-0048) are per-theme too: without
				// them every host chip renders unpainted in multi-host view.
				// --host-5 is checked, so a truncated set fails as well.
				if !declaresToken(sheet, theme, variant, "--host-5:") {
					t.Errorf("app.css defines no --host-0…5 slots for %q%s", theme, variant)
				}
			}
		})
	}
}

func TestIndexHTML_OffersEveryBuiltInThemeInThePicker(t *testing.T) {
	html := embeddedAsset(t, "static/index.html")

	for _, theme := range uitheme.ValidThemes {
		if !strings.Contains(html, `<option value="`+theme+`">`) {
			t.Errorf("the theme picker has no <option> for built-in theme %q", theme)
		}
	}
}

func embeddedAsset(t *testing.T, name string) string {
	t.Helper()
	data, err := staticFS.ReadFile(name)
	if err != nil {
		t.Fatalf("reading the embedded %s: %v", name, err)
	}
	return string(data)
}

// declaresToken reports whether any rule selecting the theme (and variant)
// declares token. The palette is split across several such blocks — the base
// colours and the per-host slots each have their own — so every matching block
// is searched, not just the first.
func declaresToken(sheet, theme, variant, token string) bool {
	selector := `:root[data-theme="` + theme + `"]` + variant + ` {`
	for i := 0; ; {
		start := strings.Index(sheet[i:], selector)
		if start < 0 {
			return false
		}
		start += i
		end := strings.Index(sheet[start:], "}")
		if end < 0 {
			return false
		}
		if strings.Contains(sheet[start:start+end], token) {
			return true
		}
		i = start + len(selector)
	}
}
