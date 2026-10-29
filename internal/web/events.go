package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/varogonz95/clawsh/internal/api"
	"github.com/varogonz95/clawsh/internal/hub"
)

const (
	subBuffer = 4                // overview events queued per browser before it is dropped
	ssePing   = 20 * time.Second // keeps idle streams from being cut by proxies or sleep
)

// broadcaster polls the agents while at least one browser is subscribed and
// sends each subscriber the overview whenever it changes.
type broadcaster struct {
	hosts *hostsFile
	every time.Duration
	kickc chan struct{}

	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	last   []byte             // latest overview JSON, replayed to new subscribers
	cancel context.CancelFunc // stops the poller; nil while nobody is subscribed
}

func newBroadcaster(hosts *hostsFile, every time.Duration) *broadcaster {
	return &broadcaster{
		hosts: hosts,
		every: every,
		kickc: make(chan struct{}, 1),
		subs:  map[chan []byte]struct{}{},
	}
}

func (b *broadcaster) subscribe() chan []byte {
	ch := make(chan []byte, subBuffer)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[ch] = struct{}{}
	if b.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		b.cancel = cancel
		b.last = nil // data from an earlier run may be stale
		go b.poll(ctx)
	} else if b.last != nil {
		ch <- b.last
	}
	return ch
}

func (b *broadcaster) unsubscribe(ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[ch]; !ok {
		return // already dropped by sendLocked
	}
	b.removeLocked(ch)
}

// removeLocked drops a subscriber and stops the poller when none remain.
func (b *broadcaster) removeLocked(ch chan []byte) {
	delete(b.subs, ch)
	close(ch)
	if len(b.subs) == 0 && b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

// kick asks the poller to refresh now, e.g. right after a create or end.
func (b *broadcaster) kick() {
	select {
	case b.kickc <- struct{}{}:
	default:
	}
}

func (b *broadcaster) poll(ctx context.Context) {
	t := time.NewTicker(b.every)
	defer t.Stop()
	for {
		b.publish(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-b.kickc:
		}
	}
}

func (b *broadcaster) publish(ctx context.Context) {
	hosts, cfgErr := b.hosts.State()
	ov := api.Overview{Version: api.Version, ConfigPath: b.hosts.path, ConfigError: cfgErr, Hosts: hub.Overview(ctx, hosts)}
	data, err := json.Marshal(ov)
	if err != nil {
		log.Printf("clawsh web: encode overview: %v", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil {
		return // stopped while polling; cancel happens under b.mu, so this is reliable
	}
	b.sendLocked(data)
}

// sendLocked delivers data to every subscriber unless it equals the last
// overview. A subscriber whose buffer is full is dropped; its browser's
// EventSource reconnects by itself.
func (b *broadcaster) sendLocked(data []byte) {
	if bytes.Equal(data, b.last) {
		return
	}
	b.last = data
	for ch := range b.subs {
		select {
		case ch <- data:
		default:
			b.removeLocked(ch)
		}
	}
}

// events streams overview events to one browser.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	ch := s.bc.subscribe()
	defer s.bc.unsubscribe(ch)
	ping := time.NewTicker(ssePing)
	defer ping.Stop()
	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return // dropped as a slow subscriber
			}
			if _, err := fmt.Fprintf(w, "event: overview\ndata: %s\n\n", data); err != nil {
				return
			}
			fl.Flush()
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
