package agent

import (
	"sync"
	"time"

	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/ptyx"
)

// Session is one claude process running inside a PTY owned by the agent.
// It outlives any viewer: viewers attach and detach, the process keeps running.
type Session struct {
	ID      string
	Name    string
	Dir     string
	Args    []string
	Created time.Time

	p         ptyx.PTY
	ptyMu     sync.Mutex // serializes Resize with the final Close
	ptyClosed bool       // set under ptyMu by waitLoop; Resize is a no-op after

	mu         sync.Mutex
	sb         *Scrollback
	subs       map[*subscriber]struct{}
	readerDone bool // PTY output finished; no more data will arrive
	exited     bool
	exitCode   int
	status     api.Status // hook-driven; empty until the first hook
	claudeID   string
	transcript string
	lastEvent  time.Time

	done chan struct{} // closed once the process has exited and the PTY is closed
}

type subscriber struct {
	ch chan []byte
}

func newSession(id, name, dir string, args []string, p ptyx.PTY, scrollback int) *Session {
	s := &Session{
		ID: id, Name: name, Dir: dir, Args: args, Created: time.Now(),
		p:    p,
		sb:   NewScrollback(scrollback),
		subs: map[*subscriber]struct{}{},
		done: make(chan struct{}),
	}
	go s.readLoop()
	go s.waitLoop()
	return s
}

func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.p.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.sb.Write(chunk)
			for sub := range s.subs {
				select {
				case sub.ch <- chunk:
				default:
					// Viewer can't keep up; drop it rather than stall the session.
					// It can re-attach and replay from scrollback.
					close(sub.ch)
					delete(s.subs, sub)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	s.mu.Lock()
	s.readerDone = true
	for sub := range s.subs {
		close(sub.ch)
	}
	s.subs = map[*subscriber]struct{}{}
	s.mu.Unlock()
}

func (s *Session) waitLoop() {
	code, _ := s.p.Wait()
	s.mu.Lock()
	s.exited, s.exitCode = true, code
	s.mu.Unlock()
	// Let the reader drain any final output, then close the PTY.
	// On Windows the ConPTY read only unblocks once it is closed.
	time.Sleep(200 * time.Millisecond)
	s.ptyMu.Lock()
	s.ptyClosed = true
	_ = s.p.Close()
	s.ptyMu.Unlock()
	close(s.done)
}

// Attach returns the scrollback so far plus a live output subscription.
// sub is nil when the session has already finished producing output.
func (s *Session) Attach() (snapshot []byte, sub *subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot = s.sb.Snapshot()
	if s.readerDone {
		return snapshot, nil
	}
	sub = &subscriber{ch: make(chan []byte, 256)}
	s.subs[sub] = struct{}{}
	return snapshot, sub
}

func (s *Session) Detach(sub *subscriber) {
	if sub == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[sub]; ok {
		close(sub.ch)
		delete(s.subs, sub)
	}
}

func (s *Session) Write(p []byte) (int, error) { return s.p.Write(p) }

func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	// Fd() inside Setsize races with Close; serialize them.
	s.ptyMu.Lock()
	defer s.ptyMu.Unlock()
	if s.ptyClosed {
		return nil // the handle is gone (freed on Windows); nothing to resize
	}
	return s.p.Resize(cols, rows)
}

func (s *Session) Kill(force bool) error { return s.p.Kill(force) }

func (s *Session) Done() <-chan struct{} { return s.done }

// ExitCode reports the exit code once the process has exited.
func (s *Session) ExitCode() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode, s.exited
}

// ApplyHook records a hook event. The exit state always wins.
func (s *Session) ApplyHook(ev api.HookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Event {
	case "UserPromptSubmit":
		s.status = api.StatusWorking
	case "Stop", "SessionStart":
		s.status = api.StatusIdle
	case "Notification":
		s.status = api.StatusNeedsInput
	}
	if ev.SessionID != "" {
		s.claudeID = ev.SessionID
	}
	if ev.TranscriptPath != "" {
		s.transcript = ev.TranscriptPath
	}
	s.lastEvent = time.Now()
}

func (s *Session) Info() api.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := api.Session{
		ID: s.ID, Name: s.Name, Dir: s.Dir, Args: s.Args,
		Pid: s.p.Pid(), Created: s.Created,
		Status:  api.StatusRunning,
		Viewers: len(s.subs),
		Origin:  api.OriginManaged, ClaudeSessionID: s.claudeID, Transcript: s.transcript,
	}
	if s.status != "" {
		info.Status = s.status
	}
	if !s.lastEvent.IsZero() {
		t := s.lastEvent
		info.LastEvent = &t
	}
	if s.exited {
		code := s.exitCode
		info.Status, info.ExitCode = api.StatusExited, &code
	}
	return info
}
