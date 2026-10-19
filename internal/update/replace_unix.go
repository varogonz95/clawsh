//go:build !windows

package update

import (
	"io"
	"os"
	"path/filepath"
)

// Replace puts newBin in place of exe. The new file is written next to exe
// (same filesystem, mode of the old binary) and renamed over it, which is
// atomic and fine for a running executable on Unix.
func Replace(exe, newBin string) error {
	mode := os.FileMode(0o755)
	if st, err := os.Stat(exe); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(exe), ".clawsh.new")
	if err := copyFile(newBin, tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// CleanupOld is a no-op on Unix.
func CleanupOld(exe string) {}
