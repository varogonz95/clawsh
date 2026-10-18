package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/mcp"
	"github.com/varogonz95/clawsh/internal/update"
)

func releasesURL() string {
	if u := os.Getenv("CLAWSH_RELEASES_URL"); u != "" {
		return u
	}
	return update.DefaultBase
}

func isDevBuild() bool { return strings.HasPrefix(api.Version, "dev") }

// launchedByNPM is read once at start and the variable removed, so agents and
// sessions this process spawns don't inherit it.
var launchedByNPM = func() bool {
	v := os.Getenv("CLAWSH_LAUNCHER") == "npm"
	os.Unsetenv("CLAWSH_LAUNCHER")
	return v
}()

func installedWithNPM(exe string) bool {
	if launchedByNPM {
		return true
	}
	for _, p := range strings.Split(filepath.ToSlash(exe), "/") {
		if p == "node_modules" {
			return true
		}
	}
	return false
}

func runUpdate(args []string) error {
	flags := flag.NewFlagSet("update", flag.ExitOnError)
	check := flags.Bool("check", false, "only report whether a newer version exists")
	version := flags.String("version", "", "install this exact version (pre-releases and downgrades allowed)")
	force := flags.Bool("force", false, "allow updating a development build")
	parseInterspersed(flags, args)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	base := releasesURL()
	cur := api.Version

	target := strings.TrimPrefix(*version, "v")
	if target == "" {
		v, err := update.Latest(ctx, base)
		if err != nil {
			return err
		}
		target = v
	}

	if *check {
		if !isDevBuild() && update.Compare(target, cur) <= 0 {
			fmt.Printf("clawsh %s is up to date\n", cur)
		} else {
			fmt.Printf("clawsh %s is available (you have %s)\n", target, cur)
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if installedWithNPM(exe) {
		return errors.New("installed with npm: run npm install -g clawsh@latest")
	}
	if isDevBuild() && !*force {
		return errors.New("this is a development build (" + cur + "); use --force to replace it anyway")
	}

	if !isDevBuild() {
		switch c := update.Compare(target, cur); {
		case c == 0 || (c < 0 && *version == ""):
			fmt.Printf("clawsh %s is up to date\n", cur)
			return nil
		case c < 0:
			fmt.Printf("note: %s is older than the installed %s\n", target, cur)
		}
	}

	tmp, err := os.MkdirTemp("", "clawsh-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	fmt.Printf("downloading clawsh %s\n", target)
	bin, err := update.Download(ctx, base, target, runtime.GOOS, runtime.GOARCH, tmp)
	if err != nil {
		return err
	}
	if err := update.Replace(exe, bin); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			hint := "curl -fsSL https://varogonz95.github.io/clawsh/install.sh | sh, or run this command with sudo"
			if runtime.GOOS == "windows" {
				hint = "irm https://varogonz95.github.io/clawsh/install.ps1 | iex"
			}
			return fmt.Errorf("can't write %s: %w\nrerun the installer (%s)", exe, err, hint)
		}
		return err
	}
	fmt.Printf("updated clawsh %s → %s at %s\n", cur, target, exe)

	if v := localAgentVersion(); v != "" && v != target {
		fmt.Printf("the agent on this machine still runs %s; restart it when its sessions are done (sessions don't survive a restart)\n", v)
	}
	return nil
}

// localAgentVersion asks the agent on this machine for its version, or ""
// when none answers within a second.
func localAgentVersion() string {
	u, err := mcp.LocalURL(":7420")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"/v1/health", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var h api.Health
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&h) != nil {
		return ""
	}
	return h.Version
}
