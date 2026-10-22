package hub

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RenameFile is os.Rename; tests replace it to simulate a failed write.
var RenameFile = os.Rename

// WriteAtomic replaces path with data: backup of the old file, then a temp
// file in the same directory (0600, fsynced) renamed over it.
func WriteAtomic(path string, data []byte) error {
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
	if err := RenameFile(name, path); err != nil {
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
