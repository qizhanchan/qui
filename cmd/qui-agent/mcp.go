package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/qizhanchan/qui/agent/client"
)

// An MCP server (Model Context Protocol, stdio transport) over the qui
// protocol. Tool names follow chrome-devtools-mcp / Playwright MCP —
// take_snapshot, click, fill, take_screenshot, wait_for — so an agent that
// knows those drives a qui app the same way. Every Agent.* command becomes
// a tool automatically, its input schema converted from the live protocol
// schema; a handful of curated tools cover screenshots, console, HTML,
// styles, targets and the inspect picker.

const mcpInstructions = `Drive a running qui desktop app (Go GUI). Start with take_snapshot: it prints the UI one node per line with [ref=@N].
Act with click / fill / type / press / select_option, passing an @ref from the snapshot (or a CSS / [role=…][name=…] selector) as "target".
Each action auto-waits for its target and reports what changed (focus, new overlays, tree hash). Errors carry a reason:
no_match / stale_node (re-snapshot), modal_blocked (handle the dialog), not_text_target (pick a control).
take_screenshot with annotate:true labels every node with its @ref. get_styles explains CSS; list_console_messages shows logs and panics.`

type mcpServer struct {
	r       *runner
	out     io.Writer
	outMu   sync.Mutex
	tools   []mcpTool
	toolsMu sync.Mutex
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	run         func(ctx context.Context, args map[string]any) ([]map[string]any, error)
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func serveMCP(ctx context.Context, r *runner, in io.Reader, out io.Writer) error {
	s := &mcpServer{r: r, out: out}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var wg sync.WaitGroup
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req mcpRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.reply(nil, nil, &mcpError{Code: -32700, Message: "parse error: " + err.Error()})
			continue
		}
		if len(req.ID) == 0 {
			continue // a notification (notifications/initialized, …)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.handle(ctx, req)
			s.reply(req.ID, res, err)
		}()
	}
	wg.Wait()
	return sc.Err()
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *mcpServer) reply(id json.RawMessage, result any, err *mcpError) {
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		msg["error"] = err
	} else {
		msg["result"] = result
	}
	b, _ := json.Marshal(msg)
	s.outMu.Lock()
	defer s.outMu.Unlock()
	s.out.Write(append(b, '\n'))
}

func (s *mcpServer) handle(ctx context.Context, req mcpRequest) (any, *mcpError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := p.ProtocolVersion
		if version == "" {
			version = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "qui-agent", "version": "2.0"},
			"instructions":    mcpInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools, err := s.toolList(ctx)
		if err != nil {
			return nil, &mcpError{Code: -32603, Message: err.Error()}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &mcpError{Code: -32602, Message: err.Error()}
		}
		tools, err := s.toolList(ctx)
		if err != nil {
			return toolError(err), nil
		}
		for _, t := range tools {
			if t.Name == p.Name {
				if p.Arguments == nil {
					p.Arguments = map[string]any{}
				}
				content, err := t.run(ctx, p.Arguments)
				if err != nil {
					return toolError(err), nil
				}
				return map[string]any{"content": content}, nil
			}
		}
		return nil, &mcpError{Code: -32602, Message: "unknown tool " + p.Name}
	}
	return nil, &mcpError{Code: -32601, Message: "method not found: " + req.Method}
}

func toolError(err error) map[string]any {
	msg := err.Error()
	var re *client.RemoteError
	if errors.As(err, &re) {
		if hint, _ := re.Data["hint"].(string); hint != "" {
			msg += "\nhint: " + hint
		}
	}
	return map[string]any{"isError": true, "content": []map[string]any{textContent(msg)}}
}

func textContent(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func jsonContent(raw json.RawMessage) []map[string]any {
	var v any
	if json.Unmarshal(raw, &v) == nil {
		if b, err := json.MarshalIndent(v, "", "  "); err == nil {
			raw = b
		}
	}
	return []map[string]any{textContent(string(raw))}
}

// toolList builds the tools from the server's schema (once).
func (s *mcpServer) toolList(ctx context.Context) ([]mcpTool, error) {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	if s.tools != nil {
		return s.tools, nil
	}
	b, err := s.r.c.Get(ctx, "/json/protocol")
	if err != nil {
		return nil, fmt.Errorf("cannot reach the qui app (is it running with QUI_AGENT=1?): %w", err)
	}
	var schema struct {
		Domains []struct {
			Domain   string `json:"domain"`
			Commands []struct {
				Name        string           `json:"name"`
				Description string           `json:"description"`
				Parameters  []map[string]any `json:"parameters"`
			} `json:"commands"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		return nil, err
	}
	var tools []mcpTool
	for _, d := range schema.Domains {
		if d.Domain != "Agent" {
			continue
		}
		for _, c := range d.Commands {
			method := "Agent." + c.Name
			name := snake(c.Name)
			if c.Name == "snapshot" {
				name = "take_snapshot"
			}
			tool := mcpTool{Name: name, Description: c.Description, InputSchema: jsonSchema(c.Parameters)}
			if c.Name == "snapshot" {
				tool.run = func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
					raw, err := s.r.call(method, args)
					if err != nil {
						return nil, err
					}
					var out struct{ Text string }
					if json.Unmarshal(raw, &out) == nil && out.Text != "" {
						return []map[string]any{textContent(out.Text)}, nil
					}
					return jsonContent(raw), nil
				}
			} else {
				tool.run = func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
					raw, err := s.r.call(method, args)
					if err != nil {
						return nil, err
					}
					return jsonContent(raw), nil
				}
			}
			tools = append(tools, tool)
		}
	}
	tools = append(tools, s.curatedTools()...)
	s.tools = tools
	return tools, nil
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func (s *mcpServer) passthrough(method string) func(context.Context, map[string]any) ([]map[string]any, error) {
	return func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
		raw, err := s.r.call(method, args)
		if err != nil {
			return nil, err
		}
		return jsonContent(raw), nil
	}
}

func (s *mcpServer) curatedTools() []mcpTool {
	r := s.r
	return []mcpTool{
		{
			Name:        "take_screenshot",
			Description: "PNG of the window (or a clip). annotate:true labels every node with its @ref, role and name.",
			InputSchema: obj(map[string]any{
				"scale":    prop("number", "1 = logical pixels, 0.5 halves the image"),
				"annotate": prop("boolean", "draw node bounds and @ref labels"),
				"clip":     obj(map[string]any{"x": prop("number", ""), "y": prop("number", ""), "width": prop("number", ""), "height": prop("number", "")}),
			}),
			run: func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
				p := map[string]any{"scale": args["scale"], "clip": args["clip"]}
				if a, _ := args["annotate"].(bool); a {
					p["annotate"] = map[string]any{}
				}
				for k, v := range p {
					if v == nil {
						delete(p, k)
					}
				}
				raw, err := r.call("Page.captureScreenshot", p)
				if err != nil {
					return nil, err
				}
				var out struct {
					Data          string
					Width, Height int
				}
				_ = json.Unmarshal(raw, &out)
				return []map[string]any{
					{"type": "image", "data": out.Data, "mimeType": "image/png"},
					textContent(fmt.Sprintf("%dx%d", out.Width, out.Height)),
				}, nil
			},
		},
		{
			Name:        "list_console_messages",
			Description: "Log output and panics (Runtime.exceptionThrown) since a seq. Pass the returned lastSeq as since to get only newer ones.",
			InputSchema: obj(map[string]any{"since": prop("integer", "only events after this seq")}),
			run: func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
				raw, err := r.c.Call(ctx, "Target.getEvents", map[string]any{"since": args["since"], "domains": []string{"Log", "Runtime"}}, "")
				if err != nil {
					return nil, err
				}
				return jsonContent(raw), nil
			},
		},
		{
			Name:        "get_html",
			Description: "The live UI serialized as HTML — read back what the app actually rendered. target: @ref or selector; omit for the whole window.",
			InputSchema: obj(map[string]any{"target": prop("string", "@ref or selector")}),
			run: func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
				p := map[string]any{}
				if t, _ := args["target"].(string); t != "" {
					id, err := r.resolve(t)
					if err != nil {
						return nil, err
					}
					p["nodeId"] = id
				}
				raw, err := r.call("DOM.getOuterHTML", p)
				if err != nil {
					return nil, err
				}
				var out struct{ OuterHTML string }
				_ = json.Unmarshal(raw, &out)
				return []map[string]any{textContent(out.OuterHTML)}, nil
			},
		},
		{
			Name:        "get_styles",
			Description: "Why an element looks the way it does: matched CSS rules (overridden declarations flagged), computed values, box model.",
			InputSchema: obj(map[string]any{"target": prop("string", "@ref or CSS selector")}, "target"),
			run: func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
				t, _ := args["target"].(string)
				p := map[string]any{"selector": t}
				if id, ok := refID(t); ok {
					p = map[string]any{"nodeId": id}
				}
				raw, err := r.call("CSS.getNodeStyles", p)
				if err != nil {
					return nil, err
				}
				return jsonContent(raw), nil
			},
		},
		{
			Name:        "list_targets",
			Description: "The app's windows. With several, pass one to select_target.",
			InputSchema: obj(map[string]any{}),
			run: func(ctx context.Context, _ map[string]any) ([]map[string]any, error) {
				raw, err := r.c.Call(ctx, "Target.getTargets", nil, "")
				if err != nil {
					return nil, err
				}
				return jsonContent(raw), nil
			},
		},
		{
			Name:        "select_target",
			Description: "Direct every following tool call at this window.",
			InputSchema: obj(map[string]any{"targetId": prop("string", "from list_targets")}, "targetId"),
			run: func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
				id, _ := args["targetId"].(string)
				r.target = id
				return []map[string]any{textContent("selected " + id)}, nil
			},
		},
		{
			Name:        "pick_element",
			Description: "Ask the user to click the element they mean in the app window; returns its nodeId (use as @<nodeId>).",
			InputSchema: obj(map[string]any{"timeoutMs": prop("integer", "default 120000")}),
			run: func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
				timeout := 2 * time.Minute
				if ms, ok := args["timeoutMs"].(float64); ok && ms > 0 {
					timeout = time.Duration(ms) * time.Millisecond
				}
				node, err := r.pick(ctx, timeout)
				if err != nil {
					return nil, err
				}
				b, _ := json.MarshalIndent(node, "", "  ")
				return []map[string]any{textContent(string(b))}, nil
			},
		},
		{
			Name:        "highlight",
			Description: "Outline a node in the app window (for the human watching).",
			InputSchema: obj(map[string]any{"target": prop("string", "@ref or selector")}, "target"),
			run: func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
				t, _ := args["target"].(string)
				p := map[string]any{"selector": t}
				if id, ok := refID(t); ok {
					p = map[string]any{"nodeId": id}
				}
				return s.passthrough("Overlay.highlightNode")(ctx, p)
			},
		},
		{
			Name:        "get_component_tree",
			Description: "The reactive component tree: mounted components, hook state, pending re-renders.",
			InputSchema: obj(map[string]any{}),
			run: func(ctx context.Context, _ map[string]any) ([]map[string]any, error) {
				raw, err := r.call("Reactive.getComponentTree", nil)
				if err != nil {
					return nil, err
				}
				var out struct{ Text string }
				if json.Unmarshal(raw, &out) == nil && out.Text != "" {
					return []map[string]any{textContent(out.Text)}, nil
				}
				return []map[string]any{textContent("(no reactive runtime in this window)")}, nil
			},
		},
		{
			Name:        "call_protocol",
			Description: "Any qui protocol method (CDP-shaped: DOM.*, CSS.*, Accessibility.*, Input.*, Page.*, Emulation.*, Animation.*, Debug.*). The schema is at the server's /json/protocol.",
			InputSchema: obj(map[string]any{
				"method": prop("string", "Domain.command"),
				"params": map[string]any{"type": "object", "description": "the method's parameters"},
			}, "method"),
			run: func(ctx context.Context, args map[string]any) ([]map[string]any, error) {
				m, _ := args["method"].(string)
				return s.passthrough(m)(ctx, toMap(args["params"]))
			},
		},
	}
}

// jsonSchema converts CDP-style parameter entries to a JSON Schema object.
func jsonSchema(params []map[string]any) map[string]any {
	props := map[string]any{}
	var required []string
	for _, p := range params {
		name, _ := p["name"].(string)
		props[name] = schemaOf(p)
		if opt, _ := p["optional"].(bool); !opt {
			required = append(required, name)
		}
	}
	return obj(props, required...)
}

func schemaOf(p map[string]any) map[string]any {
	out := map[string]any{}
	switch t, _ := p["type"].(string); t {
	case "any", "":
	case "array":
		out["type"] = "array"
		if items, ok := p["items"].(map[string]any); ok {
			out["items"] = schemaOf(items)
		}
	default:
		out["type"] = t
	}
	if d, ok := p["description"].(string); ok {
		out["description"] = d
	}
	if props, ok := p["properties"].([]any); ok {
		ps := make([]map[string]any, 0, len(props))
		for _, x := range props {
			if m, ok := x.(map[string]any); ok {
				ps = append(ps, m)
			}
		}
		sub := jsonSchema(ps)
		out["properties"] = sub["properties"]
	}
	return out
}

// snake turns waitForIdle into wait_for_idle.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
