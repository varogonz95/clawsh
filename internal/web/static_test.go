//go:build !windows

package web

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Every file referenced by index.html and app.css must be embedded and served.
func TestStaticAssetsServed(t *testing.T) {
	_, base := startServer(t, noHosts(t))
	c := login(t, base)

	read := func(path string) string {
		resp, err := c.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %s", path, resp.Status)
		}
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	index := read("/")
	refs := regexp.MustCompile(`(?:src|href)="(/static/[^"]+)"`).FindAllStringSubmatch(index, -1)
	if len(refs) < 5 {
		t.Fatalf("index.html references only %d static files", len(refs))
	}
	for _, m := range refs {
		read(m[1])
	}
	css := read("/static/app.css")
	for _, m := range regexp.MustCompile(`url\('([^']+)'\)`).FindAllStringSubmatch(css, -1) {
		read("/static/" + m[1])
	}
}

// The page must not be framed by another site (clickjacking).
func TestIndexNotFramable(t *testing.T) {
	_, base := startServer(t, noHosts(t))
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
		t.Errorf("Content-Security-Policy = %q, want frame-ancestors 'none'", got)
	}
}

// The duplicate-session route and its helpers must ship in app.js.
func TestAppHasDuplicate(t *testing.T) {
	_, base := startServer(t, noHosts(t))
	c := login(t, base)
	resp, err := c.Get(base + "/static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"'dup'", "nextName", "joinArgs", "splitArgs"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %s", want)
		}
	}
}
