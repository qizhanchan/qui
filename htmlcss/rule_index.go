package htmlcss

import (
	"slices"
	"sync/atomic"
	"unsafe"
)

// ruleIndex buckets a stylesheet's rules by the most selective key of each
// selector's rightmost (subject) compound — its id, else its first class,
// else its tag — so the cascade only runs selector matching on rules that
// could match a given element instead of on every rule. A selector whose
// subject has none of those (`*`, `:hover`, `[type=x]`) lands in any.
type ruleIndex struct {
	// fingerprint of the Rules slice the index was built from: rebuilt
	// when the sheet grows or is replaced.
	n     int
	first unsafe.Pointer

	byID    map[string][]int
	byClass map[string][]int
	byTag   map[string][]int
	any     []int

	// states[i] is the interactive-state pseudos rule i uses anywhere in
	// any selector (stateHover | …), for stateMask.
	states []uint8
}

const (
	stateHover uint8 = 1 << iota
	stateFocus
	stateFocusVisible
	stateActive
)

// ruleIndexHolder carries the lazily built index; a field of Stylesheet.
type ruleIndexHolder struct{ p atomic.Pointer[ruleIndex] }

func (s *Stylesheet) index() *ruleIndex {
	var first unsafe.Pointer
	if len(s.Rules) > 0 {
		first = unsafe.Pointer(&s.Rules[0])
	}
	if ix := s.ruleIdx.p.Load(); ix != nil && ix.n == len(s.Rules) && ix.first == first {
		return ix
	}
	ix := &ruleIndex{n: len(s.Rules), first: first,
		byID: map[string][]int{}, byClass: map[string][]int{}, byTag: map[string][]int{},
		states: make([]uint8, len(s.Rules))}
	add := func(m map[string][]int, k string, i int) {
		if l := m[k]; len(l) == 0 || l[len(l)-1] != i {
			m[k] = append(l, i)
		}
	}
	for i, rule := range s.Rules {
		for _, sel := range rule.Selectors {
			if len(sel.parts) == 0 {
				continue
			}
			for _, st := range [...]struct {
				kind pseudoKind
				bit  uint8
			}{{pseudoHover, stateHover}, {pseudoFocus, stateFocus},
				{pseudoFocusVisible, stateFocusVisible}, {pseudoActive, stateActive}} {
				if sel.hasPseudo(st.kind) {
					ix.states[i] |= st.bit
				}
			}
			subj := sel.parts[len(sel.parts)-1].sel
			switch {
			case subj.id != "":
				add(ix.byID, subj.id, i)
			case len(subj.classes) > 0:
				add(ix.byClass, subj.classes[0], i)
			case subj.tag != "" && subj.tag != "*":
				add(ix.byTag, subj.tag, i)
			default:
				if l := ix.any; len(l) == 0 || l[len(l)-1] != i {
					ix.any = append(l, i)
				}
			}
		}
	}
	s.ruleIdx.p.Store(ix)
	return ix
}

// candidates returns, in source order and without duplicates, the indices
// of the rules that may match n. buf is reused for the result.
func (ix *ruleIndex) candidates(n *Node, buf []int) []int {
	out := append(buf[:0], ix.any...)
	if id := n.ID(); id != "" {
		out = append(out, ix.byID[id]...)
	}
	if len(ix.byClass) > 0 {
		for _, cl := range n.cachedClasses() {
			out = append(out, ix.byClass[cl]...)
		}
	}
	out = append(out, ix.byTag[n.Tag]...)
	slices.Sort(out)
	return slices.Compact(out)
}

// stateMask ORs the state pseudos of every rule in cands. A state absent
// from it cannot change which of n's rules match, so the state variant of
// n's style equals the resting one (see computeNode).
func (ix *ruleIndex) stateMask(cands []int) uint8 {
	var m uint8
	for _, i := range cands {
		m |= ix.states[i]
	}
	return m
}
