// Command qui-agent is a small CLI client for the qui "agent" debugging
// surface. It replaces ad-hoc `curl --unix-socket ... | python3` invocations
// with a friendlier set of subcommands.
//
// A running qui app started with QUI_AGENT=1 listens on a Unix domain socket
// at $TMPDIR/qui-agent-<pid>.sock (and possibly /tmp/qui-agent-<pid>.sock).
// This tool discovers that socket, speaks HTTP over it, and exposes the agent
// endpoints (tree, act, wait, screenshot, health, diagnostics, raw) as
// subcommands.
//
// Usage:
//
//	qui-agent [-sock PATH] <command> [args...]
//
// Pure stdlib only.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const baseURL = "http://qui"

func main() {
	args := os.Args[1:]

	// Manual flag parsing so subcommand-specific flags (e.g. --gone) stay free.
	sockOverride := os.Getenv("QUI_AGENT_SOCK")
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-sock" || a == "--sock":
			if i+1 >= len(args) {
				fatalf("flag %s requires a value", a)
			}
			sockOverride = args[i+1]
			i++
		case strings.HasPrefix(a, "-sock="):
			sockOverride = strings.TrimPrefix(a, "-sock=")
		case strings.HasPrefix(a, "--sock="):
			sockOverride = strings.TrimPrefix(a, "--sock=")
		default:
			rest = append(rest, a)
		}
	}

	if len(rest) == 0 {
		usage()
		os.Exit(2)
	}

	cmd := rest[0]
	cmdArgs := rest[1:]

	// Subcommands that never need a socket.
	switch cmd {
	case "help", "-h", "--help":
		usage()
		return
	}

	sock, err := resolveSocket(sockOverride)
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	errf("using socket: %s", sock)
	client := newClient(sock)

	if err := dispatch(client, cmd, cmdArgs); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
}

func dispatch(c *http.Client, cmd string, args []string) error {
	switch cmd {
	case "tree":
		return cmdTree(c, args)
	case "click", "rightclick", "doubleclick", "hover", "focus":
		return cmdAction(c, cmd, args)
	case "pinch":
		return cmdPinch(c, args)
	case "type":
		return cmdType(c, args)
	case "wait":
		return cmdWait(c, args)
	case "shot":
		return cmdShot(c, args)
	case "health":
		return cmdGetPretty(c, "/health")
	case "diagnostics":
		return cmdGetPretty(c, "/diagnostics")
	case "raw":
		return cmdRaw(c, args)
	case "dom":
		return cmdDOM(c, args)
	case "reactive":
		return cmdReactive(c, args)
	default:
		usage()
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// ---------------------------------------------------------------------------
// Socket discovery
// ---------------------------------------------------------------------------

func resolveSocket(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("socket %q: %w", override, err)
		}
		return override, nil
	}

	seen := map[string]bool{}
	var candidates []string
	for _, dir := range []string{os.TempDir(), "/tmp"} {
		matches, _ := filepath.Glob(filepath.Join(dir, "qui-agent-*.sock"))
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				candidates = append(candidates, m)
			}
		}
	}

	type live struct {
		path string
		mod  time.Time
	}
	var alive []live
	for _, path := range candidates {
		pid, ok := pidFromSock(path)
		if !ok || !processAlive(pid) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		alive = append(alive, live{path: path, mod: info.ModTime()})
	}

	if len(alive) == 0 {
		return "", fmt.Errorf("no live qui-agent socket found.\n"+
			"  Looked in: %s and /tmp for qui-agent-*.sock\n"+
			"  Start an app with QUI_AGENT=1 (e.g. QUI_AGENT=1 ./app), or pass -sock PATH / set QUI_AGENT_SOCK.",
			os.TempDir())
	}

	// Most recently modified live socket wins.
	sort.Slice(alive, func(i, j int) bool {
		return alive[i].mod.After(alive[j].mod)
	})
	return alive[0].path, nil
}

// pidFromSock extracts the <pid> from a "qui-agent-<pid>.sock" filename.
func pidFromSock(path string) (int, bool) {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".sock")
	base = strings.TrimPrefix(base, "qui-agent-")
	pid, err := strconv.Atoi(base)
	if err != nil {
		return 0, false
	}
	return pid, true
}

// processAlive reports whether pid refers to a running process. Signal 0 is a
// no-op probe: nil or EPERM => alive, ESRCH => dead.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}

// ---------------------------------------------------------------------------
// HTTP over unix socket
// ---------------------------------------------------------------------------

func newClient(sock string) *http.Client {
	dialer := &net.Dialer{}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "unix", sock)
			},
		},
	}
}

func httpGet(c *http.Client, path string) ([]byte, error) {
	resp, err := c.Get(baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading GET %s: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func httpDo(c *http.Client, method, path string, body []byte) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, baseURL+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading %s %s: %w", method, path, err)
	}
	return out, resp.StatusCode, nil
}

// ---------------------------------------------------------------------------
// tree
// ---------------------------------------------------------------------------

type node struct {
	Path     string  `json:"path"`
	ID       string  `json:"id"`
	Role     string  `json:"role"`
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	State    string  `json:"state"`
	Shortcut string  `json:"shortcut"`
	HasPopup bool    `json:"hasPopup"`
	Bounds   bounds  `json:"bounds"`
	Visible  bool    `json:"visible"`
	Layer    string  `json:"layer"`
	Children []*node `json:"children"`
}

type bounds struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func cmdTree(c *http.Client, args []string) error {
	path := "/tree"
	if len(args) > 0 && args[0] != "" {
		path += "?selector=" + queryEscape(args[0])
	}
	body, err := httpGet(c, path)
	if err != nil {
		return err
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		// Selector form: JSON array of matched subtrees.
		var nodes []*node
		if err := json.Unmarshal(trimmed, &nodes); err != nil {
			return fmt.Errorf("decode tree array: %w", err)
		}
		if len(nodes) == 0 {
			fmt.Println("(no matches)")
			return nil
		}
		for _, n := range nodes {
			printNode(n, 0)
		}
		return nil
	}

	// Object form: {root, overlays}.
	var doc struct {
		Root     *node   `json:"root"`
		Overlays []*node `json:"overlays"`
	}
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return fmt.Errorf("decode tree: %w", err)
	}
	if doc.Root != nil {
		printNode(doc.Root, 0)
	}
	if len(doc.Overlays) > 0 {
		fmt.Println("overlays:")
		for _, o := range doc.Overlays {
			printNode(o, 1)
		}
	}
	return nil
}

func printNode(n *node, depth int) {
	if n == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	line := fmt.Sprintf("%s%s", indent, n.Role)
	if n.Name != "" {
		line += fmt.Sprintf(" %q", n.Name)
	}
	if n.ID != "" {
		line += fmt.Sprintf(" #%s", n.ID)
	}
	line += fmt.Sprintf(" (%g,%g,%g,%g)", n.Bounds.X, n.Bounds.Y, n.Bounds.W, n.Bounds.H)
	if n.Value != "" {
		line += fmt.Sprintf(" =%q", n.Value)
	}
	if n.Shortcut != "" {
		line += " " + n.Shortcut
	}
	if n.HasPopup {
		line += " >"
	}
	if n.State != "" {
		line += " [" + n.State + "]"
	}
	// State already carries a "hidden" bit for zero-bounds widgets; only add
	// the standalone marker when it doesn't, so nothing prints twice.
	if !n.Visible && !strings.Contains(n.State, "hidden") {
		line += " [hidden]"
	}
	fmt.Println(line)
	for _, ch := range n.Children {
		printNode(ch, depth+1)
	}
}

// ---------------------------------------------------------------------------
// actions (click / rightclick / doubleclick / hover / focus / type)
// ---------------------------------------------------------------------------

func cmdAction(c *http.Client, action string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%s requires a <selector>", action)
	}
	payload := map[string]any{
		"action": action,
		"target": args[0],
	}
	return postAct(c, payload)
}

// cmdPinch drives a two-finger magnify gesture on the target. scale is
// the cumulative magnification to end at, so `pinch #doc 1.5` zooms the
// document to 150% of where it was.
func cmdPinch(c *http.Client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("pinch requires <selector> <scale>  (e.g. pinch '#doc' 1.5)")
	}
	scale, err := strconv.ParseFloat(args[1], 32)
	if err != nil {
		return fmt.Errorf("pinch scale %q: %w", args[1], err)
	}
	if scale <= 0 {
		return fmt.Errorf("pinch scale must be > 0, got %v", scale)
	}
	return postAct(c, map[string]any{
		"action": "pinch",
		"target": args[0],
		"scale":  scale,
	})
}

func cmdType(c *http.Client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("type requires <selector> <text...>")
	}
	payload := map[string]any{
		"action": "type",
		"target": args[0],
		"text":   strings.Join(args[1:], " "),
	}
	return postAct(c, payload)
}

func postAct(c *http.Client, payload map[string]any) error {
	return postActTo(c, "/act", payload)
}

func postActTo(c *http.Client, path string, payload map[string]any) error {
	body, _ := json.Marshal(payload)
	out, status, err := httpDo(c, "POST", path, body)
	if err != nil {
		return err
	}

	var resp map[string]any
	if jerr := json.Unmarshal(bytes.TrimSpace(out), &resp); jerr != nil {
		// Not JSON — surface raw.
		if status >= 400 {
			return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(out)))
		}
		fmt.Println(strings.TrimSpace(string(out)))
		return nil
	}

	ok, _ := resp["ok"].(bool)
	if !ok || status >= 400 {
		if msg, has := resp["error"]; has {
			return fmt.Errorf("action failed: %v", msg)
		}
		return fmt.Errorf("action failed (HTTP %d): %s", status, compact(resp))
	}

	parts := []string{"ok"}
	if v, has := resp["overlayCount"]; has {
		parts = append(parts, fmt.Sprintf("overlayCount=%v", numStr(v)))
	}
	if v, has := resp["focusChange"]; has && v != nil && v != "" {
		parts = append(parts, fmt.Sprintf("focusChange=%v", v))
	}
	if v, has := resp["target"]; has && v != nil && v != "" {
		parts = append(parts, fmt.Sprintf("target=%v", v))
	}
	if m, has := resp["matched"].(map[string]any); has {
		if sel, ok := m["selector"]; ok {
			parts = append(parts, fmt.Sprintf("matched=%v", sel))
		}
	}
	fmt.Println(strings.Join(parts, " "))
	return nil
}

// ---------------------------------------------------------------------------
// wait
// ---------------------------------------------------------------------------

func cmdWait(c *http.Client, args []string) error {
	var selector string
	state := "appears"
	timeoutMs := 5000

	for _, a := range args {
		switch {
		case a == "--gone":
			state = "gone"
		case strings.HasPrefix(a, "--timeout="):
			v := strings.TrimPrefix(a, "--timeout=")
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("bad --timeout value %q", v)
			}
			timeoutMs = n
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown wait flag %q", a)
		default:
			if selector == "" {
				selector = a
			}
		}
	}
	if selector == "" {
		return fmt.Errorf("wait requires a <selector>")
	}

	path := fmt.Sprintf("/wait?for=selector&selector=%s&state=%s&timeoutMs=%d",
		queryEscape(selector), state, timeoutMs)

	out, status, err := httpDo(c, "GET", path, nil)
	if err != nil {
		return err
	}

	var resp map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(out), &resp)

	ok := status < 400
	if v, has := resp["ok"].(bool); has {
		ok = ok && v
	}
	timedOut := false
	if v, has := resp["timedOut"].(bool); has {
		timedOut = v
	}

	if !ok || timedOut {
		fmt.Printf("timeout: %q did not become %s within %dms\n", selector, state, timeoutMs)
		return fmt.Errorf("wait failed")
	}
	fmt.Printf("ok: %q is %s\n", selector, state)
	return nil
}

// ---------------------------------------------------------------------------
// shot (screenshot)
// ---------------------------------------------------------------------------

func cmdShot(c *http.Client, args []string) error {
	scale := "1"
	annotate := false
	outfile := "/tmp/qui-shot.png"

	for _, a := range args {
		switch {
		case a == "--annotate":
			annotate = true
		case strings.HasPrefix(a, "--scale="):
			scale = strings.TrimPrefix(a, "--scale=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown shot flag %q", a)
		default:
			outfile = a
		}
	}

	path := "/screenshot?scale=" + queryEscape(scale)
	if annotate {
		path += "&annotate=1"
	}
	body, err := httpGet(c, path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outfile, body, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", outfile, err)
	}
	fmt.Println(outfile)
	return nil
}

// ---------------------------------------------------------------------------
// health / diagnostics (pretty-print JSON)
// ---------------------------------------------------------------------------

func cmdGetPretty(c *http.Client, path string) error {
	body, err := httpGet(c, path)
	if err != nil {
		return err
	}
	return printJSON(body)
}

// ---------------------------------------------------------------------------
// raw escape hatch
// ---------------------------------------------------------------------------

func cmdRaw(c *http.Client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("raw requires <METHOD> <path> [body]")
	}
	method := strings.ToUpper(args[0])
	path := args[1]
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var body []byte
	if len(args) >= 3 {
		body = []byte(args[2])
	}
	out, status, err := httpDo(c, method, path, body)
	if err != nil {
		return err
	}
	if status >= 400 {
		errf("HTTP %d", status)
	}
	os.Stdout.Write(out)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

// ---------------------------------------------------------------------------
// dom (HTML-level surface)
// ---------------------------------------------------------------------------

type domNode struct {
	Path     string            `json:"path"`
	Tag      string            `json:"tag"`
	ID       string            `json:"id"`
	Classes  []string          `json:"classes"`
	Attrs    map[string]string `json:"attrs"`
	Text     string            `json:"text"`
	Role     string            `json:"role"`
	Display  string            `json:"display"`
	State    string            `json:"state"`
	Bounds   bounds            `json:"bounds"`
	Visible  bool              `json:"visible"`
	Layer    string            `json:"layer"`
	Children []*domNode        `json:"children"`
}

func cmdDOM(c *http.Client, args []string) error {
	if len(args) == 0 {
		return cmdDOMTree(c, nil)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "tree":
		return cmdDOMTree(c, rest)
	case "query":
		if len(rest) < 1 {
			return fmt.Errorf("dom query requires a <selector>")
		}
		return cmdDOMTree(c, rest[:1])
	case "styles":
		if len(rest) < 1 {
			return fmt.Errorf("dom styles requires a <selector>")
		}
		return cmdDOMStyles(c, rest[0])
	case "click", "doubleclick", "rightclick", "hover", "focus", "scroll":
		if len(rest) < 1 {
			return fmt.Errorf("dom %s requires a <selector>", sub)
		}
		return postActTo(c, "/dom/act", map[string]any{"action": sub, "target": rest[0]})
	case "blur":
		return postActTo(c, "/dom/act", map[string]any{"action": "blur"})
	case "type":
		if len(rest) < 2 {
			return fmt.Errorf("dom type requires <selector> <text...>")
		}
		return postActTo(c, "/dom/act", map[string]any{
			"action": "type", "target": rest[0], "text": strings.Join(rest[1:], " "),
		})
	default:
		return fmt.Errorf("unknown dom subcommand: %s (try tree|query|styles|click|type|hover|focus|scroll|blur)", sub)
	}
}

func cmdDOMTree(c *http.Client, args []string) error {
	path := "/dom"
	if len(args) > 0 && args[0] != "" {
		path += "?selector=" + queryEscape(args[0])
	}
	body, err := httpGet(c, path)
	if err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var nodes []*domNode
		if err := json.Unmarshal(trimmed, &nodes); err != nil {
			return fmt.Errorf("decode dom array: %w", err)
		}
		if len(nodes) == 0 {
			fmt.Println("(no matches)")
			return nil
		}
		for _, n := range nodes {
			printDOMNode(n, 0)
		}
		return nil
	}
	var doc struct {
		Root     *domNode   `json:"root"`
		Overlays []*domNode `json:"overlays"`
	}
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return fmt.Errorf("decode dom tree: %w", err)
	}
	if doc.Root != nil {
		printDOMNode(doc.Root, 0)
	}
	for _, o := range doc.Overlays {
		printDOMNode(o, 0)
	}
	return nil
}

func printDOMNode(n *domNode, depth int) {
	if n == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	line := indent + domSelectorText(n)
	if n.Display != "" && n.Display != "block" {
		line += " {" + n.Display + "}"
	}
	if n.Text != "" {
		t := n.Text
		if len(t) > 40 {
			t = t[:37] + "..."
		}
		line += fmt.Sprintf(" %q", t)
	}
	if n.State != "" {
		line += " [" + n.State + "]"
	}
	if !n.Visible {
		line += " [hidden]"
	}
	fmt.Println(line)
	for _, ch := range n.Children {
		printDOMNode(ch, depth+1)
	}
}

func domSelectorText(n *domNode) string {
	s := n.Tag
	if n.ID != "" {
		s += "#" + n.ID
	}
	for _, cl := range n.Classes {
		s += "." + cl
	}
	return s
}

type domStyles struct {
	Path         string            `json:"path"`
	Selector     string            `json:"selector"`
	Computed     map[string]string `json:"computed"`
	MatchedRules []struct {
		Selector     string `json:"selector"`
		Origin       string `json:"origin"`
		Specificity  [3]int `json:"specificity"`
		Declarations []struct {
			Property  string `json:"property"`
			Value     string `json:"value"`
			Important bool   `json:"important"`
			Active    bool   `json:"active"`
		} `json:"declarations"`
	} `json:"matchedRules"`
	BoxModel struct {
		Content bounds `json:"content"`
		Padding insets `json:"padding"`
		Border  insets `json:"border"`
		Margin  insets `json:"margin"`
	} `json:"boxModel"`
}

type insets struct {
	Top    float64 `json:"Top"`
	Right  float64 `json:"Right"`
	Bottom float64 `json:"Bottom"`
	Left   float64 `json:"Left"`
}

func (i insets) str() string {
	return fmt.Sprintf("%g %g %g %g", i.Top, i.Right, i.Bottom, i.Left)
}

func cmdDOMStyles(c *http.Client, selector string) error {
	body, err := httpGet(c, "/dom/styles?selector="+queryEscape(selector))
	if err != nil {
		return err
	}
	var styles []*domStyles
	if err := json.Unmarshal(bytes.TrimSpace(body), &styles); err != nil {
		return fmt.Errorf("decode styles: %w", err)
	}
	if len(styles) == 0 {
		fmt.Println("(no matches)")
		return nil
	}
	for i, es := range styles {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s\n", es.Selector)
		bm := es.BoxModel
		fmt.Printf("  box: content=(%g,%g,%g,%g) padding=[%s] border=[%s] margin=[%s]\n",
			bm.Content.X, bm.Content.Y, bm.Content.W, bm.Content.H,
			bm.Padding.str(), bm.Border.str(), bm.Margin.str())
		fmt.Println("  rules (winning first):")
		for _, r := range es.MatchedRules {
			spec := ""
			if r.Origin == "author" {
				spec = fmt.Sprintf(" (%d,%d,%d)", r.Specificity[0], r.Specificity[1], r.Specificity[2])
			}
			fmt.Printf("    [%s] %s%s\n", r.Origin, r.Selector, spec)
			for _, d := range r.Declarations {
				mark := ""
				if !d.Active {
					mark = "  (overridden)"
				}
				imp := ""
				if d.Important {
					imp = " !important"
				}
				fmt.Printf("      %s: %s%s%s\n", d.Property, d.Value, imp, mark)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// reactive (component-tree introspection)
// ---------------------------------------------------------------------------

type reactiveNode struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Key        string `json:"key"`
	Dirty      bool   `json:"dirty"`
	WidgetRole string `json:"widgetRole"`
	WidgetID   string `json:"widgetId"`
	Props      string `json:"props"`
	Hooks      []struct {
		Kind     string `json:"kind"`
		Value    string `json:"value"`
		Watchers int    `json:"watchers"`
	} `json:"hooks"`
	Children []*reactiveNode `json:"children"`
}

func cmdReactive(c *http.Client, _ []string) error {
	out, status, err := httpDo(c, "GET", "/reactive", nil)
	if err != nil {
		return err
	}
	if status == 204 || len(bytes.TrimSpace(out)) == 0 {
		fmt.Println("(this window is not driven by a reactive runtime)")
		return nil
	}
	trimmed := bytes.TrimSpace(out)
	if string(trimmed) == "null" {
		fmt.Println("(reactive runtime present but nothing mounted yet)")
		return nil
	}
	var root reactiveNode
	if err := json.Unmarshal(trimmed, &root); err != nil {
		return fmt.Errorf("decode reactive tree: %w", err)
	}
	printReactiveNode(&root, 0)
	return nil
}

func printReactiveNode(n *reactiveNode, depth int) {
	if n == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	label := n.Kind
	if n.Name != "" {
		label = "<" + n.Name + ">"
	}
	line := indent + label
	if n.Key != "" {
		line += "[" + n.Key + "]"
	}
	if n.WidgetRole != "" {
		line += " " + n.WidgetRole
		if n.WidgetID != "" {
			line += "#" + n.WidgetID
		}
	}
	if n.Props != "" {
		line += " props=" + n.Props
	}
	if n.Dirty {
		line += " *dirty*"
	}
	fmt.Println(line)
	for _, h := range n.Hooks {
		hl := fmt.Sprintf("%s· %s", strings.Repeat("  ", depth+1), h.Kind)
		if h.Value != "" {
			hl += " = " + h.Value
		}
		if h.Watchers > 0 {
			hl += fmt.Sprintf(" (%d watchers)", h.Watchers)
		}
		fmt.Println(hl)
	}
	for _, ch := range n.Children {
		printReactiveNode(ch, depth+1)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func printJSON(body []byte) error {
	var v any
	if err := json.Unmarshal(bytes.TrimSpace(body), &v); err != nil {
		// Not JSON — print raw.
		os.Stdout.Write(body)
		if len(body) > 0 && body[len(body)-1] != '\n' {
			fmt.Println()
		}
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func compact(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// numStr formats a JSON number without a trailing ".0" for whole values.
func numStr(v any) string {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return fmt.Sprintf("%v", v)
}

// queryEscape escapes a value for use in a URL query string.
func queryEscape(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteByte(r)
		default:
			b.WriteString(fmt.Sprintf("%%%02X", r))
		}
	}
	return b.String()
}

func usage() {
	fmt.Fprint(os.Stderr, `qui-agent — drive the qui agent debugging surface

Usage:
  qui-agent [-sock PATH] <command> [args...]

Global:
  -sock PATH              override socket (also env QUI_AGENT_SOCK)
                          default: most-recently-modified live
                          $TMPDIR/qui-agent-*.sock or /tmp/qui-agent-*.sock

Commands:
  tree [selector]        print the accessibility tree (optionally filtered by selector)
  click <selector>       click the matched widget
  rightclick <selector>  right-click the matched widget
  doubleclick <selector> double-click the matched widget
  hover <selector>       hover over the matched widget
  focus <selector>       focus the matched widget
  type <selector> <text> type text into the matched widget
  pinch <selector> <scale>
                         two-finger magnify gesture (1.5 = zoom to 150%)
  wait <selector> [--gone] [--timeout=ms]
                         wait until selector appears (or is gone); default 5000ms
  shot [outfile] [--scale=N] [--annotate]
                         save a screenshot PNG (default /tmp/qui-shot.png)
  health                 GET /health
  diagnostics            GET /diagnostics
  raw <METHOD> <path> [body]
                         raw request; prints the response body verbatim
  dom <subcommand>       HTML-level (DOM) surface — see below
  reactive               print the reactive component tree (state, signals,
                         props, pending-render) — like React DevTools
  help                   show this help

Widget selectors (CSS-attribute style, address the rendered widget tree):
  #id  [role=button]  [name="Save"]  [name*="ave"]  [text="..."]
  [key=qui.save]  :nth(N)  :visible  :focused  :layer(modal)

  In a localized app, [name=] matches the CURRENT language's caption and
  breaks on a language switch. Use #id or [key=<message key>], both of
  which are locale-independent. The tree reports each node's key as
  "nameKey"; empty means the text is a literal, not a translation.

DOM commands (address the app as HTML, with real CSS selectors — use these
to debug htmlcss / reactive-html business UIs):
  dom [tree] [selector]  print the DOM tree (optionally filtered by CSS selector)
  dom query <selector>   print DOM nodes matching a CSS selector
  dom styles <selector>  show computed style, matched rules (which won & why),
                         and box model for matching elements (DevTools "Styles")
  dom click <selector>   click the first element matching a CSS selector
  dom type <selector> <text...>   type into the matching element
  dom hover|focus|scroll|doubleclick|rightclick <selector>
  dom blur               clear focus

DOM CSS selectors are the full engine grammar: tag, #id, .class, [attr],
:nth-child, and combinators (descendant, > + ~). Example: ".card > button.primary"
Text locator (Playwright-style): :has-text("label") matches by visible text,
e.g. 'button:has-text("Save")'. DOM actions auto-wait for the target to appear.

Examples:
  qui-agent tree
  qui-agent tree '[role=button]'
  qui-agent click '[role=button][name="New"]'
  qui-agent type '#search' hello world
  qui-agent wait ':layer(modal)' --timeout=3000
  qui-agent shot /tmp/ui.png --scale=0.5 --annotate
  qui-agent dom tree
  qui-agent dom query '.card.active'
  qui-agent dom styles '.card > button.primary'
  qui-agent dom click '.toolbar button.save'
  qui-agent dom click 'button:has-text("Save")'
`)
}

func errf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

func fatalf(format string, a ...any) {
	errf(format, a...)
	os.Exit(2)
}
