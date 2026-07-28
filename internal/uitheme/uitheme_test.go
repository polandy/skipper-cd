package uitheme

import "testing"

func TestIsValidThemeAcceptsEveryBuiltIn(t *testing.T) {
	for _, name := range ValidThemes {
		if !IsValidTheme(name) {
			t.Errorf("IsValidTheme(%q) = false, want true", name)
		}
	}
}

func TestIsValidThemeRejectsUnknownNames(t *testing.T) {
	for _, name := range []string{"", "dracula", "Catppuccin", "rose_pine"} {
		if IsValidTheme(name) {
			t.Errorf("IsValidTheme(%q) = true, want false", name)
		}
	}
}
