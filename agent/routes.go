package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent/internal/discovery"
	"github.com/qizhanchan/qui/agent/internal/ws"
)

// HTTP surface:
//
//	POST /rpc                      one JSON-RPC call: {method, params, target?}
//	GET  /devtools/browser/<id>    WebSocket, app-level root session
//	GET  /devtools/page/<target>   WebSocket, root session attached to a window
//	GET  /json/version             CDP discovery: version + browser WebSocket URL
//	GET  /json/list  (or /json)    CDP discovery: one entry per window
//	GET  /json/protocol            the full schema (browser_protocol.json shape)
//	GET  /llm.txt                  the guide for agents, plus the method reference
//	GET  /                         what this is and where to go next

func (s *Server) installRoutes() {
	s.mux.HandleFunc("/", s.handleRoot)
	s.mux.HandleFunc("/llm.txt", s.handleLLM)
	s.mux.HandleFunc("/rpc", s.handleRPC)
	s.mux.HandleFunc("/json", s.handleJSONList)
	s.mux.HandleFunc("/json/list", s.handleJSONList)
	s.mux.HandleFunc("/json/version", s.handleJSONVersion)
	s.mux.HandleFunc("/json/protocol", s.handleJSONProtocol)
	s.mux.HandleFunc("/devtools/", s.handleWS)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     "qui inspection protocol",
		"protocol": "CDP-shaped JSON-RPC: POST /rpc {\"method\":\"Agent.snapshot\"} or a WebSocket at /devtools/browser/" + browserID(),
		"guide":    "/llm.txt",
		"schema":   "/json/protocol",
		"targets":  "/json/list",
	})
}

func (s *Server) handleLLM(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(llmText())
}

// handleRPC runs one call. The HTTP status is 200 whenever a JSON-RPC
// response is produced — the error object carries the failure.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, `POST a JSON-RPC message: {"method":"Agent.snapshot","params":{}}`, http.StatusMethodNotAllowed)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		status := http.StatusOK
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, rpcResponse{Error: &qui.ProtocolError{Code: qui.ProtocolParseError, Reason: "invalid_params", Message: "bad JSON: " + err.Error()}})
		return
	}
	res, err := s.dispatch(r.Context(), &session{server: s}, req)
	resp := rpcResponse{ID: req.ID, Result: res}
	if err != nil {
		resp.Error = qui.ProtocolErrorFor(err)
	}
	writeJSON(w, http.StatusOK, resp)
}

// browserID names the process in WebSocket URLs, as Chrome's does.
func browserID() string { return "qui-" + itoa(os.Getpid()) }

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// wsBase is the ws:// prefix for this request's host, with the DevTools
// path token when the caller authenticated with one.
func (s *Server) wsBase(r *http.Request) string {
	host := r.Host
	if host == "" || host == "." {
		host = "localhost"
	}
	return "ws://" + host
}

func (s *Server) handleJSONVersion(w http.ResponseWriter, r *http.Request) {
	v := versionInfo()
	writeJSON(w, http.StatusOK, map[string]string{
		"Browser":              v.Product,
		"Protocol-Version":     v.ProtocolVersion,
		"User-Agent":           v.UserAgent,
		"V8-Version":           "",
		"WebKit-Version":       "",
		"webSocketDebuggerUrl": s.wsBase(r) + "/devtools/browser/" + browserID(),
	})
}

func (s *Server) handleJSONList(w http.ResponseWriter, r *http.Request) {
	out := []map[string]string{}
	for _, win := range s.windows() {
		info := infoFor(s, win)
		wsPath := strings.TrimPrefix(s.wsBase(r), "ws://") + "/devtools/page/" + info.TargetID
		out = append(out, map[string]string{
			"id":                   info.TargetID,
			"type":                 info.Type,
			"title":                info.Title,
			"url":                  info.URL,
			"description":          "",
			"webSocketDebuggerUrl": "ws://" + wsPath,
			"devtoolsFrontendUrl":  "devtools://devtools/bundled/inspector.html?ws=" + wsPath,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleJSONProtocol(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, qui.ProtocolSchema())
}

// handleWS upgrades /devtools/browser/<id> and /devtools/page/<targetId>.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/devtools/")
	kind, id, _ := strings.Cut(rest, "/")
	targetID := ""
	switch kind {
	case "browser":
	case "page":
		found := false
		for _, win := range s.windows() {
			if win.TargetID() == id {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "no target "+id, http.StatusNotFound)
			return
		}
		targetID = id
	default:
		http.NotFound(w, r)
		return
	}
	ws, err := ws.Upgrade(w, r)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(s.baseCtx)
	c := &clientConn{server: s, ws: ws, ctx: ctx, cancel: cancel, out: make(chan []byte, 1024)}
	c.root = &session{server: s, conn: c, targetID: targetID}
	s.connsMu.Lock()
	s.conns[c] = struct{}{}
	s.connsMu.Unlock()
	defer func() {
		s.connsMu.Lock()
		delete(s.conns, c)
		s.connsMu.Unlock()
		ws.Close()
	}()
	c.serve()
}

// -------------------------------------------------------------------
// Discovery file

func (s *Server) writeDiscovery() {
	info := discovery.Info{Name: s.name, PID: os.Getpid(), Socket: s.UDSPath(), TCP: s.TCPAddress(), StartedAt: time.Now().Format(time.RFC3339)}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(discovery.Dir(), 0o700); err != nil {
		return
	}
	path := discovery.Path(s.name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		s.logger.Printf("agent: discovery file: %v", err)
		return
	}
	s.discoveryPath = path
}

func (s *Server) removeDiscovery() {
	if s.discoveryPath == "" {
		return
	}
	// Only remove the file if it is still ours: a second instance of the
	// same app may have replaced it.
	if b, err := os.ReadFile(s.discoveryPath); err == nil {
		var info discovery.Info
		if json.Unmarshal(b, &info) == nil && info.PID == os.Getpid() {
			_ = os.Remove(s.discoveryPath)
		}
	}
}
