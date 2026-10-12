package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/varogonz95/clawsh/internal/hub"
)

// hostInfo is a host as the browser sees it. Tokens are write-only.
type hostInfo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	HasToken bool   `json:"has_token"`
}

type hostBody struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

// testResult is the answer of a connection test. Error is "unreachable",
// "unauthorized" or an agent message; it never contains the URL.
type testResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

const testTimeout = 5 * time.Second

func info(h hub.Host) hostInfo {
	return hostInfo{Name: h.Name, URL: h.URL, HasToken: h.Token != ""}
}

func readBody(w http.ResponseWriter, r *http.Request) (hostBody, bool) {
	var b hostBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return b, false
	}
	b.Name, b.URL = strings.TrimSpace(b.Name), strings.TrimRight(strings.TrimSpace(b.URL), "/")
	return b, true
}

// writeMutateErr maps a hostsFile.Mutate error to a status.
func writeMutateErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNoHost):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, errDupHost), errors.Is(err, errBrokenFile):
		writeErr(w, http.StatusConflict, err)
	case errors.As(err, new(validationError)):
		writeErr(w, http.StatusBadRequest, err)
	default:
		writeErr(w, http.StatusInternalServerError, errors.New("could not write hosts.toml"))
	}
}

type validationError struct{ error }

func (s *Server) listHosts(w http.ResponseWriter, _ *http.Request) {
	out := []hostInfo{}
	for _, h := range s.hosts.Hosts() {
		out = append(out, info(h))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) addHost(w http.ResponseWriter, r *http.Request) {
	b, ok := readBody(w, r)
	if !ok {
		return
	}
	nh := hub.Host{Name: b.Name, URL: b.URL, Token: b.Token}
	err := s.hosts.Mutate(func(hosts []hub.Host) ([]hub.Host, error) {
		if err := hub.ValidateHost(nh); err != nil {
			return nil, validationError{err}
		}
		if nh.Token == "" {
			return nil, validationError{errors.New("token is required")}
		}
		for _, h := range hosts {
			if h.Name == nh.Name {
				return nil, errDupHost
			}
		}
		return append(hosts, nh), nil
	})
	if err != nil {
		writeMutateErr(w, err)
		return
	}
	s.bc.kick()
	writeJSON(w, http.StatusCreated, info(nh))
}

func (s *Server) editHost(w http.ResponseWriter, r *http.Request) {
	old := r.PathValue("name")
	b, ok := readBody(w, r)
	if !ok {
		return
	}
	var saved hub.Host
	err := s.hosts.Mutate(func(hosts []hub.Host) ([]hub.Host, error) {
		idx := -1
		for i, h := range hosts {
			if h.Name == old {
				idx = i
			}
		}
		if idx < 0 {
			return nil, errNoHost
		}
		nh := hub.Host{Name: b.Name, URL: b.URL, Token: b.Token}
		if nh.Token == "" {
			nh.Token = hosts[idx].Token // write-only: empty keeps the stored one
		}
		if err := hub.ValidateHost(nh); err != nil {
			return nil, validationError{err}
		}
		for i, h := range hosts {
			if i != idx && h.Name == nh.Name {
				return nil, errDupHost
			}
		}
		hosts[idx] = nh
		saved = nh
		return hosts, nil
	})
	if err != nil {
		writeMutateErr(w, err)
		return
	}
	s.bc.kick()
	writeJSON(w, http.StatusOK, info(saved))
}

func (s *Server) deleteHost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := s.hosts.Mutate(func(hosts []hub.Host) ([]hub.Host, error) {
		for i, h := range hosts {
			if h.Name == name {
				return append(hosts[:i], hosts[i+1:]...), nil
			}
		}
		return nil, errNoHost
	})
	if err != nil {
		writeMutateErr(w, err)
		return
	}
	s.bc.kick()
	w.WriteHeader(http.StatusNoContent)
}

// testHost checks a connection: a saved host by name (fields in the request
// override it), or a url and token that aren't saved yet.
func (s *Server) testHost(w http.ResponseWriter, r *http.Request) {
	b, ok := readBody(w, r)
	if !ok {
		return
	}
	h := hub.Host{URL: b.URL, Token: b.Token}
	if b.Name != "" {
		// A saved machine fills in what the request leaves out, so editing a
		// URL can be tested without retyping the write-only key.
		saved, found := s.hosts.Find(b.Name)
		if found {
			if h.URL == "" {
				h.URL = saved.URL
			}
			if h.Token == "" {
				h.Token = saved.Token
			}
		} else if b.URL == "" {
			writeErr(w, http.StatusNotFound, errNoHost)
			return
		}
	}
	if h.URL == "" {
		writeErr(w, http.StatusBadRequest, errors.New("url is required"))
		return
	}
	if err := hub.ValidateURL(h.URL); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), testTimeout)
	defer cancel()
	writeJSON(w, http.StatusOK, probe(ctx, h))
}

// probe calls /v1/health and an authenticated list. Errors from the http
// package embed the URL (and with it any userinfo or query), so only the
// agent's own message is ever passed on.
func probe(ctx context.Context, h hub.Host) testResult {
	c := hub.NewClient(h)
	if _, err := c.Health(ctx); err != nil {
		return failure(err)
	}
	if _, err := c.List(ctx); err != nil {
		return failure(err)
	}
	return testResult{OK: true}
}

func failure(err error) testResult {
	var he *hub.HTTPError
	if !errors.As(err, &he) {
		return testResult{Error: "unreachable"}
	}
	if he.Code == http.StatusUnauthorized || he.Code == http.StatusForbidden {
		return testResult{Error: "unauthorized"}
	}
	if he.Msg != "" {
		return testResult{Error: he.Status + ": " + he.Msg}
	}
	return testResult{Error: he.Status}
}
