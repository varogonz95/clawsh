//go:build !windows

package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/hub"
)

// End-to-end over real HTTP/WebSocket, with /bin/sh standing in for claude.
func TestCreateAttachEchoExit(t *testing.T) {
	m := agent.NewManager(agent.Options{Command: "/bin/sh"})
	defer m.Shutdown()
	srv := httptest.NewServer(agent.NewServer(m, "secret").Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bad := hub.NewClient(hub.Host{Name: "t", URL: srv.URL, Token: "wrong"})
	if _, err := bad.List(ctx); err == nil {
		t.Fatal("expected auth failure with wrong token")
	}

	c := hub.NewClient(hub.Host{Name: "t", URL: srv.URL, Token: "secret"})
	s, err := c.Create(ctx, api.CreateRequest{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	conn, err := c.Dial(ctx, s.ID[:4]) // prefix lookup
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	resize, _ := json.Marshal(api.Control{Type: api.ControlResize, Cols: 100, Rows: 30})
	_ = conn.WriteMessage(websocket.TextMessage, resize)
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("echo hello-$((40+2))\nexit 3\n"))

	var out strings.Builder
	var code *int
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	for code == nil {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v (output so far: %q)", err, out.String())
		}
		if mt == websocket.BinaryMessage {
			out.Write(data)
			continue
		}
		var ctl api.Control
		if json.Unmarshal(data, &ctl) == nil && ctl.Type == api.ControlExit {
			code = ctl.Code
		}
	}
	if !strings.Contains(out.String(), "hello-42") {
		t.Fatalf("missing command output, got %q", out.String())
	}
	if *code != 3 {
		t.Fatalf("exit code = %d, want 3", *code)
	}

	list, err := c.List(ctx)
	if err != nil || len(list) != 1 || list[0].Status != api.StatusExited {
		t.Fatalf("list after exit = %+v, err %v", list, err)
	}
	if err := c.Kill(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.List(ctx); len(list) != 0 {
		t.Fatalf("session not removed: %+v", list)
	}
}

// A second viewer attaching later must get the earlier output replayed.
func TestReplayOnLateAttach(t *testing.T) {
	m := agent.NewManager(agent.Options{Command: "/bin/sh"})
	defer m.Shutdown()
	srv := httptest.NewServer(agent.NewServer(m, "k").Handler())
	defer srv.Close()
	ctx := context.Background()
	c := hub.NewClient(hub.Host{Name: "t", URL: srv.URL, Token: "k"})

	s, err := c.Create(ctx, api.CreateRequest{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Dial(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.WriteMessage(websocket.BinaryMessage, []byte("echo marker-$((1+1))\n"))
	time.Sleep(500 * time.Millisecond)
	first.Close()

	late, err := c.Dial(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer late.Close()
	_ = late.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := late.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "marker-2") {
		t.Fatalf("replay missing earlier output: %q", data)
	}
}

// External sessions: announce is an upsert, list drops expired leases,
// withdraw removes.
func TestExternalLease(t *testing.T) {
	m := agent.NewManager(agent.Options{Command: "/bin/sh", ExternalTTL: time.Second})
	defer m.Shutdown()
	srv := httptest.NewServer(agent.NewServer(m, "k").Handler())
	defer srv.Close()
	ctx := context.Background()
	c := hub.NewClient(hub.Host{Name: "t", URL: srv.URL, Token: "k"})

	if _, err := c.Announce(ctx, "bad/id", api.AnnounceRequest{}); err == nil {
		t.Fatal("expected error for invalid id")
	}
	first, err := c.Announce(ctx, "abc", api.AnnounceRequest{Dir: "/src/api", Pid: 7})
	if err != nil || first.Name != "api" {
		t.Fatalf("announce = %+v, %v", first, err)
	}
	time.Sleep(200 * time.Millisecond)
	again, err := c.Announce(ctx, "abc", api.AnnounceRequest{Dir: "/src/api", Pid: 7})
	if err != nil || !again.Created.Equal(first.Created) || !again.Seen.After(first.Seen) {
		t.Fatalf("renew = %+v, %v (first %+v)", again, err, first)
	}
	if _, err := c.Announce(ctx, "def", api.AnnounceRequest{Name: "other"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // "abc" renewed 300ms ago, still live
	if x, _ := c.Externals(ctx); len(x) != 2 || x[0].ID != "abc" {
		t.Fatalf("list = %+v", x)
	}
	if err := c.Withdraw(ctx, "def"); err != nil {
		t.Fatal(err)
	}
	if err := c.Withdraw(ctx, "def"); err == nil {
		t.Fatal("expected not found on second withdraw")
	}
	time.Sleep(1200 * time.Millisecond) // "abc" now past its TTL
	if x, _ := c.Externals(ctx); len(x) != 0 {
		t.Fatalf("expired lease still listed: %+v", x)
	}
	if s, _ := c.List(ctx); len(s) != 0 {
		t.Fatalf("externals leaked into sessions: %+v", s)
	}
}

func TestRecentDirsEndpoint(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	m := agent.NewManager(agent.Options{Command: "/bin/sh", HistoryFile: filepath.Join(t.TempDir(), "recent-dirs.json")})
	defer m.Shutdown()
	srv := httptest.NewServer(agent.NewServer(m, "secret").Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := http.Get(srv.URL + "/v1/dirs/recent")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d, want 401", resp.StatusCode)
	}

	c := hub.NewClient(hub.Host{Name: "t", URL: srv.URL, Token: "secret"})
	if got, err := c.RecentDirs(ctx); err != nil || got.Dirs == nil || len(got.Dirs) != 0 {
		t.Fatalf("empty history = %#v, %v", got, err)
	}
	for _, d := range []string{a, b} {
		if _, err := c.Create(ctx, api.CreateRequest{Dir: d}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := c.RecentDirs(ctx)
	if err != nil || !reflect.DeepEqual(got.Dirs, []string{b, a}) {
		t.Fatalf("history = %v, %v; want [%s %s]", got.Dirs, err, b, a)
	}
	os.RemoveAll(a)
	if got, _ := c.RecentDirs(ctx); !reflect.DeepEqual(got.Dirs, []string{b}) {
		t.Fatalf("after delete = %v", got.Dirs)
	}

	// History disabled: empty list, not null.
	m2 := agent.NewManager(agent.Options{Command: "/bin/sh"})
	defer m2.Shutdown()
	srv2 := httptest.NewServer(agent.NewServer(m2, "secret").Handler())
	defer srv2.Close()
	req, _ := http.NewRequest(http.MethodGet, srv2.URL+"/v1/dirs/recent", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	if strings.TrimSpace(string(body)) != `{"dirs":[]}` {
		t.Fatalf("disabled body = %s", body)
	}
}
