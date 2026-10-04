package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qizhanchan/qui"
)

// Server hosts the HTTP surface for one Window. A single Server can
// listen on a Unix domain socket, a TCP address, or both
// simultaneously. Stop() closes all listeners and removes the UDS
// file.
type Server struct {
	mu          sync.Mutex
	window      *qui.Window
	mux         *http.ServeMux
	listenrs    []net.Listener
	httpd       *http.Server
	udsPath     string
	tcpAddr     string
	token       string
	started     bool
	stopped     bool
	listenerTok qui.EventListenerToken

	// recordedEvents holds the most recent dispatched events for
	// /console and /events. Filled by the listener installed in
	// Start.
	recordedEvents *eventRing
}

// Options configures a Server. Pass to Bind.
type Options struct {
	// UDSPath overrides the default $TMPDIR/qui-agent-<pid>.sock.
	// Pass "-" to disable UDS entirely (e.g. when only TCP is
	// desired).
	UDSPath string

	// TCPAddr enables a TCP listener (e.g. ":9119" or
	// "127.0.0.1:8080"). Empty disables TCP. When set, Token MUST
	// be set unless DangerouslyAllowAnonymousTCP is true.
	TCPAddr string

	// Token is a bearer token validated against the
	// Authorization header on TCP requests. UDS requests are
	// implicitly trusted (same-machine, same-user filesystem
	// permissions).
	Token string

	// DangerouslyAllowAnonymousTCP relaxes the auth requirement on
	// TCP for development. Off by default — production deployments
	// should never set this.
	DangerouslyAllowAnonymousTCP bool

	// Logger receives operational messages (listener bind, errors,
	// dropped jobs). Defaults to log.Default().
	Logger *log.Logger
}

// Bind creates a Server attached to window using opts. The Server
// is not running until Start is called. Returns the Server (for
// programmatic control) and any setup error.
func Bind(window *qui.Window, opts Options) (*Server, error) {
	if window == nil {
		return nil, errors.New("agent: nil window")
	}
	if opts.TCPAddr != "" && opts.Token == "" && !opts.DangerouslyAllowAnonymousTCP {
		return nil, errors.New("agent: TCP listener requires Options.Token or DangerouslyAllowAnonymousTCP")
	}
	s := &Server{
		window:         window,
		mux:            http.NewServeMux(),
		token:          opts.Token,
		udsPath:        opts.UDSPath,
		tcpAddr:        opts.TCPAddr,
		recordedEvents: newEventRing(512),
	}
	if s.udsPath == "" {
		s.udsPath = defaultUDSPath()
	}
	s.installRoutes()
	s.httpd = &http.Server{
		Handler: s.authMiddleware(s.mux, opts),
	}
	return s, nil
}

// BindEnv inspects QUI_AGENT / QUI_AGENT_SOCK / QUI_AGENT_TCP /
// QUI_AGENT_TOKEN and either starts a Server (when env opts are set) or
// returns (nil, nil) if no env-based opt-in is present. The non-nil error
// case is reserved for misconfiguration (e.g. TCP without token).
//
// QUI_AGENT_SOCK pins the socket path. Without it the path carries the
// PID, and a client that discovers "the newest live qui-agent-*.sock"
// can pick a DIFFERENT app when several are running — so any script that
// drives a specific app should set it on both sides:
//
//	QUI_AGENT=1 QUI_AGENT_SOCK=/tmp/my-app.sock ./my-app &
//	qui-agent -sock /tmp/my-app.sock tree
//
// Setting QUI_AGENT_SOCK also implies QUI_AGENT=1: naming a socket is an
// unambiguous request for one.
func BindEnv(window *qui.Window) (*Server, error) {
	udsPath := os.Getenv("QUI_AGENT_SOCK")
	udsRequested := os.Getenv("QUI_AGENT") == "1" || udsPath != ""
	tcpAddr := os.Getenv("QUI_AGENT_TCP")
	token := os.Getenv("QUI_AGENT_TOKEN")
	if !udsRequested && tcpAddr == "" {
		return nil, nil
	}
	opts := Options{TCPAddr: tcpAddr, Token: token, UDSPath: udsPath}
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

// Start binds the configured listeners and begins serving. Safe to
// call multiple times — subsequent calls are no-ops while the
// server is running.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	// Subscribe to the window's event stream so /console and
	// /events have content to serve.
	s.listenerTok = s.window.AddEventListener(func(rec qui.EventRecord) {
		s.recordedEvents.Push(EventRecord{
			Timestamp: rec.Timestamp.UnixNano(),
			Kind:      rec.Kind,
			Target:    rec.Target,
			Payload: map[string]any{
				"targetId":  rec.TargetID,
				"x":         rec.X,
				"y":         rec.Y,
				"dx":        rec.DX,
				"dy":        rec.DY,
				"key":       rec.Key,
				"rune":      rec.Rune,
				"modifiers": rec.Modifiers,
				"value":     rec.Value,
				"oldValue":  rec.OldValue,
				"source":    rec.Source,
			},
		})
	})
	if s.udsPath != "-" {
		// If a socket file already exists, probe it: a live owner
		// answers a dial, in which case we leave it alone and let
		// net.Listen fail loudly (address in use). A refused/no-
		// listener dial means the file is stale (previous instance
		// crashed without cleanup) — remove it so the restart binds.
		removeStaleSocket(s.udsPath)
		if err := os.MkdirAll(filepath.Dir(s.udsPath), 0o755); err != nil {
			return fmt.Errorf("agent: mkdir for UDS: %w", err)
		}
		l, err := net.Listen("unix", s.udsPath)
		if err != nil {
			return fmt.Errorf("agent: listen UDS %q: %w", s.udsPath, err)
		}
		s.listenrs = append(s.listenrs, l)
		go func() {
			if err := s.httpd.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("agent: UDS serve: %v", err)
			}
		}()
		log.Printf("agent: listening on unix://%s", s.udsPath)
	}
	if s.tcpAddr != "" {
		l, err := net.Listen("tcp", s.tcpAddr)
		if err != nil {
			return fmt.Errorf("agent: listen TCP %q: %w", s.tcpAddr, err)
		}
		s.listenrs = append(s.listenrs, l)
		go func() {
			if err := s.httpd.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("agent: TCP serve: %v", err)
			}
		}()
		log.Printf("agent: listening on tcp://%s", l.Addr())
	}
	s.started = true
	return nil
}

// Stop shuts down listeners and removes the UDS file. Idempotent.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopped = true
	if s.window != nil {
		s.window.RemoveEventListener(s.listenerTok)
	}
	if s.httpd != nil {
		_ = s.httpd.Shutdown(context.Background())
	}
	for _, l := range s.listenrs {
		_ = l.Close()
	}
	if s.udsPath != "" && s.udsPath != "-" {
		_ = os.Remove(s.udsPath)
	}
	return nil
}

// UDSPath returns the resolved Unix domain socket path, or "" if
// UDS is disabled.
func (s *Server) UDSPath() string {
	if s.udsPath == "-" {
		return ""
	}
	return s.udsPath
}

// TCPAddress returns the TCP listener's local address, or "" if
// TCP is disabled. Only meaningful after Start.
func (s *Server) TCPAddress() string {
	for _, l := range s.listenrs {
		if l.Addr().Network() == "tcp" {
			return l.Addr().String()
		}
	}
	return ""
}

// Handler returns the HTTP handler tree used by the server. Useful
// for embedding the agent surface into a host's existing HTTP
// router or for in-process httptest.
func (s *Server) Handler() http.Handler {
	return s.httpd.Handler
}

// removeStaleSocket removes a Unix-socket file at path only if no
// live process is listening on it. If the path doesn't exist, it's a
// no-op. If a dial succeeds (someone is listening), the file is left
// in place so the subsequent net.Listen fails with a clear "address
// already in use" rather than silently stealing a live socket.
func removeStaleSocket(path string) {
	if _, err := os.Stat(path); err != nil {
		return // nothing there
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		// A live listener owns it; don't touch.
		_ = conn.Close()
		return
	}
	// Dial failed (connection refused / no listener) → stale file.
	_ = os.Remove(path)
}

func defaultUDSPath() string {
	dir := os.TempDir()
	return filepath.Join(dir, fmt.Sprintf("qui-agent-%d.sock", os.Getpid()))
}

// authMiddleware enforces bearer-token auth on TCP requests. UDS
// requests bypass it (filesystem permission is the auth).
func (s *Server) authMiddleware(h http.Handler, opts Options) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isUDS := r.RemoteAddr == "@" || r.RemoteAddr == ""
		if !isUDS && opts.Token != "" {
			authHdr := r.Header.Get("Authorization")
			if authHdr != "Bearer "+opts.Token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}
