package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.env")
	body := "# comment\n\nPATH=/usr/bin:/bin\n  TOKEN = a=b=c \nEMPTY=\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParseEnvFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"PATH": "/usr/bin:/bin", "TOKEN": "a=b=c", "EMPTY": ""}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	bad := filepath.Join(t.TempDir(), "bad.env")
	_ = os.WriteFile(bad, []byte("novalue\n"), 0o600)
	if _, err := ParseEnvFile(bad); err == nil {
		t.Fatal("expected error for a line without =")
	}
}

func TestApplyEnvFileBeforeSessions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.env")
	_ = os.WriteFile(p, []byte("CLAWSH_TEST_ENVFILE=x=y\n"), 0o600)
	t.Setenv("CLAWSH_TEST_ENVFILE", "")
	if err := ApplyEnvFile(p, true); err != nil {
		t.Fatal(err)
	}
	// Manager.Create builds session env from os.Environ.
	found := false
	for _, kv := range os.Environ() {
		if kv == "CLAWSH_TEST_ENVFILE=x=y" {
			found = true
		}
	}
	if !found {
		t.Fatal("env file not applied to the agent's environment")
	}
	missing := filepath.Join(t.TempDir(), "none")
	if err := ApplyEnvFile(missing, false); err != nil {
		t.Fatalf("implicit missing file: %v", err)
	}
	if err := ApplyEnvFile(missing, true); err == nil {
		t.Fatal("explicit missing file should fail")
	}
}
