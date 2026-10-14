package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/hub"
)

func TestServeHandshake(t *testing.T) {
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"two","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"x"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"ping"}`,
	}, "\n") + "\n"
	pr, pw := io.Pipe()
	go func() { _ = Serve(strings.NewReader(in), pw); pw.Close() }()

	var got []map[string]json.RawMessage
	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad line %q: %v", sc.Text(), err)
		}
		got = append(got, m)
	}
	if len(got) != 4 { // the notification gets no reply
		t.Fatalf("got %d replies, want 4: %v", len(got), got)
	}
	var init struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
	}
	_ = json.Unmarshal(got[0]["result"], &init)
	if init.ProtocolVersion != "2025-11-25" || len(init.Capabilities) != 0 {
		t.Fatalf("initialize result = %s", got[0]["result"])
	}
	if string(got[1]["id"]) != `"two"` || string(got[1]["result"]) != `{"tools":[]}` {
		t.Fatalf("tools/list reply = %v", got[1])
	}
	if got[2]["error"] == nil {
		t.Fatalf("tools/call should fail: %v", got[2])
	}
	if string(got[3]["result"]) != `{}` {
		t.Fatalf("ping reply = %v", got[3])
	}
}

// Run against a live agent: the session is listed while ctx lives and
// withdrawn when it ends.
func TestRunAnnouncesAndWithdraws(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "agent.token")
	token, _, err := agent.LoadOrCreateToken(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(agent.NewServer(agent.NewManager(agent.Options{}), token).Handler())
	defer srv.Close()
	c := hub.NewClient(hub.Host{URL: srv.URL, Token: token})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, Options{
			Listen: strings.TrimPrefix(srv.URL, "http://"), TokenFile: tokenFile,
			Announce: true, Dir: "/src/api", Pid: 42, Renew: 50 * time.Millisecond,
		})
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		x, err := c.Externals(context.Background())
		if err == nil && len(x) == 1 {
			if x[0].Name != "api" || x[0].Pid != 42 {
				t.Fatalf("external = %+v", x[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never announced: %v %v", x, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if x, _ := c.Externals(context.Background()); len(x) != 0 {
		t.Fatalf("not withdrawn: %+v", x)
	}
}

func TestAgentEnv(t *testing.T) {
	in := []string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_PLUGIN_ROOT=/p",
		"CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_API_KEY=k", "CLAWSH_SESSION_ID=x"}
	got := strings.Join(agentEnv(in), " ")
	if got != "PATH=/bin CLAUDE_CODE_USE_BEDROCK=1 ANTHROPIC_API_KEY=k" {
		t.Fatalf("agentEnv = %q", got)
	}
}

func TestLocalURL(t *testing.T) {
	for in, want := range map[string]string{
		":7420": "http://127.0.0.1:7420", "0.0.0.0:1": "http://127.0.0.1:1",
		"10.0.0.2:7420": "http://10.0.0.2:7420", "[::]:7420": "http://127.0.0.1:7420",
	} {
		if got, err := LocalURL(in); err != nil || got != want {
			t.Errorf("LocalURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := LocalURL("7420"); err == nil {
		t.Error("want error for missing colon")
	}
}

// The spike's claim: a message posted to the agent for session B reaches B's
// MCP server and leaves it as a channel notification, with the capability
// declared at initialize. A scripted client stands in for Claude Code.
func TestChannelDeliversPostedMessage(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "agent.token")
	token, _, err := agent.LoadOrCreateToken(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(agent.NewServer(agent.NewManager(agent.Options{}), token).Handler())
	defer srv.Close()
	c := hub.NewClient(hub.Host{URL: srv.URL, Token: token})

	// Session B: its stub announces id "bbbb" and serves MCP on a pipe pair.
	msgs := make(chan api.Message, 4)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { _ = ServeChannel(inR, outW, msgs); outW.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, Options{
		Listen: strings.TrimPrefix(srv.URL, "http://"), TokenFile: tokenFile,
		Announce: true, Dir: "/src/b", Pid: 7, ID: "bbbb", Messages: msgs,
	})

	lines := bufio.NewScanner(outR)
	read := func() map[string]any {
		t.Helper()
		if !lines.Scan() {
			t.Fatalf("stream ended: %v", lines.Err())
		}
		var m map[string]any
		if err := json.Unmarshal(lines.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`+"\n")
	caps := read()["result"].(map[string]any)["capabilities"].(map[string]any)
	if _, ok := caps["experimental"].(map[string]any)["claude/channel"]; !ok {
		t.Fatalf("capabilities = %v", caps)
	}

	// Unknown targets are refused; B is known once announced.
	if _, err := c.Send(context.Background(), api.SendRequest{To: "nope", Content: "x"}); err == nil {
		t.Fatal("send to unknown session should fail")
	}
	var posted api.Message
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		posted, err = c.Send(context.Background(), api.SendRequest{
			To: "bbbb", From: "A/1234", Content: "implement feature X",
			Meta: map[string]string{"stage": "implement", "bad-key": "dropped", "from": "spoofed"},
		})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("send: %v", err)
		}
	}

	n := read()
	if n["method"] != "notifications/claude/channel" || n["id"] != nil {
		t.Fatalf("notification = %v", n)
	}
	p := n["params"].(map[string]any)
	meta := p["meta"].(map[string]any)
	if p["content"] != "implement feature X" || meta["from"] != "A/1234" || meta["stage"] != "implement" ||
		meta["message_id"] != posted.ID || meta["bad-key"] != nil {
		t.Fatalf("params = %v", p)
	}
}
