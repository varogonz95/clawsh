package web

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/varogonz95/clawsh/internal/hub"
)

var (
	errBrokenFile = errors.New("hosts.toml has an error; fix it by hand first")
	errNoHost     = errors.New("unknown machine")
	errDupHost    = errors.New("a machine with that name already exists")
)

// renameFile is os.Rename; tests replace it to simulate a failed write.
var renameFile = os.Rename

// Mutate applies fn to the hosts on disk and writes the result. It holds the
// lock the reader uses, so a write is never observed half done. A file that
// doesn't parse is never overwritten. The previous file is kept as
// hosts.toml.bak (0600).
func (f *hostsFile) Mutate(fn func([]hub.Host) ([]hub.Host, error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.reloadLocked()
	if f.err != nil {
		return errBrokenFile
	}
	cur := append([]hub.Host(nil), f.hosts...)
	next, err := fn(cur)
	if err != nil {
		return err
	}
	data, err := hub.EncodeConfig(next)
	if err != nil {
		return err
	}
	if err := writeAtomic(f.path, data); err != nil {
		return err
	}
	f.hosts, f.err = next, nil
	if st, err := os.Stat(f.path); err == nil {
		f.modTime, f.size = st.ModTime(), st.Size()
	} else {
		f.modTime, f.size = time.Time{}, 0
	}
	return nil
}

// writeAtomic replaces path with data: backup of the old file, then a temp
// file in the same directory (0600, fsynced) renamed over it.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if old, err := os.Open(path); err == nil {
		err = copyFile(old, path+".bak")
		old.Close()
		if err != nil {
			return fmt.Errorf("back up hosts.toml: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".hosts.toml.*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameFile(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func copyFile(src *os.File, dst string) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// OpenFile keeps the mode of a pre-existing file.
	_ = out.Chmod(0o600)
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
