//go:build darwin && cgo && webview_cef

package webview

import (
	"strings"
	"testing"
)

// parseEvaluateResult lives in tag-gated code; this test pins its JSON
// translation contract so future CEF version bumps that change the
// DevTools-protocol reply shape fail loudly rather than silently
// returning the wrong JSValue.
func TestParseEvaluateResult(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		success bool
		want    JSValue
		wantErr string // substring; empty means no error expected
	}{
		{
			name:    "number primitive",
			raw:     `{"result":{"type":"number","value":3,"description":"3"}}`,
			success: true,
			want:    JSValue{Kind: JSKindNumber, Number: 3},
		},
		{
			name:    "string primitive",
			raw:     `{"result":{"type":"string","value":"hi"}}`,
			success: true,
			want:    JSValue{Kind: JSKindString, String: "hi"},
		},
		{
			name:    "bool primitive",
			raw:     `{"result":{"type":"boolean","value":true}}`,
			success: true,
			want:    JSValue{Kind: JSKindBool, Bool: true},
		},
		{
			name:    "undefined",
			raw:     `{"result":{"type":"undefined"}}`,
			success: true,
			want:    JSValue{Kind: JSKindNull},
		},
		{
			name:    "null object subtype",
			raw:     `{"result":{"type":"object","subtype":"null","value":null}}`,
			success: true,
			want:    JSValue{Kind: JSKindNull},
		},
		{
			name:    "JS exception",
			raw:     `{"result":{"type":"object","subtype":"error","className":"SyntaxError","description":"SyntaxError: Unexpected end of input"},"exceptionDetails":{"text":"Uncaught","exception":{"description":"SyntaxError: Unexpected end of input","className":"SyntaxError"}}}`,
			success: true,
			wantErr: "SyntaxError",
		},
		{
			name:    "DevTools transport error",
			raw:     `{"code":-32000,"message":"Cannot find context"}`,
			success: false,
			wantErr: "Cannot find context",
		},
		{
			name:    "unserializable NaN",
			raw:     `{"result":{"type":"number","unserializableValue":"NaN"}}`,
			success: true,
			want:    JSValue{Kind: JSKindString, String: "NaN"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseEvaluateResult([]byte(tc.raw), tc.success)
			if tc.wantErr != "" {
				if got.Err == nil || !strings.Contains(got.Err.Error(), tc.wantErr) {
					t.Fatalf("err = %v; want substring %q", got.Err, tc.wantErr)
				}
				return
			}
			if got.Err != nil {
				t.Fatalf("unexpected err: %v", got.Err)
			}
			if got.Value.Kind != tc.want.Kind ||
				got.Value.Bool != tc.want.Bool ||
				got.Value.Number != tc.want.Number ||
				got.Value.String != tc.want.String {
				t.Errorf("value = %+v; want %+v", got.Value, tc.want)
			}
		})
	}
}

// JSON-object passthrough preserves the raw bytes so callers can
// Unmarshal into their target struct.
func TestParseEvaluateResultObjectPassthrough(t *testing.T) {
	raw := `{"result":{"type":"object","value":{"a":1,"b":[2,3]}}}`
	got := parseEvaluateResult([]byte(raw), true)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if got.Value.Kind != JSKindJSON {
		t.Fatalf("kind = %v; want JSKindJSON", got.Value.Kind)
	}
	if !strings.Contains(string(got.Value.JSON), `"a":1`) {
		t.Errorf("JSON = %s; want it to contain a:1", got.Value.JSON)
	}
}

// buildEvaluateMessage must produce a JSON object that the DevTools
// agent will accept. We pin a couple of structural invariants:
//   - id is the integer we passed
//   - method is Runtime.evaluate
//   - params.expression contains the user script even when it has
//     control chars / quotes
func TestBuildEvaluateMessageEscapes(t *testing.T) {
	script := "var x = \"quoted\\n\";\nx"
	msg := buildEvaluateMessage(7, script)
	if !strings.Contains(msg, `"id":7`) {
		t.Errorf("missing id: %s", msg)
	}
	if !strings.Contains(msg, `"method":"Runtime.evaluate"`) {
		t.Errorf("missing method: %s", msg)
	}
	// The quoted "n" inside the user string survives JSON-escaping
	// as "\\n" (literal backslash-n). The raw newline becomes "\n".
	if !strings.Contains(msg, `quoted\\n`) {
		t.Errorf("user backslash-n not preserved: %s", msg)
	}
}
