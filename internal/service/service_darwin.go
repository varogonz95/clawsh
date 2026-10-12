package service

import (
	"fmt"
	"os"
	"path/filepath"
)

// PlistPath is where the LaunchAgent lives.
func PlistPath() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "Library", "LaunchAgents", Label+".plist"), nil
}

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// Install writes the plist and loads it. A previously loaded copy is booted
// out first so re-running picks up changes.
func Install(o Options, e Env) error {
	p, err := PlistPath()
	if err != nil {
		return err
	}
	if err := e.writeFile(p, LaunchdPlist(o)); err != nil {
		return err
	}
	e.tryRun("launchctl", "bootout", domain()+"/"+Label)
	return e.run("launchctl", "bootstrap", domain(), p)
}

// Uninstall unloads the agent and removes the plist.
func Uninstall(e Env) error {
	p, err := PlistPath()
	if err != nil {
		return err
	}
	e.tryRun("launchctl", "bootout", domain()+"/"+Label)
	return e.remove(p)
}
