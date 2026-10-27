package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
)

// The calls the Chrome DevTools frontend makes when it opens a page target
// and its Elements / Styles panes, in order. None may fail: the frontend
// treats an error on most of them as a broken target.
var devtoolsStartup = []struct {
	method string
	params map[string]any
}{
	{"Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}},
	{"Page.enable", nil},
	{"Page.getResourceTree", nil},
	{"Runtime.enable", nil},
	{"Runtime.runIfWaitingForDebugger", nil},
	{"Network.enable", map[string]any{"maxPostDataSize": 65536}},
	{"Debugger.enable", map[string]any{"maxScriptsCacheSize": 1e7}},
	{"Debugger.setPauseOnExceptions", map[string]any{"state": "none"}},
	{"Debugger.setAsyncCallStackDepth", map[string]any{"maxDepth": 32}},
	{"Profiler.enable", nil},
	{"Log.enable", nil},
	{"Log.startViolationsReport", map[string]any{"config": []any{}}},
	{"Emulation.setEmulatedMedia", map[string]any{"media": "", "features": []any{}}},
	{"Emulation.setFocusEmulationEnabled", map[string]any{"enabled": false}},
	{"Overlay.enable", nil},
	{"Overlay.setShowViewportSizeOnResize", map[string]any{"show": true}},
	{"DOM.enable", nil},
	{"CSS.enable", nil},
	{"Accessibility.enable", nil},
	{"Animation.enable", nil},
	{"Page.getNavigationHistory", nil},
	{"DOM.getDocument", map[string]any{"depth": 1}},
}

func TestDevToolsStartupAndElementsPane(t *testing.T) {
	eng := htmlcss.NewStyleEngine(`.card { padding: 8px; color: #333 }`)
	app := eng.NewEl("div")
	app.SetClass("card")
	p := eng.NewEl("p")
	p.SetTextContent("hello")
	app.SetElementChildren([]qui.Widget{p})
	eng.SetRoot(app)
	w := qui.NewTestWindow(qui.Size{W: 300, H: 200})
	w.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{W: 300, H: 200})
	_, c := newUDSServer(t, w)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := c.Dial(ctx, "/devtools/page/"+w.TargetID())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, step := range devtoolsStartup {
		if _, err := conn.Call(ctx, step.method, step.params, ""); err != nil {
			t.Errorf("%s: %v", step.method, err)
		}
	}
	// Runtime.enable announced an execution context.
	gotContext := false
	for !gotContext {
		select {
		case ev := <-conn.Events():
			gotContext = ev.Method == "Runtime.executionContextCreated"
		case <-ctx.Done():
			t.Fatal("no Runtime.executionContextCreated")
		}
	}

	// Elements pane: expand the document, select the element, read styles.
	if _, err := conn.Call(ctx, "DOM.requestChildNodes", map[string]any{"nodeId": w.DocumentNodeID(), "depth": -1}, ""); err != nil {
		t.Fatal(err)
	}
	var children struct {
		ParentID int64 `json:"parentId"`
		Nodes    []struct {
			NodeID    int64  `json:"nodeId"`
			LocalName string `json:"localName"`
			Children  []struct {
				NodeName string `json:"nodeName"`
			} `json:"children"`
		} `json:"nodes"`
	}
	for children.ParentID == 0 {
		select {
		case ev := <-conn.Events():
			if ev.Method == "DOM.setChildNodes" {
				_ = json.Unmarshal(ev.Params, &children)
			}
		case <-ctx.Done():
			t.Fatal("no DOM.setChildNodes")
		}
	}
	if len(children.Nodes) != 1 || children.Nodes[0].LocalName != "div" || len(children.Nodes[0].Children) != 1 {
		t.Fatalf("setChildNodes = %+v", children)
	}
	div := children.Nodes[0].NodeID
	for _, m := range []string{"CSS.getMatchedStylesForNode", "CSS.getComputedStyleForNode", "CSS.getInlineStylesForNode", "CSS.getPlatformFontsForNode", "DOM.getBoxModel"} {
		if _, err := conn.Call(ctx, m, map[string]any{"nodeId": div}, ""); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
	if _, err := conn.Call(ctx, "Overlay.highlightNode", map[string]any{"nodeId": div, "highlightConfig": map[string]any{"showInfo": true}}, ""); err != nil {
		t.Errorf("highlightNode: %v", err)
	}
	if _, err := conn.Call(ctx, "Overlay.setInspectMode", map[string]any{"mode": "searchForNode", "highlightConfig": map[string]any{}}, ""); err != nil {
		t.Errorf("setInspectMode: %v", err)
	}
	raw, err := conn.Call(ctx, "DOM.pushNodesByBackendIdsToFrontend", map[string]any{"backendNodeIds": []int64{div}}, "")
	if err != nil {
		t.Fatalf("pushNodesByBackendIdsToFrontend: %v", err)
	}
	if string(raw) != `{"nodeIds":[`+itoa(int(div))+`]}` {
		t.Errorf("pushNodesByBackendIdsToFrontend = %s", raw)
	}
	if _, err := conn.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "1+1"}, ""); err != nil {
		t.Errorf("Runtime.evaluate: %v", err)
	}
	// A method that is neither implemented nor housekeeping still fails.
	if _, err := conn.Call(ctx, "Debugger.getScriptSource", map[string]any{"scriptId": "1"}, ""); remoteReason(err) != "unsupported" {
		t.Errorf("unknown method: %v", err)
	}
}
