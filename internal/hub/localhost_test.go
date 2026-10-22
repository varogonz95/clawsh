package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalHostURL(t *testing.T) {
	for in, want := range map[string]string{
		":7420":             "http://localhost:7420",
		":7500":             "http://localhost:7500",
		"0.0.0.0:7420":      "http://localhost:7420",
		"[::]:7420":         "http://localhost:7420",
		"192.168.1.20:7420": "http://192.168.1.20:7420",
		"127.0.0.1:1":       "http://127.0.0.1:1",
		"[::1]:7420":        "http://[::1]:7420",
		"box.lan:7420":      "http://box.lan:7420",
	} {
		if got := LocalHostURL(in); got != want {
			t.Errorf("LocalHostURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureLocalHost(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "hosts.toml")
	created, err := EnsureLocalHost(p, ":7420", "tok")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	h, err := c.Find("local")
	if err != nil || h.URL != "http://localhost:7420" || h.Token != "tok" {
		t.Fatalf("host %+v err %v", h, err)
	}
	before, _ := os.ReadFile(p)
	created, err = EnsureLocalHost(p, ":9", "other")
	if err != nil || created {
		t.Fatalf("second: created=%v err=%v", created, err)
	}
	if after, _ := os.ReadFile(p); string(after) != string(before) {
		t.Error("existing file modified")
	}
}

func TestEnsureLocalHostWriteFailure(t *testing.T) {
	RenameFile = func(string, string) error { return os.ErrPermission }
	defer func() { RenameFile = os.Rename }()
	p := filepath.Join(t.TempDir(), "hosts.toml")
	if created, err := EnsureLocalHost(p, ":7420", "t"); err == nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("file left behind")
	}
}
