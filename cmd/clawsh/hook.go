package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/hub"
	"github.com/varogonz95/clawsh/internal/mcp"
)

const hookTimeout = time.Second

// runHook is a Claude Code hook command: it must never disturb claude, so it
// prints nothing on stdout, reports problems on stderr and always exits 0.
func runHook(args []string) {
	hookCmd(args, os.Stdin, os.Stderr, os.Getenv("CLAWSH_SESSION_ID"), os.Getppid())
}

func hookCmd(args []string, stdin io.Reader, stderr io.Writer, clawshID string, ppid int) {
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", ":7420", "agent address")
	tokenFile := fs.String("token-file", agent.DefaultTokenPath(), "agent token file")
	// Accept flags before or after the event name.
	var event string
	for rest := args; len(rest) > 0; {
		if err := fs.Parse(rest); err != nil {
			return
		}
		rest = fs.Args()
		if len(rest) > 0 {
			if event == "" {
				event = rest[0]
			}
			rest = rest[1:]
		}
	}
	if event == "" {
		fmt.Fprintln(stderr, "clawsh hook: missing event name")
		return
	}
	// Never create the token: with no token file there is no agent to tell.
	tok, err := os.ReadFile(*tokenFile)
	if err != nil {
		fmt.Fprintln(stderr, "clawsh hook:", err)
		return
	}
	u, err := mcp.LocalURL(*listen)
	if err != nil {
		fmt.Fprintln(stderr, "clawsh hook:", err)
		return
	}
	var in struct {
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
		Cwd            string `json:"cwd"`
		Message        string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in); err != nil {
		fmt.Fprintln(stderr, "clawsh hook: stdin:", err)
		return
	}
	ev := api.HookEvent{
		Event: event, SessionID: in.SessionID, TranscriptPath: in.TranscriptPath, Cwd: in.Cwd,
		ClawshSessionID: clawshID, Pid: ppid,
	}
	if event == "Notification" {
		ev.Message = in.Message
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	c := hub.NewClient(hub.Host{Name: "local", URL: u, Token: strings.TrimSpace(string(tok))})
	if err := c.Hook(ctx, ev); err != nil {
		fmt.Fprintln(stderr, "clawsh hook:", err)
	}
}
