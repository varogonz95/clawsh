package agent

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/varogonz95/clawsh/internal/paths"
)

// maxRecentDirs caps the folder history kept per machine.
const maxRecentDirs = 20

// DefaultHistoryPath is where the agent keeps the folders it started
// sessions in.
func DefaultHistoryPath() string {
	return filepath.Join(paths.ConfigDir(), "recent-dirs.json")
}

// dirHistory is a most-recent-first list of folders persisted as a JSON array.
// An empty path disables it.
type dirHistory struct {
	path string
	mu   sync.Mutex
}

func (h *dirHistory) load() []string {
	b, err := os.ReadFile(h.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("recent dirs: read %s: %v", h.path, err)
		}
		return nil
	}
	var dirs []string
	if err := json.Unmarshal(b, &dirs); err != nil {
		log.Printf("recent dirs: ignoring corrupt %s: %v", h.path, err)
		return nil
	}
	return dirs
}

// record moves dir to the front. Failures are logged, never returned.
func (h *dirHistory) record(dir string) {
	if h.path == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	dirs := []string{dir}
	for _, d := range h.load() {
		if d != dir {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) > maxRecentDirs {
		dirs = dirs[:maxRecentDirs]
	}
	if err := h.write(dirs); err != nil {
		log.Printf("recent dirs: write %s: %v", h.path, err)
	}
}

func (h *dirHistory) write(dirs []string) error {
	b, err := json.Marshal(dirs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(h.path), ".recent-dirs-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), h.path)
}

// recent returns the history without folders that no longer exist.
func (h *dirHistory) recent() []string {
	out := []string{}
	if h.path == "" {
		return out
	}
	h.mu.Lock()
	dirs := h.load()
	h.mu.Unlock()
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			out = append(out, d)
		}
	}
	return out
}
