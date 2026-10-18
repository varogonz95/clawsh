// Package web serves the browser UI for the hub: `clawsh web`.
//
// The server listens on loopback only, holds every agent token from
// hosts.toml, and is the only thing the browser talks to. Design:
// docs/superpowers/specs/2026-10-01-web-ui-design.md.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/varogonz95/clawsh/internal/api"
)

//go:embed static
var staticFiles embed.FS

// Options configures a Server.
type Options struct {
	ConfigPath string        // hosts.toml; a missing file means no hosts yet
	Secret     string        // access key from NewSecret; required
	Port       int           // port the server listens on, for the Host check
	PollEvery  time.Duration // agent polling interval while a browser is connected; 0 means 2s
}

type Server struct {
	hosts  *hostsFile
	bc     *broadcaster
	secret string // the access key
	port   int
	up     websocket.Upgrader
}

// New loads hosts.toml and prepares the server. A missing file is fine (the
// UI shows the first-run screen); a file that doesn't parse is an error.
func New(o Options) (*Server, error) {
	if o.Secret == "" {
		return nil, errors.New("web: empty access key")
	}
	hosts, err := loadHostsFile(o.ConfigPath)
	if err != nil {
		return nil, err
	}
	if o.PollEvery <= 0 {
		o.PollEvery = 2 * time.Second
	}
	s := &Server{
		hosts: hosts, bc: newBroadcaster(hosts, o.PollEvery),
		secret: o.Secret, port: o.Port,
	}
	s.up = websocket.Upgrader{
		ReadBufferSize:  32 * 1024,
		WriteBufferSize: 32 * 1024,
		// Browsers always send Origin on WebSockets; require it to be us so a
		// hostile page can't drive a terminal.
		CheckOrigin: func(r *http.Request) bool { return s.ownOrigin(r.Header.Get("Origin")) },
	}
	return s, nil
}

// Handler returns every route behind the Host check.
func (s *Server) Handler() http.Handler {
	return s.checkHost(s.routes())
}

func (s *Server) routes() *http.ServeMux {
	static, _ := fs.Sub(staticFiles, "static")
	mux := http.NewServeMux()
	// The page and its assets hold no secrets; everything that does is under /api.
	mux.HandleFunc("GET /{$}", s.serveIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", revalidate(static, http.FileServerFS(static))))
	mux.Handle("GET /api/events", s.api(s.events))
	mux.Handle("POST /api/hosts/{host}/sessions", s.api(s.create))
	mux.Handle("DELETE /api/hosts/{host}/sessions/{id}", s.api(s.remove))
	mux.Handle("GET /api/hosts/{host}/sessions/{id}/attach", s.api(s.attach))
	mux.Handle("GET /api/config/hosts", s.api(s.listHosts))
	mux.Handle("POST /api/config/hosts", s.api(s.addHost))
	mux.Handle("POST /api/config/hosts/test", s.api(s.testHost))
	mux.Handle("PUT /api/config/hosts/{name}", s.api(s.editHost))
	mux.Handle("DELETE /api/config/hosts/{name}", s.api(s.deleteHost))
	// Unknown /api paths still demand the key, and answer in JSON.
	mux.Handle("/api/", s.api(func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusNotFound, errors.New("not found"))
	}))
	return mux
}

var staticRef = regexp.MustCompile(`="(/static/[^"?]+)"`)

// revalidate makes browsers check /static/ files on every load. The embed FS
// has no modtime, so the ETag is the hub version, the path and a hash of the
// content: builds that share a version string (dev) still get new ETags.
func revalidate(root fs.FS, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		etag := `"` + api.Version + `:` + r.URL.Path + `"`
		if b, err := fs.ReadFile(root, r.URL.Path); err == nil {
			sum := sha256.Sum256(b)
			etag = `"` + api.Version + `:` + r.URL.Path + `:` + hex.EncodeToString(sum[:8]) + `"`
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", etag)
		for _, c := range strings.Split(r.Header.Get("If-None-Match"), ",") {
			if strings.TrimSpace(c) == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// indexHTML returns index.html with the hub version in a meta tag and on
// every /static/ URL, so a new version never runs an old cached script.
func indexHTML() ([]byte, error) {
	b, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		return nil, err
	}
	out := staticRef.ReplaceAll(b, []byte(`="$1?v=`+url.QueryEscape(api.Version)+`"`))
	meta := `<meta name="clawsh-version" content="` + html.EscapeString(api.Version) + `">` + "\n</head>"
	return []byte(strings.Replace(string(out), "</head>", meta, 1)), nil
}

func (s *Server) serveIndex(w http.ResponseWriter, _ *http.Request) {
	b, err := indexHTML()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// No framing by other pages. Only frame-ancestors: a script or style
	// policy could break xterm.js.
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, api.Error{Error: err.Error()})
}
