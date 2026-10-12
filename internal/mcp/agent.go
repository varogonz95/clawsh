package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/detach"
	"github.com/varogonz95/clawsh/internal/hub"
)

// Options configure the stub. Listen, Claude and TokenFile are passed to a
// spawned agent unchanged.
type Options struct {
	Listen    string // agent --listen, e.g. ":7420"
	Claude    string // agent --claude
	TokenFile string // agent --token-file; also read here to authenticate
	LogFile   string // spawned agent's stdout/stderr
	Spawn     bool   // start an agent if none answers
	Announce  bool   // announce this session to the agent
	Dir       string // session directory to announce
	Pid       int    // claude's pid to announce
	Renew     time.Duration
}

// LocalURL turns a listen address into a URL reachable from this machine.
func LocalURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("listen %q: %w", listen, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// DefaultLogPath is where a spawned agent logs.
func DefaultLogPath() string {
	return filepath.Join(filepath.Dir(agent.DefaultTokenPath()), "agent.log")
}

// Run does the stub's side work until ctx is cancelled: ensure an agent,
// announce, renew, and withdraw on the way out. Failures are logged, never
// fatal: the MCP connection must stay up so the Claude session isn't bothered.
func Run(ctx context.Context, o Options) {
	if o.Renew <= 0 {
		o.Renew = agent.DefaultExternalTTL / 3
	}
	u, err := LocalURL(o.Listen)
	if err != nil {
		log.Print(err)
		return
	}
	// LoadOrCreate so the stub and a spawned agent agree on a token even on
	// the very first run (the agent then just reads it).
	token, _, err := agent.LoadOrCreateToken(o.TokenFile)
	if err != nil {
		log.Printf("token: %v", err)
		return
	}
	c := hub.NewClient(hub.Host{Name: "local", URL: u, Token: token})

	if err := ensure(ctx, c, o); err != nil {
		log.Printf("agent: %v", err)
	}
	if !o.Announce {
		<-ctx.Done()
		return
	}

	id := newID()
	req := api.AnnounceRequest{Dir: o.Dir, Pid: o.Pid}
	announce := func() {
		actx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, err := c.Announce(actx, id, req); err != nil {
			log.Printf("announce: %v", err)
			// The agent may have died since; bring it back for the next round.
			if err := ensure(actx, c, o); err != nil {
				log.Printf("agent: %v", err)
			}
		}
	}
	announce()
	t := time.NewTicker(o.Renew)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			announce()
		case <-ctx.Done():
			wctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = c.Withdraw(wctx, id)
			cancel()
			return
		}
	}
}

// ensure returns once an agent answers /v1/health, spawning one if needed.
func ensure(ctx context.Context, c *hub.Client, o Options) error {
	if healthy(ctx, c) {
		return nil
	}
	if !o.Spawn {
		return errors.New("no agent at " + c.Host.URL + " and spawning is disabled")
	}
	if err := spawn(o); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	// Another session may have raced us to it; either way, wait for one.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if healthy(ctx, c) {
			log.Printf("agent up at %s", c.Host.URL)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return fmt.Errorf("spawned agent not answering at %s; see %s", c.Host.URL, o.LogFile)
}

func healthy(ctx context.Context, c *hub.Client) bool {
	hctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err := c.Health(hctx)
	return err == nil
}

// spawn starts `<this binary> agent ...` detached from the Claude session, so
// it outlives it, and returns without waiting for it.
func spawn(o Options) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.LogFile), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(o.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()

	cmd := exec.Command(exe, "agent", "--listen", o.Listen, "--claude", o.Claude, "--token-file", o.TokenFile)
	cmd.Dir, _ = os.UserHomeDir() // don't pin the project dir (Windows locks cwd)
	cmd.Env = agentEnv(os.Environ())
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := detach.Start(cmd); err != nil {
		return err
	}
	log.Printf("spawned agent pid %d (log: %s)", cmd.Process.Pid, o.LogFile)
	return cmd.Process.Release()
}

// sessionEnv are variables Claude Code sets for its children that describe
// that one session. The agent outlives the session that spawned it and passes
// its env on to every claude it launches, which must not look nested inside
// (or be confused with) the spawning session.
var sessionEnv = map[string]bool{
	"CLAUDECODE": true, "CLAUDE_PID": true, "CLAUDE_PROJECT_DIR": true, "CLAWSH_SESSION_ID": true,
	"CLAUDE_CODE_ENTRYPOINT": true, "CLAUDE_CODE_SSE_PORT": true, "CLAUDE_CODE_SESSION_ID": true,
	"CLAUDE_CODE_CHILD_SESSION": true, "CLAUDE_CODE_EXECPATH": true,
}

func agentEnv(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if sessionEnv[k] || strings.HasPrefix(k, "CLAUDE_PLUGIN_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
