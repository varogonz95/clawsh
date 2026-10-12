package service

import (
	"fmt"
	"os"
	"path/filepath"
)

// UnitPath is where the systemd user unit lives.
func UnitPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, err := home()
		if err != nil {
			return "", err
		}
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "systemd", "user", Name+".service"), nil
}

// Install writes the unit and enables it now and at login. Re-running
// rewrites the unit and restarts nothing it doesn't have to.
func Install(o Options, e Env) error {
	p, err := UnitPath()
	if err != nil {
		return err
	}
	if err := e.writeFile(p, SystemdUnit(o)); err != nil {
		return err
	}
	if err := e.run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := e.run("systemctl", "--user", "enable", "--now", Name); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "On a headless machine the agent only runs while you are logged in; run `loginctl enable-linger $USER` to keep it running without a login.")
	return nil
}

// Uninstall stops and disables the unit and removes its file.
func Uninstall(e Env) error {
	p, err := UnitPath()
	if err != nil {
		return err
	}
	e.tryRun("systemctl", "--user", "disable", "--now", Name)
	if err := e.remove(p); err != nil {
		return err
	}
	e.tryRun("systemctl", "--user", "daemon-reload")
	return nil
}
