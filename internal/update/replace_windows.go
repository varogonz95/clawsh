//go:build windows

package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Replace puts newBin in place of exe. A running .exe can be renamed but not
// overwritten, so the old one moves to exe+".old" (removed by CleanupOld on a
// later start). On failure the old binary is put back.
func Replace(exe, newBin string) error {
	// A still-running process from an earlier update holds exe+".old" open,
	// so it can't be removed: use a unique name instead.
	old := exe + ".old"
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		old = fmt.Sprintf("%s.old.%d", exe, os.Getpid())
		os.Remove(old)
	}
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

// CleanupOld removes leftover exe+".old*" files from earlier updates (best
// effort; ones still in use fail and are retried on a later start).
func CleanupOld(exe string) {
	matches, _ := filepath.Glob(exe + ".old*")
	for _, m := range matches {
		os.Remove(m)
	}
}
