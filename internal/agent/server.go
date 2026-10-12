package agent

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/gorilla/websocket"

	"github.com/varogonz95/clawsh/internal/api"
)

const (
	pingEvery = 20 * time.Second
	pongWait  = 60 * time.Second
)

type Server struct {
	m        *Manager
	token    string
	hostname string
	up       websocket.Upgrader
}

func NewServer(m *Manager, token string) *Server {
	host, _ := os.Hostname()
	return &Server{
		m: m, token: token, hostname: host,
		up: websocket.Upgrader{
			ReadBufferSize:  32 * 1024,
			WriteBufferSize: 32 * 1024,
			// Auth is a bearer header, which browsers can't send cross-site,
			// so origin checks add nothing here. Revisit for the web UI.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.Handle("GET /v1/sessions", s.auth(s.list))
	mux.Handle("POST /v1/sessions", s.auth(s.create))
	mux.Handle("DELETE /v1/sessions/{id}", s.auth(s.remove))
	mux.Handle("GET /v1/sessions/{id}/attach", s.auth(s.attach))
	mux.Handle("GET /v1/external", s.auth(s.listExternal))
	mux.Handle("PUT /v1/external/{id}", s.auth(s.announce))
	mux.Handle("DELETE /v1/external/{id}", s.auth(s.withdraw))
	mux.Handle("POST /v1/hooks", s.auth(s.hook))
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.Handler {
	want := []byte("Bearer " + s.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		next(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.Health{Host: s.hostname, OS: runtime.GOOS, Version: api.Version})
}

func (s *Server) list(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.m.List())
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req api.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, err := s.m.Create(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	log.Printf("session %s started in %s", sess.ID, sess.Dir)
	writeJSON(w, http.StatusCreated, sess.Info())
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	if err := s.m.Remove(r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listExternal(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.m.Externals())
}

func (s *Server) announce(w http.ResponseWriter, r *http.Request) {
	var req api.AnnounceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	x, err := s.m.Announce(r.PathValue("id"), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) withdraw(w http.ResponseWriter, r *http.Request) {
	if err := s.m.Withdraw(r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	log.Printf("external session %s withdrawn", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) hook(w http.ResponseWriter, r *http.Request) {
	var ev api.HookEvent
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&ev); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, err := s.m.Get(ev.ClawshSessionID)
	if ev.ClawshSessionID == "" || err != nil {
		w.WriteHeader(http.StatusAccepted) // not a managed session: ignored
		return
	}
	sess.ApplyHook(ev)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) attach(w http.ResponseWriter, r *http.Request) {
	sess, err := s.m.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the error response
	}
	defer conn.Close()

	snapshot, sub := sess.Attach()
	defer sess.Detach(sub)

	// gorilla/websocket allows one concurrent writer; this goroutine is it.
	if len(snapshot) > 0 {
		if conn.WriteMessage(websocket.BinaryMessage, snapshot) != nil {
			return
		}
	}
	if sub == nil {
		sendExit(conn, sess)
		return
	}

	clientGone := make(chan struct{})
	go s.readClient(conn, sess, clientGone)

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case chunk, ok := <-sub.ch:
			if !ok {
				// Output ended (process exited) or we were dropped as a slow viewer.
				if _, exited := sess.ExitCode(); exited || waitDone(sess, 2*time.Second) {
					sendExit(conn, sess)
				}
				return
			}
			if conn.WriteMessage(websocket.BinaryMessage, chunk) != nil {
				return
			}
		case <-ping.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		case <-clientGone:
			return
		}
	}
}

// readClient forwards keystrokes and resize requests from the viewer to the PTY.
func (s *Server) readClient(conn *websocket.Conn, sess *Session, gone chan<- struct{}) {
	defer close(gone)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })

	firstResize := true
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		switch mt {
		case websocket.BinaryMessage:
			if _, err := sess.Write(data); err != nil {
				return
			}
		case websocket.TextMessage:
			var c api.Control
			if json.Unmarshal(data, &c) != nil || c.Type != api.ControlResize {
				continue
			}
			if firstResize {
				// Replayed scrollback rarely lines up with the viewer's screen.
				// Nudging the size forces claude to repaint cleanly.
				firstResize = false
				_ = sess.Resize(c.Cols, max(c.Rows-1, 1))
				time.Sleep(50 * time.Millisecond)
			}
			_ = sess.Resize(c.Cols, c.Rows)
		}
	}
}

func sendExit(conn *websocket.Conn, sess *Session) {
	code, _ := sess.ExitCode()
	msg, _ := json.Marshal(api.Control{Type: api.ControlExit, Code: &code})
	_ = conn.WriteMessage(websocket.TextMessage, msg)
	_ = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session exited"))
}

func waitDone(sess *Session, d time.Duration) bool {
	select {
	case <-sess.Done():
		return true
	case <-time.After(d):
		return false
	}
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrAmbiguous):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, api.Error{Error: err.Error()})
}
