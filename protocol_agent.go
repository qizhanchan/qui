package qui

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The Agent domain: the high-level surface an LLM drives. It is to the raw
// domains what Playwright is to CDP — a compact text snapshot of the UI
// with @refs, actions addressed by selector or @ref that auto-wait for
// their target, resolve it exactly once, and report what the action
// changed (focus, new overlays, the tree hash, idle state).

const (
	agentDefaultWaitMs = 1000
	agentPollInterval  = 25 * time.Millisecond
	// agentMaxWait caps every client-supplied wait: each poll is a UI-thread
	// hop, and an abandoned request must not poll for days.
	agentMaxWait = time.Minute
)

// semanticRoles are the roles a snapshot always shows; nodes with any other
// role appear only when they carry a name, value or ID (plumbing boxes are
// flattened away, their children promoted).
var semanticRoles = map[string]bool{
	RoleButton: true, RoleTextbox: true, RoleTextarea: true, RoleLabel: true, RoleCheckbox: true,
	RoleRadio: true, RoleSlider: true, RoleCombobox: true, RoleListbox: true, RoleListitem: true,
	RoleTable: true, RoleTableRow: true, RoleTableCell: true, RoleColumnHeader: true, RoleRowHeader: true,
	RoleTabs: true, RoleTab: true, RoleMenu: true, RoleMenuitem: true, RoleDialog: true, RolePopup: true,
	RoleTooltip: true, RoleFrame: true, RoleFiledrop: true, RoleViewport3d: true, RoleVideo: true,
	RoleWebview: true, RoleChart: true, RoleDiagram: true, RoleDiagramNode: true, RoleDiagramEdge: true,
	RoleLink: true, RoleSeparator: true, RoleImage: true, RoleHeading: true, RoleScrollview: true,
	"spinner": true, "tree": true, "treeitem": true, "split": true, "colorpicker": true, "switch": true,
	"progressbar": true, "list": true, "paragraph": true,
}

// SnapshotOptions controls Window.SnapshotText.
type SnapshotOptions struct {
	// Root limits the snapshot to the subtree at this node (0 = the whole
	// window: main tree, then overlays bottom-to-top).
	Root NodeID
	// MaxDepth caps the printed depth (0 = unlimited).
	MaxDepth int
	// All keeps hidden nodes and plumbing (no name, value, ID or semantic
	// role) that the default snapshot flattens away.
	All bool
}

// SnapshotText renders the AX tree as an indented outline in the shape of
// Playwright's aria snapshot — one node per line, `[ref=@N]` to address it:
//
//   - button "Save" [ref=@14] [disabled]
//   - textbox "Search" [ref=@20]: hello
//   - combobox "Method" [ref=@31]: GET options=[GET*, POST]
//
// UI goroutine only.
func (w *Window) SnapshotText(opts SnapshotOptions) (string, error) {
	return snapshotText(w, w.AccessibilityTree(), opts)
}

func snapshotText(w *Window, tree *AccessibilityTree, opts SnapshotOptions) (string, error) {
	var b strings.Builder
	if opts.Root == 0 {
		size := tree.WindowSize
		fmt.Fprintf(&b, "# %s %q %gx%g", w.TargetID(), w.Title(), size.W, size.H)
		if tree.DevicePixelRatio != 0 && tree.DevicePixelRatio != 1 {
			fmt.Fprintf(&b, " dpr=%g", tree.DevicePixelRatio)
		}
		b.WriteByte('\n')
		if len(tree.Accelerators) > 0 {
			fmt.Fprintf(&b, "# accelerators: %s\n", strings.Join(tree.Accelerators, ", "))
		}
	}
	roots, err := axRoots(tree, opts.Root)
	if err != nil {
		return "", err
	}
	for _, r := range roots {
		writeSnapshotNode(&b, r, 0, opts, true)
	}
	return b.String(), nil
}

func snapshotInteresting(n *AXNode) bool {
	return semanticRoles[n.Role] || n.Name != "" || n.Value != "" || n.ID != "" ||
		strings.Contains(n.State, "focused") || len(n.Options) > 0
}

func writeSnapshotNode(b *strings.Builder, n *AXNode, depth int, opts SnapshotOptions, isRoot bool) {
	if !opts.All && !n.Visible && !isRoot {
		return
	}
	show := opts.All || isRoot || snapshotInteresting(n)
	childDepth := depth
	if show {
		if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
			return
		}
		b.WriteString(strings.Repeat("  ", depth))
		b.WriteString("- ")
		b.WriteString(snapshotLine(n, isRoot))
		b.WriteByte('\n')
		childDepth = depth + 1
	}
	for _, c := range n.Children {
		if show && !opts.All && redundantChild(n, c) {
			continue
		}
		writeSnapshotNode(b, c, childDepth, opts, false)
	}
}

// redundantChild reports a child that only repeats its parent: the backing
// label of a heading or button (same text), an icon's inner image. Such a
// child adds a line and a ref nobody needs; actions on the parent reach it.
func redundantChild(parent, c *AXNode) bool {
	if c.ID != "" || c.Value != "" || len(c.Options) > 0 || strings.Contains(c.State, "focused") {
		return false
	}
	for _, gc := range c.Children {
		if !redundantChild(c, gc) {
			return false
		}
	}
	switch {
	case c.Name != "" && c.Name == parent.Name:
		return c.Role == RoleLabel || c.Role == parent.Role
	case c.Name == "":
		return c.Role == parent.Role
	}
	return false
}

func snapshotLine(n *AXNode, isRoot bool) string {
	var b strings.Builder
	role := n.Role
	if role == "" {
		role = "node"
	}
	b.WriteString(role)
	if n.Name != "" {
		fmt.Fprintf(&b, " %q", n.Name)
	}
	if n.NodeID != 0 {
		fmt.Fprintf(&b, " [ref=%s]", n.NodeID)
	}
	if n.ID != "" {
		b.WriteString(" #" + n.ID)
	}
	if n.NameKey != "" {
		b.WriteString(" key=" + n.NameKey)
	}
	if isRoot && n.Layer != "" && n.Layer != "root" {
		b.WriteString(" layer=" + n.Layer)
	}
	var flags []string
	for _, s := range strings.Split(n.State, "|") {
		if s != "" && s != "hovered" {
			flags = append(flags, s)
		}
	}
	if !n.Visible {
		flags = append(flags, "invisible")
	}
	if n.HasPopup {
		flags = append(flags, "haspopup")
	}
	if len(flags) > 0 {
		b.WriteString(" [" + strings.Join(flags, ", ") + "]")
	}
	if n.Shortcut != "" {
		b.WriteString(" shortcut=" + n.Shortcut)
	}
	if n.Value != "" {
		b.WriteString(": " + strings.ReplaceAll(n.Value, "\n", `\n`))
	}
	if n.TextState != nil && (n.TextState.SelStart >= 0 || n.TextState.Preedit != "") {
		fmt.Fprintf(&b, " caret=%d", n.TextState.Caret)
		if n.TextState.SelStart >= 0 && n.TextState.SelEnd != n.TextState.SelStart {
			fmt.Fprintf(&b, " sel=%d..%d", n.TextState.SelStart, n.TextState.SelEnd)
		}
		if n.TextState.Preedit != "" {
			fmt.Fprintf(&b, " preedit=%q", n.TextState.Preedit)
		}
	}
	if len(n.Options) > 0 {
		labels := make([]string, 0, len(n.Options))
		for _, o := range n.Options {
			l := o.Label
			if o.Selected {
				l += "*"
			}
			labels = append(labels, l)
		}
		b.WriteString(" options=[" + strings.Join(labels, ", ") + "]")
	}
	return b.String()
}

// -------------------------------------------------------------------
// Resolution

// agentResolve resolves target to one VISIBLE node, retrying until timeout
// while the target is absent or hidden — states that occur before any event
// is dispatched, so a retried action is never partially applied. A selector
// error or a stale @ref fails at once.
func agentResolve(ctx context.Context, w *Window, target string, timeout time.Duration) (*AXNode, error) {
	if strings.TrimSpace(target) == "" {
		return nil, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: `"target" is required: a selector or an @ref`}
	}
	_, isRef := ParseNodeRef(strings.TrimSpace(target))
	deadline := time.Now().Add(timeout)
	for {
		nodes, err := w.FindNodesSynced(target)
		if err != nil {
			return nil, err
		}
		var hidden *AXNode
		for _, n := range nodes {
			if n.Visible {
				return n, nil
			}
			if hidden == nil {
				hidden = n
			}
		}
		if isRef && len(nodes) == 0 {
			return nil, noMatchFor(target)
		}
		if timeout <= 0 || !time.Now().Before(deadline) {
			if hidden != nil {
				return nil, fmt.Errorf("%w: %s", ErrNotVisible, target)
			}
			return nil, noMatchFor(target)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(agentPollInterval):
		}
	}
}

func waitTimeout(ms *int, def int) time.Duration {
	if ms == nil {
		return time.Duration(def) * time.Millisecond
	}
	if *ms <= 0 {
		return 0
	}
	return min(time.Duration(*ms)*time.Millisecond, agentMaxWait)
}

// ParseModifierNames decodes ["cmd","shift",…] (cmd = super = meta,
// alt = option, ctrl = control).
func ParseModifierNames(names []string) (Modifiers, error) {
	var m Modifiers
	for _, s := range names {
		switch strings.ToLower(s) {
		case "shift":
			m |= ModShift
		case "ctrl", "control":
			m |= ModControl
		case "alt", "option":
			m |= ModAlt
		case "cmd", "super", "meta", "command":
			m |= ModSuper
		default:
			return 0, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: "unknown modifier " + s}
		}
	}
	return m, nil
}

// -------------------------------------------------------------------
// Results

type agentMatched struct {
	NodeID NodeID `json:"nodeId"`
	Role   string `json:"role"`
	Name   string `json:"name,omitempty"`
}

type agentFocusChange struct {
	From NodeID `json:"from"`
	To   NodeID `json:"to"`
}

type agentOverlay struct {
	NodeID NodeID `json:"nodeId"`
	Layer  string `json:"layer"`
}

// AgentActionResult is what every Agent action reports: what it acted on
// and what changed.
type AgentActionResult struct {
	Matched     *agentMatched     `json:"matched,omitempty"`
	TreeHash    string            `json:"treeHash" desc:"AX content hash after the action; unchanged = the action changed nothing visible"`
	FocusChange *agentFocusChange `json:"focusChange,omitempty"`
	NewOverlays []agentOverlay    `json:"newOverlays,omitempty" desc:"overlays (dialogs, menus, popups) that appeared"`
	Diagnostics int               `json:"diagnostics,omitempty" desc:"layout diagnostics now reported; read them with Debug.getDiagnostics"`
	IdleState   IdleState         `json:"idleState"`
}

type agentObservation struct {
	focus    NodeID
	overlays map[NodeID]string
}

func agentObserve(tree *AccessibilityTree) agentObservation {
	o := agentObservation{overlays: map[NodeID]string{}}
	for _, n := range tree.flatten() {
		if strings.Contains(n.State, "focused") {
			o.focus = n.NodeID
		}
	}
	for _, ov := range tree.Overlays {
		o.overlays[ov.NodeID] = ov.Layer
	}
	return o
}

// agentAct runs one action: observe, resolve the target once, act on its
// @ref, observe again.
func agentAct(ctx *ProtocolContext, target string, timeoutMs *int, act func(ref string) error) (AgentActionResult, error) {
	w := ctx.Window
	pre, err := w.syncedTree()
	if err != nil {
		return AgentActionResult{}, err
	}
	before := agentObserve(pre)
	var matched *agentMatched
	ref := ""
	if target != "" {
		n, err := agentResolve(ctx.ctx(), w, target, waitTimeout(timeoutMs, agentDefaultWaitMs))
		if err != nil {
			return AgentActionResult{}, err
		}
		matched = &agentMatched{NodeID: n.NodeID, Role: n.Role, Name: n.Name}
		ref = n.NodeID.String()
	}
	if err := act(ref); err != nil {
		return AgentActionResult{}, err
	}
	post, err := w.syncedTree()
	if err != nil {
		return AgentActionResult{}, err
	}
	after := agentObserve(post)
	res := AgentActionResult{Matched: matched, TreeHash: post.Hash(), IdleState: w.IdleStateSynced()}
	if before.focus != after.focus {
		res.FocusChange = &agentFocusChange{From: before.focus, To: after.focus}
	}
	for _, ov := range post.Overlays {
		if _, had := before.overlays[ov.NodeID]; !had {
			res.NewOverlays = append(res.NewOverlays, agentOverlay{NodeID: ov.NodeID, Layer: ov.Layer})
		}
	}
	res.Diagnostics = len(w.DebugLayoutDiagnosticsSynced())
	return res, nil
}

// -------------------------------------------------------------------
// Params

type agentSnapshotParams struct {
	Format   string `json:"format,omitempty" desc:"text (default) | json"`
	Root     string `json:"root,omitempty" desc:"selector or @ref: snapshot only this subtree"`
	MaxDepth int    `json:"maxDepth,omitempty" desc:"0 = unlimited"`
	All      bool   `json:"all,omitempty" desc:"keep hidden nodes and unnamed plumbing"`
}

type agentSnapshotResult struct {
	Text     string             `json:"text,omitempty"`
	Tree     *AccessibilityTree `json:"tree,omitempty" desc:"format=json: the full nested AX tree (or the root subtree)"`
	Subtree  *AXNode            `json:"subtree,omitempty"`
	TreeHash string             `json:"treeHash"`
}

type agentTarget struct {
	Target    string `json:"target" desc:"selector (CSS, widget grammar) or @ref"`
	TimeoutMs *int   `json:"timeoutMs,omitempty" desc:"auto-wait for the target to appear and be visible; default 1000, 0 = no wait"`
}

type agentClickParams struct {
	Target         string   `json:"target,omitempty" desc:"selector or @ref; omit to click the point (x, y)"`
	X              *float32 `json:"x,omitempty" desc:"window point, when there is no target (a drawn surface)"`
	Y              *float32 `json:"y,omitempty"`
	Button         string   `json:"button,omitempty" desc:"left (default) | right | middle"`
	ClickCount     int      `json:"clickCount,omitempty" desc:"2 = double click"`
	Modifiers      []string `json:"modifiers,omitempty" desc:"cmd, shift, ctrl, alt"`
	ScrollIntoView *bool    `json:"scrollIntoView,omitempty" desc:"default true"`
	TimeoutMs      *int     `json:"timeoutMs,omitempty" desc:"auto-wait; default 1000, 0 = no wait"`
}

type agentHoverParams struct {
	Target    string   `json:"target,omitempty" desc:"selector or @ref; omit to hover the point (x, y)"`
	X         *float32 `json:"x,omitempty"`
	Y         *float32 `json:"y,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
	TimeoutMs *int     `json:"timeoutMs,omitempty"`
}

type agentFillParams struct {
	Target    string `json:"target" desc:"a text control, a <select>, or a widget that owns its editing"`
	Value     string `json:"value"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

type agentTypeParams struct {
	Target    string `json:"target"`
	Text      string `json:"text"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

type agentPressParams struct {
	Target    string `json:"target,omitempty" desc:"focus this first; omit to press at the focused widget"`
	Key       string `json:"key" desc:"key or chord: Enter, Escape, Tab, Left, Cmd+Shift+Z, Ctrl+A"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

type agentSelectParams struct {
	Target    string `json:"target"`
	Label     string `json:"label,omitempty" desc:"option label (forgiving: trimmed, then case-insensitive)"`
	Value     string `json:"value,omitempty" desc:"option value, when it differs from the label"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

type agentScrollParams struct {
	Target    string  `json:"target"`
	DX        float32 `json:"dx,omitempty" desc:"wheel notches; positive scrolls right"`
	DY        float32 `json:"dy,omitempty" desc:"wheel notches; positive scrolls down"`
	TimeoutMs *int    `json:"timeoutMs,omitempty"`
}

type agentDragParams struct {
	From      string   `json:"from,omitempty" desc:"selector or @ref; omit both from and to to drag between points"`
	To        string   `json:"to,omitempty"`
	X         *float32 `json:"x,omitempty"`
	Y         *float32 `json:"y,omitempty"`
	X2        *float32 `json:"x2,omitempty"`
	Y2        *float32 `json:"y2,omitempty"`
	Steps     int      `json:"steps,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
	TimeoutMs *int     `json:"timeoutMs,omitempty"`
}

type agentPinchParams struct {
	Target    string  `json:"target"`
	Scale     float32 `json:"scale" desc:"cumulative magnification to end at: 1.5 = zoom in to 150%"`
	TimeoutMs *int    `json:"timeoutMs,omitempty"`
}

type agentWaitForParams struct {
	Selector  string `json:"selector" desc:"selector or @ref"`
	State     string `json:"state,omitempty" desc:"visible (default) | hidden | attached | detached"`
	TimeoutMs *int   `json:"timeoutMs,omitempty" desc:"default 2000"`
}

type agentWaitParams struct {
	TimeoutMs *int `json:"timeoutMs,omitempty" desc:"default 2000 (waitForReady: 5000)"`
}

type agentQueryParams struct {
	Selector string `json:"selector" desc:"selector or @ref, as for any Agent target"`
}

type agentQueryResult struct {
	Matches []agentMatched `json:"matches" desc:"every match, in tree order"`
}

type agentReadyResult struct {
	ReadyState string    `json:"readyState" desc:"loading | interactive | complete, as document.readyState"`
	IdleState  IdleState `json:"idleState"`
	TreeHash   string    `json:"treeHash"`
}

type agentWaitOverlayParams struct {
	Pattern   string `json:"pattern,omitempty" desc:"substring of the overlay's layer: modal, overlay"`
	State     string `json:"state,omitempty" desc:"visible (default) | hidden"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

type agentWaitResult struct {
	IdleState IdleState `json:"idleState"`
	TreeHash  string    `json:"treeHash"`
}

func agentWaitDone(w *Window, err error) (agentWaitResult, error) {
	if err != nil {
		return agentWaitResult{}, err
	}
	return agentWaitResult{IdleState: w.IdleStateSynced(), TreeHash: w.AccessibilityTreeSynced().Hash()}, nil
}

// -------------------------------------------------------------------
// Domain

func agentDomain() ProtocolDomain {
	cmds := []ProtocolCommand{
		NewProtocolCommand("snapshot", "The UI as an indented outline with an @ref per node (default), or the nested AX tree as JSON. Read this first; act on the @refs.",
			func(ctx *ProtocolContext, p agentSnapshotParams) (out agentSnapshotResult, err error) {
				err = ctx.Window.synchronously(func() error {
					tree := ctx.Window.AccessibilityTree()
					out.TreeHash = tree.Hash()
					var root *AXNode
					if p.Root != "" {
						nodes, err := ctx.Window.findNodesIn(tree, p.Root)
						if err != nil {
							return err
						}
						if len(nodes) == 0 {
							return noMatchFor(p.Root)
						}
						root = nodes[0]
					}
					switch p.Format {
					case "", "text":
						opts := SnapshotOptions{MaxDepth: p.MaxDepth, All: p.All}
						if root != nil {
							opts.Root = root.NodeID
						}
						out.Text, err = snapshotText(ctx.Window, tree, opts)
						return err
					case "json":
						if root != nil {
							out.Subtree = root
						} else {
							out.Tree = tree
						}
						return nil
					}
					return &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: "format must be text or json"}
				})
				return out, err
			}),
		NewProtocolCommand("click", "Click a node (auto-waits for it), or a raw window point. button/clickCount cover right and double clicks.",
			agentClick).WithStrict(),
		NewProtocolCommand("hover", "Move the pointer onto a node (or a raw point).",
			func(ctx *ProtocolContext, p agentHoverParams) (AgentActionResult, error) {
				mods, err := ParseModifierNames(p.Modifiers)
				if err != nil {
					return AgentActionResult{}, err
				}
				if p.Target == "" {
					if p.X == nil || p.Y == nil {
						return AgentActionResult{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: `hover needs "target" or both "x" and "y"`}
					}
					return agentAct(ctx, "", nil, func(string) error {
						return ctx.Window.HoverAt(*p.X, *p.Y, ClickOptions{Modifiers: mods})
					})
				}
				return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error {
					return ctx.Window.Hover(ref, ClickOptions{Modifiers: mods, ScrollIntoView: true})
				})
			}).WithStrict(),
		NewProtocolCommand("focus", "Give a node keyboard focus (a form element hands it to its control).",
			func(ctx *ProtocolContext, p agentTarget) (AgentActionResult, error) {
				return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error { return ctx.Window.Focus(ref) })
			}).WithStrict(),
		NewProtocolCommand("blur", "Clear keyboard focus.",
			func(ctx *ProtocolContext, _ empty) (AgentActionResult, error) {
				return agentAct(ctx, "", nil, func(string) error { return ctx.Window.Blur() })
			}).WithStrict(),
		NewProtocolCommand("fill", "Set a text control's value in one step (one undo entry, change handler fired), or pick a <select> option by label. Fails with not_text_target, changing nothing, on a node that takes no text.",
			func(ctx *ProtocolContext, p agentFillParams) (AgentActionResult, error) {
				return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error {
					return ctx.Window.Type(ref, p.Value, TypeOptions{ScrollIntoView: true})
				})
			}).WithStrict(),
		NewProtocolCommand("type", "Type text key by key (per-rune events) — for IME, key handlers and controlled-value races that fill skips.",
			func(ctx *ProtocolContext, p agentTypeParams) (AgentActionResult, error) {
				return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error {
					return ctx.Window.Type(ref, p.Text, TypeOptions{Raw: true, ScrollIntoView: true})
				})
			}).WithStrict(),
		NewProtocolCommand("press", "Press a key or chord, at the target (focused first) or at whatever is focused.",
			func(ctx *ProtocolContext, p agentPressParams) (AgentActionResult, error) {
				key, mods, err := parseProtocolKey(p.Key)
				if err != nil {
					return AgentActionResult{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: err.Error()}
				}
				return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error {
					return ctx.Window.SendKey(ref, key, mods)
				})
			}).WithStrict(),
		NewProtocolCommand("selectOption", "Pick a choice-widget option by label or value, in one call, without opening its popup.",
			func(ctx *ProtocolContext, p agentSelectParams) (AgentActionResult, error) {
				if (p.Label == "") == (p.Value == "") {
					return AgentActionResult{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: `give exactly one of "label" or "value"`}
				}
				return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error {
					label := p.Label
					if p.Value != "" {
						n, err := firstNode(ctx.Window, ref)
						if err != nil {
							return err
						}
						for _, o := range n.Options {
							if o.Value == p.Value || (o.Value == "" && o.Label == p.Value) {
								label = o.Label
								break
							}
						}
						if label == "" {
							return NewProtocolError("no_match", "no option with value %q", p.Value)
						}
					}
					n, err := firstNode(ctx.Window, ref)
					if err != nil {
						return err
					}
					if len(n.Options) == 0 {
						if inner := firstOptioned(ctx.Window, n); inner != nil {
							n = inner
						} else {
							return NewProtocolError("not_text_target", "%s has no options", ref)
						}
					}
					if err := ctx.Window.Type(n.NodeID.String(), label, TypeOptions{ScrollIntoView: true}); err != nil {
						return err
					}
					after, err := firstNode(ctx.Window, n.NodeID.String())
					if err == nil && after.Value != label && !optionSelected(after, label) {
						return NewProtocolError("no_match", "no option labelled %q (options: %s)", label, optionLabels(after))
					}
					return nil
				})
			}).WithStrict(),
		NewProtocolCommand("scroll", "Scroll at a node by wheel notches.",
			func(ctx *ProtocolContext, p agentScrollParams) (AgentActionResult, error) {
				return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error {
					return ctx.Window.Scroll(ref, p.DX, p.DY)
				})
			}).WithStrict(),
		NewProtocolCommand("drag", "Drag from one node to another, or between raw points (x,y → x2,y2).",
			func(ctx *ProtocolContext, p agentDragParams) (AgentActionResult, error) {
				mods, err := ParseModifierNames(p.Modifiers)
				if err != nil {
					return AgentActionResult{}, err
				}
				if p.From == "" && p.To == "" {
					if p.X == nil || p.Y == nil || p.X2 == nil || p.Y2 == nil {
						return AgentActionResult{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: `drag needs "from"/"to" or all of x, y, x2, y2`}
					}
					return agentAct(ctx, "", nil, func(string) error {
						return ctx.Window.DragAt(*p.X, *p.Y, *p.X2, *p.Y2, p.Steps)
					})
				}
				to, err := agentResolve(ctx.ctx(), ctx.Window, p.To, waitTimeout(p.TimeoutMs, agentDefaultWaitMs))
				if err != nil {
					return AgentActionResult{}, fmt.Errorf("drag to: %w", err)
				}
				return agentAct(ctx, p.From, p.TimeoutMs, func(ref string) error {
					return ctx.Window.Drag(ref, to.NodeID.String(), DragOptions{Steps: p.Steps, Modifiers: mods})
				})
			}).WithStrict(),
		NewProtocolCommand("pinch", "A two-finger pinch on a node, ending at the cumulative scale.",
			func(ctx *ProtocolContext, p agentPinchParams) (AgentActionResult, error) {
				return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error {
					return ctx.Window.Pinch(ref, p.Scale)
				})
			}).WithStrict(),
		NewProtocolCommand("query", "Every node a selector matches, in tree order, without waiting or acting. Takes the same selectors as every Agent target (widget grammar, CSS, @ref, comma lists); DOM.querySelectorAll takes CSS only.",
			func(ctx *ProtocolContext, p agentQueryParams) (agentQueryResult, error) {
				out := agentQueryResult{Matches: []agentMatched{}}
				nodes, err := ctx.Window.FindNodesSynced(p.Selector)
				if err != nil {
					return out, err
				}
				for _, n := range nodes {
					out.Matches = append(out.Matches, agentMatched{NodeID: n.NodeID, Role: n.Role, Name: n.Name})
				}
				return out, nil
			}).WithStrict(),
		NewProtocolCommand("waitFor", "Block until a selector is visible / hidden (matches nothing visible) / attached (matches anything) / detached (matches nothing), confirmed on two consecutive frames.",
			func(ctx *ProtocolContext, p agentWaitForParams) (agentWaitResult, error) {
				err := agentWaitSelector(ctx.ctx(), ctx.Window, p.Selector, p.State, waitTimeout(p.TimeoutMs, 2000))
				return agentWaitDone(ctx.Window, err)
			}).WithStrict(),
		NewProtocolCommand("waitForReady", "Block until the window has loaded (laid out and painted once: readyState complete) and gone idle. Run it first after starting an app.",
			func(ctx *ProtocolContext, p agentWaitParams) (agentReadyResult, error) {
				w := ctx.Window
				if err := w.WaitReady(waitTimeout(p.TimeoutMs, 5000)); err != nil {
					return agentReadyResult{ReadyState: w.ReadyState()}, err
				}
				done, err := agentWaitDone(w, nil)
				return agentReadyResult{ReadyState: w.ReadyState(), IdleState: done.IdleState, TreeHash: done.TreeHash}, err
			}).WithStrict(),
		NewProtocolCommand("waitForIdle", "Block until no repaint, animation, tickable or job is pending (animators quiet for 0.5s don't count).",
			func(ctx *ProtocolContext, p agentWaitParams) (agentWaitResult, error) {
				return agentWaitDone(ctx.Window, ctx.Window.WaitIdle(waitTimeout(p.TimeoutMs, 2000)))
			}).WithStrict(),
		NewProtocolCommand("waitForStable", "Block until the AX tree hash holds across two samples (rows loaded, layout settled).",
			func(ctx *ProtocolContext, p agentWaitParams) (agentWaitResult, error) {
				return agentWaitDone(ctx.Window, ctx.Window.WaitTreeStable(waitTimeout(p.TimeoutMs, 2000)))
			}).WithStrict(),
		NewProtocolCommand("waitForOverlay", "Block until an overlay whose layer contains pattern appears (visible) or is gone (hidden).",
			func(ctx *ProtocolContext, p agentWaitOverlayParams) (agentWaitResult, error) {
				appear := p.State == "" || p.State == "visible"
				if !appear && p.State != "hidden" {
					return agentWaitResult{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: "state must be visible or hidden"}
				}
				return agentWaitDone(ctx.Window, ctx.Window.WaitOverlay(p.Pattern, appear, waitTimeout(p.TimeoutMs, 2000)))
			}).WithStrict(),
	}
	cmds = append(cmds, NewProtocolCommand("doubleClick", "Double-click a node.",
		func(ctx *ProtocolContext, p agentClickParams) (AgentActionResult, error) {
			p.ClickCount = 2
			return agentClick(ctx, p)
		}).WithStrict())
	return ProtocolDomain{
		Name: "Agent",
		Description: "The high-level surface for AI agents: a compact snapshot with @refs, and actions that take a selector or @ref, auto-wait for the target, " +
			"resolve it once and report what changed. Start here.",
		Commands: cmds,
		Buffered: true,
		Events: []ProtocolEvent{
			NewProtocolEvent[EventRecord]("inputRecorded", "A raw input event was dispatched (mouse, key, scroll, gesture, focus)."),
			NewProtocolEvent[EventRecord]("valueChanged", "A text control's value changed. source: user | program | program-overwrite — the last is a binding writing a stale value over what the user just typed."),
			NewProtocolEvent[EventRecord]("submitted", "Enter / commit on a single-line input."),
		},
	}
}

func firstNode(w *Window, ref string) (*AXNode, error) {
	nodes, err := w.FindNodesSynced(ref)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, noMatchFor(ref)
	}
	return nodes[0], nil
}

// firstOptioned finds the choice widget inside an element (an htmlcss
// <select> wraps its backing Select).
func firstOptioned(w *Window, n *AXNode) *AXNode {
	for _, d := range flattenAX(n) {
		if len(d.Options) > 0 {
			return d
		}
	}
	return nil
}

func optionSelected(n *AXNode, label string) bool {
	for _, o := range n.Options {
		if o.Selected && strings.EqualFold(strings.TrimSpace(o.Label), strings.TrimSpace(label)) {
			return true
		}
	}
	return false
}

func optionLabels(n *AXNode) string {
	var ls []string
	for _, o := range n.Options {
		ls = append(ls, o.Label)
	}
	return strings.Join(ls, ", ")
}

// agentWaitSelector polls a selector until its state holds on two
// consecutive samples.
func agentWaitSelector(ctx context.Context, w *Window, selector, state string, timeout time.Duration) error {
	if state == "" {
		state = "visible"
	}
	check := func(nodes []*AXNode) bool {
		switch state {
		case "visible", "hidden":
			vis := false
			for _, n := range nodes {
				if n.Visible {
					vis = true
					break
				}
			}
			return vis == (state == "visible")
		case "attached":
			return len(nodes) > 0
		default: // detached
			return len(nodes) == 0
		}
	}
	switch state {
	case "visible", "hidden", "attached", "detached":
	default:
		return &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: "state must be visible, hidden, attached or detached"}
	}
	deadline := time.Now().Add(timeout)
	streak := 0
	for {
		nodes, err := w.FindNodesSynced(selector)
		if err != nil {
			return err
		}
		if check(nodes) {
			if streak++; streak >= 2 {
				return nil
			}
		} else {
			streak = 0
		}
		if !time.Now().Before(deadline) {
			return ErrWaitTimeout
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(16 * time.Millisecond):
		}
	}
}

func agentClick(ctx *ProtocolContext, p agentClickParams) (AgentActionResult, error) {
	mods, err := ParseModifierNames(p.Modifiers)
	if err != nil {
		return AgentActionResult{}, err
	}
	count := max(p.ClickCount, 1)
	button := cdpButton(p.Button)
	if p.Target == "" {
		if p.X == nil || p.Y == nil {
			return AgentActionResult{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: `click needs "target" or both "x" and "y"`}
		}
		return agentAct(ctx, "", nil, func(string) error {
			return ctx.Window.synchronously(func() error {
				ctx.Window.dispatchClicks(Point{X: *p.X, Y: *p.Y}, button, count, mods)
				return nil
			})
		})
	}
	siv := p.ScrollIntoView == nil || *p.ScrollIntoView
	return agentAct(ctx, p.Target, p.TimeoutMs, func(ref string) error {
		opts := ClickOptions{Modifiers: mods, ScrollIntoView: siv}
		return ctx.Window.synchronously(func() error { return ctx.Window.runClick(ref, button, count, opts) })
	})
}
