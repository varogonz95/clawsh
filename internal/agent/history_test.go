//go:build !windows

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/varogonz95/clawsh/internal/api"
)

func newHistoryManager(t *testing.T, file string) *Manager {
	t.Helper()
	m := NewManager(Options{Command: "/bin/sh", HistoryFile: file})
	t.Cleanup(m.Shutdown)
	return m
}

func TestRecentDirsOrderAndDedup(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	file := filepath.Join(t.TempDir(), "recent-dirs.json")
	m := newHistoryManager(t, file)
	for _, d := range []string{a, b, a} {
		if _, err := m.Create(api.CreateRequest{Dir: d}); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := m.RecentDirs(), []string{a, b}; !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("history file: %v, %v", fi, err)
	}
	// A new manager (agent restart) reads the same file.
	if got := newHistoryManager(t, file).RecentDirs(); !reflect.DeepEqual(got, []string{a, b}) {
		t.Fatalf("after restart = %v", got)
	}
}

func TestRecentDirsCap(t *testing.T) {
	m := newHistoryManager(t, filepath.Join(t.TempDir(), "h.json"))
	root := t.TempDir()
	for i := 0; i < maxRecentDirs+5; i++ {
		d := filepath.Join(root, fmt.Sprint(i))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Create(api.CreateRequest{Dir: d}); err != nil {
			t.Fatal(err)
		}
	}
	got := m.RecentDirs()
	if len(got) != maxRecentDirs {
		t.Fatalf("len = %d, want %d", len(got), maxRecentDirs)
	}
	if want := filepath.Join(root, fmt.Sprint(maxRecentDirs+4)); got[0] != want {
		t.Fatalf("first = %s, want %s", got[0], want)
	}
}

func TestRecentDirsEmptyDirRecordsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newHistoryManager(t, filepath.Join(t.TempDir(), "h.json"))
	if _, err := m.Create(api.CreateRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := m.RecentDirs(); !reflect.DeepEqual(got, []string{home}) {
		t.Fatalf("history = %v, want [%s]", got, home)
	}
}

func TestRecentDirsFailedCreateRecordsNothing(t *testing.T) {
	m := newHistoryManager(t, filepath.Join(t.TempDir(), "h.json"))
	if _, err := m.Create(api.CreateRequest{Dir: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("expected error")
	}
	if got := m.RecentDirs(); len(got) != 0 {
		t.Fatalf("history = %v, want empty", got)
	}
}

func TestRecentDirsSkipsDeletedAndCorrupt(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	file := filepath.Join(t.TempDir(), "h.json")
	m := newHistoryManager(t, file)
	for _, d := range []string{a, b} {
		if _, err := m.Create(api.CreateRequest{Dir: d}); err != nil {
			t.Fatal(err)
		}
	}
	os.RemoveAll(a)
	if got := m.RecentDirs(); !reflect.DeepEqual(got, []string{b}) {
		t.Fatalf("history = %v, want [%s]", got, b)
	}
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := m.RecentDirs(); got == nil || len(got) != 0 {
		t.Fatalf("corrupt file: %#v, want empty non-nil", got)
	}
}

func TestRecentDirsUnwritableDoesNotFailCreate(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m := newHistoryManager(t, filepath.Join(blocker, "h.json")) // parent is a file
	if _, err := m.Create(api.CreateRequest{Dir: t.TempDir()}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
}

func TestRecentDirsDisabled(t *testing.T) {
	m := newHistoryManager(t, "")
	if _, err := m.Create(api.CreateRequest{Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if got := m.RecentDirs(); got == nil || len(got) != 0 {
		t.Fatalf("disabled: %#v", got)
	}
}
