package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDryRunWritesNothing(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	var out bytes.Buffer
	if err := Install(plain, Env{Dry: &out}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(Env{Dry: &out}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		filepath.Join(cfg, "systemd", "user", "clawsh-agent.service"), "ExecStart=",
		"$ systemctl --user daemon-reload", "$ systemctl --user enable --now clawsh-agent",
		"$ systemctl --user disable --now clawsh-agent",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry-run output lacks %q:\n%s", want, out.String())
		}
	}
	if entries, _ := os.ReadDir(cfg); len(entries) != 0 {
		t.Fatalf("dry-run wrote files: %v", entries)
	}
}
