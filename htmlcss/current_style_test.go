package htmlcss

import (
	"github.com/qizhanchan/qui"
	"testing"
)

func TestCurrentComputedStyleTracksPaintOnlyHover(t *testing.T) {
	r := RenderDoc(`<div id="parent"><a id="link">Link</a></div>`, `#link{background-color:transparent} #link:hover{background-color:rgba(20,40,60,.5)} #parent:hover #link{color:red}`, Options{})
	p := r.ByID["parent"].(*El)
	e := r.ByID["link"].(*El)
	if e.CurrentComputedStyle().Background.A != 0 {
		t.Fatal("unexpected resting background")
	}
	p.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 1, 1, 0, 0))
	e.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 1, 1, 0, 0))
	cs := e.CurrentComputedStyle()
	if cs.Background.A != .5 || cs.Color.R != 1 || cs.Color.G != 0 {
		t.Fatalf("hover style background=%v color=%v", cs.Background, cs.Color)
	}
	if e.ComputedStyle().Background.A != 0 {
		t.Fatal("hover must not replace resting style")
	}
	e.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 1, 1, 0, 0))
	p.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 1, 1, 0, 0))
	if e.CurrentComputedStyle().Background.A != 0 {
		t.Fatal("background did not reset on leave")
	}
}
