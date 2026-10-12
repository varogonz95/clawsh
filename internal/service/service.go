// Package service installs the agent as a per-user login service: a systemd
// user unit on Linux, a launchd agent on macOS, a logon scheduled task on
// Windows. The generators here are OS-independent so they can be tested
// anywhere; Install and Uninstall live in the per-OS files.
package service

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// Name is the systemd unit and scheduled task name.
	Name = "clawsh-agent"
	// Label is the launchd label.
	Label = "dev.clawsh.agent"
)

// Options describe the agent the service runs. All paths are absolute.
type Options struct {
	Exe     string // the clawsh binary
	Claude  string // --claude
	Listen  string // --listen
	EnvFile string // --env-file
	LogFile string // launchd stdout/stderr
}

// Env carries the side effects of Install and Uninstall. With Dry set,
// nothing runs or is written: the commands and file contents are printed to it.
type Env struct {
	Dry io.Writer
}

func (e Env) run(name string, args ...string) error {
	if e.Dry != nil {
		fmt.Fprintf(e.Dry, "$ %s\n", strings.Join(append([]string{name}, quoteAll(args)...), " "))
		return nil
	}
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tryRun is run for steps whose failure is fine (stopping a service that
// isn't running).
func (e Env) tryRun(name string, args ...string) {
	_ = e.run(name, args...)
}

func (e Env) writeFile(path, content string) error {
	if e.Dry != nil {
		fmt.Fprintf(e.Dry, "# %s\n%s", path, content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Fprintln(e.Dry)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func (e Env) remove(path string) error {
	if e.Dry != nil {
		fmt.Fprintf(e.Dry, "# remove %s\n", path)
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return out
}

// shellQuote double-quotes s when it has whitespace or quotes; for display
// and for systemd ExecStart, which accepts C-style double-quoted words.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// agentArgs are the arguments after the executable.
func (o Options) agentArgs() []string {
	return []string{"agent", "--listen", o.Listen, "--claude", o.Claude, "--env-file", o.EnvFile}
}

// SystemdUnit is the user unit file.
func SystemdUnit(o Options) string {
	words := append([]string{o.Exe}, o.agentArgs()...)
	return fmt.Sprintf(`[Unit]
Description=clawsh agent

[Service]
ExecStart=%s
Restart=on-failure

[Install]
WantedBy=default.target
`, strings.Join(quoteAll(words), " "))
}

// LaunchdPlist is the LaunchAgent plist.
func LaunchdPlist(o Options) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range append([]string{o.Exe}, o.agentArgs()...) {
		b.WriteString("\t\t<string>" + xmlEscape(a) + "</string>\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>` + xmlEscape(o.LogFile) + `</string>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(o.LogFile) + `</string>
</dict>
</plist>
`)
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// SchtasksCreateArgs are the arguments of `schtasks` that create the logon
// task. The task runs `agent --detach`, which re-execs the agent without a
// console and exits.
func SchtasksCreateArgs(o Options) []string {
	tr := fmt.Sprintf(`%s agent --detach --listen %s --claude %s --env-file %s`,
		winQuote(o.Exe), o.Listen, winQuote(o.Claude), winQuote(o.EnvFile))
	return []string{"/Create", "/F", "/SC", "ONLOGON", "/RL", "LIMITED", "/TN", Name, "/TR", tr}
}

func winQuote(s string) string { return `"` + s + `"` }

// Path helpers shared by the OS files.

func home() (string, error) { return os.UserHomeDir() }
