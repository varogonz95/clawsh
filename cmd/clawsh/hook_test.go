package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/varogonz95/clawsh/internal/api"
)

func tokenFile(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "agent.token")
	if err := os.WriteFile(p, []byte("k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHookPostsEvent(t *testing.T) {
	var got api.HookEvent
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/hooks" || r.Method != "POST" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	var stderr bytes.Buffer
	in := `{"session_id":"c1","transcript_path":"/t.jsonl","cwd":"/w","message":"needs you"}`
	hookCmd([]string{"Notification", "--listen", strings.TrimPrefix(srv.URL, "http://"), "--token-file", tokenFile(t)},
		strings.NewReader(in), &stderr, "abc", 42)
	want := api.HookEvent{Event: "Notification", SessionID: "c1", TranscriptPath: "/t.jsonl", Cwd: "/w",
		Message: "needs you", ClawshSessionID: "abc", Pid: 42}
	if got != want || auth != "Bearer k" {
		t.Fatalf("got %+v auth %q (stderr %q)", got, auth, stderr.String())
	}
}

func TestHookNeverFails(t *testing.T) {
	var stderr bytes.Buffer
	// No agent listening (port 1), then malformed stdin, then missing token.
	start := time.Now()
	hookCmd([]string{"Stop", "--listen", "127.0.0.1:1", "--token-file", tokenFile(t)}, strings.NewReader(`{}`), &stderr, "", 1)
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("took %v", time.Since(start))
	}
	hookCmd([]string{"Stop", "--listen", "127.0.0.1:1", "--token-file", tokenFile(t)}, strings.NewReader(`not json`), &stderr, "", 1)
	hookCmd([]string{"Stop", "--token-file", filepath.Join(t.TempDir(), "none")}, strings.NewReader(`{}`), &stderr, "", 1)
	if stderr.Len() == 0 {
		t.Fatal("expected problems reported on stderr")
	}
}
