package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/varogonz95/clawsh/internal/api"
)

// Client talks to one agent.
type Client struct {
	Host Host
	http *http.Client
}

func NewClient(h Host) *Client {
	return &Client{Host: h, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) Health(ctx context.Context) (api.Health, error) {
	var out api.Health
	return out, c.do(ctx, http.MethodGet, "/v1/health", nil, &out)
}

func (c *Client) List(ctx context.Context) ([]api.Session, error) {
	var out []api.Session
	return out, c.do(ctx, http.MethodGet, "/v1/sessions", nil, &out)
}

func (c *Client) Create(ctx context.Context, req api.CreateRequest) (api.Session, error) {
	var out api.Session
	return out, c.do(ctx, http.MethodPost, "/v1/sessions", req, &out)
}

func (c *Client) Kill(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/sessions/"+url.PathEscape(id), nil, nil)
}

func (c *Client) Externals(ctx context.Context) ([]api.External, error) {
	var out []api.External
	return out, c.do(ctx, http.MethodGet, "/v1/external", nil, &out)
}

// Announce creates or renews an external session lease on the agent.
func (c *Client) Announce(ctx context.Context, id string, req api.AnnounceRequest) (api.External, error) {
	var out api.External
	return out, c.do(ctx, http.MethodPut, "/v1/external/"+url.PathEscape(id), req, &out)
}

func (c *Client) Withdraw(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/external/"+url.PathEscape(id), nil, nil)
}

// Hook posts a Claude Code hook event to the agent.
func (c *Client) Hook(ctx context.Context, ev api.HookEvent) error {
	return c.do(ctx, http.MethodPost, "/v1/hooks", ev, nil)
}

// Dial opens the attach WebSocket for a session.
func (c *Client) Dial(ctx context.Context, id string) (*websocket.Conn, error) {
	u := c.Host.URL + "/v1/sessions/" + url.PathEscape(id) + "/attach"
	u = strings.Replace(u, "http", "ws", 1) // http->ws, https->wss
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, u, c.headers())
	if err != nil {
		if resp != nil {
			return nil, decodeErr(resp)
		}
		return nil, err
	}
	return conn, nil
}

func (c *Client) headers() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.Host.Token)
	return h
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body *bytes.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Host.URL+path, body)
	if err != nil {
		return err
	}
	req.Header = c.headers()
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeErr(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// HTTPError is a non-2xx response from an agent.
type HTTPError struct {
	Code   int    // HTTP status code
	Status string // e.g. "404 Not Found"
	Msg    string // the agent's api.Error message; may be empty
}

func (e *HTTPError) Error() string {
	if e.Msg != "" {
		return e.Status + ": " + e.Msg
	}
	return e.Status
}

func decodeErr(resp *http.Response) error {
	he := &HTTPError{Code: resp.StatusCode, Status: resp.Status}
	var e api.Error
	if json.NewDecoder(resp.Body).Decode(&e) == nil {
		he.Msg = e.Error
	}
	return he
}
