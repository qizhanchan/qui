package htmlcss

import (
	"reflect"

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
// Restyle is SUBTREE-SCOPED: each mutation records its element and how
// far it can reach, and the coalesced flush recomputes only those scopes.
// A node's match depends only on itself, its ancestors and its PRECEDING
// siblings, so a change to X can affect X, X's descendants (descendant /
// child combinators + inheritance) and — only through "X + Y" / "X ~ Y" —
// its following siblings. Hence:
//
//   - an attribute / text / state change restyles X's own subtree, or X's
//     parent's when a structural rule (sibling combinator, :first-child,
//     :nth-*, :empty …) concerns X or its parent — "concerns" is keyed by
//     the classes / ids / tags those rules name, so one :first-child rule
//     for a tab strip does not widen every list's restyle;
//   - a children-list change styles only the children never styled under
//     that parent (removals and reorders change nothing), or the parent's
//     subtree when a structural rule concerns the parent or a child. Either
//     way the parent's inline-flow arrangement is rebuilt (reflow) without
//     recomputing its style;
//   - a stylesheet containing :has() promotes a dirty element to its
//     top-level root, because a descendant mutation can change any ancestor.
type StyleEngine struct {
	lazyImages map[*El]bool          // script-enabled loading=lazy images awaiting a nearby viewport
	animEvents AnimationEventHandler // CSS animation / transition events (anim.go)
	// linkHandler, when set, decides what a link click does (events.go).
	linkHandler LinkHandler
	// tooltipStyled: the stylesheet set the window's tooltip colors.
	tooltipStyled bool

	distributionDirty bool
	slotChange        func(*El)
	allowDeclarative  bool
	shadowRoots       []*El
	shadowSheetDirty  bool
	sheet             *Stylesheet
	// scoped is set while flush restyles dirty scopes (vs. a full Restyle).
	scoped    bool
	viewportW float32 // @media evaluation width (0 = default), kept for SetCSS
	viewportH float32
	cssSrc    string            // the stylesheet source, kept for SetViewport
	opts      Options           // BaseDir (img resolution) + Window (select backing)
	root      *El               // main-tree root (holds the window for invalidation)
	roots     map[*El]bool      // every top-level element: main root + portal/dialog roots
	dirty     map[*El]dirtyKind // elements mutated since the last flush
	// reflow holds elements whose own style stands but whose arranged
	// children (inline flow runs) must be rebuilt: their children list
	// changed, or an inline child folded into their runs did.
	reflow  map[*El]bool
	pending bool
	// mustApply, during a scoped flush, holds the elements that were
	// dirtied or need a reflow: they re-apply their style even when it
	// computes the same (restyleWalk).
	mustApply map[*El]bool

	// edit is the document's editing state (contenteditable; edit.go).
	edit *editState

	// styled counts the elements restyle walks reached, applied those
	// that re-applied their style (applyComputed), across the engine's
	// lifetime. Test-only observability for asserting restyle scope; no
	// runtime use.
	styled, applied int
	// pass counts restyle walks; El.styledPass records the last one that
	// reached an element.
	pass int
	// memo caches computeNode results within one style pass: a parent's
	// buildFlow styles its children (childStyle) before the walk reaches
	// them with the very same parent style. Reset whenever pass changes.
	memo     map[cascadeMemoKey]*ComputedStyle
	memoPass int

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
	unhookFocus func() // removes the focus-change listener (Close)

	// unsubscribeLocale drops the engine's language-change hook when the
	// tree unmounts. A locale switch changes what every data-i18n
	// element renders, so unlike a class change it has no useful scope —
	// it triggers the full-document pass.
	unsubscribeLocale func()

	// radioGroups coordinates <input type=radio> single-selection: all
	// radios sharing a `name` attribute join one RadioGroup, so selecting
	// one clears its peers even when they were built by separate elements.
	radioGroups    map[string]*radioGroupState
	suppressToggle int // programmatic checked assignments do not activate controls
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
				changed := m.checked != on
				// Sync the checked flag + `checked` attribute and relink
				// :checked (scoped restyle) for every member; the selected
				// one gains it, the rest lose it.
				m.setCheckedState(on)
				if changed && on && eng.suppressToggle == 0 && m.onToggle != nil {
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
	return &StyleEngine{sheet: ParseCSS(css), viewportH: viewportUnits[1], cssSrc: css, roots: map[*El]bool{}}
}

// NewStyleEngineViewport parses css evaluating @media against viewportW.
func NewStyleEngineViewport(css string, viewportW float32) *StyleEngine {
	previous := viewportUnits
	defer func() { viewportUnits = previous }()
	SetViewportUnits(viewportW, 0)
	return &StyleEngine{sheet: ParseCSSViewport(css, viewportW), viewportW: viewportW, viewportH: viewportUnits[1], cssSrc: css, roots: map[*El]bool{}}
}

// useViewport scopes declaration parsing to this document. Subframes can
// restyle between parent jobs without changing the parent's vw/vh units.
func (eng *StyleEngine) useViewport() func() {
	previous := viewportUnits
	SetViewportUnits(eng.viewportW, eng.viewportH)
	return func() { viewportUnits = previous }
}

// CSS returns the stylesheet source the engine was built with (or last
// given to SetCSS).
func (eng *StyleEngine) CSS() string { return eng.cssSrc }

// SetViewport re-evaluates the stylesheet's @media queries and the
// viewport units for a new viewport size and restyles every mounted root
// in place — a browser window resized without rebuilding its document.
func (eng *StyleEngine) SetViewport(w, h float32) {
	changed := w != eng.viewportW || h > 0 && h != eng.viewportH
	eng.viewportW = w
	if h > 0 {
		eng.viewportH = h
	}
	defer eng.useViewport()()
	if !changed {
		eng.Restyle()
		return
	}
	eng.viewportW = w
	eng.sheet = ParseCSSViewport(eng.cssSrc, w)
	eng.sheet.FontFamilies = eng.opts.FontFamilies
	eng.shadowSheetDirty = true
	eng.Restyle()
}

// SetCSS replaces the engine's stylesheet (re-parsed with the same @media
// viewport the engine was built with) and recascades every mounted root.
// This is the runtime theming hook: regenerate a stylesheet — e.g. a new
// palette's :root variables — and swap it in place without remounting
// the element tree.
func (eng *StyleEngine) SetCSS(css string) {
	defer eng.useViewport()()
	eng.cssSrc = css
	if eng.viewportW > 0 {
		eng.sheet = ParseCSSViewport(css, eng.viewportW)
	} else {
		eng.sheet = ParseCSS(css)
	}
	eng.sheet.FontFamilies = eng.opts.FontFamilies
	eng.shadowSheetDirty = true
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
	eng.unhookFocus = win.AddFocusChangeListener(func() {
		for dep := range eng.focusDeps {
			dep.Invalidate()
			if dep.stateLayout {
				eng.markDirtyKind(dep, dirtySelf)
			}
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

// AdoptRoot makes root the engine's tree root and styles it, if the engine
// has no root yet and root is an *El; otherwise it does nothing. The
// reactive runtime calls it in the pass that first produces the tree,
// before that pass's effects run, so h.Mount's initial effects already see
// computed styles and the widgets custom elements host.
func (eng *StyleEngine) AdoptRoot(root qui.Widget) {
	if eng == nil || eng.root != nil {
		return
	}
	el, ok := root.(*El)
	if !ok {
		return
	}
	eng.SetRoot(el)
	eng.dirty = nil
	eng.pending = false
	eng.sheet.FontFamilies = eng.opts.FontFamilies
	eng.shadowSheetDirty = true
	eng.Restyle()
}

// Close releases the engine's global subscriptions — the locale
// subscriber list and its window's focus listeners. Multi-window apps and
// browsers (a tab navigating away) must call it when a tree goes away, or
// its style engine, and every element it styled, stays reachable forever.
func (eng *StyleEngine) Close() {
	if eng.unsubscribeLocale != nil {
		eng.unsubscribeLocale()
		eng.unsubscribeLocale = nil
	}
	if eng.unhookFocus != nil {
		eng.unhookFocus()
		eng.unhookFocus = nil
	}
	eng.focusDeps = nil
}

// Root returns the current root element (nil before mount).
func (eng *StyleEngine) Root() *El { return eng.root }

// dirtyKind is how far a mutation can reach.
type dirtyKind uint8

const (
	// dirtyStyleAttr: only the element's style attribute changed. No
	// selector reads it (unless the sheet has a [style] clause — then the
	// change is dirtySelf), so the element alone is restyled, and its
	// descendants only when it changed something they inherit.
	dirtyStyleAttr dirtyKind = iota
	// dirtySelf: the element's own subtree (its attributes or text changed
	// and nothing in the sheet lets that affect a sibling or an ancestor).
	dirtySelf
	// dirtyParent: the parent's subtree — the element and every sibling.
	dirtyParent
)

// sheetFlags reports which reach-widening features the author and
// framework sheets use (see Stylesheet).
func (eng *StyleEngine) sheetFlags() (has, siblingComb, structural bool) {
	if eng.sheet != nil {
		has, siblingComb, structural = eng.sheet.HasHas, eng.sheet.HasSiblingCombinator, eng.sheet.HasStructural
	}
	frameworkCSS.mu.RLock()
	has = has || frameworkCSS.has
	siblingComb = siblingComb || frameworkCSS.siblingComb
	structural = structural || frameworkCSS.structural
	frameworkCSS.mu.RUnlock()
	return
}

// hasHasRule reports whether the author sheet or a framework sheet uses
// :has(), which makes a mutation able to restyle any ancestor.
func (eng *StyleEngine) hasHasRule() bool {
	has, _, _ := eng.sheetFlags()
	return has
}

// structRelevant reports whether a structural or sibling-combinator rule
// (author or framework) can concern n.
func (eng *StyleEngine) structRelevant(n *Node) bool {
	if eng.sheet.StructuralRelevant(n) {
		return true
	}
	frameworkCSS.mu.RLock()
	defer frameworkCSS.mu.RUnlock()
	return frameworkCSS.structural && frameworkCSS.structKeys.relevant(n)
}

// markDirty records an element whose attributes, state or text changed.
// Its own subtree is restyled — or its parent's, when a structural rule
// concerns it (now or at its last style: a sibling combinator lets its
// attributes reach the following siblings) or its parent (:empty).
func (eng *StyleEngine) markDirty(e *El) {
	// A child folded into its parent's inline runs shows in the parent:
	// rebuild those after restyling the child. Record the reflow before
	// marking the child dirty, since without a window the mark flushes at
	// once. The structural case needs it too: a scoped walk from the parent
	// skips re-applying a parent whose own style is unchanged unless it is
	// marked for reflow, so its run would keep the child's old text.
	if p := e.elParent; p != nil && (p.flowKids != nil || e.isTextSeg()) {
		eng.addReflow(p)
	}
	eng.reflowRunOwner(e.elParent)
	if _, _, structural := eng.sheetFlags(); structural &&
		(e.structRel || eng.structRelevant(e.node) || e.elParent != nil && eng.structRelevant(e.elParent.node)) {
		eng.markDirtyKind(e, dirtyParent)
		return
	}
	eng.markDirtyKind(e, dirtySelf)
}

// reflowRunOwner records for a rebuild the element whose inline run shows
// e's content when e is folded into an ancestor's run (a <b> inside a
// paragraph): e's own arrangement is not where its text renders.
func (eng *StyleEngine) reflowRunOwner(e *El) {
	for p := e; p != nil && p.folded; p = p.elParent {
		if p.elParent != nil {
			eng.addReflow(p.elParent)
		}
	}
}

// markStyleAttr records an element whose style attribute changed (see
// dirtyStyleAttr): a resize through it (a dragged panel border) restyles
// that element, not everything inside it.
func (eng *StyleEngine) markStyleAttr(e *El) {
	if eng.mentionsAttr("style") {
		eng.markDirty(e)
		return
	}
	if p := e.elParent; p != nil && p.flowKids != nil {
		eng.addReflow(p)
	}
	eng.markDirtyKind(e, dirtyStyleAttr)
}

// mentionsAttr reports whether any author or framework selector has an
// attribute clause on name.
func (eng *StyleEngine) mentionsAttr(name string) bool {
	if eng.sheet != nil && rulesMentionAttr(eng.sheet.Rules, name) {
		return true
	}
	frameworkCSS.mu.RLock()
	defer frameworkCSS.mu.RUnlock()
	return rulesMentionAttr(frameworkCSS.rules, name)
}

func rulesMentionAttr(rules []Rule, name string) bool {
	for _, r := range rules {
		if selectorsMentionAttr(r.Selectors, name) {
			return true
		}
	}
	return false
}

func selectorsMentionAttr(sels []Selector, name string) bool {
	for _, sel := range sels {
		for _, c := range sel.parts {
			for _, a := range c.sel.attrs {
				if a.name == name {
					return true
				}
			}
			for _, p := range c.sel.pseudos {
				if selectorsMentionAttr(p.args, name) {
					return true
				}
			}
		}
	}
	return false
}

// markReflow records e for an arrangement rebuild (see reflow) and
// schedules a flush.
func (eng *StyleEngine) markReflow(e *El) {
	if eng.root == nil {
		return
	}
	eng.addReflow(e)
	eng.schedule()
}

// addReflow records e for an arrangement rebuild without scheduling.
func (eng *StyleEngine) addReflow(e *El) {
	if eng.reflow == nil {
		eng.reflow = map[*El]bool{}
	}
	eng.reflow[e] = true
}

// childrenChanged records that e's children list was replaced. Without
// structural selectors only the children that were never styled under e
// need a style (removals and reorders change nothing); with them, e's
// subtree is restyled (positions changed for every child).
func (eng *StyleEngine) childrenChanged(e *El) {
	eng.distributionDirty = true
	if e.tag == "slot" && len(e.assigned) == 0 && shadowScope(e.node) != nil && eng.slotChange != nil {
		eng.slotChange(e)
	}
	eng.reflowRunOwner(e)
	has, _, structural := eng.sheetFlags()
	if structural {
		// Only matters when a structural rule concerns e or a child.
		structural = eng.structRelevant(e.node)
		for _, k := range e.elementKids {
			if structural {
				break
			}
			if ke, ok := k.(*El); ok {
				structural = ke.structRel || eng.structRelevant(ke.node)
			}
		}
	}
	switch {
	case has:
		eng.markDirtyKind(e, dirtyParent)
	case structural:
		eng.markDirtyKind(e, dirtySelf)
	default:
		// e's own style stands; its arrangement of the new list doesn't
		// (recorded first: without a window each mark flushes at once).
		eng.addReflow(e)
		for _, k := range e.elementKids {
			if ke, ok := k.(*El); ok && (ke.lastCS == nil || ke.styledIn != e) {
				eng.markDirtyKind(ke, dirtySelf)
			}
		}
		eng.markReflow(e)
	}
}

// markDirtyKind records e with reach k and coalesces a scoped restyle onto
// the next frame via the window's job queue, or runs it inline when there
// is no live window (initial mount before attach, or test mode).
func (eng *StyleEngine) markDirtyKind(e *El, k dirtyKind) {
	if eng.root == nil {
		return // pre-mount: h.Mount does an explicit Restyle after attach
	}
	if eng.dirty == nil {
		eng.dirty = map[*El]dirtyKind{}
	}
	if old, ok := eng.dirty[e]; !ok || k > old {
		eng.dirty[e] = k
	}
	eng.schedule()
}

// schedule coalesces a flush onto the next frame via the window's job
// queue, or runs it inline when there is no live window (initial mount
// before attach, or test mode).
func (eng *StyleEngine) schedule() {
	win := eng.root.Window()
	if win == nil || win.IsHeadless() {
		eng.flush()
		return
	}
	if eng.pending {
		return
	}
	eng.pending = true
	run := func() {
		if !eng.pending {
			return // the other path ran it
		}
		eng.pending = false
		eng.flush()
	}
	// Before this frame's layout, so mutations made by a job (a script
	// timer) are styled in the frame they happen in; the job is the
	// fallback for windows that are not stepping (tests drain jobs).
	win.BeforeLayout(run)
	win.PostJob(run)
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
	if eng == nil || len(eng.dirty) == 0 && len(eng.reflow) == 0 {
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
	if len(eng.shadowRoots) > 0 {
		eng.Restyle()
		return
	}
	defer eng.useViewport()()
	dirty := eng.dirty
	eng.dirty = nil
	reflow := eng.reflow
	eng.reflow = nil
	startPass := eng.pass
	// After the walks: rebuild the arrangement of elements whose style
	// stands (their new or changed children are styled by now). An element
	// a walk of this flush restyled has been rebuilt already.
	defer func() {
		for e := range reflow {
			if e.lastCS != nil && e.styledPass <= startPass && e.engine == eng {
				e.applyComputed(e.lastCS)
				e.invalidateStyledLayout()
			}
		}
	}()
	if len(dirty) == 0 {
		return
	}
	scopes := map[*El]bool{}
	// attrOnly are scopes reached only by a style-attribute change: the
	// element is restyled, its subtree only as far as it changed what the
	// subtree inherits (restyleAttrScope).
	attrOnly := map[*El]bool{}
	for e, k := range dirty {
		s := e.restyleScope()
		switch k {
		case dirtySelf:
			s = e.selfScope()
		case dirtyStyleAttr:
			s = e.selfScope()
			if s == e {
				attrOnly[e] = true
			}
		}
		if s != nil {
			scopes[s] = true
		}
		// nil scope: detached element — styled when (re)adopted.
	}
	for s := range attrOnly {
		if k, ok := dirty[s]; ok && k != dirtyStyleAttr {
			delete(attrOnly, s)
		}
	}
	// Prune scopes nested inside another full scope, so a burst that
	// builds a whole new subtree (every new element dirty) collapses to the
	// one scope around the adoption point. An attribute-only scope may not
	// reach its subtree, so it prunes nothing.
	for s := range scopes {
		for p := s.inheritParent(); p != nil; p = p.inheritParent() {
			if scopes[p] && !attrOnly[p] {
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
	eng.scoped = true
	eng.mustApply = make(map[*El]bool, len(dirty)+len(reflow))
	for e := range dirty {
		eng.mustApply[e] = true
	}
	for e := range reflow {
		eng.mustApply[e] = true
	}
	defer func() { eng.scoped, eng.mustApply = false, nil }()
	for s := range scopes {
		var parentCS *ComputedStyle
		if p := s.inheritParent(); p != nil {
			parentCS = p.lastCS
		}
		if attrOnly[s] {
			eng.restyleAttrScope(s, parentCS)
			continue
		}
		eng.restyleWalk(s, parentCS)
	}
}

type cascadeMemoKey struct {
	n      *Node
	parent *ComputedStyle
}

// computeMemo is computeNode through the per-pass memo.
func (eng *StyleEngine) computeMemo(n *Node, parent *ComputedStyle) *ComputedStyle {
	if eng.memoPass != eng.pass || eng.memo == nil {
		eng.memo = map[cascadeMemoKey]*ComputedStyle{}
		eng.memoPass = eng.pass
	}
	k := cascadeMemoKey{n, parent}
	if cs, ok := eng.memo[k]; ok {
		return cs
	}
	cs := computeNode(n, eng.sheet, parent)
	eng.memo[k] = cs
	return cs
}

// restyleAttrScope restyles e after a change to its style attribute alone,
// and walks its subtree only when the change reaches what the subtree
// inherits: resizing an element leaves everything inside it as it was.
func (eng *StyleEngine) restyleAttrScope(e *El, parent *ComputedStyle) {
	prev := e.lastCS
	cs := computeNode(e.node, eng.sheet, parent)
	eng.styled++
	e.styledPass = eng.pass
	e.styledIn = e.elParent
	e.structRel = eng.structRelevant(e.node)
	if eng.pointerEventsOnly(e, prev, cs) {
		return
	}
	eng.applied++
	e.applyComputed(cs)
	if prev != nil && prev != cs && !reflect.DeepEqual(layoutView(prev), layoutView(cs)) {
		e.invalidateStyledLayout()
	}
	if prev != nil && reflect.DeepEqual(inheritView(prev), inheritView(cs)) {
		return
	}
	for _, kid := range e.renderKids() {
		if ke, ok := kid.(*El); ok {
			eng.restyleWalk(ke, cs)
		}
	}
	for k := range e.styleKids {
		eng.restyleWalk(k, cs)
	}
}

// inheritView returns cs with the element's own box sizes cleared — the
// properties a style attribute most often changes and no descendant
// inherits or resolves against at style time (percentages resolve at
// layout). Two styles equal under it style the subtree alike.
func inheritView(cs *ComputedStyle) *ComputedStyle {
	if cs == nil {
		return nil
	}
	v := *cs
	// The state variants carry the element's sizes as well.
	v.Hover, v.Focus, v.Active, v.FocusVisible = inheritView(cs.Hover), inheritView(cs.Focus), inheritView(cs.Active), inheritView(cs.FocusVisible)
	v.AncestorHover, v.AncestorFocus, v.AncestorActive = inheritView(cs.AncestorHover), inheritView(cs.AncestorFocus), inheritView(cs.AncestorActive)
	v.Width, v.HasWidth, v.Height, v.HasHeight = 0, false, 0, false
	v.WidthPct, v.HeightPct = 0, 0
	v.MinWidth, v.MinHeight, v.MaxWidth, v.MaxHeight = 0, 0, 0, 0
	v.MinWidthPct, v.MinHeightPct, v.MaxWidthPct, v.MaxHeightPct = 0, 0, 0, 0
	// The merged declarations carry them too (children read the parent's
	// only for inherited extension properties, never these).
	v.raw = make(map[string]string, len(cs.raw))
	for k, val := range cs.raw {
		if !boxSizeProp[k] {
			v.raw[k] = val
		}
	}
	return &v
}

var boxSizeProp = map[string]bool{
	"width": true, "height": true, "min-width": true, "min-height": true,
	"max-width": true, "max-height": true,
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
		cs = eng.computeMemo(e.node, parent)
		// An active trigger of a layout-changing ancestor-state rule (an
		// open hover menu) styles the element through that rule.
		e.stateBase = nil
		if v := e.layoutStateVariant(cs, parent); v != nil {
			e.stateBase, cs = cs, v
		}
	}
	eng.styled++
	e.styledPass = eng.pass
	e.styledIn = e.elParent
	e.structRel = !e.isTextSeg() && eng.structRelevant(e.node)
	prev := e.lastCS
	// A scoped walk reaches the whole subtree of what changed, but an
	// element that is not itself dirty and computes the very style it has
	// keeps its widgets as they are (Chrome restyles only what the change
	// invalidates): re-applying rebuilds its inline flow and drops every
	// cached measurement below it, so resizing a panel through its style
	// re-shaped all the text inside, every frame of the drag.
	if eng.scoped && prev != nil && !eng.mustApply[e] && !e.pseudoRules && e.onStyle == nil && e.canvasUser == nil &&
		reflect.DeepEqual(widgetStyleView(prev), widgetStyleView(cs)) {
		// An inherited custom-property table may change without changing any
		// resolved declaration here. Keep that table for descendants and
		// future mutations, while preserving this element's measured widgets.
		e.lastCS = cs
		for _, kid := range e.renderKids() {
			if ke, ok := kid.(*El); ok {
				eng.restyleWalk(ke, cs)
			}
		}
		for k := range e.styleKids {
			eng.restyleWalk(k, cs)
		}
		return
	}
	if eng.pointerEventsOnly(e, prev, cs) {
		return
	}
	eng.applied++
	e.applyComputed(cs)
	// applyComputed rebuilds widget state straight from cs, partly through
	// plain fields (layout engines, paragraph flags) that don't invalidate
	// on their own. A full Restyle re-measures everything anyway; a scoped
	// one must drop the cached measurements of what it really changed —
	// but only for a change that can move something: a hover color swap
	// stays a repaint.
	if eng.scoped && prev != nil && prev != cs && !reflect.DeepEqual(layoutView(prev), layoutView(cs)) {
		e.invalidateStyledLayout()
	}
	for _, kid := range e.renderKids() {
		if ke, ok := kid.(*El); ok {
			eng.restyleWalk(ke, cs)
		}
	}
	e.refreshSVGStyles()
	// Portal content declared under this element inherits from it.
	for k := range e.styleKids {
		eng.restyleWalk(k, cs)
	}
}

// pointerEventsOnly handles a restyle whose only change is pointer-events
// (sites disable hover while scrolling): it moves and paints nothing, so
// hit testing is retargeted and the widgets are kept, instead of
// rebuilding and repainting the subtree on every scroll step. Reports
// whether it handled e (and walked its subtree). Elements with
// pseudo-element rules qualify too: pointer-events cannot change what
// ::before / ::after generate or how they paint.
func (eng *StyleEngine) pointerEventsOnly(e *El, prev, cs *ComputedStyle) bool {
	if !eng.scoped || prev == nil || prev == cs || e.onStyle != nil ||
		e.canvasUser != nil ||
		(prev.PointerNone == cs.PointerNone && prev.raw["pointer-events"] == cs.raw["pointer-events"]) ||
		!onlyPointerEventsDiffer(prev, cs) {
		return false
	}
	e.lastCS = cs
	applyPointerEvents(e, cs)
	if e.backing != nil {
		applyPointerEvents(e.backing, cs)
	}
	if e.imgWidget != nil {
		applyPointerEvents(e.imgWidget, cs)
	}
	for _, kid := range e.elementKids {
		if ke, ok := kid.(*El); ok {
			eng.inheritPointerEvents(ke, cs)
		}
	}
	for k := range e.styleKids {
		eng.restyleWalk(k, cs)
	}
	return true
}

// inheritPointerEvents updates a descendant of an element whose only change
// was pointer-events. pointer-events is inherited and feeds no selector, so
// the descendant's cascade is what it was with the inherited flag swapped:
// no need to recompute the subtree's styles on every scroll-time toggle.
// Elements that are dirty for other reasons, declare pointer-events
// themselves or were never styled take the full walk.
func (eng *StyleEngine) inheritPointerEvents(e *El, parent *ComputedStyle) {
	prev := e.lastCS
	if prev == nil || eng.mustApply[e] || e.isTextSeg() || len(e.styleKids) > 0 || e.onStyle != nil || e.canvasUser != nil {
		eng.restyleWalk(e, parent)
		return
	}
	if _, own := prev.raw["pointer-events"]; own || prev.PointerNone == parent.PointerNone {
		// Its value, and so its whole subtree's, does not follow the
		// change: nothing below here moves.
		return
	}
	eng.styled++
	e.styledPass = eng.pass
	e.styledIn = e.elParent
	cp := *prev
	cp.PointerNone = parent.PointerNone
	e.lastCS = &cp
	applyPointerEvents(e, &cp)
	if e.backing != nil {
		applyPointerEvents(e.backing, &cp)
	}
	if e.imgWidget != nil {
		applyPointerEvents(e.imgWidget, &cp)
	}
	for _, kid := range e.elementKids {
		if ke, ok := kid.(*El); ok {
			eng.inheritPointerEvents(ke, &cp)
		}
	}
}

// onlyPointerEventsDiffer reports whether a and b differ in nothing but
// pointer-events (computed flag and declaration), ignoring custom-property
// tables like widgetStyleView.
func onlyPointerEventsDiffer(a, b *ComputedStyle) bool {
	va, vb := widgetStyleView(a), widgetStyleView(b)
	stripPointerEvents(va)
	stripPointerEvents(vb)
	return reflect.DeepEqual(va, vb)
}

func stripPointerEvents(v *ComputedStyle) {
	if v == nil {
		return
	}
	v.PointerNone = false
	if _, ok := v.raw["pointer-events"]; ok {
		raw := make(map[string]string, len(v.raw))
		for k, val := range v.raw {
			if k != "pointer-events" {
				raw[k] = val
			}
		}
		v.raw = raw
	}
	for _, s := range []**ComputedStyle{&v.Hover, &v.Focus, &v.Active, &v.FocusVisible, &v.AncestorHover, &v.AncestorFocus, &v.AncestorActive} {
		stripPointerEvents(*s)
	}
}

// Custom properties only affect widget state through the declarations that
// consume them. Pseudo-element owners still reapply to resolve their content.
func widgetStyleView(cs *ComputedStyle) *ComputedStyle {
	if cs == nil {
		return nil
	}
	v := *cs
	v.customProps = nil
	v.Hover, v.Focus, v.Active, v.FocusVisible = widgetStyleView(cs.Hover), widgetStyleView(cs.Focus), widgetStyleView(cs.Active), widgetStyleView(cs.FocusVisible)
	v.AncestorHover, v.AncestorFocus, v.AncestorActive = widgetStyleView(cs.AncestorHover), widgetStyleView(cs.AncestorFocus), widgetStyleView(cs.AncestorActive)
	return &v
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
	defer eng.useViewport()()
	eng.prepareShadowTrees()
	eng.dirty = nil // a full pass satisfies any pending scoped work
	eng.reflow = nil
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

// invalidateStyledLayout drops the cached layout of an element whose
// computed style changed, and of the internal widgets it built for itself
// (text label, list marker, li content box) — everything up to the next
// child element, which a restyle checks on its own.
func (e *El) invalidateStyledLayout() {
	e.InvalidateLayout()
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if _, isEl := w.(*El); isEl {
			return
		}
		w.InvalidateLayout()
		if l, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, c := range l.ChildList() {
				walk(c)
			}
		}
	}
	for _, c := range e.ChildList() {
		walk(c)
	}
}

// layoutView returns cs with every property that only affects painting
// cleared, so two styles compare equal exactly when they lay out the same.
// The list names paint-only properties rather than layout ones: a property
// missing from it costs a needless relayout, never a stale size.
func layoutView(cs *ComputedStyle) *ComputedStyle {
	if cs == nil {
		return nil
	}
	v := *cs
	v.Hidden = false // visibility:hidden keeps its box
	v.Color, v.HasColor, v.ColorOwn = qui.Color{}, false, false
	v.AccentColor, v.HasAccentColor = qui.Color{}, false
	v.ScrollbarThumb, v.ScrollbarTrack, v.HasScrollbarColor = qui.Color{}, qui.Color{}, false
	v.Underline, v.LineThrough, v.Overline = false, false, false
	v.DecorationColor, v.HasDecorationColor = qui.Color{}, false
	v.DecorationStyle, v.DecorationThickness = 0, 0
	v.UnderlineOffset, v.HasUnderlineOffset = 0, false
	v.Background, v.HasBackground, v.BackgroundImageURL = qui.Color{}, false, ""
	v.Gradient = nil
	v.BorderColor, v.SideColors = qui.Color{}, qui.SideColors{}
	v.Radius, v.Corners = 0, qui.CornerRadii{}
	v.OutlineWidth, v.OutlineColor, v.HasOutline = 0, qui.Color{}, false
	v.Shadow, v.HasShadow, v.ExtraShadows = qui.ShadowStyle{}, false, nil
	v.Opacity, v.HasOpacity = 0, false
	v.Transform = nil // transforms move pixels, not boxes
	v.Filter = nil
	v.TextShadows, v.ClipPath, v.Mask, v.BackdropFilter = nil, nil, nil, nil // paint-only
	v.Cursor, v.HasCursor = 0, false
	v.NoSelect, v.PointerNone = false, false
	v.AppRegion = ""
	// The declarations and custom properties are already resolved into the
	// fields above.
	v.raw, v.customProps = nil, nil
	v.Hover, v.Focus, v.Active, v.FocusVisible = layoutView(cs.Hover), layoutView(cs.Focus), layoutView(cs.Active), layoutView(cs.FocusVisible)
	v.AncestorHover, v.AncestorFocus, v.AncestorActive = layoutView(cs.AncestorHover), layoutView(cs.AncestorFocus), layoutView(cs.AncestorActive)
	return &v
}
