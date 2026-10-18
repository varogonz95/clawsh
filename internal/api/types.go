// Package api holds the wire types shared by the agent and the hub.
//
// Transport:
//   - REST (JSON) for session lifecycle: /v1/health, /v1/sessions, /v1/sessions/{id}
//   - REST (JSON) for external sessions (claude started outside clawsh, announced
//     by `clawsh mcp`): GET /v1/external, PUT/DELETE /v1/external/{id}
//   - WebSocket for attach: /v1/sessions/{id}/attach
//     binary frames = raw terminal bytes (both directions)
//     text frames   = JSON Control messages
//
// Auth: every endpoint except /v1/health requires "Authorization: Bearer <token>".
package api

import "time"

type Health struct {
	Host    string `json:"host"`
	OS      string `json:"os"`
	Version string `json:"version"`
}

type Status string

const (
	StatusRunning Status = "running"
	StatusExited  Status = "exited"
	// M3 adds hook-driven states: working, idle, needs_input.
)

type Session struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Dir      string    `json:"dir"`
	Args     []string  `json:"args,omitempty"`
	Pid      int       `json:"pid"`
	Created  time.Time `json:"created"`
	Status   Status    `json:"status"`
	ExitCode *int      `json:"exit_code,omitempty"`
	Viewers  int       `json:"viewers"`
}

type CreateRequest struct {
	Name string   `json:"name,omitempty"`
	Dir  string   `json:"dir,omitempty"`  // defaults to the agent user's home; "~" is expanded on the agent
	Args []string `json:"args,omitempty"` // extra args for claude, e.g. ["--resume"]
	Cols int      `json:"cols,omitempty"`
	Rows int      `json:"rows,omitempty"`
}

// External is a Claude Code session the agent does not own: claude was
// started in a plain terminal and its `clawsh mcp` stub announced it. It is
// listed but cannot be attached or killed through clawsh. Entries are a lease:
// the announcer re-sends PUT periodically and the agent forgets entries not
// renewed within its TTL.
type External struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Dir     string    `json:"dir"`
	Pid     int       `json:"pid"` // the claude process (parent of the announcer)
	Created time.Time `json:"created"`
	Seen    time.Time `json:"seen"`
}

type AnnounceRequest struct {
	Name string `json:"name,omitempty"`
	Dir  string `json:"dir,omitempty"`
	Pid  int    `json:"pid,omitempty"`
}

type ControlType string

const (
	ControlResize ControlType = "resize" // hub -> agent
	ControlExit   ControlType = "exit"   // agent -> hub: session process ended
)

type Control struct {
	Type ControlType `json:"type"`
	Cols int         `json:"cols,omitempty"`
	Rows int         `json:"rows,omitempty"`
	Code *int        `json:"code,omitempty"`
}

type Error struct {
	Error string `json:"error"`
}

// HostOverview is one configured host as the hub sees it: health and
// sessions, or why it could not be reached. Tokens are never included.
type HostOverview struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Online   bool      `json:"online"`
	Health   *Health   `json:"health,omitempty"`
	Error    string    `json:"error,omitempty"`
	Sessions []Session `json:"sessions"`
}

// Overview is the payload of the web UI's "overview" event.
// ConfigError is set while hosts.toml doesn't parse; Hosts then holds the
// last hosts that did.
// Version is the hub's clawsh version; a page running another one reloads.
type Overview struct {
	Version     string         `json:"version"`
	ConfigPath  string         `json:"config_path"`
	ConfigError string         `json:"config_error,omitempty"`
	Hosts       []HostOverview `json:"hosts"`
}
