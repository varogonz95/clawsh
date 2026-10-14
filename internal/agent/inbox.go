package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/varogonz95/clawsh/internal/api"
)

// maxInbox bounds the messages queued per target; more are refused with 429.
const maxInbox = 100

// maxWait caps the long-poll of GET /v1/inbox/{id}.
const maxWait = 30 * time.Second

// inbox queues messages per target session until the target's stub fetches
// them. In memory only: like sessions, it does not survive an agent restart.
type inbox struct {
	mu sync.Mutex
	q  map[string]*queue
}

type queue struct {
	msgs []api.Message
	ping chan struct{} // closed and replaced when a message arrives
}

func newInbox() *inbox { return &inbox{q: map[string]*queue{}} }

func (b *inbox) get(id string) *queue {
	q := b.q[id]
	if q == nil {
		q = &queue{ping: make(chan struct{})}
		b.q[id] = q
	}
	return q
}

func (b *inbox) post(m api.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.get(m.To)
	if len(q.msgs) >= maxInbox {
		return errors.New("inbox full")
	}
	q.msgs = append(q.msgs, m)
	close(q.ping)
	q.ping = make(chan struct{})
	return nil
}

// take drains the queue, or waits up to wait (or until done closes) for the
// first message.
func (b *inbox) take(id string, wait time.Duration, done <-chan struct{}) []api.Message {
	t := time.NewTimer(wait)
	defer t.Stop()
	for {
		b.mu.Lock()
		q := b.get(id)
		if len(q.msgs) > 0 {
			out := q.msgs
			q.msgs = nil
			b.mu.Unlock()
			return out
		}
		ping := q.ping
		b.mu.Unlock()
		select {
		case <-ping:
		case <-t.C:
			return []api.Message{}
		case <-done:
			return []api.Message{}
		}
	}
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	var req api.SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.To == "" || req.Content == "" {
		writeErr(w, http.StatusBadRequest, errors.New("to and content are required"))
		return
	}
	// The target must be a session this agent knows: managed or announced.
	if !s.m.Knows(req.To) {
		writeErr(w, http.StatusNotFound, errors.New("no such session: "+req.To))
		return
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	m := api.Message{ID: hex.EncodeToString(b), To: req.To, From: req.From, Content: req.Content, Meta: req.Meta, Time: time.Now()}
	if err := s.inbox.post(m); err != nil {
		writeErr(w, http.StatusTooManyRequests, err)
		return
	}
	writeJSON(w, http.StatusAccepted, m)
}

func (s *Server) getInbox(w http.ResponseWriter, r *http.Request) {
	wait := time.Duration(0)
	if v, err := time.ParseDuration(r.URL.Query().Get("wait")); err == nil {
		wait = min(v, maxWait)
	}
	writeJSON(w, http.StatusOK, s.inbox.take(r.PathValue("id"), wait, r.Context().Done()))
}
