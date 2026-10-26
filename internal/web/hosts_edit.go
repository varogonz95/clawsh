package web

import (
	"errors"
	"os"
	"time"

	"github.com/varogonz95/clawsh/internal/hub"
)

var (
	errBrokenFile = errors.New("hosts.toml has an error; fix it by hand first")
	errNoHost     = errors.New("unknown machine")
	errDupHost    = errors.New("a machine with that name already exists")
)

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
	if err := hub.WriteAtomic(f.path, data); err != nil {
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
