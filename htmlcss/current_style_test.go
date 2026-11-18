package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestComputedStyleForStateTracksPaintOnlyHover(t *testing.T) {
	r := RenderDoc(`<div id="parent"><a id="link">Link</a></div>`, `#link{background-color:transparent} #link:hover{background-color:rgba(20,40,60,.5)} #parent:hover #link{color:red}`, Options{})
	p := r.ByID["parent"].(*El)
	e := r.ByID["link"].(*El)
	if e.ComputedStyleForState(nil, nil).Background.A != 0 {
		t.Fatal("unexpected resting background")
	}
	p.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 1, 1, 0, 0))
	e.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 1, 1, 0, 0))
	cs := e.ComputedStyleForState(nil, nil)
	if cs.Background.A != .5 || cs.Color.R != 1 || cs.Color.G != 0 {
		t.Fatalf("hover style background=%v color=%v", cs.Background, cs.Color)
	}
	if e.ComputedStyle().Background.A != 0 {
		t.Fatal("hover must not replace resting style")
	}
	e.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 1, 1, 0, 0))
	p.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 1, 1, 0, 0))
	if e.ComputedStyleForState(nil, nil).Background.A != 0 {
		t.Fatal("background did not reset on leave")
	}
}

// DOM targets can belong to folded inlines with no retained widget state.
// Their ancestors' current inherited values must reach unhovered siblings too.
func TestComputedStyleForStateFoldedTargets(t *testing.T) {
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(false) })
	r := RenderDoc(`<html><body><div id="parent"><a id="link">Link</a><span id="other">Other</span></div></body></html>`, `
		#parent { color:black; --ink:black }
		#parent:hover { color:red; --ink:red }
		#parent:focus-within { background-color:blue }
		#link:hover { text-decoration:underline }
		#link:focus { color:green }
		#other { border-color:var(--ink) }
	`, Options{DocumentRoot: true})
	sz := r.Root.Measure(qui.Size{W: 400})
	r.Root.Layout(qui.Rect{W: 400, H: sz.H})
	p := r.ByID["parent"].(*El)
	link := r.ByID["link"].(*El)
	other := r.ByID["other"].(*El)
	if !link.folded {
		t.Fatal("expected link folded into its parent inline run")
	}
	if link.Hovering() || link.Focused() {
		t.Fatal("DOM targets must be independent of retained state")
	}
	red := qui.Color{R: 1, A: 1}
	if cs := link.ComputedStyleForState(link, link); !cs.Underline || cs.Color != (qui.Color{G: 128.0 / 255, A: 1}) {
		t.Fatalf("folded hover/focus: underline=%v color=%v", cs.Underline, cs.Color)
	}
	if cs := other.ComputedStyleForState(link, link); cs.Color != red || cs.BorderColor != red {
		t.Fatalf("current inherited color/custom property: color=%v border=%v", cs.Color, cs.BorderColor)
	}
	if cs := p.ComputedStyleForState(link, link); cs.Background != (qui.Color{B: 1, A: 1}) {
		t.Fatalf("DOM focus-within: background=%v", cs.Background)
	}
	if cs := other.ComputedStyleForState(nil, nil); cs.Color == red || cs.BorderColor == red {
		t.Fatal("DOM target state did not reset")
	}
}

// Native sibling triggers and active ancestors must combine with host targets,
// rather than either source of state replacing the other.
func TestComputedStyleForStateTriggersAndActive(t *testing.T) {
	r := RenderDoc(`<div id="trigger"></div><div id="parent"><div id="child">Child</div></div>`, `
		#trigger { width:20px; height:20px }
		#trigger:hover + #parent { background-color:red }
		#trigger:focus + #parent { border-color:blue }
		#trigger:active + #parent { color:green }
		#parent:active #child { background-color:blue }
		#child:hover { border-color:red }
	`, Options{})
	trigger := r.ByID["trigger"].(*El)
	parent := r.ByID["parent"].(*El)
	child := r.ByID["child"].(*El)
	trigger.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 1, 1, 0, 0))
	trigger.SetFocused(true)
	trigger.Handle(qui.NewMouseEvent(qui.EventMouseDown, 1, 1, qui.MouseButtonLeft, 0))
	parent.Handle(qui.NewMouseEvent(qui.EventMouseDown, 1, 1, qui.MouseButtonLeft, 0))
	red, blue := qui.Color{R: 1, A: 1}, qui.Color{B: 1, A: 1}
	if cs := parent.ComputedStyleForState(child, nil); cs.Background != red || cs.BorderColor != blue || cs.Color != (qui.Color{G: 128.0 / 255, A: 1}) {
		t.Fatalf("sibling triggers: background=%v border=%v color=%v", cs.Background, cs.BorderColor, cs.Color)
	}
	if cs := child.ComputedStyleForState(child, nil); cs.Background != blue || cs.BorderColor != red || cs.Color != (qui.Color{G: 128.0 / 255, A: 1}) {
		t.Fatalf("active ancestor and DOM hover: background=%v border=%v color=%v", cs.Background, cs.BorderColor, cs.Color)
	}
	trigger.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 1, 1, 0, 0))
	trigger.SetFocused(false)
	trigger.Handle(qui.NewMouseEvent(qui.EventMouseUp, 1, 1, qui.MouseButtonLeft, 0))
	parent.Handle(qui.NewMouseEvent(qui.EventMouseUp, 1, 1, qui.MouseButtonLeft, 0))
	if cs := child.ComputedStyleForState(nil, nil); cs.Background == blue || cs.BorderColor == red || cs.Color.G != 0 {
		t.Fatal("trigger/active state did not reset")
	}
}
