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
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.cacheStatic(static, http.FileServerFS(static))))
	mux.Handle("GET /api/events", s.api(s.events))
	mux.Handle("POST /api/hosts/{host}/sessions", s.api(s.create))
	mux.Handle("GET /api/hosts/{host}/dirs/recent", s.api(s.recentDirs))
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

// staticRef matches a /static/ URL in an attribute of index.html.
var staticRef = regexp.MustCompile(`((?:src|href)=")(/static/[^"?]+)"`)

// versionedIndex stamps index.html with the hub version: a meta tag the page
// compares with the overview's version, and ?v=<version> on every static URL
// so an upgrade never reuses a cached file.
func versionedIndex(b []byte, version string) []byte {
	q := "?v=" + url.QueryEscape(version)
	out := staticRef.ReplaceAll(b, []byte(`${1}${2}`+q+`"`))
	meta := `<meta name="clawsh-version" content="` + html.EscapeString(version) + `">` + "\n</head>"
	return []byte(strings.Replace(string(out), "</head>", meta, 1))
}

// cacheStatic makes browsers revalidate static files. The embed FS has no
// modtime, so the ETag is a hash of the file's content: two builds with the
// same version string but different files never share one.
func (s *Server) cacheStatic(files fs.FS, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(files, strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			next.ServeHTTP(w, r) // let the file server answer 404
			return
		}
		sum := sha256.Sum256(b)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, _ *http.Request) {
	b, err := staticFiles.ReadFile("static/index.html")
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
	_, _ = w.Write(versionedIndex(b, api.Version))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, api.Error{Error: err.Error()})
}
