//go:build !windows

package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/varogonz95/clawsh/internal/agent"
	"github.com/varogonz95/clawsh/internal/hub"
)

// waitExit attaches stand-in: it polls the session until it exits and
// returns its exit code, like hub.AttachExit does from the exit control.
func waitExit(t *testing.T, dirs *[]string) func(context.Context, *hub.Client, string) (*int, error) {
	return func(ctx context.Context, c *hub.Client, id string) (*int, error) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			list, err := c.List(ctx)
			if err != nil {
				return nil, err
			}
			for _, s := range list {
				if s.ID == id {
					*dirs = append(*dirs, s.Dir)
					if s.ExitCode != nil {
						return s.ExitCode, nil
					}
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		return nil, errors.New("session did not exit")
	}
}

func TestRunExitCodeAndDir(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "agent.token")
	token, _, err := agent.LoadOrCreateToken(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(agent.NewServer(agent.NewManager(agent.Options{Command: "/bin/sh"}), token).Handler())
	defer srv.Close()
	listen := strings.TrimPrefix(srv.URL, "http://")

	var dirs []string
	err = runLocal(context.Background(), listen, tokenFile, "t", []string{"-c", "exit 3"}, waitExit(t, &dirs))
	var ee exitError
	if !errors.As(err, &ee) || ee.code != 3 {
		t.Fatalf("err = %v, want exit code 3", err)
	}
	cwd, _ := os.Getwd()
	if len(dirs) == 0 || dirs[0] != cwd {
		t.Fatalf("session dirs = %v, want %s", dirs, cwd)
	}
}

func TestRunNoAgent(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "agent.token")
	srv := httptest.NewServer(nil)
	listen := strings.TrimPrefix(srv.URL, "http://")
	srv.Close() // nothing listens there now
	err := runLocal(context.Background(), listen, tokenFile, "", nil, nil)
	want := "no local agent at http://" + listen + "; start one with 'clawsh agent' or 'clawsh agent install-service'"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
