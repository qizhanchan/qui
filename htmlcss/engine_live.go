package htmlcss

import (
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// StyleEngine holds a parsed stylesheet and drives on-demand restyling of
// a live *El tree. It is the bridge that lets the reactive layer
// (reactive/html) run "web-like React" over the html-css engine: reactive
// mutates the El tree (class / attr / text / children); the engine
// recomputes CSS and re-applies it in place.
//
// Threading: qui runs on a single UI goroutine, so restyle scheduling uses
// the window's main-thread job queue (like reactive's signal binding).
//
// Restyle is SUBTREE-SCOPED: each mutation records its element, and the
// coalesced flush recomputes only from each dirty element's parent down.
// Without :has(), the parent is the complete impact scope of any
// single-element change: a node's match depends only on itself, its
// ancestors, and its PRECEDING siblings — so a change to X can affect X,
// X's descendants (descendant/child combinators + inheritance), and X's
// following siblings' subtrees ("X ~ Y", "X + Y"), all of which live under
// X's parent. A stylesheet containing :has() promotes a dirty element to its
// top-level root because a descendant mutation can change any ancestor.
type StyleEngine struct {
	// linkHandler, when set, decides what a link click does (events.go).
	linkHandler LinkHandler
	// tooltipStyled: the stylesheet set the window's tooltip colors.
	tooltipStyled bool

	sheet     *Stylesheet
	viewportW float32      // @media evaluation width (0 = default), kept for SetCSS
	opts      Options      // BaseDir (img resolution) + Window (select backing)
	root      *El          // main-tree root (holds the window for invalidation)
	roots     map[*El]bool // every top-level element: main root + portal/dialog roots
	dirty     map[*El]bool // elements mutated since the last flush
	pending   bool

	// styled counts applyComputed calls across the engine's lifetime.
	// Test-only observability for asserting restyle scope; no runtime use.
	styled int
	// pass counts restyle walks; El.styledPass records the last one that
	// reached an element.
	pass int

	// stateDeps maps a TRIGGER element to the elements whose ancestor-state
	// styling (`.row:hover .del`, `.a:active ~ .b`) its interactive state
	// drives. The trigger's hover / press boundaries invalidate the
	// dependents (El.notifyStateDeps) — necessary because a sibling
	// dependent lies outside the trigger's own paint rect. Maintained from
	// the dependent side by El.linkAncestorState / Unmount.
	stateDeps map[*El]map[*El]bool
	// focusDeps are the dependents with a focus-kind trigger. Focus can move
	// without any event reaching the trigger (an inner <input> gains focus),
	// so these are invalidated from a window focus-change listener instead.
	focusDeps   map[*El]bool
	focusHooked bool

	// unsubscribeLocale drops the engine's language-change hook when the
	// tree unmounts. A locale switch changes what every data-i18n
	// element renders, so unlike a class change it has no useful scope —
	// it triggers the full-document pass.
	unsubscribeLocale func()

	// radioGroups coordinates <input type=radio> single-selection: all
	// radios sharing a `name` attribute join one RadioGroup, so selecting
	// one clears its peers even when they were built by separate elements.
	radioGroups map[string]*radioGroupState
}

// radioGroupState is one named radio group plus its member elements, so the
// group's OnChange can push the new selection back onto each member (clears
// e.checked + fires onToggle).
type radioGroupState struct {
	group   *widgets.RadioGroup
	members []*El
}

// radioGroup returns the RadioGroup for the given name, creating it (and
// its selection dispatcher) on first use, and registers member as belonging
// to it. An empty name groups all unnamed radios together (author error,
// but keeps single-selection sane).
func (eng *StyleEngine) radioGroup(name string, member *El) *widgets.RadioGroup {
	if eng.radioGroups == nil {
		eng.radioGroups = map[string]*radioGroupState{}
	}
	st := eng.radioGroups[name]
	if st == nil {
		st = &radioGroupState{group: widgets.NewRadioGroup()}
		st.group.OnChange = func(sel *widgets.RadioButton) {
			for _, m := range st.members {
				rb, _ := m.backing.(*widgets.RadioButton)
				on := rb != nil && rb == sel
				// Sync the checked flag + `checked` attribute and relink
				// :checked (scoped restyle) for every member; the selected
				// one gains it, the rest lose it.
				m.setCheckedState(on)
				if m.onToggle != nil {
					m.onToggle(on)
				}
			}
		}
		eng.radioGroups[name] = st
	}
	for _, m := range st.members {
		if m == member {
			return st.group
		}
	}
	st.members = append(st.members, member)
	return st.group
}

// NewStyleEngine parses css once into a reusable engine (default viewport).
func NewStyleEngine(css string) *StyleEngine {
	return &StyleEngine{sheet: ParseCSS(css), roots: map[*El]bool{}}
}

// NewStyleEngineViewport parses css evaluating @media against viewportW.
func NewStyleEngineViewport(css string, viewportW float32) *StyleEngine {
	return &StyleEngine{sheet: ParseCSSViewport(css, viewportW), viewportW: viewportW, roots: map[*El]bool{}}
}

// SetCSS replaces the engine's stylesheet (re-parsed with the same @media
// viewport the engine was built with) and recascades every mounted root.
// This is the runtime theming hook: regenerate a stylesheet — e.g. a new
// palette's :root variables — and swap it in place without remounting
// the element tree.
func (eng *StyleEngine) SetCSS(css string) {
	if eng.viewportW > 0 {
		eng.sheet = ParseCSSViewport(css, eng.viewportW)
	} else {
		eng.sheet = ParseCSS(css)
	}
	eng.Restyle()
}

// register marks e as a restyle root (called at construction). An element
// stops being a root once a parent adopts it via SetElementChildren.
func (eng *StyleEngine) register(e *El) {
	if eng.roots == nil {
		eng.roots = map[*El]bool{}
	}
	eng.roots[e] = true
}

func (eng *StyleEngine) unregister(e *El) { delete(eng.roots, e) }

// addStateDep records that dep's ancestor-state styling is driven by
// trigger. focus marks a focus-kind dependency, which additionally hooks
// the window's focus-change listener (installed lazily — the window may
// not exist yet during the first pre-attach restyle; a later relink
// retries).
func (eng *StyleEngine) addStateDep(trigger, dep *El, focus bool) {
	if eng.stateDeps == nil {
		eng.stateDeps = map[*El]map[*El]bool{}
	}
	set := eng.stateDeps[trigger]
	if set == nil {
		set = map[*El]bool{}
		eng.stateDeps[trigger] = set
	}
	set[dep] = true
	if focus {
		if eng.focusDeps == nil {
			eng.focusDeps = map[*El]bool{}
		}
		eng.focusDeps[dep] = true
		eng.ensureFocusHook()
	}
}

// removeStateDep detaches dep from every trigger it registered with (the
// dependent side keeps the authoritative trigger list in dep.stateTriggers).
func (eng *StyleEngine) removeStateDep(dep *El) {
	for _, t := range dep.stateTriggers {
		if set := eng.stateDeps[t.el]; set != nil {
			delete(set, dep)
			if len(set) == 0 {
				delete(eng.stateDeps, t.el)
			}
		}
	}
	if eng.focusDeps != nil {
		delete(eng.focusDeps, dep)
	}
}

// ensureFocusHook subscribes once to the window's focus changes and
// invalidates every focus-kind dependent on each change — focus moves
// carry no event on the trigger element itself, so this is the only
// boundary an ancestor-:focus reveal can repaint on.
func (eng *StyleEngine) ensureFocusHook() {
	if eng.focusHooked {
		return
	}
	win := eng.opts.Window
	if win == nil && eng.root != nil {
		win = eng.root.Window()
	}
	if win == nil {
		return // pre-attach; a later addStateDep retries
	}
	eng.focusHooked = true
	win.AddFocusChangeListener(func() {
		for dep := range eng.focusDeps {
			dep.Invalidate()
		}
	})
}

// NewEl creates a live element bound to this engine. The reactive host's
// Create hook calls this.
func (eng *StyleEngine) NewEl(tag string) *El { return newEl(tag, eng) }

// NewTextEl creates an anonymous text-segment element (a DOM text node)
// bound to this engine. The reactive host uses it for text that sits
// BETWEEN element children — `<p>Hello <b>world</b></p>` — where the text
// cannot live in the parent's own text content.
func (eng *StyleEngine) NewTextEl(text string) *El { return newTextEl(text, eng) }

// SetRoot records the tree root the engine restyles from, and hooks the
// engine to language changes.
//
// A locale switch is the one mutation with no meaningful scope: every
// data-i18n element in the document resolves to different text, and
// those strings measure differently, so this runs the full pass rather
// than the usual parent-scoped one. Language switches are user-initiated
// and rare — paying for a whole-document restyle is the right trade
// against tracking which elements carry a message key.
func (eng *StyleEngine) SetRoot(root *El) {
	eng.root = root
	if eng.unsubscribeLocale == nil {
		eng.unsubscribeLocale = qui.SubscribeLocale(func() { eng.Restyle() })
	}
}

// Close releases the engine's global subscriptions. Multi-window apps
// must call it when a window's tree goes away, or its style engine stays
// reachable from the locale subscriber list forever.
func (eng *StyleEngine) Close() {
	if eng.unsubscribeLocale != nil {
		eng.unsubscribeLocale()
		eng.unsubscribeLocale = nil
	}
}

// Root returns the current root element (nil before mount).
func (eng *StyleEngine) Root() *El { return eng.root }

// markDirty records a mutated element and coalesces a scoped restyle onto
// the next frame via the window's job queue, or runs it inline when there
// is no live window (initial mount before attach, or test mode).
func (eng *StyleEngine) markDirty(e *El) {
	if eng.root == nil {
		return // pre-mount: h.Mount does an explicit Restyle after attach
	}
	if eng.dirty == nil {
		eng.dirty = map[*El]bool{}
	}
	eng.dirty[e] = true
	win := eng.root.Window()
	if win == nil || win.IsHeadless() {
		eng.flush()
		return
	}
	if eng.pending {
		return
	}
	eng.pending = true
	win.PostJob(func() {
		eng.pending = false
		eng.flush()
	})
}

// FlushPendingStyles applies any coalesced restyle synchronously, now.
//
// markDirty normally defers the flush to the next frame's job queue, which
// batches a burst of mutations into one restyle. But when the reactive layer
// mounts a fresh subtree (a filtered list growing back, a dialog opening) it
// creates brand-new Els whose deferred restyle would land a frame AFTER the
// structural change invalidates the window — so the new elements paint one
// frame unstyled (a visible flash). The reactive runtime calls this at the
// end of a reconcile / bound-sync pass, before invalidating, so those
// elements are styled in the same job they were created. No-op when nothing
// is dirty.
func (eng *StyleEngine) FlushPendingStyles() {
	if eng == nil || len(eng.dirty) == 0 {
		return
	}
	// A pending PostJob may still be queued; drop the flag so it becomes a
	// harmless no-op (dirty is cleared by flush) and future mutations can
	// reschedule.
	eng.pending = false
	eng.flush()
}

// flush restyles the accumulated dirty burst, scoped: each dirty element
// contributes its parent (or itself, for a top-level root) as a restyle
// scope; scopes nested inside another scope are pruned; each surviving
// scope is walked with its parent's cached ComputedStyle so inheritance
// is seamless. Falls back to a full Restyle when a scope's inherited
// style isn't known yet (first style pass still pending).
func (eng *StyleEngine) flush() {
	dirty := eng.dirty
	eng.dirty = nil
	if len(dirty) == 0 {
		return
	}
	scopes := map[*El]bool{}
	for e := range dirty {
		if s := e.restyleScope(); s != nil {
			scopes[s] = true
		}
		// nil scope: detached element — styled when (re)adopted.
	}
	// Prune scopes nested inside another scope, so a burst that builds a
	// whole new subtree (every new element dirty) collapses to the one
	// scope around the adoption point.
	for s := range scopes {
		for p := s.inheritParent(); p != nil; p = p.inheritParent() {
			if scopes[p] {
				delete(scopes, s)
				break
			}
		}
	}
	for s := range scopes {
		if p := s.inheritParent(); p != nil && p.lastCS == nil {
			// The scope's inheritance source hasn't been computed yet —
			// only before the first full pass reaches it. Play safe.
			eng.Restyle()
			return
		}
	}
	eng.pass++
	for s := range scopes {
		var parentCS *ComputedStyle
		if p := s.inheritParent(); p != nil {
			parentCS = p.lastCS
		}
		eng.restyleWalk(s, parentCS)
	}
}

// restyleWalk recomputes and re-applies CSS for e's subtree, top-down so
// inheritance sees resolved parent values. Reuses computeNode — the exact
// cascade + selector matching + inheritance the one-shot Render path uses
// — against each element's backing *Node mirror.
func (eng *StyleEngine) restyleWalk(e *El, parent *ComputedStyle) {
	var cs *ComputedStyle
	if e.isTextSeg() {
		cs = parent // text segments match no selectors; pure inheritance
	} else {
		cs = computeNode(e.node, eng.sheet, parent)
	}
	eng.styled++
	e.styledPass = eng.pass
	e.applyComputed(cs)
	for _, kid := range e.elementKids {
		if ke, ok := kid.(*El); ok {
			eng.restyleWalk(ke, cs)
		}
	}
	// Portal content declared under this element inherits from it.
	for k := range e.styleKids {
		eng.restyleWalk(k, cs)
	}
}

// Restyle recomputes and re-applies CSS for EVERY element in the tree.
// Every top-level element is its own restyle root: the main body plus any
// portal/dialog subtree mounted into the overlay stack (which the body
// walk can't reach). A portal root linked with SetStyleParent is styled
// by its style parent's walk, inheriting from it; any other root starts
// a fresh inheritance chain (nil parent).
//
// h.Mount calls this once after attach; incremental updates go through
// the scoped flush instead.
func (eng *StyleEngine) Restyle() {
	eng.dirty = nil // a full pass satisfies any pending scoped work
	eng.pass++
	for r := range eng.roots {
		if r.styleParent == nil {
			eng.restyleWalk(r, nil)
		}
	}
	// A style-linked root whose parent no walk reached (its declaring
	// subtree detached) still gets styled, from the parent's last style.
	for r := range eng.roots {
		if r.styleParent != nil && r.styledPass != eng.pass {
			eng.restyleWalk(r, r.styleParent.lastCS)
		}
	}
	if eng.root != nil {
		if win := eng.root.Window(); win != nil {
			win.InvalidateLayout()
			win.Invalidate()
		}
	}
}
