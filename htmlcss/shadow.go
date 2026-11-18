package htmlcss

import (
	"github.com/qizhanchan/qui"
	"strings"
)

// Shadow roots remain separate DOM trees. Only renderKids projects them into
// the widget tree; Kids, ParentEl and the selector mirror never change scope.
func (e *El) ShadowRoot() *El    { return e.shadowRoot }
func (e *El) ShadowHost() *El    { return e.shadowHost }
func (e *El) ShadowMode() string { return e.shadowMode }
func (e *El) AttachShadow(mode string) *El {
	if e.shadowRoot != nil {
		return nil
	}
	r := e.engine.NewEl("#shadow-root")
	r.shadowHost, r.shadowMode = e, mode
	e.shadowRoot = r
	e.engine.unregister(r)
	e.engine.shadowRoots = append(e.engine.shadowRoots, r)
	e.engine.shadowSheetDirty = true
	e.engine.distributionDirty = true
	// A label leaf becomes a real light-DOM text node when a root is attached.
	if e.text != "" {
		t := newTextEl(e.TextContent(), e.engine)
		e.text = ""
		e.domTextSet = false
		e.SetElementChildren(append([]qui.Widget{t}, e.elementKids...))
	}
	e.markDirty()
	return r
}
func (e *El) SetShadowCSS(css string) {
	e.shadowCSSOverride = true
	if e.shadowCSS == css {
		return
	}
	e.shadowCSS = css
	e.engine.shadowSheetDirty = true
	e.markDirty()
}
func (eng *StyleEngine) SetSlotChangeHandler(f func(*El)) { eng.slotChange = f }
func (e *El) AssignedSlot() *El                           { e.engine.distributeSlots(); return e.assignedSlot }
func (e *El) AssignedNodes(flatten bool) []*El {
	e.engine.distributeSlots()
	out := append([]*El(nil), e.assigned...)
	if flatten {
		if len(out) == 0 {
			for _, w := range e.elementKids {
				if n, ok := w.(*El); ok && !n.IsComment() {
					out = append(out, n)
				}
			}
		}
		var flat []*El
		for _, n := range out {
			if n.tag == "slot" {
				flat = append(flat, n.AssignedNodes(true)...)
			} else {
				flat = append(flat, n)
			}
		}
		out = flat
	}
	return out
}
func (e *El) renderKids() []qui.Widget {
	if e.shadowRoot != nil {
		return e.shadowRoot.elementKids
	}
	if e.tag == "slot" && len(e.assigned) > 0 {
		out := make([]qui.Widget, len(e.assigned))
		for i, n := range e.assigned {
			out[i] = n
		}
		return out
	}
	return e.elementKids
}
func shadowScope(n *Node) *El {
	for ; n != nil; n = n.Parent {
		if n.live != nil && n.live.shadowHost != nil {
			return n.live
		}
	}
	return nil
}
func (eng *StyleEngine) distributeSlots() {
	if !eng.distributionDirty {
		return
	}
	eng.distributionDirty = false
	for _, r := range eng.shadowRoots {
		h := r.shadowHost
		oldSlots := r.shadowSlots
		for _, s := range oldSlots {
			for _, n := range s.assigned {
				if n.assignedSlot == s {
					n.assignedSlot = nil
				}
			}
		}
		for _, w := range h.elementKids {
			if e, ok := w.(*El); ok {
				e.assignedSlot = nil
			}
		}
		slots := map[string]*El{}
		var all []*El
		var walk func(*El)
		walk = func(e *El) {
			if e.tag == "slot" {
				all = append(all, e)
				name := e.attrs["name"]
				if slots[name] == nil {
					slots[name] = e
				}
			}
			for _, w := range e.elementKids {
				if k, ok := w.(*El); ok {
					walk(k)
				}
			}
		}
		walk(r)
		r.shadowSlots = all
		for _, s := range oldSlots {
			found := false
			for _, n := range all {
				if n == s {
					found = true
					break
				}
			}
			if !found && len(s.assigned) > 0 {
				s.assigned = nil
				if eng.slotChange != nil {
					eng.slotChange(s)
				}
			}
		}
		next := map[*El][]*El{}
		for _, w := range h.elementKids {
			if e, ok := w.(*El); ok && !e.IsComment() {
				if s := slots[e.attrs["slot"]]; s != nil {
					next[s] = append(next[s], e)
					e.assignedSlot = s
				}
			}
		}
		for _, s := range all {
			ns := next[s]
			changed := len(ns) != len(s.assigned)
			if !changed {
				for i, n := range ns {
					if n != s.assigned[i] {
						changed = true
						break
					}
				}
			}
			s.assigned = ns
			if changed && eng.slotChange != nil {
				eng.slotChange(s)
			}
		}
	}
}
func (eng *StyleEngine) prepareShadowTrees() {
	if len(eng.shadowRoots) == 0 {
		return
	}
	eng.distributeSlots()
	for _, r := range eng.shadowRoots {
		if !r.shadowCSSOverride {
			css := collectShadowStyles(r)
			if css != r.shadowCSS {
				r.shadowCSS = css
				eng.shadowSheetDirty = true
			}
		}
	}
	if !eng.shadowSheetDirty {
		return
	}
	eng.shadowSheetDirty = false
	// Parse the whole source once to retain all selector dependency flags. Bind
	// each selector to its originating tree afterwards, without rewriting CSS.
	src := eng.cssSrc
	counts := []int{len(ParseCSSViewport(src, eng.viewportW).Rules)}
	for _, r := range eng.shadowRoots {
		src += "\n" + r.shadowCSS
		counts = append(counts, len(ParseCSSViewport(r.shadowCSS, eng.viewportW).Rules))
	}
	sheet := ParseCSSViewport(src, eng.viewportW)
	sheet.FontFamilies = eng.opts.FontFamilies
	at := 0
	for i, count := range counts {
		var scope *El
		if i > 0 {
			scope = eng.shadowRoots[i-1]
		}
		for j := 0; j < count && at < len(sheet.Rules); j++ {
			for k := range sheet.Rules[at].Selectors {
				s := &sheet.Rules[at].Selectors[k]
				s.treeScoped = true
				s.scope = scope
			}
			at++
		}
	}
	eng.sheet = sheet
}

// collectShadowStyles deliberately stops at nested hosts' DOM roots and inert
// templates. Adopted CSS is supplied by the owning document's CSSOM.
func collectShadowStyles(r *El) string {
	var b strings.Builder
	var walk func(*El)
	walk = func(e *El) {
		if e.tag == "style" {
			b.WriteString(e.TextContent())
			b.WriteByte('\n')
			return
		}
		if e.tag == "template" {
			return
		}
		for _, w := range e.elementKids {
			if k, ok := w.(*El); ok {
				walk(k)
			}
		}
	}
	walk(r)
	return b.String()
}
