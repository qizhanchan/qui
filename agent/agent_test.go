package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent/client"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/widgets"
)

// The tests drive the real transport — HTTP /rpc and WebSocket sessions —
// against a NewTestWindow, whose actions run inline (no frame pump).

func newSampleWindow(t *testing.T) (*qui.Window, *widgets.Button, *int) {
	t.Helper()
	clicked := new(int)
	w := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	btn := widgets.NewButton("Save", func() { *clicked++ })
	btn.SetID("save")
	tf := widgets.NewInput("name")
	tf.SetID("name")
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(btn)
	root.AddChild(tf)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 400, H: 200})
	btn.Layout(qui.Rect{X: 10, Y: 10, W: 80, H: 28})
	tf.Layout(qui.Rect{X: 10, Y: 50, W: 200, H: 28})
	return w, btn, clicked
}

// newUDSServer starts a server on a private socket and returns a client.
func newUDSServer(t *testing.T, w *qui.Window) (*Server, *client.Client) {
	t.Helper()
	dir, err := os.MkdirTemp("", "qa") // short: UDS paths are length-limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s, err := Bind(w, Options{UDSPath: filepath.Join(dir, "a.sock"), Name: "agent-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s, client.NewClient(client.Endpoint{Socket: s.UDSPath()})
}

func call(t *testing.T, c *client.Client, method string, params any) map[string]any {
	t.Helper()
	raw, err := c.Call(context.Background(), method, params, "")
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %s", method, raw)
	}
	return out
}

func remoteReason(err error) string {
	var re *client.RemoteError
	if errors.As(err, &re) {
		return re.Reason()
	}
	return ""
}

func TestRPC_SnapshotClickAndErrors(t *testing.T) {
	w, btn, clicked := newSampleWindow(t)
	_, c := newUDSServer(t, w)

	snap := call(t, c, "Agent.snapshot", nil)
	ref := btn.NodeID().String()
	if !strings.Contains(snap["text"].(string), `button "Save" [ref=`+ref+`] #save`) {
		t.Fatalf("snapshot:\n%s", snap["text"])
	}
	res := call(t, c, "Agent.click", map[string]any{"target": ref})
	if *clicked != 1 || res["treeHash"] == "" {
		t.Fatalf("click: clicked=%d res=%v", *clicked, res)
	}

	ctx := context.Background()
	if _, err := c.Call(ctx, "Agent.click", map[string]any{"selector": "#save"}, ""); remoteReason(err) != "invalid_params" {
		t.Errorf("misnamed field: %v", err)
	}
	if _, err := c.Call(ctx, "Agent.click", map[string]any{"target": "#nope", "timeoutMs": 0}, ""); remoteReason(err) != "no_match" {
		t.Errorf("no match: %v", err)
	}
	if _, err := c.Call(ctx, "Agent.fill", map[string]any{"target": "#save", "value": "x"}, ""); remoteReason(err) != "not_text_target" {
		t.Errorf("fill a button: %v", err)
	}
	if _, err := c.Call(ctx, "Nope.nothing", nil, ""); remoteReason(err) != "unsupported" {
		t.Errorf("unknown method: %v", err)
	}
	if _, err := c.Call(ctx, "Agent.snapshot", nil, "window-999999"); remoteReason(err) != "target_closed" {
		t.Errorf("unknown target: %v", err)
	}
}

func TestRPC_DOMAndCSSOnHTML(t *testing.T) {
	eng := htmlcss.NewStyleEngine(`.btn { padding: 4px } .btn.hot { color: #ff0000 }`)
	app := eng.NewEl("div")
	app.SetElementID("app")
	btn := eng.NewEl("button")
	btn.SetClass("btn hot")
	btn.SetTextContent("Save")
	app.SetElementChildren([]qui.Widget{btn})
	eng.SetRoot(app)
	w := qui.NewTestWindow(qui.Size{W: 300, H: 200})
	w.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{W: 300, H: 200})
	_, c := newUDSServer(t, w)

	q := call(t, c, "DOM.querySelector", map[string]any{"selector": "#app > button.hot"})
	if q["nodeId"] != float64(btn.NodeID()) {
		t.Fatalf("querySelector = %v, want %d", q, btn.NodeID())
	}
	html := call(t, c, "DOM.getOuterHTML", map[string]any{"nodeId": app.NodeID()})["outerHTML"].(string)
	if !strings.Contains(html, `<button class="btn hot">Save</button>`) {
		t.Fatalf("outerHTML = %s", html)
	}
	m := call(t, c, "CSS.getMatchedStylesForNode", map[string]any{"nodeId": btn.NodeID()})
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `".btn.hot"`) || !strings.Contains(string(b), `"name":"color"`) {
		t.Fatalf("matched styles = %s", b)
	}
	// The click resolves the CSS selector once and reports what it hit.
	res := call(t, c, "Agent.click", map[string]any{"target": "button.hot"})
	if res["matched"].(map[string]any)["nodeId"] != float64(btn.NodeID()) {
		t.Fatalf("matched = %v", res["matched"])
	}
}

func TestWS_SessionsAndEvents(t *testing.T) {
	w, btn, clicked := newSampleWindow(t)
	_, c := newUDSServer(t, w)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := c.Dial(ctx, "/devtools/browser/x")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	raw, err := conn.Call(ctx, "Target.getTargets", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var targets struct {
		TargetInfos []struct{ TargetID string } `json:"targetInfos"`
	}
	_ = json.Unmarshal(raw, &targets)
	if len(targets.TargetInfos) != 1 || targets.TargetInfos[0].TargetID != w.TargetID() {
		t.Fatalf("targets = %s", raw)
	}
	raw, err = conn.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": w.TargetID(), "flatten": true}, "")
	if err != nil {
		t.Fatal(err)
	}
	var att struct{ SessionID string }
	_ = json.Unmarshal(raw, &att)
	if att.SessionID == "" {
		t.Fatalf("attach = %s", raw)
	}
	if _, err := conn.Call(ctx, "Agent.enable", nil, att.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Call(ctx, "Agent.click", map[string]any{"target": btn.NodeID().String()}, att.SessionID); err != nil {
		t.Fatal(err)
	}
	if *clicked != 1 {
		t.Fatalf("clicked = %d", *clicked)
	}
	for {
		select {
		case ev := <-conn.Events():
			if ev.Method == "Agent.inputRecorded" && ev.SessionID == att.SessionID && strings.Contains(string(ev.Params), "MouseDown") {
				// The buffered copy is readable by a polling client too.
				res := call(t, c, "Target.getEvents", map[string]any{"domains": []string{"Agent"}})
				if len(res["events"].([]any)) == 0 {
					t.Fatal("getEvents returned nothing")
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("no Agent.inputRecorded event for the click")
		}
	}
}

func TestDiscoveryEndpoints(t *testing.T) {
	w, _, _ := newSampleWindow(t)
	s, c := newUDSServer(t, w)
	ctx := context.Background()

	var list []map[string]string
	b, err := c.Get(ctx, "/json/list")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(b, &list)
	if len(list) != 1 || list[0]["id"] != w.TargetID() || !strings.HasSuffix(list[0]["webSocketDebuggerUrl"], "/devtools/page/"+w.TargetID()) {
		t.Fatalf("/json/list = %s", b)
	}
	if b, err := c.Get(ctx, "/json/version"); err != nil || !strings.Contains(string(b), "webSocketDebuggerUrl") {
		t.Fatalf("/json/version = %s, %v", b, err)
	}
	if b, err := c.Get(ctx, "/json/protocol"); err != nil || !strings.Contains(string(b), `"domain":"Agent"`) {
		t.Fatalf("/json/protocol: %v", err)
	}
	llm, err := c.Get(ctx, "/llm.txt")
	if err != nil || !strings.Contains(string(llm), "Agent.snapshot") {
		t.Fatalf("/llm.txt: %v", err)
	}
	// The discovery file names the app and points at its socket.
	ep, err := client.FindEndpoint("", "", "", "agent-test")
	if err != nil || ep.Socket != s.UDSPath() {
		t.Fatalf("client.FindEndpoint(agent-test) = %+v, %v", ep, err)
	}
}

func TestTCPAuth(t *testing.T) {
	w, _, _ := newSampleWindow(t)
	if _, err := Bind(w, Options{UDSPath: "-", TCPAddr: ":0"}); err == nil {
		t.Fatal("TCP without a token must be refused")
	}
	s, err := Bind(w, Options{UDSPath: "-", TCPAddr: "127.0.0.1:0", Token: "sekrit", Name: "agent-tcp-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	ctx := context.Background()
	if _, err := client.NewClient(client.Endpoint{TCP: s.TCPAddress()}).Call(ctx, "Agent.snapshot", nil, ""); err == nil {
		t.Fatal("unauthenticated TCP call succeeded")
	}
	good := client.NewClient(client.Endpoint{TCP: s.TCPAddress(), Token: "sekrit"})
	if _, err := good.Call(ctx, "Agent.snapshot", nil, ""); err != nil {
		t.Fatalf("authenticated call: %v", err)
	}
	// A web page's WebSocket is refused even with the token: browsers
	// send their Origin, CLIs and DevTools don't (or send devtools://).
	req, _ := http.NewRequest(http.MethodGet, "http://"+s.TCPAddress()+"/devtools/browser/x", nil)
	req.Header.Set("Authorization", "Bearer sekrit")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin WebSocket: status %d, want 403", resp.StatusCode)
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	w, _, _ := newSampleWindow(t)
	s, err := Bind(w, Options{UDSPath: "-"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	big := `{"method":"Agent.click","params":{"target":"` + strings.Repeat("x", 2<<20) + `"}}`
	resp, err := http.Post(ts.URL+"/rpc", "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d for a 2MB body, want 413", resp.StatusCode)
	}
}

func TestScreenshotScaleBounded(t *testing.T) {
	w, _, _ := newSampleWindow(t)
	_, c := newUDSServer(t, w)
	for _, scale := range []float64{200, -1} {
		if _, err := c.Call(context.Background(), "Page.captureScreenshot", map[string]any{"scale": scale}, ""); remoteReason(err) != "invalid_params" {
			t.Errorf("scale=%g: %v", scale, err)
		}
	}
}

// Stop returns promptly with a WebSocket open, and the socket file it
// created is private to the user.
func TestStopWithOpenWebSocket(t *testing.T) {
	w, _, _ := newSampleWindow(t)
	s, c := newUDSServer(t, w)
	if fi, err := os.Stat(s.UDSPath()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
	conn, err := c.Dial(context.Background(), "/devtools/browser/x")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	done := make(chan struct{})
	go func() { _ = s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop hung with a WebSocket client connected")
	}
}

// A regular file at the socket path is never deleted.
func TestStaleSocketCleanupKeepsRegularFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	removeStaleSocket(path)
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep me" {
		t.Fatalf("regular file was removed or changed: %v", err)
	}
}

// BindEnv is the startup path most apps use, so the socket path has to be
// pinnable there.
func TestBindEnvHonorsSocketPath(t *testing.T) {
	w, _, _ := newSampleWindow(t)
	dir, _ := os.MkdirTemp("", "qa")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "p.sock")
	t.Setenv("QUI_AGENT_SOCK", path)
	t.Setenv("QUI_AGENT", "") // naming a socket is an unambiguous opt-in
	s, err := BindEnv(w)
	if err != nil || s == nil {
		t.Fatalf("BindEnv = %v, %v", s, err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	if got := s.UDSPath(); got != path {
		t.Errorf("UDSPath = %q, want %q", got, path)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial %q: %v", path, err)
	}
	_ = conn.Close()
}

func TestBindEnvWithoutOptInIsNoOp(t *testing.T) {
	w, _, _ := newSampleWindow(t)
	t.Setenv("QUI_AGENT", "")
	t.Setenv("QUI_AGENT_SOCK", "")
	t.Setenv("QUI_AGENT_TCP", "")
	s, err := BindEnv(w)
	if err != nil || s != nil {
		t.Errorf("BindEnv = (%v, %v), want (nil, nil) with no env opt-in", s, err)
	}
}

func TestBindEnvDefaultsToPIDPath(t *testing.T) {
	w, _, _ := newSampleWindow(t)
	t.Setenv("QUI_AGENT", "1")
	t.Setenv("QUI_AGENT_SOCK", "")
	s, err := BindEnv(w)
	if err != nil {
		t.Fatalf("BindEnv: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	want := filepath.Join(os.TempDir(), fmt.Sprintf("qui-agent-%d.sock", os.Getpid()))
	if got := s.UDSPath(); got != want {
		t.Errorf("UDSPath = %q, want the PID-derived default %q", got, want)
	}
}
