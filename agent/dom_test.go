package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
)

// newDOMWindow builds and lays out a small htmlcss element tree:
//
//	div#app.page
//	  h1              "Title"
//	  button.btn.hot  "Save"   (onClick increments *clicked)
//	  ul
//	    li.row
//	    li.row.sel
func newDOMWindow(t *testing.T, clicked *int) *qui.Window {
	t.Helper()
	css := `.row { color: #808080 }
	        .row.sel { color: #00cc00; font-weight: 700 }
	        .btn { padding: 4px }`
	eng := htmlcss.NewStyleEngine(css)
	app := eng.NewEl("div")
	app.SetElementID("app")
	app.SetClass("page")

	h1 := eng.NewEl("h1")
	h1.SetTextContent("Title")
	btn := eng.NewEl("button")
	btn.SetClass("btn hot")
	btn.SetTextContent("Save")
	btn.SetOnClick(func() { *clicked++ })

	input := eng.NewEl("input")
	input.SetClass("field")

	ul := eng.NewEl("ul")
	li1 := eng.NewEl("li")
	li1.SetClass("row")
	li2 := eng.NewEl("li")
	li2.SetClass("row sel")
	ul.SetElementChildren([]qui.Widget{li1, li2})
	app.SetElementChildren([]qui.Widget{h1, btn, input, ul})
	eng.SetRoot(app)

	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	w.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	return w
}

func getJSON(t *testing.T, ts, path string, v any) {
	t.Helper()
	resp, err := http.Get(ts + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode %s: %v\nbody: %s", path, err, body)
	}
}

func TestDOMTree(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	var doc struct {
		Root struct {
			Tag      string `json:"tag"`
			Children []struct {
				Tag     string   `json:"tag"`
				ID      string   `json:"id"`
				Classes []string `json:"classes"`
			} `json:"children"`
		} `json:"root"`
	}
	getJSON(t, ts.URL, "/dom", &doc)
	if doc.Root.Tag != "#document" {
		t.Fatalf("root tag = %q, want #document", doc.Root.Tag)
	}
	if len(doc.Root.Children) != 1 || doc.Root.Children[0].ID != "app" {
		t.Fatalf("top-level = %+v, want single div#app", doc.Root.Children)
	}
}

func TestDOMQuery(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	var rows []struct {
		Tag     string   `json:"tag"`
		Classes []string `json:"classes"`
	}
	getJSON(t, ts.URL, "/dom?selector="+url(".row"), &rows)
	if len(rows) != 2 {
		t.Fatalf(".row matched %d, want 2", len(rows))
	}

	var sel []struct {
		Tag string `json:"tag"`
	}
	getJSON(t, ts.URL, "/dom?selector="+url("ul > li.sel"), &sel)
	if len(sel) != 1 || sel[0].Tag != "li" {
		t.Fatalf("ul > li.sel matched %+v, want single li", sel)
	}
}

func TestDOMStyles(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	var styles []struct {
		Selector     string            `json:"selector"`
		Computed     map[string]string `json:"computed"`
		MatchedRules []struct {
			Selector     string `json:"selector"`
			Origin       string `json:"origin"`
			Declarations []struct {
				Property string `json:"property"`
				Value    string `json:"value"`
				Active   bool   `json:"active"`
			} `json:"declarations"`
		} `json:"matchedRules"`
	}
	getJSON(t, ts.URL, "/dom/styles?selector="+url("li.sel"), &styles)
	if len(styles) != 1 {
		t.Fatalf("styles matched %d, want 1", len(styles))
	}
	if styles[0].Computed["color"] != "#00cc00" {
		t.Errorf("computed color = %q, want #00cc00", styles[0].Computed["color"])
	}
	// .row's color declaration must be present but overridden.
	var sawOverridden bool
	for _, r := range styles[0].MatchedRules {
		if r.Selector == ".row" {
			for _, d := range r.Declarations {
				if d.Property == "color" && !d.Active {
					sawOverridden = true
				}
			}
		}
	}
	if !sawOverridden {
		t.Errorf(".row color should be reported overridden; rules=%+v", styles[0].MatchedRules)
	}
}

func TestDOMActClick(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	body := `{"action":"click","target":"button.btn.hot"}`
	resp, err := http.Post(ts.URL+"/dom/act", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("dom/act click not ok: %s", raw)
	}
	if clicked != 1 {
		t.Errorf("button onClick fired %d times, want 1 (resp: %s)", clicked, raw)
	}
	if m, has := out["matched"].(map[string]any); has {
		if m["selector"] != "button.btn.hot" {
			t.Errorf("matched selector = %v, want button.btn.hot", m["selector"])
		}
	} else {
		t.Errorf("response missing matched: %s", raw)
	}
}

func TestDOMActTypeReachesBackingAndTextState(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	body := `{"action":"type","target":"input.field","text":"hello","timeoutMs":0}`
	resp, err := http.Post(ts.URL+"/dom/act", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("type not ok: %s", raw)
	}

	// The typed text must land in the backing edit widget and be visible in
	// the element's textState (not swallowed by the El's content SetText).
	var nodes []struct {
		TextState *qui.TextState `json:"textState"`
	}
	getJSON(t, ts.URL, "/dom?selector="+url("input.field"), &nodes)
	if len(nodes) != 1 || nodes[0].TextState == nil {
		t.Fatalf("input.field textState missing: %+v", nodes)
	}
	if nodes[0].TextState.Value != "hello" {
		t.Errorf("textState.Value = %q, want hello", nodes[0].TextState.Value)
	}
	if nodes[0].TextState.Caret != 5 {
		t.Errorf("textState.Caret = %d, want 5", nodes[0].TextState.Caret)
	}
}

func TestDOMActNoMatch(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	body := `{"action":"click","target":".does-not-exist","timeoutMs":0}`
	resp, err := http.Post(ts.URL+"/dom/act", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("expected failure for no-match, got: %s", raw)
	}
	if out["errorCode"] != "no_match" {
		t.Errorf("errorCode = %v, want no_match", out["errorCode"])
	}
}

func TestDOMActAutoWaitTimesOut(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	// A selector that never matches must engage the auto-wait loop and
	// return no_match only after roughly the requested timeout.
	start := time.Now()
	body := `{"action":"click","target":".never-appears","timeoutMs":200}`
	resp, err := http.Post(ts.URL+"/dom/act", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out["errorCode"] != "no_match" {
		t.Fatalf("errorCode = %v, want no_match (%s)", out["errorCode"], raw)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("returned after %v, expected to wait ~200ms", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("waited %v, far longer than the 200ms timeout", elapsed)
	}
}

func TestDOMActNoWaitWhenDisabled(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	// timeoutMs:0 disables auto-wait: a missing selector fails immediately.
	start := time.Now()
	body := `{"action":"click","target":".never-appears","timeoutMs":0}`
	resp, err := http.Post(ts.URL+"/dom/act", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out["errorCode"] != "no_match" {
		t.Fatalf("errorCode = %v, want no_match", out["errorCode"])
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("with timeoutMs:0 returned after %v, expected near-immediate", elapsed)
	}
}

// url percent-escapes a selector for a query string (spaces, >, etc.).
func url(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteByte(r)
		default:
			b.WriteString("%" + hex2(r))
		}
	}
	return b.String()
}

func hex2(b byte) string {
	const h = "0123456789ABCDEF"
	return string([]byte{h[b>>4], h[b&0xf]})
}
