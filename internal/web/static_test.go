//go:build !windows

package web

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/varogonz95/clawsh/internal/api"
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

func TestIndexCarriesVersion(t *testing.T) {
	_, base := startServer(t, noHosts(t))
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	index := string(b)
	if want := `<meta name="clawsh-version" content="` + api.Version + `">`; !strings.Contains(index, want) {
		t.Errorf("index lacks %s", want)
	}
	refs := regexp.MustCompile(`="(/static/[^"]+)"`).FindAllStringSubmatch(index, -1)
	if len(refs) < 5 {
		t.Fatalf("only %d static refs", len(refs))
	}
	for _, m := range refs {
		if !strings.HasSuffix(m[1], "?v="+url.QueryEscape(api.Version)) {
			t.Errorf("%s has no version query", m[1])
		}
	}
}

func TestStaticRevalidates(t *testing.T) {
	_, base := startServer(t, noHosts(t))
	resp, err := http.Get(base + "/static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	etag := resp.Header.Get("ETag")
	if resp.Header.Get("Cache-Control") != "no-cache" || etag == "" {
		t.Fatalf("Cache-Control = %q, ETag = %q", resp.Header.Get("Cache-Control"), etag)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/static/app.js", nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304", resp2.StatusCode)
	}
}
