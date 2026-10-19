//go:build !windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRelease serves release 9.9.9 whose binary is a shell script printing "fake-new".
func fakeRelease(t *testing.T) *httptest.Server {
	name := fmt.Sprintf("clawsh_9.9.9_%s_%s", runtime.GOOS, runtime.GOARCH)
	script := "#!/bin/sh\necho fake-new\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: name + "/clawsh", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	tw.Write([]byte(script))
	tw.Close()
	gz.Close()
	sums := fmt.Sprintf("%x  %s.tar.gz\n", sha256.Sum256(buf.Bytes()), name)

	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/tag/v9.9.9", http.StatusFound) })
	mux.HandleFunc("/tag/v9.9.9", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/download/v9.9.9/checksums.txt", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sums)) })
	mux.HandleFunc("/download/v9.9.9/"+name+".tar.gz", func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) })
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func buildClawsh(t *testing.T, version string) string {
	out := filepath.Join(t.TempDir(), "clawsh")
	ld := "-X github.com/varogonz95/clawsh/internal/api.Version=" + version
	if b, err := exec.Command("go", "build", "-ldflags", ld, "-o", out, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, b)
	}
	return out
}

func runBin(bin string, env []string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func TestUpdateCommand(t *testing.T) {
	s := fakeRelease(t)
	env := []string{"CLAWSH_RELEASES_URL=" + s.URL, "CLAWSH_LAUNCHER="}
	old := buildClawsh(t, "0.1.0")

	if out, err := runBin(old, env, "update", "--check"); err != nil || !strings.Contains(out, "clawsh 9.9.9 is available (you have 0.1.0)") {
		t.Fatalf("--check: %v\n%s", err, out)
	}

	if out, err := runBin(old, env, "update", "--check", "--version", "0.0.1"); err != nil || !strings.Contains(out, "0.0.1 is older than the installed 0.1.0") {
		t.Fatalf("--check older: %v\n%s", err, out)
	}

	if out, err := runBin(old, append(env, "CLAWSH_LAUNCHER=npm"), "update"); err == nil || !strings.Contains(out, "installed with npm: run npm install -g clawsh-cli@latest") {
		t.Fatalf("npm: %v\n%s", err, out)
	}

	if out, err := runBin(old, env, "update"); err != nil || !strings.Contains(out, "updated clawsh 0.1.0 → 9.9.9") {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if out, _ := runBin(old, nil); !strings.Contains(out, "fake-new") {
		t.Fatalf("binary not replaced: %s", out)
	}

	cur := buildClawsh(t, "9.9.9")
	if out, err := runBin(cur, env, "update"); err != nil || !strings.Contains(out, "clawsh 9.9.9 is up to date") {
		t.Fatalf("up to date: %v\n%s", err, out)
	}
}

func TestUpdateRefusesDevBuild(t *testing.T) {
	s := fakeRelease(t)
	dev := buildClawsh(t, "dev")
	out, err := runBin(dev, []string{"CLAWSH_RELEASES_URL=" + s.URL, "CLAWSH_LAUNCHER="}, "update")
	if err == nil || !strings.Contains(out, "this is a development build") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestUpdateRefusesDescribeBuild(t *testing.T) {
	s := fakeRelease(t)
	for _, v := range []string{"0.1.0-3-gabc1234", "0.1.0-dirty"} {
		bin := buildClawsh(t, v)
		out, err := runBin(bin, []string{"CLAWSH_RELEASES_URL=" + s.URL, "CLAWSH_LAUNCHER="}, "update")
		if err == nil || !strings.Contains(out, "this is a development build") {
			t.Fatalf("%s: %v\n%s", v, err, out)
		}
	}
}
