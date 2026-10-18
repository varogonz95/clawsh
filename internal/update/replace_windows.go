//go:build windows

package update

import (
	"io"
	"os"
)

// Replace puts newBin in place of exe. A running .exe can be renamed but not
// overwritten, so the old one moves to exe+".old" (removed by CleanupOld on a
// later start). On failure the old binary is put back.
func Replace(exe, newBin string) error {
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := moveFile(newBin, exe); err != nil {
		os.Remove(exe)
		if rerr := os.Rename(old, exe); rerr != nil {
			return rerr
		}
		return err
	}
	return nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// CleanupOld removes the leftover exe+".old" from an earlier update (best effort).
func CleanupOld(exe string) { os.Remove(exe + ".old") }
