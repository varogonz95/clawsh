package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/detach"
	"github.com/varogonz95/clawsh/internal/mcp"
	"github.com/varogonz95/clawsh/internal/paths"
	"github.com/varogonz95/clawsh/internal/service"
)

// detachAgent re-runs this binary as `agent <args without --detach>`, detached
// from the terminal, with output going to the agent log, then returns.
func detachAgent(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var rest []string
	for _, a := range args {
		switch a {
		case "--detach", "-detach", "--detach=true", "-detach=true":
		default:
			rest = append(rest, a)
		}
	}
	logPath := mcp.DefaultLogPath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, append([]string{"agent"}, rest...)...)
	cmd.Dir, _ = os.UserHomeDir() // don't pin the current dir (Windows locks cwd)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := detach.Start(cmd); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "agent started, pid %d (log: %s)\n", cmd.Process.Pid, logPath)
	return cmd.Process.Release()
}

func runInstallService(args []string) error {
	fs := flag.NewFlagSet("agent install-service", flag.ExitOnError)
	uninstall := fs.Bool("uninstall", false, "stop and remove the service")
	dry := fs.Bool("dry-run", false, "print the files and commands without running anything")
	listen := fs.String("listen", ":7420", "address the service's agent listens on")
	claude := fs.String("claude", "claude", "claude executable (name on PATH or full path)")
	_ = fs.Parse(args)

	e := service.Env{}
	if *dry {
		e.Dry = os.Stdout
	}
	if *uninstall {
		return service.Uninstall(e)
	}

	claudePath, err := exec.LookPath(*claude)
	if err != nil {
		return fmt.Errorf("claude not found (%s): pass --claude with its full path", err)
	}
	if claudePath, err = filepath.Abs(claudePath); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	o := service.Options{
		Exe: exe, Claude: claudePath, Listen: *listen,
		EnvFile: paths.DefaultEnvFile(), LogFile: mcp.DefaultLogPath(),
	}

	// The service starts with a bare environment: keep this shell's PATH so
	// claude finds node, git and friends. Never overwrite what the user edited.
	envBody := "# Applied to the agent's environment; sessions inherit it.\nPATH=" + os.Getenv("PATH") + "\n"
	if _, err := os.Stat(o.EnvFile); os.IsNotExist(err) {
		if *dry {
			fmt.Printf("# %s (created if missing)\n%s", o.EnvFile, envBody)
		} else if err := os.MkdirAll(filepath.Dir(o.EnvFile), 0o700); err != nil {
			return err
		} else if err := os.WriteFile(o.EnvFile, []byte(envBody), 0o600); err != nil {
			return err
		}
	}

	if err := service.Install(o, e); err != nil {
		return err
	}
	if *dry {
		return nil
	}
	token, _, err := agent.LoadOrCreateToken(agent.DefaultTokenPath())
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	fmt.Printf("agent service installed (listening on %s, env file %s)\n", *listen, o.EnvFile)
	fmt.Printf("add it to hosts.toml on your hub machine:  token = %q\n", token)
	if strings.HasPrefix(*listen, ":") || strings.HasPrefix(*listen, "0.0.0.0") {
		fmt.Println("it listens on every interface; use --listen 127.0.0.1:7420 for local-only use")
	}
	return nil
}
