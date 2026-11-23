package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/hub"
)

func TestEnsureLocalConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clawsh")
	cfg := filepath.Join(dir, "hosts.toml")
	tok := filepath.Join(dir, "agent.token")

	t.Run("creates when missing", func(t *testing.T) {
		var out bytes.Buffer
		ensureLocalConfig(cfg, cfg, tok, &out)
		if !strings.Contains(out.String(), "created") || strings.Count(out.String(), "\n") != 1 {
			t.Fatalf("output %q", out.String())
		}
		want := `clawsh: created ` + cfg + ` with host "local" (http://localhost:7420); run "clawsh agent" on this machine to use it` + "\n"
		if out.String() != want {
			t.Errorf("output %q, want %q", out.String(), want)
		}
		if runtime.GOOS != "windows" {
			if st, err := os.Stat(cfg); err != nil || st.Mode().Perm() != 0o600 {
				t.Errorf("stat %v err %v", st, err)
			}
		}
		c, err := hub.LoadConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Hosts) != 1 {
			t.Fatalf("hosts %+v", c.Hosts)
		}
		token, created, err := agent.LoadOrCreateToken(tok)
		if err != nil || created {
			t.Fatalf("token created=%v err=%v", created, err)
		}
		h := c.Hosts[0]
		if h.Name != "local" || h.URL != "http://localhost:7420" || h.Token != token {
			t.Errorf("host %+v, token %q", h, token)
		}
	})

	t.Run("existing file untouched", func(t *testing.T) {
		before, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		ensureLocalConfig(cfg, cfg, tok, &out)
		if out.Len() != 0 {
			t.Errorf("output %q", out.String())
		}
		if after, _ := os.ReadFile(cfg); !bytes.Equal(before, after) {
			t.Error("existing file modified")
		}
	})

	t.Run("non-default path not created", func(t *testing.T) {
		other := filepath.Join(t.TempDir(), "custom.toml")
		var out bytes.Buffer
		ensureLocalConfig(other, cfg, tok, &out)
		if out.Len() != 0 {
			t.Errorf("output %q", out.String())
		}
		if _, err := os.Stat(other); !os.IsNotExist(err) {
			t.Errorf("custom path created: %v", err)
		}
	})

	t.Run("write failure warns", func(t *testing.T) {
		hub.RenameFile = func(string, string) error { return os.ErrPermission }
		defer func() { hub.RenameFile = os.Rename }()
		p := filepath.Join(t.TempDir(), "hosts.toml")
		var out bytes.Buffer
		ensureLocalConfig(p, p, tok, &out)
		if !strings.HasPrefix(out.String(), "clawsh: could not create "+p+" with a local host: ") ||
			strings.Count(out.String(), "\n") != 1 {
			t.Errorf("output %q", out.String())
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("file exists after failed write: %v", err)
		}
	})

	t.Run("token failure warns", func(t *testing.T) {
		d := t.TempDir()
		blocker := filepath.Join(d, "file")
		if err := os.WriteFile(blocker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, "hosts.toml")
		var out bytes.Buffer
		ensureLocalConfig(p, p, filepath.Join(blocker, "agent.token"), &out)
		if !strings.HasPrefix(out.String(), "clawsh: could not create "+p+" with a local host: ") {
			t.Errorf("output %q", out.String())
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("config created despite token error: %v", err)
		}
	})
}
