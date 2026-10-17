package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func postAct(t *testing.T, base, body string) map[string]any {
	t.Helper()
	resp, err := http.Post(base+"/act", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

// A class / tag / combinator target on /act goes through the DOM: the
// widget grammar has none of them, and the caller shouldn't need to know.
func TestActFallsBackToDOMForCSSSelectors(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	for _, sel := range []string{".btn.hot", "button.hot", "#app > button", `button:has-text("Save")`} {
		before := clicked
		out := postAct(t, ts.URL, `{"action":"click","target":`+jsonStr(sel)+`}`)
		if ok, _ := out["ok"].(bool); !ok || out["via"] != "dom" {
			t.Fatalf("click %q: %v", sel, out)
		}
		if clicked != before+1 {
			t.Errorf("click %q: onClick fired %d times", sel, clicked-before)
		}
	}

	out := postAct(t, ts.URL, `{"action":"type","target":"input.field","text":"hi"}`)
	if ok, _ := out["ok"].(bool); !ok || out["via"] != "dom" {
		t.Fatalf("type input.field: %v", out)
	}
}

// Widget-grammar selectors keep the widget path untouched.
func TestActWidgetSelectorStaysOnWidgetPath(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)
	out := postAct(t, ts.URL, `{"action":"click","target":"[role=button][name=\"Save\"]"}`)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("widget click: %v", out)
	}
	if _, has := out["via"]; has {
		t.Errorf("widget selector reported via=%v", out["via"])
	}
}

func TestActCSSSelectorFailures(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	// No match through the fallback: the DOM's no_match, plus the widget
	// parser's complaint for context.
	out := postAct(t, ts.URL, `{"action":"click","target":".nope"}`)
	if out["errorCode"] != "no_match" || out["via"] != "dom" || out["widgetSelectorError"] == nil {
		t.Errorf("click .nope: %v", out)
	}

	// pinch has no DOM form: bad_selector with a hint, not a silent no-op.
	out = postAct(t, ts.URL, `{"action":"pinch","target":".btn","scale":1.5}`)
	if out["errorCode"] != "bad_selector" || out["hint"] == nil {
		t.Errorf("pinch .btn: %v", out)
	}
}

func getWait(t *testing.T, base, query string) map[string]any {
	t.Helper()
	var out map[string]any
	getJSON(t, base, "/wait?"+query, &out)
	return out
}

func TestWaitCSSSelectors(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)

	if out := getWait(t, ts.URL, "for=selector&selector="+url("li.row.sel")+"&timeoutMs=500"); out["ok"] != true || out["via"] != "dom" {
		t.Errorf("for=selector li.row.sel: %v", out)
	}
	if out := getWait(t, ts.URL, "for=dom&selector="+url(".btn")+"&timeoutMs=500"); out["ok"] != true {
		t.Errorf("for=dom .btn: %v", out)
	}
	if out := getWait(t, ts.URL, "for=dom&selector="+url(".nope")+"&state=gone&timeoutMs=500"); out["ok"] != true {
		t.Errorf("for=dom .nope gone: %v", out)
	}
	if out := getWait(t, ts.URL, "for=dom&selector="+url(".nope")+"&timeoutMs=100"); out["errorCode"] != "timeout" {
		t.Errorf("for=dom .nope: %v", out)
	}
	// A widget selector that can't parse used to read as a bare timeout;
	// the DOM retry may still time out, but the parse error comes along.
	if out := getWait(t, ts.URL, "for=selector&selector="+url(":bogus")+"&timeoutMs=100"); out["widgetSelectorError"] == nil {
		t.Errorf("for=selector :bogus: %v", out)
	}
}

func TestTreeCSSSelectorPointsAtDOM(t *testing.T) {
	clicked := 0
	w := newDOMWindow(t, &clicked)
	ts, _ := newAgentServer(t, w)
	resp, err := http.Get(ts.URL + "/tree?selector=" + url(".row"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "/dom?selector=") {
		t.Errorf("tree .row: %d %s", resp.StatusCode, raw)
	}
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
