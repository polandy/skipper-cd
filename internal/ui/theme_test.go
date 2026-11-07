package ui

import (
	"os"
	"reflect"
	"regexp"
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

// The favicon and the PWA icons draw the small-size ship mark, the header
// keeps the primary one: the primary mark's thin hull and half-opacity wave
// blur into mush at 16–32 px.
func TestFaviconSVG_DrawsTheSmallMarkNotTheHeaderLogo(t *testing.T) {
	favicon := faviconSVG(themeIdentityFor(uitheme.ThemeCatppuccin))
	header := embeddedAsset(t, "static/index.html")
	header = header[strings.Index(header, `<div class="brand-icon"`):]
	header = header[:strings.Index(header, "</svg>")]

	if reflect.DeepEqual(markGeometry(favicon), markGeometry(header)) {
		t.Error("the favicon draws the header's primary mark; want the small-size variant")
	}
	if strings.Contains(favicon, "opacity") {
		t.Error("the favicon still draws the half-opacity wave")
	}
}

// The PWA icon sources are hand-kept copies of the favicon's shapes with fixed
// colours; they must not drift into a third ship.
func TestPWAIconSources_DrawTheFaviconMark(t *testing.T) {
	want := markGeometry(faviconSVG(themeIdentityFor(uitheme.ThemeCatppuccin)))
	for _, name := range []string{"static/pwa-icon.svg", "static/pwa-icon-maskable.svg"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if got := markGeometry(string(data)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s shapes = %q, want the favicon's %q", name, got, want)
		}
	}
}

var (
	markShape     = regexp.MustCompile(`<(?:path|rect)\b[^>]*>`)
	geometryAttrs = regexp.MustCompile(`\b(?:d|x|y|width|height|rx|stroke-width)="[^"]*"`)
)

// markGeometry lists each ship shape's geometry attributes, colour left out,
// skipping a full-size background rect.
func markGeometry(svg string) []string {
	var shapes []string
	for _, el := range markShape.FindAllString(svg, -1) {
		if strings.Contains(el, `width="512"`) || strings.Contains(el, `width="64"`) {
			continue
		}
		shapes = append(shapes, strings.Join(geometryAttrs.FindAllString(el, -1), " "))
	}
	return shapes
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
