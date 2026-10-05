package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// mountLive builds root under a test window, styles it, and lays it out.
func mountLive(t *testing.T, css string, build func(eng *StyleEngine) *El) (*qui.Window, *StyleEngine, *El) {
	t.Helper()
	eng := NewStyleEngine(css)
	root := build(eng)
	eng.SetRoot(root)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(root)
	eng.Restyle()
	root.Measure(win.Size())
	root.Layout(qui.Rect{W: 400, H: 300})
	return win, eng, root
}

func TestFocusVisibleIsKeyboardOnly(t *testing.T) {
	var btn *El
	win, _, _ := mountLive(t, `button:focus-visible { background: #ff0000; }`, func(eng *StyleEngine) *El {
		root := eng.NewEl("div")
		btn = eng.NewEl("button")
		btn.SetTextContent("Go")
		root.SetElementChildren([]qui.Widget{btn})
		return root
	})
	if btn.Box.Focus != nil || btn.Box.FocusVisible == nil {
		t.Fatalf(":focus-visible should compile to its own variant (Focus=%v FocusVisible=%v)", btn.Box.Focus, btn.Box.FocusVisible)
	}
	if btn.FocusOnClick() {
		t.Fatal("a :focus-visible rule opted the button into click focus")
	}
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyTab, 0))
	if win.Focused() != qui.Widget(btn) || !btn.FocusVisibleNow() {
		t.Fatalf("Tab should focus the button visibly (focused %v)", win.Focused())
	}
}

func TestLinkHandlerAndSafeDefault(t *testing.T) {
	var a *El
	var got string
	win, eng, _ := mountLive(t, `a { display: block; width: 100px; height: 20px; }`, func(eng *StyleEngine) *El {
		root := eng.NewEl("div")
		a = eng.NewEl("a")
		a.SetAttr("href", "#/settings")
		a.SetTextContent("Settings")
		root.SetElementChildren([]qui.Widget{a})
		return root
	})
	eng.SetLinkHandler(func(href string, from *El) bool { got = href; return true })
	b := qui.InteractionBoundsOf(a)
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, b.X+5, b.Y+5, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, b.X+5, b.Y+5, qui.MouseButtonLeft, 0))
	if got != "#/settings" {
		t.Fatalf("link handler got %q", got)
	}
	for href, want := range map[string]bool{
		"https://x.dev": true, "mailto:a@b.c": true, "#/settings": false,
		"file:///etc/passwd": false, "javascript:alert(1)": false, "/relative": false,
	} {
		if isExternalLink(href) != want {
			t.Errorf("isExternalLink(%q) = %v", href, !want)
		}
	}
}

type gauge struct {
	widgets.Label
	width float32
}

func TestCustomElementAndProperty(t *testing.T) {
	RegisterProperty("gauge-width", PropertyDef{Inherited: true, Initial: "1"})
	RegisterElement("x-gauge", ElementDef{
		Create: func(*El) qui.Widget {
			g := &gauge{Label: *widgets.NewLabel("g")}
			g.SetSelf(g)
			return g
		},
		Apply: func(_ *El, w qui.Widget, cs *ComputedStyle) {
			if cs.Property("gauge-width") == "3" {
				w.(*gauge).width = 3
			}
		},
	})
	var g *El
	mountLive(t, `.panel { gauge-width: 3; } .wrap > x-gauge:first-child { width: 90px; height: 12px; }`, func(eng *StyleEngine) *El {
		root := eng.NewEl("div")
		root.SetClass("panel")
		wrap := eng.NewEl("div")
		wrap.SetClass("wrap")
		g = eng.NewEl("x-gauge")
		wrap.SetElementChildren([]qui.Widget{g})
		root.SetElementChildren([]qui.Widget{wrap})
		return root
	})
	w, ok := g.HostedWidget().(*gauge)
	if !ok {
		t.Fatalf("custom element didn't create its widget: %T", g.HostedWidget())
	}
	if w.width != 3 {
		t.Fatal("inherited custom property didn't reach Apply")
	}
	if b := g.Bounds(); b.W != 90 || b.H != 12 {
		t.Fatalf("custom element not styled by selectors: %v", b)
	}
}

func TestPopupPaletteFromCSS(t *testing.T) {
	var sel, scroller *El
	win, _, _ := mountLive(t, `
		:root { --popup-background: #101010; --popup-color: #eeeeee; --tooltip-background: #202020; }
		.s { overflow-y: auto; height: 50px; scrollbar-color: #ff0000 #00ff00; }
	`, func(eng *StyleEngine) *El {
		root := eng.NewEl("div")
		sel = eng.NewEl("select")
		sel.SetSelectOptions([]string{"a", "b"})
		scroller = eng.NewEl("div")
		scroller.SetClass("s")
		root.SetElementChildren([]qui.Widget{sel, scroller})
		return root
	})
	s := sel.backing.(*widgets.Select)
	if s.DropdownColors.Background != (qui.Color{R: 0x10 / 255.0, G: 0x10 / 255.0, B: 0x10 / 255.0, A: 1}) {
		t.Fatalf("select dropdown ignored --popup-background: %+v", s.DropdownColors.Background)
	}
	if scroller.scrollView == nil || scroller.scrollView.BarColors.Thumb != (qui.Color{R: 1, A: 1}) {
		t.Fatal("scrollbar-color not applied")
	}
	if win.TooltipStyle().Background != (qui.Color{R: 0x20 / 255.0, G: 0x20 / 255.0, B: 0x20 / 255.0, A: 1}) {
		t.Fatalf("tooltip style from :root vars: %+v", win.TooltipStyle())
	}
}

func TestElFileDropAndPaste(t *testing.T) {
	var zone, field *El
	var dropped []string
	var pasted string
	win, _, _ := mountLive(t, `.zone { width: 200px; height: 100px; }`, func(eng *StyleEngine) *El {
		root := eng.NewEl("div")
		zone = eng.NewEl("div")
		zone.SetClass("zone")
		zone.SetOnFileDrop(func(p []string) { dropped = p })
		field = eng.NewEl("input")
		field.SetOnPaste(func(s string) bool { pasted = s; return true })
		root.SetElementChildren([]qui.Widget{zone, field})
		return root
	})
	b := qui.InteractionBoundsOf(zone)
	if !zone.AcceptsDrop(&qui.DragData{Files: []string{"/a"}}) {
		t.Fatal("a file drop zone must accept files")
	}
	zone.Handle(qui.NewDragEvent(qui.EventDrop, b.X+5, b.Y+5, nil))
	ev := qui.NewDragEvent(qui.EventDrop, b.X+5, b.Y+5, nil)
	ev.Data.Files = []string{"/tmp/a.png"}
	zone.Handle(ev)
	if len(dropped) != 1 || dropped[0] != "/tmp/a.png" {
		t.Fatalf("dropped %v", dropped)
	}
	qui.SetClipboardProvider(&memClipboard{text: "clip"})
	defer qui.SetClipboardProvider(nil)
	field.RequestFocus()
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyV, qui.CommandMod()))
	if pasted != "clip" {
		t.Fatalf("paste hook got %q", pasted)
	}
}

type memClipboard struct{ text string }

func (m *memClipboard) Get() string  { return m.text }
func (m *memClipboard) Set(s string) { m.text = s }
