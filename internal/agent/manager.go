package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/ptyx"
)

var (
	ErrNotFound  = errors.New("session not found")
	ErrAmbiguous = errors.New("session id prefix is ambiguous")
)

type Options struct {
	// Command is the program every session runs. It is fixed by the agent
	// operator and never taken from API requests: only args are.
	Command    string
	Scrollback int // bytes of output kept per session for replay on attach
	// ExternalTTL is how long an announced external session survives
	// without renewal (default DefaultExternalTTL).
	ExternalTTL time.Duration
	// HistoryFile is where the folders of started sessions are remembered
	// (see DefaultHistoryPath). Empty disables the history.
	HistoryFile string
}

type Manager struct {
	opts     Options
	mu       sync.RWMutex
	sessions map[string]*Session
	ext      *externals
	history  *dirHistory
}

func NewManager(o Options) *Manager {
	if o.Command == "" {
		o.Command = "claude"
	}
	if o.Scrollback <= 0 {
		o.Scrollback = 2 << 20
	}
	return &Manager{opts: o, sessions: map[string]*Session{}, ext: newExternals(o.ExternalTTL), history: &dirHistory{path: o.HistoryFile}}
}

func (m *Manager) Create(req api.CreateRequest) (*Session, error) {
	dir, err := resolveDir(req.Dir)
	if err != nil {
		return nil, err
	}
	cols, rows := req.Cols, req.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 120, 40
	}
	name := req.Name
	if name == "" {
		name = filepath.Base(dir)
	}

	id := newID()
	env := append(os.Environ(), "CLAWSH_SESSION_ID="+id)
	if runtime.GOOS != "windows" {
		env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	}

	p, err := ptyx.Start(ptyx.Options{
		Command: m.opts.Command, Args: req.Args, Dir: dir, Env: env,
		Cols: cols, Rows: rows,
	})
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", m.opts.Command, err)
	}
	s := newSession(id, name, dir, req.Args, p, m.opts.Scrollback)

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	m.history.record(dir)
	return s, nil
}

// RecentDirs lists the folders sessions were started in, most recent first,
// leaving out folders that no longer exist.
func (m *Manager) RecentDirs() []string { return m.history.recent() }

// Get resolves a full id or a unique id prefix.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[id]; ok {
		return s, nil
	}
	var match *Session
	for k, s := range m.sessions {
		if id != "" && strings.HasPrefix(k, id) {
			if match != nil {
				return nil, ErrAmbiguous
			}
			match = s
		}
	}
	if match == nil {
		return nil, ErrNotFound
	}
	return match, nil
}

func (m *Manager) List() []api.Session {
	m.mu.RLock()
	out := make([]api.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info())
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// Announce creates or renews the lease of an external session.
func (m *Manager) Announce(id string, req api.AnnounceRequest) (api.External, error) {
	return m.ext.announce(id, req)
}

// Withdraw forgets an external session.
func (m *Manager) Withdraw(id string) error { return m.ext.withdraw(id) }

// Externals lists announced external sessions whose lease is still valid.
func (m *Manager) Externals() []api.External { return m.ext.list() }

// Remove stops the session (if still running) and forgets it.
func (m *Manager) Remove(id string) error {
	s, err := m.Get(id)
	if err != nil {
		return err
	}
	stop(s)
	m.mu.Lock()
	delete(m.sessions, s.ID)
	m.mu.Unlock()
	return nil
}

// Shutdown stops every session. Sessions do not survive the agent (yet — see M4).
func (m *Manager) Shutdown() {
	m.mu.RLock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.RUnlock()
	var wg sync.WaitGroup
	for _, s := range all {
		wg.Add(1)
		go func(s *Session) { defer wg.Done(); stop(s) }(s)
	}
	wg.Wait()
}

func stop(s *Session) {
	if _, exited := s.ExitCode(); exited {
		return
	}
	_ = s.Kill(false)
	select {
	case <-s.Done():
		return
	case <-time.After(3 * time.Second):
	}
	_ = s.Kill(true)
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
	}
}

func resolveDir(dir string) (string, error) {
	home, _ := os.UserHomeDir()
	switch {
	case dir == "":
		dir = home
	case dir == "~":
		dir = home
	case strings.HasPrefix(dir, "~/"), strings.HasPrefix(dir, `~\`):
		dir = filepath.Join(home, dir[2:])
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("dir %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("dir %q: not a directory", dir)
	}
	return dir, nil
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
