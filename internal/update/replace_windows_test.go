//go:build windows

package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceWindows(t *testing.T) {
	dir := t.TempDir()
	exe, nb := filepath.Join(dir, "clawsh.exe"), filepath.Join(dir, "new.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(nb, []byte("new"), 0o755)
	if err := Replace(exe, nb); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("content %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Errorf(".old content %q", b)
	}
	CleanupOld(exe)
	if _, err := os.Stat(exe + ".old"); err == nil {
		t.Error(".old left behind")
	}
}
