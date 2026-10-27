// Command qui-agent drives a running qui app over its inspection protocol
// (the agent server, started with QUI_AGENT=1).
//
// It finds the app through the discovery directory (pick one by name with
// -app when several run), and turns subcommands into protocol calls:
//
//	qui-agent snapshot                    the UI as text, one @ref per node
//	qui-agent click @14                   act on a ref, or on any selector
//	qui-agent fill 'input.search' hello
//	qui-agent shot /tmp/ui.png -annotate
//	qui-agent call DOM.getDocument '{"depth":-1}'
//	qui-agent mcp                         serve the protocol to an MCP client
//
// It imports only the standard library and the stdlib-only agent/client
// package, so it builds anywhere and does not link the GUI engine.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/qizhanchan/qui/agent/client"
)

const usageText = `qui-agent — drive a running qui app (start it with QUI_AGENT=1)

usage: qui-agent [-app NAME | -sock PATH | -tcp HOST:PORT] [-target ID] <command> [args]

finding the app
  -app NAME        the app's discovery name (default: the only running app)
  -sock PATH       a Unix socket (also QUI_AGENT_SOCK)
  -tcp HOST:PORT   a TCP listener; token from -token or QUI_AGENT_TOKEN
  -target ID       the window (from 'targets') when the app has several

observe
  apps                          running apps
  targets                       the app's windows
  snapshot [ROOT] [-all] [-depth N] [-json]
                                the UI as text, one [ref=@N] per node
  query SELECTOR                every node it matches: nodeId, role, name
  html [TARGET]                 the live tree serialized as HTML
  styles TARGET                 matched rules, computed values, box model
  reactive                      the reactive component tree
  shot FILE [-scale S] [-annotate] [-clip x,y,w,h]
  events [-domains Log,Runtime] [-since SEQ] [-follow]
  diagnostics                   layout warnings

act (TARGET = @ref from snapshot, or a selector: CSS, [role=…][name=…], #id)
  click TARGET [-right] [-double] [-mods cmd,shift]   |  click -at X,Y
  hover TARGET | hover -at X,Y
  focus TARGET    blur
  fill TARGET VALUE             set a text field / pick a <select> option
  type TARGET TEXT              key by key (IME, key handlers)
  press [TARGET] KEY            Enter, Escape, Cmd+Shift+Z…
  select TARGET LABEL [-value]  pick a choice-widget option
  scroll TARGET DX DY           wheel notches; positive DY scrolls down
  drag FROM TO | drag -at X,Y -to X2,Y2
  pinch TARGET SCALE

wait
  wait-ready [-timeout MS]      the app has loaded and gone idle (run first)
  wait SELECTOR [-state visible|hidden|attached|detached] [-timeout MS]
  wait-idle [-timeout MS]   wait-stable [-timeout MS]   wait-overlay [PATTERN] [-gone]

inspect
  pick                          the user clicks a node in the window; prints it
  highlight TARGET   unhighlight
  flags [name=true|false ...]   engine debug switches (paint, layout, …)

protocol
  call METHOD [JSON]            any protocol method, e.g. call DOM.getDocument '{"depth":-1}'
  schema                        the full method schema (JSON)
  llm                           the guide + method reference for agents
  mcp                           serve the protocol as an MCP server on stdio

Every action auto-waits up to 1s for its target and prints what changed.
`

type globals struct {
	app, sock, tcp, token, target string
}

func main() {
	g, rest := parseGlobals(os.Args[1:])
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		fmt.Print(usageText)
		if len(rest) == 0 {
			os.Exit(2)
		}
		return
	}
	cmd, args := rest[0], rest[1:]
	if cmd == "apps" {
		printJSON(client.RunningApps())
		return
	}
	ep, err := client.FindEndpoint(g.sock, g.tcp, g.token, g.app)
	if err != nil {
		fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	r := &runner{c: client.NewClient(ep), ctx: ctx, target: g.target}
	if cmd == "mcp" {
		if err := serveMCP(ctx, r, os.Stdin, os.Stdout); err != nil {
			fail(err)
		}
		return
	}
	if err := r.run(cmd, args); err != nil {
		fail(err)
	}
}

func parseGlobals(args []string) (globals, []string) {
	var g globals
	for i := 0; i < len(args); i++ {
		a := strings.TrimPrefix(args[i], "-")
		name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "-"), "=")
		ptr := map[string]*string{"app": &g.app, "sock": &g.sock, "tcp": &g.tcp, "token": &g.token, "target": &g.target}[name]
		if !strings.HasPrefix(args[i], "-") || ptr == nil {
			return g, args[i:]
		}
		if !hasVal {
			if i+1 >= len(args) {
				fail(fmt.Errorf("flag -%s needs a value", name))
			}
			i++
			val = args[i]
		}
		*ptr = val
	}
	return g, nil
}

func fail(err error) {
	var re *client.RemoteError
	if errors.As(err, &re) {
		fmt.Fprintf(os.Stderr, "qui-agent: %s\n", re.Error())
		if hint, _ := re.Data["hint"].(string); hint != "" {
			fmt.Fprintf(os.Stderr, "  hint: %s\n", hint)
		}
	} else {
		fmt.Fprintf(os.Stderr, "qui-agent: %v\n", err)
	}
	os.Exit(1)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func printRaw(raw json.RawMessage) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		os.Stdout.Write(raw)
		return
	}
	printJSON(v)
}

type runner struct {
	c      *client.Client
	ctx    context.Context
	target string
}

func (r *runner) call(method string, params any) (json.RawMessage, error) {
	return r.c.Call(r.ctx, method, params, r.target)
}

// flags parses args with flags allowed anywhere among the positionals.
func flags(name string, args []string, define func(fs *flag.FlagSet)) []string {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	define(fs)
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			fail(err)
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	return pos
}

func need(pos []string, n int, usage string) {
	if len(pos) < n {
		fail(fmt.Errorf("usage: qui-agent %s", usage))
	}
}

func point(s string) (*float64, *float64) {
	xs, ys, ok := strings.Cut(s, ",")
	x, err1 := strconv.ParseFloat(strings.TrimSpace(xs), 64)
	y, err2 := strconv.ParseFloat(strings.TrimSpace(ys), 64)
	if !ok || err1 != nil || err2 != nil {
		fail(fmt.Errorf("bad point %q: want X,Y", s))
	}
	return &x, &y
}

func mods(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func (r *runner) act(method string, params map[string]any) error {
	raw, err := r.call(method, params)
	if err != nil {
		return err
	}
	printRaw(raw)
	return nil
}

func (r *runner) run(cmd string, args []string) error {
	switch cmd {
	case "targets":
		return r.act("Target.getTargets", nil)
	case "snapshot":
		var all, asJSON bool
		var depth int
		pos := flags(cmd, args, func(fs *flag.FlagSet) {
			fs.BoolVar(&all, "all", false, "keep hidden nodes and plumbing")
			fs.IntVar(&depth, "depth", 0, "max depth")
			fs.BoolVar(&asJSON, "json", false, "nested JSON tree")
		})
		p := map[string]any{"all": all, "maxDepth": depth}
		if len(pos) > 0 {
			p["root"] = pos[0]
		}
		if asJSON {
			p["format"] = "json"
		}
		raw, err := r.call("Agent.snapshot", p)
		if err != nil {
			return err
		}
		if asJSON {
			printRaw(raw)
			return nil
		}
		var out struct{ Text string }
		_ = json.Unmarshal(raw, &out)
		fmt.Print(out.Text)
		return nil
	case "html":
		p := map[string]any{}
		if len(args) > 0 {
			id, err := r.resolve(args[0])
			if err != nil {
				return err
			}
			p["nodeId"] = id
		}
		raw, err := r.call("DOM.getOuterHTML", p)
		if err != nil {
			return err
		}
		var out struct{ OuterHTML string }
		_ = json.Unmarshal(raw, &out)
		fmt.Println(out.OuterHTML)
		return nil
	case "styles":
		need(args, 1, "styles TARGET")
		if strings.HasPrefix(args[0], "@") {
			id, err := r.resolve(args[0])
			if err != nil {
				return err
			}
			return r.act("CSS.getNodeStyles", map[string]any{"nodeId": id})
		}
		return r.act("CSS.getNodeStyles", map[string]any{"selector": args[0]})
	case "reactive":
		raw, err := r.call("Reactive.getComponentTree", nil)
		if err != nil {
			return err
		}
		var out struct{ Text string }
		_ = json.Unmarshal(raw, &out)
		if out.Text == "" {
			fmt.Println("(no reactive runtime in this window)")
		}
		fmt.Print(out.Text)
		return nil
	case "diagnostics":
		return r.act("Debug.getDiagnostics", nil)
	case "shot":
		var scale float64
		var annotate bool
		var clip string
		pos := flags(cmd, args, func(fs *flag.FlagSet) {
			fs.Float64Var(&scale, "scale", 1, "0.5 halves the size")
			fs.BoolVar(&annotate, "annotate", false, "label every node with its @ref")
			fs.StringVar(&clip, "clip", "", "x,y,w,h in window coordinates")
		})
		need(pos, 1, "shot FILE [-scale S] [-annotate] [-clip x,y,w,h]")
		p := map[string]any{"scale": scale}
		if annotate {
			p["annotate"] = map[string]any{}
		}
		if clip != "" {
			var x, y, w, h float64
			if _, err := fmt.Sscanf(clip, "%g,%g,%g,%g", &x, &y, &w, &h); err != nil {
				return fmt.Errorf("bad -clip %q: want x,y,w,h", clip)
			}
			p["clip"] = map[string]any{"x": x, "y": y, "width": w, "height": h}
		}
		raw, err := r.call("Page.captureScreenshot", p)
		if err != nil {
			return err
		}
		var out struct {
			Data          string
			Width, Height int
		}
		_ = json.Unmarshal(raw, &out)
		img, err := base64.StdEncoding.DecodeString(out.Data)
		if err != nil {
			return err
		}
		if err := os.WriteFile(pos[0], img, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%dx%d)\n", pos[0], out.Width, out.Height)
		return nil
	case "events":
		return r.events(args)
	case "click":
		var right, double bool
		var at, mod string
		pos := flags(cmd, args, func(fs *flag.FlagSet) {
			fs.BoolVar(&right, "right", false, "right button")
			fs.BoolVar(&double, "double", false, "double click")
			fs.StringVar(&at, "at", "", "X,Y window point instead of a target")
			fs.StringVar(&mod, "mods", "", "cmd,shift,ctrl,alt")
		})
		p := map[string]any{"modifiers": mods(mod)}
		if right {
			p["button"] = "right"
		}
		if double {
			p["clickCount"] = 2
		}
		if at != "" {
			p["x"], p["y"] = point(at)
		} else {
			need(pos, 1, "click TARGET | click -at X,Y")
			p["target"] = pos[0]
		}
		return r.act("Agent.click", p)
	case "hover":
		var at string
		pos := flags(cmd, args, func(fs *flag.FlagSet) { fs.StringVar(&at, "at", "", "X,Y") })
		p := map[string]any{}
		if at != "" {
			p["x"], p["y"] = point(at)
		} else {
			need(pos, 1, "hover TARGET | hover -at X,Y")
			p["target"] = pos[0]
		}
		return r.act("Agent.hover", p)
	case "focus":
		need(args, 1, "focus TARGET")
		return r.act("Agent.focus", map[string]any{"target": args[0]})
	case "blur":
		return r.act("Agent.blur", map[string]any{})
	case "fill":
		need(args, 2, "fill TARGET VALUE")
		return r.act("Agent.fill", map[string]any{"target": args[0], "value": args[1]})
	case "type":
		need(args, 2, "type TARGET TEXT")
		return r.act("Agent.type", map[string]any{"target": args[0], "text": args[1]})
	case "press":
		need(args, 1, "press [TARGET] KEY")
		if len(args) == 1 {
			return r.act("Agent.press", map[string]any{"key": args[0]})
		}
		return r.act("Agent.press", map[string]any{"target": args[0], "key": args[1]})
	case "select":
		var byValue bool
		pos := flags(cmd, args, func(fs *flag.FlagSet) { fs.BoolVar(&byValue, "value", false, "match the option value, not its label") })
		need(pos, 2, "select TARGET LABEL [-value]")
		key := "label"
		if byValue {
			key = "value"
		}
		return r.act("Agent.selectOption", map[string]any{"target": pos[0], key: pos[1]})
	case "scroll":
		need(args, 3, "scroll TARGET DX DY")
		dx, err1 := strconv.ParseFloat(args[1], 64)
		dy, err2 := strconv.ParseFloat(args[2], 64)
		if err1 != nil || err2 != nil {
			return errors.New("usage: qui-agent scroll TARGET DX DY")
		}
		return r.act("Agent.scroll", map[string]any{"target": args[0], "dx": dx, "dy": dy})
	case "drag":
		var at, to string
		var steps int
		pos := flags(cmd, args, func(fs *flag.FlagSet) {
			fs.StringVar(&at, "at", "", "X,Y start point")
			fs.StringVar(&to, "to", "", "X2,Y2 end point")
			fs.IntVar(&steps, "steps", 10, "intermediate moves")
		})
		p := map[string]any{"steps": steps}
		if at != "" {
			if to == "" {
				fail(errors.New("usage: qui-agent drag -at X,Y -to X2,Y2"))
			}
			p["x"], p["y"] = point(at)
			p["x2"], p["y2"] = point(to)
		} else {
			need(pos, 2, "drag FROM TO")
			p["from"], p["to"] = pos[0], pos[1]
		}
		return r.act("Agent.drag", p)
	case "pinch":
		need(args, 2, "pinch TARGET SCALE")
		s, err := strconv.ParseFloat(args[1], 64)
		if err != nil {
			return err
		}
		return r.act("Agent.pinch", map[string]any{"target": args[0], "scale": s})
	case "wait":
		var state string
		var timeout int
		pos := flags(cmd, args, func(fs *flag.FlagSet) {
			fs.StringVar(&state, "state", "visible", "visible | hidden | attached | detached")
			fs.IntVar(&timeout, "timeout", 2000, "ms")
		})
		need(pos, 1, "wait SELECTOR [-state …] [-timeout MS]")
		return r.act("Agent.waitFor", map[string]any{"selector": pos[0], "state": state, "timeoutMs": timeout})
	case "wait-ready":
		var timeout int
		flags(cmd, args, func(fs *flag.FlagSet) { fs.IntVar(&timeout, "timeout", 5000, "ms") })
		return r.act("Agent.waitForReady", map[string]any{"timeoutMs": timeout})
	case "query":
		need(args, 1, "query SELECTOR")
		return r.act("Agent.query", map[string]any{"selector": args[0]})
	case "wait-idle", "wait-stable":
		var timeout int
		flags(cmd, args, func(fs *flag.FlagSet) { fs.IntVar(&timeout, "timeout", 2000, "ms") })
		method := "Agent.waitForIdle"
		if cmd == "wait-stable" {
			method = "Agent.waitForStable"
		}
		return r.act(method, map[string]any{"timeoutMs": timeout})
	case "wait-overlay":
		var gone bool
		var timeout int
		pos := flags(cmd, args, func(fs *flag.FlagSet) {
			fs.BoolVar(&gone, "gone", false, "wait for it to disappear")
			fs.IntVar(&timeout, "timeout", 2000, "ms")
		})
		p := map[string]any{"timeoutMs": timeout, "state": "visible"}
		if gone {
			p["state"] = "hidden"
		}
		if len(pos) > 0 {
			p["pattern"] = pos[0]
		}
		return r.act("Agent.waitForOverlay", p)
	case "pick":
		node, err := r.pick(r.ctx, 5*time.Minute)
		if err != nil {
			return err
		}
		printJSON(node)
		return nil
	case "highlight":
		need(args, 1, "highlight TARGET")
		id, err := r.resolve(args[0])
		if err != nil {
			return err
		}
		return r.act("Overlay.highlightNode", map[string]any{"nodeId": id})
	case "unhighlight":
		return r.act("Overlay.hideHighlight", map[string]any{})
	case "flags":
		if len(args) == 0 {
			return r.act("Debug.getFlags", nil)
		}
		p := map[string]any{}
		for _, a := range args {
			k, v, ok := strings.Cut(a, "=")
			b, err := strconv.ParseBool(v)
			if !ok || err != nil {
				return fmt.Errorf("bad flag %q: want name=true|false", a)
			}
			p[k] = b
		}
		return r.act("Debug.setFlags", p)
	case "call":
		need(args, 1, "call METHOD [JSON]")
		var params any
		if len(args) > 1 {
			if err := json.Unmarshal([]byte(args[1]), &params); err != nil {
				return fmt.Errorf("params are not JSON: %w", err)
			}
		}
		return r.act(args[0], toMap(params))
	case "schema":
		b, err := r.c.Get(r.ctx, "/json/protocol")
		if err != nil {
			return err
		}
		os.Stdout.Write(b)
		return nil
	case "llm":
		b, err := r.c.Get(r.ctx, "/llm.txt")
		if err != nil {
			return err
		}
		os.Stdout.Write(b)
		return nil
	}
	fmt.Fprint(os.Stderr, usageText)
	return fmt.Errorf("unknown command %q", cmd)
}

func toMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func refID(s string) (int64, bool) {
	if !strings.HasPrefix(s, "@") {
		return 0, false
	}
	n, err := strconv.ParseInt(s[1:], 10, 64)
	return n, err == nil && n > 0
}

// resolve turns a TARGET into a nodeId: a ref directly, a selector through
// Agent.query, so TARGET means what it means for every action (DOM.*
// would read it as CSS only).
func (r *runner) resolve(target string) (int64, error) {
	if id, ok := refID(target); ok {
		return id, nil
	}
	raw, err := r.call("Agent.query", map[string]any{"selector": target})
	if err != nil {
		return 0, err
	}
	var out struct{ Matches []struct{ NodeID int64 } }
	_ = json.Unmarshal(raw, &out)
	if len(out.Matches) == 0 {
		return 0, fmt.Errorf("no_match: %s matched nothing", target)
	}
	return out.Matches[0].NodeID, nil
}

// events prints buffered events, and with -follow streams new ones.
func (r *runner) events(args []string) error {
	var domains string
	var since int64
	var follow bool
	flags("events", args, func(fs *flag.FlagSet) {
		fs.StringVar(&domains, "domains", "", "comma-separated: Log,Runtime,Agent,Target,Overlay")
		fs.Int64Var(&since, "since", 0, "events after this seq")
		fs.BoolVar(&follow, "follow", false, "keep streaming")
	})
	var ds []string
	if domains != "" {
		ds = strings.Split(domains, ",")
	}
	for {
		raw, err := r.c.Call(r.ctx, "Target.getEvents", map[string]any{"since": since, "domains": ds}, "")
		if err != nil {
			return err
		}
		var out struct {
			Events  []json.RawMessage
			LastSeq int64
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		for _, e := range out.Events {
			fmt.Println(string(e))
		}
		since = out.LastSeq
		if !follow {
			return nil
		}
		select {
		case <-r.ctx.Done():
			return nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// pick turns on inspect mode and waits for the user to click a node.
func (r *runner) pick(ctx context.Context, timeout time.Duration) (map[string]any, error) {
	conn, err := r.c.Dial(ctx, "/devtools/browser/qui-agent")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	session := ""
	if r.target != "" {
		raw, err := conn.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": r.target}, "")
		if err != nil {
			return nil, err
		}
		var att struct{ SessionID string }
		_ = json.Unmarshal(raw, &att)
		session = att.SessionID
	}
	if _, err := conn.Call(ctx, "Overlay.enable", nil, session); err != nil {
		return nil, err
	}
	if _, err := conn.Call(ctx, "Overlay.setInspectMode", map[string]any{"mode": "searchForNode"}, session); err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, "click the node in the app window (Escape cancels)…")
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-conn.Events():
			if !ok {
				return nil, errors.New("connection closed")
			}
			if ev.Method == "Overlay.inspectNodeRequested" {
				var picked map[string]any
				_ = json.Unmarshal(ev.Params, &picked)
				return picked, nil
			}
		case <-deadline:
			_, _ = conn.Call(context.Background(), "Overlay.setInspectMode", map[string]any{"mode": "none"}, session)
			return nil, errors.New("timeout: nothing was picked")
		case <-ctx.Done():
			_, _ = conn.Call(context.Background(), "Overlay.setInspectMode", map[string]any{"mode": "none"}, session)
			return nil, ctx.Err()
		}
	}
}
