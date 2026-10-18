package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarGz(t *testing.T, dir, bin, content string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: dir + "/README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: dir + "/" + bin, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, dir, bin, content string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(dir + "/" + bin)
	w.Write([]byte(content))
	zw.Close()
	return buf.Bytes()
}

// release serves a GitHub-like layout: /latest redirects to /tag/v<latest>.
func release(t *testing.T, latest string, files map[string][]byte, sums string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/v"+latest, http.StatusFound)
	})
	mux.HandleFunc("/tag/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if name == "checksums.txt" {
			w.Write([]byte(sums))
			return
		}
		if b, ok := files[name]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func sum(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func TestLatest(t *testing.T) {
	s := release(t, "0.2.0", nil, "")
	if v, err := Latest(context.Background(), s.URL); err != nil || v != "0.2.0" {
		t.Fatalf("Latest = %q, %v", v, err)
	}
	s = release(t, "0.3.0-rc.1", nil, "")
	if _, err := Latest(context.Background(), s.URL); err == nil {
		t.Fatal("pre-release accepted as latest")
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.1.9", 1},
		{"0.1.0", "0.1.0-rc.1", 1},
		{"0.1.0-rc.1", "0.1.0", -1},
		{"1.10.0", "1.9.0", 1},
		{"1.0.0", "v1.0.0", 0},
		{"0.1.0-rc.2", "0.1.0-rc.10", -1},
		{"0.1.0-alpha", "0.1.0-beta", -1},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDownload(t *testing.T) {
	tgz := tarGz(t, "clawsh_1.0.0_linux_amd64", "clawsh", "NEW-BINARY")
	zp := zipOf(t, "clawsh_1.0.0_windows_amd64", "clawsh.exe", "NEW-EXE")
	files := map[string][]byte{"clawsh_1.0.0_linux_amd64.tar.gz": tgz, "clawsh_1.0.0_windows_amd64.zip": zp}
	good := sum(tgz) + "  clawsh_1.0.0_linux_amd64.tar.gz\n" + sum(zp) + "  clawsh_1.0.0_windows_amd64.zip\n"
	ctx := context.Background()

	s := release(t, "1.0.0", files, good)
	for _, c := range []struct{ goos, want string }{{"linux", "NEW-BINARY"}, {"windows", "NEW-EXE"}} {
		p, err := Download(ctx, s.URL, "1.0.0", c.goos, "amd64", t.TempDir())
		if err != nil {
			t.Fatalf("%s: %v", c.goos, err)
		}
		if b, _ := os.ReadFile(p); string(b) != c.want {
			t.Errorf("%s: extracted %q", c.goos, b)
		}
	}

	bad := release(t, "1.0.0", files, strings.Repeat("0", 64)+"  clawsh_1.0.0_linux_amd64.tar.gz\n")
	if _, err := Download(ctx, bad.URL, "1.0.0", "linux", "amd64", t.TempDir()); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("mismatch: err = %v", err)
	}
	missing := release(t, "1.0.0", files, sum(zp)+"  clawsh_1.0.0_windows_amd64.zip\n")
	if _, err := Download(ctx, missing.URL, "1.0.0", "linux", "amd64", t.TempDir()); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("missing line: err = %v", err)
	}
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	exe, nb := filepath.Join(dir, "clawsh"), filepath.Join(dir, "new")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(nb, []byte("new"), 0o600)
	if err := Replace(exe, nb); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(exe)
	st, _ := os.Stat(exe)
	if string(b) != "new" || st.Mode().Perm()&0o100 == 0 {
		t.Errorf("content %q mode %v", b, st.Mode())
	}
	if _, err := os.Stat(filepath.Join(dir, ".clawsh.new")); err == nil {
		t.Error("temp file left behind")
	}
}

func TestDownloadRejectsBadVersion(t *testing.T) {
	for _, v := range []string{"../../etc/x", "1.0", "1.0.0/../x", ""} {
		if _, err := Download(context.Background(), "http://127.0.0.1:1", v, "linux", "amd64", t.TempDir()); err == nil || !strings.Contains(err.Error(), "invalid version") {
			t.Errorf("%q: err = %v", v, err)
		}
	}
}
