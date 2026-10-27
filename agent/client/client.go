// Package client talks to a qui agent server: one-shot calls over HTTP
// (POST /rpc), and WebSocket connections for sessions and events.
// cmd/qui-agent is built on it; it imports only the standard library, so a
// tool using it does not link the GUI engine.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui/agent/internal/discovery"
	"github.com/qizhanchan/qui/agent/internal/ws"
)

// Endpoint says where a server listens.
type Endpoint struct {
	Socket string // Unix socket path
	TCP    string // host:port
	Token  string // bearer token for TCP
}

// Client talks to one server.
type Client struct {
	ep   Endpoint
	http *http.Client
}

// NewClient returns a client for ep.
func NewClient(ep Endpoint) *Client {
	tr := &http.Transport{}
	if ep.Socket != "" {
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", ep.Socket)
		}
	}
	return &Client{ep: ep, http: &http.Client{Transport: tr}}
}

func (c *Client) baseURL() string {
	if c.ep.Socket != "" {
		return "http://localhost"
	}
	return "http://" + c.ep.TCP
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.ep.Token != "" && c.ep.Socket == "" {
		req.Header.Set("Authorization", "Bearer "+c.ep.Token)
	}
	return c.http.Do(req)
}

// Get fetches a plain HTTP path (/llm.txt, /json/list, …).
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// RemoteError is a protocol error returned by the server.
type RemoteError struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func (e *RemoteError) Error() string {
	if r := e.Reason(); r != "" {
		return r + ": " + e.Message
	}
	return e.Message
}

// Reason is the semantic error code (no_match, stale_node, …).
func (e *RemoteError) Reason() string {
	r, _ := e.Data["reason"].(string)
	return r
}

type rpcReply struct {
	ID        json.RawMessage `json:"id,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *RemoteError    `json:"error,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// Call runs one method over HTTP. target picks a window ("" = the only one).
// The raw result is returned; errors from the server are *RemoteError.
func (c *Client) Call(ctx context.Context, method string, params any, target string) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{"method": method, "params": params, "target": target})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var reply rpcReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, fmt.Errorf("%s: bad reply (%s): %w", method, resp.Status, err)
	}
	if reply.Error != nil {
		return nil, reply.Error
	}
	return reply.Result, nil
}

// Event is one protocol event received on a Conn.
type Event struct {
	Method    string
	Params    json.RawMessage
	SessionID string
}

// Conn is a WebSocket connection: calls with session IDs, plus events.
type Conn struct {
	ws      *ws.Conn
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan rpcReply
	events  chan Event
	done    chan struct{}
	err     error
}

// Dial opens a WebSocket on path: "/devtools/browser/x" for an app-level
// session, "/devtools/page/<targetId>" for one bound to a window.
func (c *Client) Dial(ctx context.Context, path string) (*Conn, error) {
	var d net.Dialer
	var nc net.Conn
	var err error
	host := "localhost"
	header := http.Header{}
	if c.ep.Socket != "" {
		nc, err = d.DialContext(ctx, "unix", c.ep.Socket)
	} else {
		nc, err = d.DialContext(ctx, "tcp", c.ep.TCP)
		host = c.ep.TCP
		if c.ep.Token != "" {
			header.Set("Authorization", "Bearer "+c.ep.Token)
		}
	}
	if err != nil {
		return nil, err
	}
	ws, err := ws.Dial(nc, host, path, header)
	if err != nil {
		nc.Close()
		return nil, err
	}
	conn := &Conn{ws: ws, pending: map[int64]chan rpcReply{}, events: make(chan Event, 256), done: make(chan struct{})}
	go conn.read()
	return conn, nil
}

func (c *Conn) read() {
	defer close(c.done)
	defer close(c.events)
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			c.mu.Lock()
			c.err = err
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		var reply rpcReply
		if json.Unmarshal(data, &reply) != nil {
			continue
		}
		if reply.Method != "" && len(reply.ID) == 0 {
			select {
			case c.events <- Event{Method: reply.Method, Params: reply.Params, SessionID: reply.SessionID}:
			default: // the reader is not draining events; drop
			}
			continue
		}
		var id int64
		if json.Unmarshal(reply.ID, &id) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- reply
		}
	}
}

// Call runs method in session sessionID ("" = the connection's root
// session) and waits for the reply.
func (c *Conn) Call(ctx context.Context, method string, params any, sessionID string) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	ch := make(chan rpcReply, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.ws.WriteMessage(ws.OpText, b); err != nil {
		return nil, err
	}
	select {
	case reply, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("%s: connection closed: %v", method, c.err)
		}
		if reply.Error != nil {
			return nil, reply.Error
		}
		return reply.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Events delivers protocol events until the connection closes.
func (c *Conn) Events() <-chan Event { return c.events }

// Close closes the connection.
func (c *Conn) Close() error {
	err := c.ws.Close()
	<-c.done
	return err
}

// -------------------------------------------------------------------
// Finding a server

// FindEndpoint resolves where to connect, in order: an explicit socket or
// TCP address; the discovery file of app name; QUI_AGENT_SOCK /
// QUI_AGENT_TCP; the only running app in the discovery directory. With
// several apps running and nothing pinned it fails and lists them — it
// never guesses, because driving the wrong app reports success.
func FindEndpoint(socket, tcp, token, app string) (Endpoint, error) {
	if token == "" {
		token = os.Getenv("QUI_AGENT_TOKEN")
	}
	switch {
	case socket != "":
		return Endpoint{Socket: socket}, nil
	case tcp != "":
		return Endpoint{TCP: tcp, Token: token}, nil
	case app != "":
		info, err := discovery.Read(discovery.Path(app))
		if err != nil {
			return Endpoint{}, fmt.Errorf("no running app %q: %w", app, err)
		}
		return endpointOf(info, token), nil
	}
	if s := os.Getenv("QUI_AGENT_SOCK"); s != "" {
		return Endpoint{Socket: s}, nil
	}
	if t := os.Getenv("QUI_AGENT_TCP"); t != "" {
		return Endpoint{TCP: t, Token: token}, nil
	}
	apps := RunningApps()
	switch len(apps) {
	case 0:
		return Endpoint{}, errors.New("no qui app with the agent enabled is running (start it with QUI_AGENT=1)")
	case 1:
		return endpointOf(apps[0], token), nil
	}
	names := make([]string, 0, len(apps))
	for _, a := range apps {
		names = append(names, fmt.Sprintf("%s (pid %d)", a.Name, a.PID))
	}
	return Endpoint{}, fmt.Errorf("%d qui apps are running — pick one with -app: %s", len(apps), strings.Join(names, ", "))
}

func endpointOf(info discovery.Info, token string) Endpoint {
	if info.Socket != "" {
		return Endpoint{Socket: info.Socket}
	}
	return Endpoint{TCP: info.TCP, Token: token}
}

// App is a running app found through its discovery file.
type App = discovery.Info

// RunningApps lists the live apps in the discovery directory, by name.
func RunningApps() []App { return discovery.Running() }

// WaitReady polls until the server answers and has a laid-out window.
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := c.Call(ctx, "Agent.snapshot", map[string]any{"maxDepth": 1}, "")
		var re *RemoteError
		if err == nil || (errors.As(err, &re) && re.Reason() == "ambiguous_target") {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}
