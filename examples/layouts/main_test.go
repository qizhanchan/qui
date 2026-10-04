package main

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func TestAbsoluteDemoBottomAnchoredWidgetsStayVisible(t *testing.T) {
	demo := makeAbsoluteDemo(nil)
	bounds := qui.Rect{W: 900, H: 608}
	demo.Layout(bounds)

	status := findLabel(demo, "Status")
	if status == nil {
		t.Fatal("status label not found")
	}
	if bottom := status.Bounds().Y + status.Bounds().H; bottom > bounds.Y+bounds.H {
		t.Fatalf("status bottom = %v, visible bottom = %v", bottom, bounds.Y+bounds.H)
	}

	fab := findButton(demo, "+")
	if fab == nil {
		t.Fatal("floating action button not found")
	}
	if bottom := fab.Bounds().Y + fab.Bounds().H; bottom > status.Bounds().Y {
		t.Fatalf("fab bottom = %v, status top = %v; button should sit above status bar", bottom, status.Bounds().Y)
	}
}

func findLabel(w qui.Widget, prefix string) *widgets.Label {
	if l, ok := w.(*widgets.Label); ok && strings.HasPrefix(l.Text(), prefix) {
		return l
	}
	if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
		for _, child := range c.ChildList() {
			if found := findLabel(child, prefix); found != nil {
				return found
			}
		}
	}
	return nil
}

func findButton(w qui.Widget, text string) *widgets.Button {
	if b, ok := w.(*widgets.Button); ok && b.Text == text {
		return b
	}
	if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
		for _, child := range c.ChildList() {
			if found := findButton(child, text); found != nil {
				return found
			}
		}
	}
	return nil
}
