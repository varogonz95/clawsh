//go:build !windows

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/hub"
)

func del(t *testing.T, c *http.Client, url string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func agentSessions(t *testing.T, url string) int {
	t.Helper()
	list, err := hub.NewClient(hub.Host{Name: "a", URL: url, Token: "tok"}).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}

func TestCreateAndEndSession(t *testing.T) {
	agentURL := startAgent(t, "tok")
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"a", agentURL, "tok"})
	_, base := startServer(t, cfg)
	c := login(t, base)

	s := createViaWeb(t, c, base, "a")
	if agentSessions(t, agentURL) != 1 {
		t.Fatal("session not created on the agent")
	}
	if got := del(t, c, base+"/api/hosts/a/sessions/"+s.ID); got != http.StatusNoContent {
		t.Fatalf("delete: %d, want 204", got)
	}
	if agentSessions(t, agentURL) != 0 {
		t.Fatal("session not removed on the agent")
	}
}

func TestHostNameWithSpace(t *testing.T) {
	agentURL := startAgent(t, "tok")
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"my desk", agentURL, "tok"})
	_, base := startServer(t, cfg)
	c := login(t, base)

	s := createViaWeb(t, c, base, "my%20desk")
	if got := del(t, c, base+"/api/hosts/my%20desk/sessions/"+s.ID); got != http.StatusNoContent {
		t.Fatalf("delete: %d, want 204", got)
	}
}

func TestSessionErrors(t *testing.T) {
	agentURL := startAgent(t, "tok")
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"a", agentURL, "tok"}, testHost{"down", closedURL(t), "tok"})
	_, base := startServer(t, cfg)
	c := login(t, base)
	post := func(url string) int {
		resp, err := c.Post(url, "application/json", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(base + "/api/hosts/nope/sessions"); got != http.StatusNotFound {
		t.Errorf("unknown host: %d, want 404", got)
	}
	if got := post(base + "/api/hosts/down/sessions"); got != http.StatusBadGateway {
		t.Errorf("offline host: %d, want 502", got)
	}
	if got := del(t, c, base+"/api/hosts/a/sessions/nope"); got != http.StatusNotFound {
		t.Errorf("unknown session: %d, want 404 relayed from the agent", got)
	}
}

func TestCreateRejectsForeignOrigin(t *testing.T) {
	agentURL := startAgent(t, "tok")
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"a", agentURL, "tok"})
	_, base := startServer(t, cfg)
	c := login(t, base)

	req, _ := http.NewRequest(http.MethodPost, base+"/api/hosts/a/sessions", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Origin", "http://evil.example")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if agentSessions(t, agentURL) != 0 {
		t.Fatal("a cross-site request created a session")
	}
}

func TestAgentRejectingKeyIsBadGateway(t *testing.T) {
	agentURL := startAgent(t, "tok")
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"badkey", agentURL, "wrong"})
	_, base := startServer(t, cfg)
	c := login(t, base)

	resp, err := c.Post(base+"/api/hosts/badkey/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var e api.Error
	json.NewDecoder(resp.Body).Decode(&e)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(e.Error, "access key rejected") {
		t.Fatalf("got %d %q, want 502 containing 'access key rejected'", resp.StatusCode, e.Error)
	}
}

func TestRecentDirsProxy(t *testing.T) {
	m := agent.NewManager(agent.Options{Command: "/bin/sh", HistoryFile: filepath.Join(t.TempDir(), "recent-dirs.json")})
	srv := httptest.NewServer(agent.NewServer(m, "tok").Handler())
	t.Cleanup(func() { srv.Close(); m.Shutdown() })
	cfg := filepath.Join(t.TempDir(), "hosts.toml")
	writeHosts(t, cfg, testHost{"a", srv.URL, "tok"})
	_, base := startServer(t, cfg)
	c := login(t, base)

	get := func(url string) (int, string) {
		resp, err := c.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	if code, body := get(base + "/api/hosts/a/dirs/recent"); code != 200 || body != `{"dirs":[]}` {
		t.Fatalf("empty: %d %s", code, body)
	}
	dir := t.TempDir()
	body, _ := json.Marshal(api.CreateRequest{Dir: dir})
	resp, err := c.Post(base+"/api/hosts/a/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	code, got := get(base + "/api/hosts/a/dirs/recent")
	want, _ := json.Marshal(api.RecentDirs{Dirs: []string{dir}})
	if code != 200 || got != string(want) {
		t.Fatalf("got %d %s, want %s", code, got, want)
	}
	if code, _ := get(base + "/api/hosts/nope/dirs/recent"); code != http.StatusNotFound {
		t.Fatalf("unknown host: %d, want 404", code)
	}
}
