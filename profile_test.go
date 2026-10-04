package qui

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStartPprofServerServesHeapEndpoint(t *testing.T) {
	srv, err := StartPprofServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartPprofServer failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Stop(ctx)
	}()

	url := "http://" + srv.Addr() + "/debug/pprof/heap?debug=1"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "heap profile") {
		t.Fatalf("heap endpoint body missing expected marker")
	}
}
