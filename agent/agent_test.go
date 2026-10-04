package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// agent tests exercise the HTTP surface against an in-process
// httptest server. Window is a NewTestWindow + manually-laid-out
// widget tree so the actuation path runs inline (no GLFW pump).

func newAgentServer(t *testing.T, w *qui.Window) (*httptest.Server, *Server) {
	t.Helper()
	s, err := Bind(w, Options{UDSPath: "-"}) // disable UDS for tests
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = s.Stop()
	})
	return ts, s
}

func newSampleWindow(t *testing.T) (*qui.Window, *widgets.Button) {
	t.Helper()
	w := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	btn := widgets.NewButton("Save", nil)
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
	return w, btn
}

func TestLLM(t *testing.T) {
	w, _ := newSampleWindow(t)
	ts, _ := newAgentServer(t, w)
	resp, err := http.Get(ts.URL + "/llm.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestTree(t *testing.T) {
	w, _ := newSampleWindow(t)
	ts, _ := newAgentServer(t, w)
	resp, err := http.Get(ts.URL + "/tree")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tree qui.AccessibilityTree
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		t.Fatal(err)
	}
	if tree.Root == nil {
		t.Fatal("nil root")
	}
	foundSave := false
	for _, n := range tree.Flatten() {
		if n.ID == "save" {
			foundSave = true
		}
	}
	if !foundSave {
		t.Errorf("#save not in tree")
	}
}

func TestAct_Click(t *testing.T) {
	clicked := 0
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	btn := widgets.NewButton("Go", func() { clicked++ })
	btn.SetID("go")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})
	btn.Layout(qui.Rect{X: 0, Y: 0, W: 100, H: 30})

	ts, _ := newAgentServer(t, w)
	body := bytes.NewReader([]byte(`{"action":"click","target":"#go"}`))
	resp, err := http.Post(ts.URL+"/act", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("/act not ok: %+v", out)
	}
	if clicked != 1 {
		t.Errorf("clicked = %d, want 1", clicked)
	}
}

func TestAct_NoMatch(t *testing.T) {
	w, _ := newSampleWindow(t)
	ts, _ := newAgentServer(t, w)
	body := bytes.NewReader([]byte(`{"action":"click","target":"#missing"}`))
	resp, err := http.Post(ts.URL+"/act", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("expected !ok, got %+v", out)
	}
	if got, _ := out["errorCode"].(string); got != "no_match" {
		t.Errorf("errorCode = %q, want no_match", got)
	}
}

func TestAct_ModalBlock(t *testing.T) {
	w, _ := newSampleWindow(t)
	d := widgets.NewDialog("hi", widgets.NewLabel("body"))
	d.SetSelf(d)
	w.PushOverlay(d)
	d.Layout(qui.Rect{W: 400, H: 200})

	ts, _ := newAgentServer(t, w)
	body := bytes.NewReader([]byte(`{"action":"click","target":"#save"}`))
	resp, err := http.Post(ts.URL+"/act", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if got, _ := out["errorCode"].(string); got != "modal_blocked" {
		t.Errorf("errorCode = %q, want modal_blocked", got)
	}
}

func TestAct_Type(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	tf := widgets.NewInput("name")
	tf.SetID("name")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(tf)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})
	tf.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 30})

	ts, _ := newAgentServer(t, w)
	body := bytes.NewReader([]byte(`{"action":"type","target":"#name","text":"hi"}`))
	resp, err := http.Post(ts.URL+"/act", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("type not ok: %+v", out)
	}
	if tf.Text != "hi" {
		t.Errorf("Text = %q, want hi", tf.Text)
	}
}

func TestWait_Idle(t *testing.T) {
	w, _ := newSampleWindow(t)
	// In test mode there's no Step pump to clear dirty/layout. Reset
	// them manually so the idle check has a chance.
	w.ClearDirtyRegion()
	if r := w.Root(); r != nil {
		r.ClearLayoutDirty()
	}
	ts, _ := newAgentServer(t, w)
	resp, err := http.Get(ts.URL + "/wait?for=idle&timeoutMs=200")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); !ok {
		t.Errorf("wait idle reported not-ok: %+v", out)
	}
}

func TestDiagnostics(t *testing.T) {
	w, _ := newSampleWindow(t)
	ts, _ := newAgentServer(t, w)
	resp, err := http.Get(ts.URL + "/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var diags []qui.LayoutDiagnostic
	_ = json.NewDecoder(resp.Body).Decode(&diags)
	// Empty is fine; the test tree has no overflow.
	_ = diags
}

func TestTCPRequiresToken(t *testing.T) {
	w, _ := newSampleWindow(t)
	_, err := Bind(w, Options{UDSPath: "-", TCPAddr: ":0"})
	if err == nil {
		t.Fatal("expected error when TCP set without token")
	}
}

// A request that names its selector field something other than "target"
// used to reach the action layer with an empty selector, which matched
// every node — so the action ran against the ROOT widget. Reject it.
func TestAct_MissingTargetIsRejected(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	tf := widgets.NewInput("name")
	tf.SetID("name")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(tf)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})

	ts, _ := newAgentServer(t, w)
	for _, path := range []string{"/act", "/dom/act"} {
		body := bytes.NewReader([]byte(`{"action":"type","selector":"#name","text":"41"}`))
		resp, err := http.Post(ts.URL+path, "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if ok, _ := out["ok"].(bool); ok {
			t.Errorf("POST %s with no target reported ok: %+v", path, out)
		}
		if msg, _ := out["error"].(string); !strings.Contains(msg, `"target"`) {
			t.Errorf("POST %s error = %q, want it to name the target field", path, msg)
		}
	}
}

// Typing at a widget that takes no text must be reported, not absorbed by
// an ancestor (the failure this guards was a phantom text node inserted at
// the top of an htmlcss root, shifting the whole app down).
func TestAct_TypeOnNonTextWidget(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	lbl := widgets.NewLabel("caption")
	lbl.SetID("caption")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(lbl)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})

	ts, _ := newAgentServer(t, w)
	body := bytes.NewReader([]byte(`{"action":"type","target":"#caption","text":"41"}`))
	resp, err := http.Post(ts.URL+"/act", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("type on a label reported ok: %+v", out)
	}
	if out["errorCode"] != "not_text_target" {
		t.Errorf("errorCode = %v, want not_text_target", out["errorCode"])
	}
	if lbl.Text() != "caption" {
		t.Errorf("label text = %q, want unchanged", lbl.Text())
	}
}

// BindEnv is the only startup path most apps use, so the socket path has to
// be pinnable there — a client that auto-discovers "the newest live
// qui-agent-*.sock" attaches to the wrong app as soon as two are running.
func TestBindEnvHonorsSocketPath(t *testing.T) {
	w, _ := newSampleWindow(t)
	path := filepath.Join(t.TempDir(), "pinned.sock")
	t.Setenv("QUI_AGENT_SOCK", path)

	// QUI_AGENT is deliberately NOT set: naming a socket is an unambiguous
	// request for one.
	t.Setenv("QUI_AGENT", "")
	s, err := BindEnv(w)
	if err != nil {
		t.Fatalf("BindEnv: %v", err)
	}
	if s == nil {
		t.Fatal("BindEnv returned no server; QUI_AGENT_SOCK should imply opt-in")
	}
	t.Cleanup(func() { _ = s.Stop() })
	if got := s.UDSPath(); got != path {
		t.Errorf("UDSPath = %q, want %q", got, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("socket file %q was not created: %v", path, err)
	}
	// And it is actually serving there.
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial %q: %v", path, err)
	}
	_ = conn.Close()
}

func TestBindEnvWithoutOptInIsNoOp(t *testing.T) {
	w, _ := newSampleWindow(t)
	t.Setenv("QUI_AGENT", "")
	t.Setenv("QUI_AGENT_SOCK", "")
	t.Setenv("QUI_AGENT_TCP", "")
	s, err := BindEnv(w)
	if err != nil || s != nil {
		t.Errorf("BindEnv = (%v, %v), want (nil, nil) with no env opt-in", s, err)
	}
}

func TestBindEnvDefaultsToPIDPath(t *testing.T) {
	w, _ := newSampleWindow(t)
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
