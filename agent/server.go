package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent/internal/ws"
)

// Server serves the inspection protocol for a process's windows. A single
// Server can listen on a Unix domain socket, a TCP address, or both. Stop()
// closes all listeners and removes the UDS and discovery files.
type Server struct {
	mu       sync.Mutex
	window   *qui.Window
	mux      *http.ServeMux
	listenrs []net.Listener
	httpd    *http.Server
	udsPath  string
	tcpAddr  string
	token    string
	devtools bool
	name     string
	logger   *log.Logger
	started  bool
	stopped  bool
	running  atomic.Bool
	// baseCtx parents every request and connection; Stop cancels it so
	// long-lived WebSockets end instead of stalling Shutdown.
	baseCtx    context.Context
	cancelBase context.CancelFunc

	connsMu sync.Mutex
	conns   map[*clientConn]struct{}
	events  *eventBuffer

	removeSink      func()
	removeLifecycle func()
	tokMu           sync.Mutex // guards listenerToks (not s.mu: Start holds that)
	listenerToks    map[*qui.Window]qui.EventListenerToken
	discoveryPath   string
}

// Options configures a Server. Pass to Bind.
type Options struct {
	// UDSPath overrides the default $TMPDIR/qui-agent-<pid>.sock. Pass "-"
	// to disable UDS entirely (e.g. when only TCP is desired).
	UDSPath string

	// TCPAddr enables a TCP listener (e.g. ":9119" or "127.0.0.1:8080").
	// Empty disables TCP. When set, Token MUST be set unless
	// DangerouslyAllowAnonymousTCP is true.
	TCPAddr string

	// Token is a bearer token validated against the Authorization header
	// on TCP requests. UDS requests are implicitly trusted (same-machine,
	// same-user filesystem permissions).
	Token string

	// DangerouslyAllowAnonymousTCP relaxes the auth requirement on TCP for
	// development. Off by default — never set it in a shipped app.
	DangerouslyAllowAnonymousTCP bool

	// DevTools lets the Chrome DevTools frontend attach over TCP. The
	// frontend cannot send an Authorization header, so the token travels
	// as a path segment (/devtools/t/<token>/page/<id>) instead, and only
	// a devtools:// page Origin is accepted. Requires TCPAddr and Token.
	DevTools bool

	// Name is the app name the discovery file is written under
	// ($TMPDIR/qui-agent/<name>.json); default: the executable's name.
	Name string

	// Logger receives operational messages. Defaults to log.Default().
	Logger *log.Logger
}

// Bind creates a Server for the process's windows: every platform-backed
// window (qui.LiveWindows, tracked as they open and close) plus window
// itself, which may be a headless test window. The Server is not running
// until Start is called.
func Bind(window *qui.Window, opts Options) (*Server, error) {
	if window == nil {
		return nil, errors.New("agent: nil window")
	}
	if opts.TCPAddr != "" && opts.Token == "" && !opts.DangerouslyAllowAnonymousTCP {
		return nil, errors.New("agent: TCP listener requires Options.Token or DangerouslyAllowAnonymousTCP")
	}
	if opts.DevTools && (opts.TCPAddr == "" || opts.Token == "") {
		return nil, errors.New("agent: DevTools needs a TCP listener with a Token")
	}
	s := &Server{
		window:       window,
		mux:          http.NewServeMux(),
		token:        opts.Token,
		udsPath:      opts.UDSPath,
		tcpAddr:      opts.TCPAddr,
		devtools:     opts.DevTools,
		name:         opts.Name,
		logger:       opts.Logger,
		conns:        map[*clientConn]struct{}{},
		events:       &eventBuffer{cap: 1024},
		listenerToks: map[*qui.Window]qui.EventListenerToken{},
	}
	if s.logger == nil {
		s.logger = log.Default()
	}
	if s.name == "" {
		s.name = filepath.Base(os.Args[0])
	}
	if s.udsPath == "" {
		s.udsPath = defaultUDSPath()
	}
	s.installRoutes()
	s.baseCtx, s.cancelBase = context.WithCancel(context.Background())
	s.httpd = &http.Server{
		Handler:     s.authMiddleware(s.mux, opts),
		BaseContext: func(net.Listener) context.Context { return s.baseCtx },
		// Bound how long a client may dribble headers: the TCP listener
		// is reachable before authentication runs.
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

// BindEnv inspects QUI_AGENT / QUI_AGENT_SOCK / QUI_AGENT_TCP /
// QUI_AGENT_TOKEN / QUI_AGENT_DEVTOOLS and either starts a Server (when env
// opts are set) or returns (nil, nil) if no env-based opt-in is present. The
// non-nil error case is reserved for misconfiguration (e.g. TCP without
// token).
//
// The server covers every window of the process, not just window: a
// multi-window app needs one BindEnv call.
//
// QUI_AGENT_SOCK pins the socket path, which also implies QUI_AGENT=1.
// Without it the path carries the PID; clients then find the app by name
// through the discovery file (qui-agent -app <name>).
func BindEnv(window *qui.Window) (*Server, error) {
	udsPath := os.Getenv("QUI_AGENT_SOCK")
	udsRequested := os.Getenv("QUI_AGENT") == "1" || udsPath != ""
	tcpAddr := os.Getenv("QUI_AGENT_TCP")
	token := os.Getenv("QUI_AGENT_TOKEN")
	if !udsRequested && tcpAddr == "" {
		return nil, nil
	}
	opts := Options{TCPAddr: tcpAddr, Token: token, UDSPath: udsPath, DevTools: os.Getenv("QUI_AGENT_DEVTOOLS") == "1"}
	if !udsRequested {
		opts.UDSPath = "-"
	}
	s, err := Bind(window, opts)
	if err != nil {
		return nil, err
	}
	if err := s.Start(); err != nil {
		return nil, err
	}
	return s, nil
}

// windows returns the windows this server serves: the live platform
// windows plus the bound window.
func (s *Server) windows() []*qui.Window {
	wins := qui.LiveWindows()
	for _, w := range wins {
		if w == s.window {
			return wins
		}
	}
	return append([]*qui.Window{s.window}, wins...)
}

// Start binds the configured listeners and begins serving. Safe to call
// multiple times — subsequent calls are no-ops while the server is running.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	ok := false
	defer func() {
		if !ok {
			// Undo a partial start: a listener already serving must not
			// outlive the error that reports Start failed.
			s.teardownHooks()
			for _, l := range s.listenrs {
				_ = l.Close()
			}
			s.listenrs = nil
		}
	}()
	s.running.Store(true)
	s.removeSink = qui.AddProtocolEventSink(s)
	for _, w := range s.windows() {
		s.watchWindow(w)
	}
	s.removeLifecycle = qui.AddWindowLifecycleListener(func(w *qui.Window, alive bool) {
		if alive {
			s.watchWindow(w)
			s.announceTarget("Target.targetCreated", w)
		} else {
			s.unwatchWindow(w)
			s.announceTarget("Target.targetDestroyed", w)
		}
	})
	if s.udsPath != "-" {
		// If a socket file already exists, probe it: a live owner answers a
		// dial, in which case we leave it alone and let net.Listen fail
		// loudly (address in use). A refused dial means the file is stale
		// (a previous instance crashed) — remove it so the restart binds.
		removeStaleSocket(s.udsPath)
		if err := os.MkdirAll(filepath.Dir(s.udsPath), 0o755); err != nil {
			return fmt.Errorf("agent: mkdir for UDS: %w", err)
		}
		l, err := net.Listen("unix", s.udsPath)
		if err != nil {
			return fmt.Errorf("agent: listen UDS %q: %w", s.udsPath, err)
		}
		s.listenrs = append(s.listenrs, l)
		// UDS requests skip token auth, so the socket's mode IS the access
		// control — don't leave it to the process umask.
		if err := os.Chmod(s.udsPath, 0o600); err != nil {
			return fmt.Errorf("agent: chmod UDS %q: %w", s.udsPath, err)
		}
		go s.serve(l, "UDS")
		s.logger.Printf("agent: listening on unix://%s", s.udsPath)
	}
	if s.tcpAddr != "" {
		l, err := net.Listen("tcp", s.tcpAddr)
		if err != nil {
			return fmt.Errorf("agent: listen TCP %q: %w", s.tcpAddr, err)
		}
		s.listenrs = append(s.listenrs, l)
		go s.serve(l, "TCP")
		s.logger.Printf("agent: listening on tcp://%s", l.Addr())
		if s.devtools {
			s.logger.Printf("agent: DevTools frontend: %s", s.devtoolsFrontendURL(l.Addr().String(), s.window.TargetID()))
		}
	}
	s.writeDiscovery()
	s.started = true
	ok = true
	return nil
}

func (s *Server) serve(l net.Listener, kind string) {
	if err := s.httpd.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Printf("agent: %s serve: %v", kind, err)
	}
}

// watchWindow records the window's dispatched input as Agent events.
func (s *Server) watchWindow(w *qui.Window) {
	s.tokMu.Lock()
	_, have := s.listenerToks[w]
	s.tokMu.Unlock()
	if have {
		return
	}
	tok := w.AddEventListener(func(rec qui.EventRecord) {
		method := "Agent.inputRecorded"
		switch rec.Kind {
		case "Input":
			method = "Agent.valueChanged"
		case "Submit":
			method = "Agent.submitted"
		}
		s.HandleEvent(w, method, rec)
	})
	s.tokMu.Lock()
	s.listenerToks[w] = tok
	s.tokMu.Unlock()
}

func (s *Server) unwatchWindow(w *qui.Window) {
	s.tokMu.Lock()
	tok, have := s.listenerToks[w]
	delete(s.listenerToks, w)
	s.tokMu.Unlock()
	if have {
		w.RemoveEventListener(tok)
	}
}

func (s *Server) teardownHooks() {
	s.running.Store(false)
	if s.removeSink != nil {
		s.removeSink()
		s.removeSink = nil
	}
	if s.removeLifecycle != nil {
		s.removeLifecycle()
		s.removeLifecycle = nil
	}
	s.tokMu.Lock()
	toks := s.listenerToks
	s.listenerToks = map[*qui.Window]qui.EventListenerToken{}
	s.tokMu.Unlock()
	for w, tok := range toks {
		w.RemoveEventListener(tok)
	}
}

// Stop shuts down listeners and removes the UDS and discovery files.
// Idempotent.
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.mu.Unlock()
	s.teardownHooks()
	if s.cancelBase != nil {
		s.cancelBase()
	}
	if s.httpd != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := s.httpd.Shutdown(ctx); err != nil {
			_ = s.httpd.Close()
		}
		cancel()
	}
	for _, l := range s.listenrs {
		_ = l.Close()
	}
	if s.udsPath != "" && s.udsPath != "-" {
		_ = os.Remove(s.udsPath)
	}
	s.removeDiscovery()
	return nil
}

// UDSPath returns the resolved Unix domain socket path, or "" if UDS is
// disabled.
func (s *Server) UDSPath() string {
	if s.udsPath == "-" {
		return ""
	}
	return s.udsPath
}

// TCPAddress returns the TCP listener's local address, or "" if TCP is
// disabled. Only meaningful after Start.
func (s *Server) TCPAddress() string {
	for _, l := range s.listenrs {
		if l.Addr().Network() == "tcp" {
			return l.Addr().String()
		}
	}
	return ""
}

// Handler returns the HTTP handler tree. Useful for embedding the protocol
// into a host's existing HTTP router or for in-process httptest.
func (s *Server) Handler() http.Handler {
	return s.httpd.Handler
}

// Call runs one protocol method in-process, as a one-shot HTTP call would:
// target picks the window ("" = the only one). For Go tests and tools that
// link the agent package.
func (s *Server) Call(ctx context.Context, method string, params any, target string) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = s.baseCtx
	}
	res, err := s.dispatch(ctx, &session{server: s}, rpcRequest{Method: method, Params: raw, Target: target})
	if err != nil {
		return nil, qui.ProtocolErrorFor(err)
	}
	return json.Marshal(res)
}

// removeStaleSocket removes a Unix-socket file at path only if no live
// process is listening on it. If the path doesn't exist, it's a no-op. If a
// dial succeeds (someone is listening), the file is left in place so the
// subsequent net.Listen fails with a clear "address already in use" rather
// than silently stealing a live socket.
func removeStaleSocket(path string) {
	fi, err := os.Lstat(path)
	if err != nil {
		return // nothing there
	}
	if fi.Mode()&os.ModeSocket == 0 {
		// Not a socket — a mistyped QUI_AGENT_SOCK naming a real file.
		// Never delete it; net.Listen will report the clash.
		return
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close() // a live listener owns it
		return
	}
	_ = os.Remove(path)
}

func defaultUDSPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("qui-agent-%d.sock", os.Getpid()))
}

// maxRequestBody bounds a request body (one JSON-RPC message).
const maxRequestBody = 1 << 20

// authMiddleware enforces auth on TCP requests; UDS requests bypass it
// (filesystem permission is the auth). TCP takes a bearer token, or — with
// DevTools on — the token as the /devtools/t/<token>/ path prefix plus a
// devtools:// Origin, since the DevTools frontend cannot set headers.
func (s *Server) authMiddleware(h http.Handler, opts Options) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isUDS := r.RemoteAddr == "@" || r.RemoteAddr == ""
		if !isUDS {
			// A web page must never drive the app through the user's
			// browser: reject any WebSocket from a page Origin (a
			// non-browser client sends none), devtools:// excepted.
			if origin := r.Header.Get("Origin"); origin != "" && ws.IsUpgrade(r) && !devtoolsOrigin(origin) {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			if opts.Token != "" {
				authed := subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+opts.Token)) == 1
				if !authed && opts.DevTools {
					prefix := "/devtools/t/"
					if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok {
						tok, tail, _ := strings.Cut(rest, "/")
						if subtle.ConstantTimeCompare([]byte(tok), []byte(opts.Token)) == 1 {
							authed = true
							r.URL.Path = "/devtools/" + tail
						}
					}
				}
				if !authed {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}
		// Every body is one small JSON message; cap it so one request
		// can't make the decoder buffer arbitrary input.
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		h.ServeHTTP(w, r)
	})
}

func devtoolsOrigin(origin string) bool {
	return strings.HasPrefix(origin, "devtools://") || strings.HasPrefix(origin, "chrome-devtools://")
}
