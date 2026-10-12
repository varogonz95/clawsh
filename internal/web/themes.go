package web

import (
	"fmt"
	"strings"
)

// Theme is a named look; its colours live in static/app.css under
// :root[data-theme="<ID>"].
type Theme struct{ ID, Name string }

var Themes = []Theme{{"clawsh", "clawsh"}, {"vscode", "VS Code"}, {"ayu", "Ayu"}}

// Appearances are the values of data-mode and of --appearance.
var Appearances = []string{"dark", "light", "system"}

const (
	DefaultTheme      = "clawsh"
	DefaultAppearance = "dark"
)

// LookupTheme matches s against theme IDs and display names, ignoring case
// and surrounding space.
func LookupTheme(s string) (Theme, bool) {
	s = strings.TrimSpace(s)
	for _, t := range Themes {
		if strings.EqualFold(s, t.ID) || strings.EqualFold(s, t.Name) {
			return t, true
		}
	}
	return Theme{}, false
}

// ParseAppearance normalizes an --appearance value.
func ParseAppearance(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, a := range Appearances {
		if a == s {
			return a, true
		}
	}
	return "", false
}

// ResolveLook resolves --theme and --appearance, returning an error that lists
// the valid values. Empty strings mean the defaults.
func ResolveLook(theme, appearance string) (Theme, string, error) {
	if strings.TrimSpace(theme) == "" {
		theme = DefaultTheme
	}
	if strings.TrimSpace(appearance) == "" {
		appearance = DefaultAppearance
	}
	t, ok := LookupTheme(theme)
	if !ok {
		ids := make([]string, len(Themes))
		for i, t := range Themes {
			ids[i] = t.ID
		}
		return Theme{}, "", fmt.Errorf("unknown theme %q (valid: %s)", theme, strings.Join(ids, ", "))
	}
	a, ok := ParseAppearance(appearance)
	if !ok {
		return Theme{}, "", fmt.Errorf("unknown appearance %q (valid: %s)", appearance, strings.Join(Appearances, ", "))
	}
	return t, a, nil
}
