//go:build !windows

package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestHookStatus(t *testing.T) {
	m := agent.NewManager(agent.Options{Command: "/bin/sh"})
	defer m.Shutdown()
	srv := httptest.NewServer(agent.NewServer(m, "k").Handler())
	defer srv.Close()
	ctx := context.Background()
	c := hub.NewClient(hub.Host{Name: "t", URL: srv.URL, Token: "k"})

	sess, err := c.Create(ctx, api.CreateRequest{Dir: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != api.StatusRunning || sess.Origin != api.OriginManaged {
		t.Fatalf("created = %+v", sess)
	}
	status := func() api.Session {
		l, err := c.List(ctx)
		if err != nil || len(l) != 1 {
			t.Fatalf("list = %+v, %v", l, err)
		}
		return l[0]
	}
	for _, step := range []struct {
		event string
		want  api.Status
	}{
		{"UserPromptSubmit", api.StatusWorking},
		{"Stop", api.StatusIdle},
		{"Notification", api.StatusNeedsInput},
		{"SessionStart", api.StatusIdle},
		{"SessionEnd", api.StatusIdle},
	} {
		err := c.Hook(ctx, api.HookEvent{Event: step.event, SessionID: "claude-1", TranscriptPath: "/t.jsonl", ClawshSessionID: sess.ID})
		if err != nil {
			t.Fatal(err)
		}
		if got := status(); got.Status != step.want {
			t.Fatalf("after %s status = %s, want %s", step.event, got.Status, step.want)
		}
	}
	got := status()
	if got.ClaudeSessionID != "claude-1" || got.Transcript != "/t.jsonl" || got.LastEvent == nil {
		t.Fatalf("hook fields = %+v", got)
	}

	// Events without a (known) managed id are accepted and ignored.
	for _, id := range []string{"", "nope"} {
		if err := c.Hook(ctx, api.HookEvent{Event: "Stop", ClawshSessionID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if status().Status != api.StatusIdle {
		t.Fatal("ignored event changed a session")
	}

	// Bad JSON is a 400; no token is a 401.
	req, _ := http.NewRequest("POST", srv.URL+"/v1/hooks", strings.NewReader("{"))
	req.Header.Set("Authorization", "Bearer k")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 400 {
		t.Fatalf("bad json: %v %v", resp, err)
	}

	// An exited session stays exited whatever arrives later.
	if err := c.Hook(ctx, api.HookEvent{Event: "Stop", ClawshSessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	conn, err := c.Dial(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("exit 3\n"))
	deadline := time.Now().Add(5 * time.Second)
	for status().Status != api.StatusExited && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	conn.Close()
	if err := c.Hook(ctx, api.HookEvent{Event: "UserPromptSubmit", ClawshSessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	if got := status(); got.Status != api.StatusExited || got.ExitCode == nil || *got.ExitCode != 3 {
		t.Fatalf("after exit = %+v", got)
	}
}
