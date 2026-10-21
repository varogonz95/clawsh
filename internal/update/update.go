// Package update implements `clawsh update`: finding the latest release,
// downloading and verifying its archive, and replacing the running binary.
// It mirrors site/install.sh and uses only the standard library.
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultBase is the releases URL used when CLAWSH_RELEASES_URL is unset.
const DefaultBase = "https://github.com/varogonz95/clawsh/releases"

var versionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// maxBinary caps the size of an extracted binary.
const maxBinary = 512 << 20

func client() *http.Client { return &http.Client{Timeout: 5 * time.Minute} }

// cleanURL drops the query string and credentials so errors don't leak them.
func cleanURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery, u.User, u.Fragment = "", nil, ""
	return u.String()
}

func get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cleanURL(rawURL), err)
	}
	resp, err := client().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("%s: %w", cleanURL(rawURL), err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", cleanURL(rawURL), resp.Status)
	}
	return resp, nil
}

// Latest returns the version (without the leading v) that <base>/latest
// redirects to. Pre-releases are ignored: GitHub's "latest" never points at one.
func Latest(ctx context.Context, base string) (string, error) {
	resp, err := get(ctx, strings.TrimRight(base, "/")+"/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	tag := path.Base(resp.Request.URL.Path)
	if len(tag) < 2 || tag[0] != 'v' || tag[1] < '0' || tag[1] > '9' {
		return "", fmt.Errorf("couldn't find the latest release at %s/latest", cleanURL(base))
	}
	v := tag[1:]
	if strings.Contains(v, "-") {
		return "", fmt.Errorf("latest release %s is a pre-release", tag)
	}
	return v, nil
}

// Compare orders two semver strings (leading v optional): -1, 0 or 1. Core
// numbers compare numerically; a pre-release sorts below the plain version.
func Compare(a, b string) int {
	ac, apre := split(a)
	bc, bpre := split(b)
	for i := 0; i < 3; i++ {
		if ac[i] != bc[i] {
			if ac[i] < bc[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	}
	return comparePre(apre, bpre)
}

func split(v string) ([3]int, string) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	var core [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		core[i], _ = strconv.Atoi(p)
	}
	return core, pre
}

func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil && an != bn:
			if an < bn {
				return -1
			}
			return 1
		case aerr == nil && berr != nil:
			return -1
		case aerr != nil && berr == nil:
			return 1
		case aerr != nil && berr != nil && as[i] != bs[i]:
			return strings.Compare(as[i], bs[i])
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// ArchiveName is the release asset name for a version and platform.
func ArchiveName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("clawsh_%s_%s_%s%s", version, goos, goarch, ext)
}

// Download fetches the release archive for version and platform from
// <base>/download/v<version>/, checks it against checksums.txt, extracts the
// binary into dir and returns its path.
func Download(ctx context.Context, base, version, goos, goarch, dir string) (string, error) {
	version = strings.TrimPrefix(version, "v")
	if !versionRE.MatchString(version) {
		return "", fmt.Errorf("invalid version %q", version)
	}
	root := strings.TrimRight(base, "/") + "/download/v" + version + "/"
	name := ArchiveName(version, goos, goarch)

	sums, err := fetchBytes(ctx, root+"checksums.txt", 1<<20)
	if err != nil {
		return "", err
	}
	want := ""
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return "", fmt.Errorf("checksums.txt has no entry for %s", name)
	}

	arch := filepath.Join(dir, "archive") // fixed name: nothing from the version reaches a local path
	if err := fetchFile(ctx, root+name, arch); err != nil {
		return "", err
	}
	got, err := fileSHA256(arch)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("checksum mismatch for %s: got %s, want %s", name, got, want)
	}

	bin := "clawsh"
	if goos == "windows" {
		bin = "clawsh.exe"
	}
	out := filepath.Join(dir, "new-binary")
	if goos == "windows" {
		err = extractZip(arch, bin, out)
	} else {
		err = extractTarGz(arch, bin, out)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

func fetchBytes(ctx context.Context, u string, limit int64) ([]byte, error) {
	resp, err := get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cleanURL(u), err)
	}
	return b, nil
}

func fetchFile(ctx context.Context, u, dst string) error {
	resp, err := get(ctx, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", cleanURL(u), err)
	}
	return f.Close()
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeBinary(r io.Reader, dst string) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxBinary+1))
	if err == nil && n > maxBinary {
		err = errors.New("binary too large")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// isBinary matches <dir>/clawsh[.exe] at depth one, as the release lays it out.
func isBinary(name, bin string) bool {
	return path.Base(name) == bin && strings.Count(strings.Trim(name, "/"), "/") == 1
}

func extractTarGz(archive, bin, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s not found in archive", bin)
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg && isBinary(h.Name, bin) {
			return writeBinary(tr, dst)
		}
	}
}

func extractZip(archive, bin, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.FileInfo().Mode().IsRegular() && isBinary(zf.Name, bin) {
			r, err := zf.Open()
			if err != nil {
				return err
			}
			defer r.Close()
			return writeBinary(r, dst)
		}
	}
	return fmt.Errorf("%s not found in archive", bin)
}
