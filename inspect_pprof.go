package qui

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
)

const defaultPprofAddr = "127.0.0.1:6060"

// PprofServer serves Go runtime profiling endpoints under /debug/pprof/.
// Use it for heap/cpu/goroutine inspection during app runtime.
type PprofServer struct {
	listener net.Listener
	server   *http.Server
}

// StartPprofServer starts an HTTP pprof server.
//
// If addr is empty, it defaults to 127.0.0.1:6060.
// You can pass 127.0.0.1:0 to let the OS choose a free port.
func StartPprofServer(addr string) (*PprofServer, error) {
	if addr == "" {
		addr = defaultPprofAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	registerPprofHandlers(mux)

	srv := &http.Server{
		Handler: mux,
	}
	ps := &PprofServer{
		listener: ln,
		server:   srv,
	}
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Best-effort background server; callers can still inspect
			// failures from StartPprofServer/Stop.
		}
	}()
	return ps, nil
}

// Addr returns the actual listen address (useful when using port 0).
func (p *PprofServer) Addr() string {
	if p == nil || p.listener == nil {
		return ""
	}
	return p.listener.Addr().String()
}

// Stop shuts the pprof server down gracefully.
func (p *PprofServer) Stop(ctx context.Context) error {
	if p == nil || p.server == nil {
		return nil
	}
	return p.server.Shutdown(ctx)
}

func registerPprofHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	profiles := []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"}
	for _, name := range profiles {
		mux.Handle("/debug/pprof/"+name, pprof.Handler(name))
	}
}
