package agent

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

func TestOversizedActBodyRejected(t *testing.T) {
	w, _ := newSampleWindow(t)
	ts, _ := newAgentServer(t, w)
	big := `{"action":"click","selector":"` + strings.Repeat("x", 2<<20) + `"}`
	resp, err := http.Post(ts.URL+"/act", "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d for a 2MB body, want 400", resp.StatusCode)
	}
}

func TestScreenshotScaleBounded(t *testing.T) {
	w, _ := newSampleWindow(t)
	ts, _ := newAgentServer(t, w)
	for _, q := range []string{"200", "0", "-1", "NaN", "x"} {
		resp, err := http.Get(ts.URL + "/screenshot?scale=" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("scale=%s: status %d, want 400", q, resp.StatusCode)
		}
	}
}

// Stop returns promptly even with an /events stream open, and the
// socket file it created is private to the user.
func TestStopWithOpenEventStream(t *testing.T) {
	dir, err := os.MkdirTemp("", "qa") // short: UDS paths are length-limited
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "a.sock")
	s, err := Bind(qui.NewTestWindow(qui.Size{W: 10, H: 10}), Options{UDSPath: sock})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(sock); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("GET /events HTTP/1.1\r\nHost: x\r\n\r\n"))
	if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop hung with an /events client connected")
	}
}

// A regular file at the socket path is never deleted.
func TestStaleSocketCleanupKeepsRegularFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	removeStaleSocket(path)
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, []byte("keep me")) {
		t.Fatalf("regular file was removed or changed: %v", err)
	}
}
