//go:build !windows

package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/varogonz95/clawsh/internal/hub"
)

const secretToken = "tok-very-secret-0123456789"

func call(t *testing.T, c *http.Client, method, url string, body any) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func errOf(body string) string {
	var e struct{ Error string }
	_ = json.Unmarshal([]byte(body), &e)
	return e.Error
}

func TestConfigHostsCRUD(t *testing.T) {
	agentURL := startAgent(t, secretToken)
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	_, base := startServer(t, cfg) // no file yet
	c := login(t, base)
	api := base + "/api/config/hosts"

	code, body := call(t, c, "POST", api, hostBody{Name: "desk", URL: agentURL + "/", Token: secretToken})
	if code != 201 {
		t.Fatalf("add: %d %s", code, body)
	}
	if strings.Contains(body, secretToken) || !strings.Contains(body, `"has_token":true`) {
		t.Fatalf("add body: %s", body)
	}
	st, err := os.Stat(cfg)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("hosts.toml: %v %v", st, err)
	}
	// The overview picks it up without a restart.
	ch := subscribe(t, c, base)
	ov := next(t, ch)
	if len(ov.Hosts) != 1 || ov.Hosts[0].Name != "desk" || !ov.Hosts[0].Online {
		t.Fatalf("overview: %+v", ov)
	}

	code, body = call(t, c, "GET", api, nil)
	if code != 200 || strings.Contains(body, secretToken) || !strings.Contains(body, `"desk"`) {
		t.Fatalf("list: %d %s", code, body)
	}

	// Edit with no token keeps the stored one; rename allowed.
	code, body = call(t, c, "PUT", api+"/desk", hostBody{Name: "desk2", URL: agentURL})
	if code != 200 || strings.Contains(body, secretToken) {
		t.Fatalf("edit: %d %s", code, body)
	}
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), secretToken) || !strings.Contains(string(raw), "desk2") {
		t.Fatalf("file after edit: %s", raw)
	}
	bak, err := os.Stat(cfg + ".bak")
	if err != nil || bak.Mode().Perm() != 0o600 {
		t.Fatalf("bak: %v %v", bak, err)
	}
	if b, _ := os.ReadFile(cfg + ".bak"); !strings.Contains(string(b), `"desk"`) || strings.Contains(string(b), "desk2") {
		t.Fatalf("bak should hold the previous contents: %s", b)
	}

	if code, _ = call(t, c, "PUT", api+"/nope", hostBody{Name: "x", URL: agentURL}); code != 404 {
		t.Fatalf("edit unknown: %d", code)
	}
	if code, _ = call(t, c, "DELETE", api+"/nope", nil); code != 404 {
		t.Fatalf("delete unknown: %d", code)
	}
	if code, _ = call(t, c, "DELETE", api+"/desk2", nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, body = call(t, c, "GET", api, nil); strings.TrimSpace(body) != "[]" {
		t.Fatalf("list after delete: %d %s", code, body)
	}
	ov = next(t, ch)
	for len(ov.Hosts) != 0 {
		ov = next(t, ch)
	}
}

func TestConfigHostsValidation(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"a", "http://127.0.0.1:1", "t1"}, testHost{"b", "http://127.0.0.1:2", "t2"})
	_, base := startServer(t, cfg)
	c := login(t, base)
	api := base + "/api/config/hosts"

	bad := []struct {
		method, path string
		body         hostBody
		want         int
	}{
		{"POST", api, hostBody{Name: "a", URL: "http://x:1", Token: "t"}, 409},
		{"POST", api, hostBody{Name: "", URL: "http://x:1", Token: "t"}, 400},
		{"POST", api, hostBody{Name: "c", URL: "", Token: "t"}, 400},
		{"POST", api, hostBody{Name: "c", URL: "ftp://x:1", Token: "t"}, 400},
		{"POST", api, hostBody{Name: "c", URL: "x:1", Token: "t"}, 400},
		{"POST", api, hostBody{Name: "c", URL: "http://x:1"}, 400},
		{"PUT", api + "/a", hostBody{Name: "b", URL: "http://x:1"}, 409},
		{"PUT", api + "/a", hostBody{Name: "a", URL: "file:///etc"}, 400},
		{"PUT", api + "/a", hostBody{Name: "", URL: "http://x:1"}, 400},
	}
	for _, tc := range bad {
		code, body := call(t, c, tc.method, tc.path, tc.body)
		if code != tc.want || errOf(body) == "" {
			t.Errorf("%s %s %+v: %d %s, want %d with a JSON error", tc.method, tc.path, tc.body, code, body, tc.want)
		}
	}
	if code, _ := call(t, c, "POST", api, strings.NewReader("not json")); code != 400 {
		t.Errorf("bad json: %d", code)
	}
	// Renaming to the same name is not a duplicate.
	if code, body := call(t, c, "PUT", api+"/a", hostBody{Name: "a", URL: "http://127.0.0.1:9"}); code != 200 {
		t.Errorf("same-name edit: %d %s", code, body)
	}
}

func TestConfigHostsRefusesBrokenFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"a", "http://127.0.0.1:1", "t1"})
	_, base := startServer(t, cfg)
	c := login(t, base)
	if err := os.WriteFile(cfg, []byte("[[host]\nbroken"), 0o600); err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, cfg)
	code, _ := call(t, c, "POST", base+"/api/config/hosts", hostBody{Name: "c", URL: "http://x:1", Token: "t"})
	if code != 409 {
		t.Fatalf("add over broken file: %d", code)
	}
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), "broken") {
		t.Fatalf("broken file was overwritten: %s", b)
	}
}

func TestConfigHostsFailedWriteKeepsFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"a", "http://127.0.0.1:1", "t1"})
	before, _ := os.ReadFile(cfg)
	_, base := startServer(t, cfg)
	c := login(t, base)

	hub.RenameFile = func(string, string) error { return os.ErrPermission }
	defer func() { hub.RenameFile = os.Rename }()
	code, body := call(t, c, "POST", base+"/api/config/hosts", hostBody{Name: "c", URL: "http://x:1", Token: "t"})
	if code != 500 {
		t.Fatalf("failed write: %d %s", code, body)
	}
	after, _ := os.ReadFile(cfg)
	if !bytes.Equal(before, after) {
		t.Fatalf("file changed:\n%s", after)
	}
	entries, _ := os.ReadDir(filepath.Dir(cfg))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".hosts.toml.") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if code, body = call(t, c, "GET", base+"/api/config/hosts", nil); strings.Contains(body, `"c"`) {
		t.Fatalf("failed add visible: %d %s", code, body)
	}
}

func TestConfigHostsTest(t *testing.T) {
	agentURL := startAgent(t, secretToken)
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"good", agentURL, secretToken})
	_, base := startServer(t, cfg)
	c := login(t, base)
	url := base + "/api/config/hosts/test"

	cases := []struct {
		name string
		body hostBody
		code int
		want testResult
	}{
		{"ok", hostBody{URL: agentURL, Token: secretToken}, 200, testResult{OK: true}},
		{"saved", hostBody{Name: "good"}, 200, testResult{OK: true}},
		{"unauthorized", hostBody{URL: agentURL, Token: "wrong"}, 200, testResult{Error: "unauthorized"}},
		{"unreachable", hostBody{URL: closedURL(t), Token: "t"}, 200, testResult{Error: "unreachable"}},
		{"saved key, new url", hostBody{Name: "good", URL: agentURL}, 200, testResult{OK: true}},
		{"unknown", hostBody{Name: "nope"}, 404, testResult{}},
		{"bad url", hostBody{URL: "ftp://x"}, 400, testResult{}},
	}
	for _, tc := range cases {
		code, body := call(t, c, "POST", url, tc.body)
		if code != tc.code {
			t.Errorf("%s: %d %s", tc.name, code, body)
			continue
		}
		if code == 200 {
			var got testResult
			_ = json.Unmarshal([]byte(body), &got)
			if got != tc.want {
				t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
			}
		}
	}

	// Credentials and query in the URL must not come back in the error.
	u := strings.Replace(closedURL(t), "http://", "http://user:pass@", 1) + "/?secret=hunter2"
	_, body := call(t, c, "POST", url, hostBody{URL: u, Token: "t"})
	for _, leak := range []string{"user", "pass", "hunter2", "127.0.0.1"} {
		if strings.Contains(body, leak) {
			t.Errorf("test result leaks %q: %s", leak, body)
		}
	}
}

func TestConfigHostsNoTokenInResponses(t *testing.T) {
	agentURL := startAgent(t, secretToken)
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"good", agentURL, secretToken})
	_, base := startServer(t, cfg)
	c := login(t, base)

	_, list := call(t, c, "GET", base+"/api/config/hosts", nil)
	_, test := call(t, c, "POST", base+"/api/config/hosts/test", hostBody{Name: "good"})
	_, edit := call(t, c, "PUT", base+"/api/config/hosts/good", hostBody{Name: "good", URL: agentURL, Token: secretToken})
	ch := subscribe(t, c, base)
	ov, _ := json.Marshal(next(t, ch))
	for name, b := range map[string]string{"list": list, "test": test, "edit": edit, "events": string(ov)} {
		if strings.Contains(b, secretToken) {
			t.Errorf("%s contains the token: %s", name, b)
		}
	}
}

func TestConfigHostsRequireKeyAndHost(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	_, base := startServer(t, cfg)
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/config/hosts"},
		{"POST", "/api/config/hosts"},
		{"POST", "/api/config/hosts/test"},
		{"PUT", "/api/config/hosts/a"},
		{"DELETE", "/api/config/hosts/a"},
	} {
		req, _ := http.NewRequest(rt.method, base+rt.path, strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without key: %d", rt.method, rt.path, resp.StatusCode)
		}
		// A foreign Host header is rejected before anything else.
		req, _ = http.NewRequest(rt.method, base+rt.path, strings.NewReader("{}"))
		req.Host = "evil.example:1"
		req.Header.Set("Authorization", "Bearer "+testSecret)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s with foreign Host: %d", rt.method, rt.path, resp.StatusCode)
		}
	}
}
