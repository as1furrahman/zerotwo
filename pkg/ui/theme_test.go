package ui

import (
	"os"
	"strings"
	"testing"
)

func TestThemeSwitching(t *testing.T) {
	for _, theme := range AvailableThemes() {
		ok := SetTheme(theme)
		if !ok {
			t.Errorf("failed to set theme %s", theme)
		}
		if CurrentTheme != ThemeStyle(theme) {
			t.Errorf("expected theme %s, got %s", theme, CurrentTheme)
		}
	}
	SetTheme("minimal")
}

func TestThemeWidthStrictness(t *testing.T) {
	_ = os.Setenv("COLUMNS", "48")
	defer os.Unsetenv("COLUMNS")

	for _, theme := range AvailableThemes() {
		mockup := GenerateDemoMockup(ThemeStyle(theme))
		lines := strings.Split(mockup, "\n")
		for idx, l := range lines {
			w := StringWidth(l)
			if w > 44 {
				t.Errorf("Theme [%s] line %d exceeded 44 columns: w=%d text=%q", theme, idx+1, w, StripANSI(l))
			}
		}
	}
}
