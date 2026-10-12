// Package paths locates clawsh's per-user config directory.
package paths

import (
	"os"
	"path/filepath"
)

// legacyName is the config directory used before the rename to clawsh.
const legacyName = "ccm"

// ConfigDir returns <user config dir>/clawsh. Installs from before the
// rename keep working: if that directory doesn't exist but the old ccm one
// does, the old one is used (move it to migrate).
func ConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return configDir(base)
}

func configDir(base string) string {
	dir := filepath.Join(base, "clawsh")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	if fi, err := os.Stat(filepath.Join(base, legacyName)); err == nil && fi.IsDir() {
		return filepath.Join(base, legacyName)
	}
	return dir
}

// DefaultEnvFile is the agent's optional environment file.
func DefaultEnvFile() string { return filepath.Join(ConfigDir(), "agent.env") }
