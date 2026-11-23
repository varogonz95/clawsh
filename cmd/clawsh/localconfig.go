package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/hub"
)

// localListen is the agent's default --listen, which the hub assumes for
// the "local" host it creates.
const localListen = ":7420"

// ensureLocalConfig creates the hosts config with a "local" host when cfgPath
// is the default config path and the file doesn't exist yet, so hub commands
// on a fresh install see this machine's agent. The token comes from tokenPath
// (created if missing) so the agent, when it first starts, reuses it. It
// never fails the command: errors become one warning line on stderr.
func ensureLocalConfig(cfgPath, defaultPath, tokenPath string, stderr io.Writer) {
	if cfgPath != defaultPath {
		return
	}
	if _, err := os.Stat(cfgPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return
	}
	created, err := func() (bool, error) {
		token, _, err := agent.LoadOrCreateToken(tokenPath)
		if err != nil {
			return false, err
		}
		return hub.EnsureLocalHost(cfgPath, localListen, token)
	}()
	if err != nil {
		fmt.Fprintf(stderr, "clawsh: could not create %s with a local host: %v\n", cfgPath, err)
		return
	}
	if created {
		fmt.Fprintf(stderr, "clawsh: created %s with host %q (%s); run \"clawsh agent\" on this machine to use it\n",
			cfgPath, "local", hub.LocalHostURL(localListen))
	}
}

// ensureDefaultLocalConfig is ensureLocalConfig with the real default paths.
func ensureDefaultLocalConfig(cfgPath string) {
	ensureLocalConfig(cfgPath, hub.DefaultConfigPath(), agent.DefaultTokenPath(), os.Stderr)
}
