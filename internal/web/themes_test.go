package web

import (
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestLookupTheme(t *testing.T) {
	for _, s := range []string{"vscode", "VSCODE", " VS Code ", "vs code"} {
		if th, ok := LookupTheme(s); !ok || th.ID != "vscode" {
			t.Errorf("LookupTheme(%q) = %v, %v", s, th, ok)
		}
	}
	if _, ok := LookupTheme("nope"); ok {
		t.Error("nope resolved")
	}
}

func TestResolveLookErrors(t *testing.T) {
	if _, _, err := ResolveLook("nope", ""); err == nil || !strings.Contains(err.Error(), "clawsh, vscode, ayu") {
		t.Errorf("theme error = %v", err)
	}
	if _, _, err := ResolveLook("", "dim"); err == nil || !strings.Contains(err.Error(), "dark, light, system") {
		t.Errorf("appearance error = %v", err)
	}
	th, a, err := ResolveLook("", "")
	if err != nil || th.ID != "clawsh" || a != "dark" {
		t.Errorf("defaults = %v %v %v", th, a, err)
	}
	if _, err := New(Options{Secret: "k", ConfigPath: "/nonexistent/x.toml", Theme: "nope"}); err == nil {
		t.Error("New accepted an unknown theme")
	}
}

func indexFor(t *testing.T, theme, appearance string) string {
	t.Helper()
	s, err := New(Options{Secret: "k", ConfigPath: t.TempDir() + "/hosts.toml", Theme: theme, Appearance: appearance})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.serveIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Body.String()
}

func TestIndexLook(t *testing.T) {
	got := indexFor(t, "ayu", "light")
	for _, w := range []string{`<html lang="en" data-theme="ayu" data-mode="light">`, `<meta name="color-scheme" content="light">`} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %s", w)
		}
	}
	if got := indexFor(t, "VS Code", "system"); !strings.Contains(got, `data-theme="vscode" data-mode="system"`) ||
		!strings.Contains(got, `content="dark light"`) {
		t.Errorf("system look: %s", got[:300])
	}
	if got := indexFor(t, "", ""); !strings.Contains(got, `data-theme="clawsh" data-mode="dark"`) ||
		!strings.Contains(got, `content="dark"`) {
		t.Error("defaults not applied")
	}
}

func cssBlock(t *testing.T, css, sel string) map[string]string {
	t.Helper()
	i := strings.Index(css, sel+" {")
	if i < 0 {
		t.Fatalf("no CSS block %s", sel)
	}
	body := css[i+len(sel)+2:]
	body = body[:strings.Index(body, "}")]
	m := map[string]string{}
	for _, mm := range regexp.MustCompile(`--([\w-]+):\s*([^;]+);`).FindAllStringSubmatch(body, -1) {
		m[mm[1]] = strings.TrimSpace(mm[2])
	}
	return m
}

func lum(hex string) float64 {
	c := func(s string) float64 {
		v, _ := strconv.ParseInt(s, 16, 0)
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	h := strings.TrimPrefix(hex, "#")
	return 0.2126*c(h[0:2]) + 0.7152*c(h[2:4]) + 0.0722*c(h[4:6])
}

func contrast(a, b string) float64 {
	x, y := lum(a), lum(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

func TestThemeCSS(t *testing.T) {
	b, err := staticFiles.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	for _, th := range Themes {
		for _, mode := range []string{"dark", "light"} {
			sel := `:root[data-theme="` + th.ID + `"][data-mode="` + mode + `"]`
			v := map[string]string{}
			if th.ID == "clawsh" && mode == "dark" {
				v = cssBlock(t, css, ":root")
			} else {
				v = cssBlock(t, css, sel)
			}
			for _, k := range []string{"ground", "ink", "term-bg", "term-fg"} {
				if v[k] == "" {
					t.Errorf("%s: missing --%s", sel, k)
				}
			}
			if n := len(strings.Split(v["term-ansi"], ",")); n != 16 {
				t.Errorf("%s: --term-ansi has %d colours", sel, n)
			}
			for _, bg := range []string{"ground", "panel"} {
				for _, fg := range []string{"ink", "ink-2", "ink-soft", "link"} {
					if c := contrast(v[fg], v[bg]); c < 4.5 {
						t.Errorf("%s: --%s on --%s contrast %.2f < 4.5", sel, fg, bg, c)
					}
				}
			}
		}
	}
}
