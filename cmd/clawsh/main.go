// clawsh — manage Claude Code sessions across machines.
//
//	clawsh agent                     run on every machine that hosts sessions
//	clawsh token                     print this machine's agent token
//	clawsh mcp                       MCP stub for the Claude Code plugin (see plugin/)
//	clawsh hosts                     check which configured agents are reachable
//	clawsh ls [host]                 list sessions on all (or one) hosts
//	clawsh new <host> [flags] [-- claude args...]
//	clawsh run [-- claude args...]   start a session on the local agent and attach
//	clawsh attach <host>/<id>        attach; Ctrl-] detaches
//	clawsh kill <host>/<id>
//	clawsh web                       browser UI for every host and session
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/hub"
	"github.com/varogonz95/clawsh/internal/mcp"
	"github.com/varogonz95/clawsh/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "agent":
		err = runAgent(args)
	case "token":
		err = runToken(args)
	case "mcp":
		err = runMCP(args)
	case "hosts":
		err = runHosts(args)
	case "ls", "list":
		err = runList(args)
	case "new":
		err = runNew(args)
	case "run":
		err = runRun(args)
	case "attach", "a":
		err = runAttach(args)
	case "kill", "rm":
		err = runKill(args)
	case "web":
		err = runWeb(args)
	case "version":
		fmt.Println("clawsh", api.Version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	var ee exitError
	if errors.As(err, &ee) {
		os.Exit(ee.code)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawsh:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: clawsh <command> [args]

agent side (run on each machine):
  agent [--listen :7420] [--claude claude] [--token-file path] [--scrollback bytes]
  token [--token-file path]
  mcp [--listen :7420] [--claude claude] [--token-file path] [--log-file path]
      [--no-spawn] [--no-announce]
                                     MCP stdio stub run by the Claude Code plugin:
                                     starts an agent if none runs, announces the session

hub side (run anywhere; reads hosts.toml):
  hosts                              reachability of every configured agent
  ls [host]                          list sessions
  new <host> [--dir d] [--name n] [--detached] [-- claude args...]
  run [--name n] [-- claude args...] start a session on the local agent, here (no hosts.toml)
  attach <host>/<id>                 attach (id prefix ok); Ctrl-] detaches
  kill <host>/<id>                   stop and remove a session
  web [--listen 127.0.0.1:7421] [--no-open]
                                     open the browser UI (loopback only)

hub commands accept --config (default: `+hub.DefaultConfigPath()+`)
`)
}

// ---------- agent side ----------

func runAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	listen := fs.String("listen", ":7420", "address to listen on")
	claude := fs.String("claude", "claude", "claude executable (name on PATH or full path)")
	tokenFile := fs.String("token-file", agent.DefaultTokenPath(), "bearer token file (created on first run)")
	scrollback := fs.Int("scrollback", 2<<20, "bytes of output kept per session for replay")
	_ = fs.Parse(args)

	token, created, err := agent.LoadOrCreateToken(*tokenFile)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	if created {
		log.Printf("generated new token in %s", *tokenFile)
		log.Printf("add it to hosts.toml on your hub machine:  token = %q", token)
	}

	m := agent.NewManager(agent.Options{Command: *claude, Scrollback: *scrollback})
	srv := &http.Server{
		Addr:              *listen,
		Handler:           agent.NewServer(m, token).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Print("shutting down: stopping sessions")
		m.Shutdown()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	log.Printf("clawsh agent %s listening on %s (claude: %s)", api.Version, *listen, *claude)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runToken(args []string) error {
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	tokenFile := fs.String("token-file", agent.DefaultTokenPath(), "bearer token file")
	_ = fs.Parse(args)
	token, _, err := agent.LoadOrCreateToken(*tokenFile)
	if err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}

// runMCP is started by Claude Code (via the plugin's .mcp.json) for every
// session. stdout belongs to the MCP protocol; logs go to stderr, which Claude
// Code keeps in its MCP logs. It exits on stdin EOF or SIGINT/SIGTERM.
func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	listen := fs.String("listen", ":7420", "agent address (and --listen for a spawned agent)")
	claude := fs.String("claude", "claude", "--claude for a spawned agent")
	tokenFile := fs.String("token-file", agent.DefaultTokenPath(), "agent token file")
	logFile := fs.String("log-file", mcp.DefaultLogPath(), "spawned agent's log")
	noSpawn := fs.Bool("no-spawn", false, "don't start an agent when none answers")
	noAnnounce := fs.Bool("no-announce", false, "don't list this session on the agent")
	_ = fs.Parse(args)
	log.SetOutput(os.Stderr)
	log.SetPrefix("clawsh mcp: ")

	dir := os.Getenv("CLAUDE_PROJECT_DIR")
	if dir == "" {
		dir, _ = os.Getwd()
	}
	// A claude launched by clawsh already belongs to the agent that launched it.
	managed := os.Getenv("CLAWSH_SESSION_ID") != ""

	// Claude Code stops MCP servers with SIGINT rather than closing stdin.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- mcp.Serve(os.Stdin, os.Stdout) }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mcp.Run(ctx, mcp.Options{
			Listen: *listen, Claude: *claude, TokenFile: *tokenFile, LogFile: *logFile,
			Spawn: !*noSpawn, Announce: !*noAnnounce && !managed,
			Dir: dir, Pid: os.Getppid(),
		})
	}()
	var err error
	select {
	case err = <-served: // stdin closed
	case <-ctx.Done():
	}
	cancel()
	select { // let Run withdraw the announcement
	case <-done:
	case <-time.After(3 * time.Second):
	}
	return err
}

// ---------- hub side ----------

func hubFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	cfg := fs.String("config", hub.DefaultConfigPath(), "hosts config file")
	return fs, cfg
}

// parseInterspersed lets flags appear before or after positional args.
// Everything after a literal "--" is returned as passthrough.
func parseInterspersed(fs *flag.FlagSet, args []string) (pos, passthrough []string) {
	for {
		_ = fs.Parse(args)
		rest := fs.Args()
		consumed := len(args) - len(rest)
		if consumed > 0 && args[consumed-1] == "--" {
			return pos, rest
		}
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func splitTarget(t string) (host, id string, err error) {
	host, id, ok := strings.Cut(t, "/")
	if !ok || host == "" || id == "" {
		return "", "", fmt.Errorf("target must be <host>/<id>, got %q", t)
	}
	return host, id, nil
}

func runHosts(args []string) error {
	fs, cfgPath := hubFlags("hosts")
	parseInterspersed(fs, args)
	cfg, err := hub.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HOST\tURL\tSTATUS")
	for _, o := range hub.Overview(context.Background(), cfg.Hosts) {
		var status string
		switch {
		case o.Online:
			status = fmt.Sprintf("ok (%s, %s, %s)", o.Health.Host, o.Health.OS, api.DisplayVersion(o.Health.Version))
		case o.Health == nil:
			status = "unreachable: " + o.Error
		default:
			status = "auth failed: " + o.Error
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", o.Name, o.URL, status)
	}
	return tw.Flush()
}

func runList(args []string) error {
	fs, cfgPath := hubFlags("ls")
	pos, _ := parseInterspersed(fs, args)
	cfg, err := hub.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	hosts := cfg.Hosts
	if len(pos) > 0 {
		h, err := cfg.Find(pos[0])
		if err != nil {
			return err
		}
		hosts = []hub.Host{h}
	}

	ov := hub.Overview(context.Background(), hosts)
	type row struct {
		api.HostOverview
		external []api.External
	}
	results := make([]row, len(ov))
	var wg sync.WaitGroup
	for i := range ov {
		results[i].HostOverview = ov[i]
		if !ov[i].Online {
			continue
		}
		wg.Add(1)
		go func(i int, h hub.Host) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), hub.OverviewTimeout)
			defer cancel()
			// Older agents lack /v1/external; treat any error as none.
			results[i].external, _ = hub.NewClient(h).Externals(ctx)
		}(i, hosts[i])
	}
	wg.Wait()
	sort.SliceStable(results, func(i, j int) bool { return results[i].Name < results[j].Name })

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tNAME\tSTATUS\tVIEWERS\tAGE\tDIR")
	var failed []row
	for _, r := range results {
		if !r.Online {
			failed = append(failed, r)
			continue
		}
		for _, s := range r.Sessions {
			status := string(s.Status)
			if s.ExitCode != nil {
				status = fmt.Sprintf("exited(%d)", *s.ExitCode)
			}
			fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%d\t%s\t%s\n",
				r.Name, s.ID, s.Name, status, s.Viewers, age(s.Created), s.Dir)
		}
		for _, x := range r.external {
			fmt.Fprintf(tw, "%s/%s\t%s\texternal\t-\t%s\t%s\n",
				r.Name, x.ID, x.Name, age(x.Created), x.Dir)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, r := range failed {
		if r.Health != nil { // it answered but failed otherwise, e.g. a rejected access key
			fmt.Fprintf(os.Stderr, "! %s: %s\n", r.Name, r.Error)
		} else {
			fmt.Fprintf(os.Stderr, "! %s unreachable: %s\n", r.Name, r.Error)
		}
	}
	return nil
}

func runNew(args []string) error {
	fs, cfgPath := hubFlags("new")
	dir := fs.String("dir", "", "working directory on the remote machine (default: remote home)")
	name := fs.String("name", "", "session name (default: dir basename)")
	detached := fs.Bool("detached", false, "don't attach after creating")
	pos, claudeArgs := parseInterspersed(fs, args)
	if len(pos) != 1 {
		return errors.New("usage: clawsh new <host> [--dir d] [--name n] [--detached] [-- claude args...]")
	}
	cfg, err := hub.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	h, err := cfg.Find(pos[0])
	if err != nil {
		return err
	}
	req := api.CreateRequest{Name: *name, Dir: *dir, Args: claudeArgs}
	if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		req.Cols, req.Rows = cols, rows
	}
	c := hub.NewClient(h)
	s, err := c.Create(context.Background(), req)
	if err != nil {
		return err
	}
	fmt.Printf("created %s/%s in %s\n", h.Name, s.ID, s.Dir)
	if *detached {
		return nil
	}
	return hub.Attach(context.Background(), c, s.ID)
}

func runAttach(args []string) error {
	fs, cfgPath := hubFlags("attach")
	pos, _ := parseInterspersed(fs, args)
	if len(pos) != 1 {
		return errors.New("usage: clawsh attach <host>/<id>")
	}
	hostName, id, err := splitTarget(pos[0])
	if err != nil {
		return err
	}
	cfg, err := hub.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	h, err := cfg.Find(hostName)
	if err != nil {
		return err
	}
	return hub.Attach(context.Background(), hub.NewClient(h), id)
}

func runKill(args []string) error {
	fs, cfgPath := hubFlags("kill")
	pos, _ := parseInterspersed(fs, args)
	if len(pos) != 1 {
		return errors.New("usage: clawsh kill <host>/<id>")
	}
	hostName, id, err := splitTarget(pos[0])
	if err != nil {
		return err
	}
	cfg, err := hub.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	h, err := cfg.Find(hostName)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hub.NewClient(h).Kill(ctx, id); err != nil {
		return err
	}
	fmt.Printf("killed %s/%s\n", hostName, id)
	return nil
}

func runWeb(args []string) error {
	fs, cfgPath := hubFlags("web")
	listen := fs.String("listen", "127.0.0.1:7421", "loopback address for the web UI")
	noOpen := fs.Bool("no-open", false, "print the URL without opening a browser")
	parseInterspersed(fs, args)
	if err := web.CheckLoopback(*listen); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	secret := web.NewSecret()
	srv, err := web.New(web.Options{
		ConfigPath: *cfgPath,
		Secret:     secret,
		Port:       ln.Addr().(*net.TCPAddr).Port,
	})
	if err != nil {
		ln.Close()
		return err
	}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if httpSrv.Shutdown(sctx) != nil {
			_ = httpSrv.Close() // open event streams don't end on their own
		}
	}()

	u := fmt.Sprintf("http://%s/?k=%s", ln.Addr(), secret)
	fmt.Printf("clawsh web: open %s\n(Ctrl-C to stop)\n", u)
	if !*noOpen {
		// The browser gets a private file that redirects to u, never u itself:
		// a command line is visible to every local user.
		if rd, err := web.WriteRedirect(u); err != nil {
			fmt.Fprintf(os.Stderr, "clawsh web: couldn't prepare the browser launch (%v); open the link above\n", err)
		} else {
			defer rd.Remove()
			time.AfterFunc(30*time.Second, func() { _ = rd.Remove() })
			if err := web.OpenBrowser(rd.URL()); err != nil {
				fmt.Fprintf(os.Stderr, "clawsh web: couldn't open a browser (%v); open the link above\n", err)
			}
		}
	}
	if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func age(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return d.String()
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// exitError makes main exit with a specific code without printing anything.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func runRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	name := fs.String("name", "", "session name (default: dir basename)")
	listen := fs.String("listen", ":7420", "local agent address")
	tokenFile := fs.String("token-file", agent.DefaultTokenPath(), "agent token file")
	pos, claudeArgs := parseInterspersed(fs, args)
	if len(pos) != 0 {
		return errors.New("usage: clawsh run [--name n] [--listen addr] [--token-file p] [-- claude args...]")
	}
	return runLocal(context.Background(), *listen, *tokenFile, *name, claudeArgs, hub.AttachExit)
}

// runLocal creates a session on the local agent in the current directory and
// attaches to it. It returns exitError with claude's code when the session
// exits, nil on detach.
func runLocal(ctx context.Context, listen, tokenFile, name string, claudeArgs []string,
	attach func(context.Context, *hub.Client, string) (*int, error)) error {
	u, err := mcp.LocalURL(listen)
	if err != nil {
		return err
	}
	token, _, err := agent.LoadOrCreateToken(tokenFile)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	c := hub.NewClient(hub.Host{Name: "local", URL: u, Token: token})
	hctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	_, err = c.Health(hctx)
	cancel()
	if err != nil {
		var he *hub.HTTPError
		if errors.As(err, &he) {
			return err // an agent answered but refused, e.g. a wrong token
		}
		return fmt.Errorf("no local agent at %s; start one with 'clawsh agent' or 'clawsh agent install-service'", u)
	}
	req := api.CreateRequest{Name: name, Dir: cwd, Args: claudeArgs}
	if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		req.Cols, req.Rows = cols, rows
	}
	s, err := c.Create(ctx, req)
	if err != nil {
		return err
	}
	code, err := attach(ctx, c, s.ID)
	if err != nil {
		return err
	}
	if code != nil && *code != 0 {
		return exitError{*code}
	}
	return nil
}
