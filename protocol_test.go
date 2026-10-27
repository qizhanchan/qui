package qui_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// call runs a protocol command in-process and round-trips the result
// through JSON, exactly as a client sees it.
func call(t *testing.T, w *qui.Window, method string, params any) (map[string]any, error) {
	t.Helper()
	cmd, ok := qui.LookupProtocolCommand(method)
	if !ok {
		t.Fatalf("no command %s", method)
	}
	raw, _ := json.Marshal(params)
	res, err := cmd.Call(&qui.ProtocolContext{Window: w}, raw)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("%s: marshal result: %v", method, err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: result is not an object: %s", method, b)
	}
	return out, nil
}

func reason(err error) string {
	if err == nil {
		return "<nil>"
	}
	return qui.ProtocolErrorFor(err).Reason
}

func TestProtocol_AgentSnapshotAndClickByRef(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	clicked := 0
	save := widgets.NewButton("Save", func() { clicked++ })
	save.SetID("save")
	name := widgets.NewInput("Name")
	c := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, save, name)
	c.SetSelf(c)
	w.SetRoot(c)
	c.Measure(qui.Size{W: 400, H: 300})
	c.Layout(qui.Rect{W: 400, H: 300})

	snap, err := call(t, w, "Agent.snapshot", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := snap["text"].(string)
	ref := save.NodeID().String()
	if !strings.Contains(text, `button "Save" [ref=`+ref+`] #save`) {
		t.Fatalf("snapshot lacks the Save line with its ref:\n%s", text)
	}

	res, err := call(t, w, "Agent.click", map[string]any{"target": ref})
	if err != nil {
		t.Fatalf("click %s: %v", ref, err)
	}
	if clicked != 1 {
		t.Fatalf("clicked = %d", clicked)
	}
	if m, _ := res["matched"].(map[string]any); m["nodeId"] != float64(save.NodeID()) {
		t.Fatalf("matched = %v", res["matched"])
	}

	if _, err := call(t, w, "Agent.fill", map[string]any{"target": "[role=textbox]", "value": "Ada"}); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if name.Text != "Ada" {
		t.Fatalf("input text = %q", name.Text)
	}

	// Strict params: a misnamed field is an error, not an empty target.
	if _, err := call(t, w, "Agent.click", map[string]any{"target": "#save", "selector": "#save"}); reason(err) != "invalid_params" {
		t.Fatalf("misnamed field: err = %v", err)
	}
	if _, err := call(t, w, "Agent.click", map[string]any{"target": "#nope", "timeoutMs": 0}); reason(err) != "no_match" {
		t.Fatalf("missing target: err = %v (%s)", err, reason(err))
	}
	c.RemoveChild(save)
	if _, err := call(t, w, "Agent.click", map[string]any{"target": ref}); reason(err) != "stale_node" {
		t.Fatalf("removed ref: err = %v (%s)", err, reason(err))
	}
}

func TestProtocol_DOMAndAccessibility(t *testing.T) {
	w, submit, _, _ := newTree(t)
	doc, err := call(t, w, "DOM.getDocument", map[string]any{"depth": -1})
	if err != nil {
		t.Fatal(err)
	}
	root := doc["root"].(map[string]any)
	if root["nodeName"] != "#document" || root["nodeId"] != float64(w.DocumentNodeID()) {
		t.Fatalf("document = %v", root)
	}
	q, err := call(t, w, "DOM.querySelector", map[string]any{"selector": "#submit"})
	if err != nil || q["nodeId"] != float64(submit.NodeID()) {
		t.Fatalf("querySelector #submit = %v, %v", q, err)
	}
	box, err := call(t, w, "DOM.getBoxModel", map[string]any{"nodeId": submit.NodeID()})
	if err != nil {
		t.Fatal(err)
	}
	if m := box["model"].(map[string]any); len(m["border"].([]any)) != 8 {
		t.Fatalf("box model = %v", m)
	}
	html, err := call(t, w, "DOM.getOuterHTML", map[string]any{"nodeId": submit.NodeID()})
	if err != nil || !strings.Contains(html["outerHTML"].(string), `id="submit"`) {
		t.Fatalf("outerHTML = %v, %v", html, err)
	}

	ax, err := call(t, w, "Accessibility.queryAXTree", map[string]any{"role": "button"})
	if err != nil {
		t.Fatal(err)
	}
	nodes := ax["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["backendDOMNodeId"] != float64(submit.NodeID()) {
		t.Fatalf("queryAXTree = %v", nodes)
	}
}

func TestProtocol_SchemaAndErrors(t *testing.T) {
	schema := qui.ProtocolSchema()
	b, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"domain":"Agent"`, `"name":"snapshot"`, `"domain":"DOM"`, `"name":"getBoxModel"`, `"name":"target"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("schema lacks %s", want)
		}
	}
	pe := qui.ProtocolErrorFor(errors.Join(qui.ErrStaleNode))
	if pe.Reason != "stale_node" {
		t.Errorf("stale reason = %q", pe.Reason)
	}
	out, _ := json.Marshal(pe)
	if !strings.Contains(string(out), `"reason":"stale_node"`) || !strings.Contains(string(out), `"code":-32000`) {
		t.Errorf("error JSON = %s", out)
	}
}
