// reactive-html example — web-like React over qui's HTML/CSS engine.
//
// Run:            go run ./examples/reactive-html
// Drive/observe:  QUI_AGENT=1 go run ./examples/reactive-html
//
// What it shows, all styled by style.css (no inline Go style calls):
//   - Signals + structural bindings: the todo list is an h.For over a
//     signal, and add / delete / reorder re-sync WITHOUT a reconcile pass.
//     A signal-bound filter (h.Show + BindClass) toggles the visible rows
//     and the active chip with zero render.
//   - Drag-to-reorder rows (El Draggable/OnDrop) and right-click menus.
//   - OS → app file drop: drag files from Finder/Explorer onto the window
//     (Window.SetOnFileDrop) and the dropped paths render into a signal-
//     bound list — no reconcile.
//   - Keyboard: Enter in the field adds a todo; Tab moves focus across the
//     controls; Esc dismisses the confirm dialog.
//   - Visuals: a CSS-grid card wall with gradients, shadows and :hover
//     transforms; icon glyphs; a light/dark theme toggled by flipping a
//     class on the root that re-scopes the CSS variables.
//   - Cross-package components imported from ./components.
//   - A component written as an HTML template (card.html) rather than Go
//     builders, bound through h.Scope.
package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/examples/reactive-html/components"
	"github.com/qizhanchan/qui/icons"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

//go:embed style.css
var styleCSS string

//go:embed card.html
var cardHTML string

// A component compiled from real HTML. Parse once at startup; Bind fills
// the holes per render, at about the cost of writing the builders by hand.
var cardTemplate = h.MustParse(cardHTML)

var priorities = []string{"Low", "Medium", "High"}

// todo is one list item. A stable id keys reconciliation and identifies a
// row for drag-reorder / delete.
type todo struct {
	id     int
	text   string
	urgent bool
	prio   int
}

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — reactive + htmlcss", 780, 760)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())
	qui.SetTheme(qui.LightTheme)

	h.Mount(window, styleCSS, func() h.Node { return App(window) })

	// AI-native operability: run with QUI_AGENT=1 to drive/observe the app.
	if srv, err := agent.BindEnv(window); err == nil && srv != nil {
		window.OnClose(func() { _ = srv.Stop() })
	}

	app.Run()
}

// App is the root component.
func App(window *qui.Window) h.Node {
	dark, setDark := reactive.UseState(false)
	draft, setDraft := reactive.UseState("")
	urgent, setUrgent := reactive.UseState(false)
	priority, setPriority := reactive.UseState(1)
	confirmID, setConfirmID := reactive.UseState(-1)

	// Signals drive the structural list. For/Show reconcile straight from a
	// signal change — no render pass — so add / delete / reorder / filter
	// never re-run App.
	todos := reactive.UseSignal([]todo{
		{id: 1, text: "Learn qui's reactive layer", prio: 1},
		{id: 2, text: "Style with plain CSS classes", prio: 0},
		{id: 3, text: "Ship the dark theme", urgent: true, prio: 2},
	})
	urgentOnly := reactive.UseSignal(false)

	// Drop indicator: which row the drag is over and on which side. The line
	// shows above the row (after=false) or below it (after=true) so a row can
	// be dropped into the very last slot too. Driven by OnDragOver during a
	// drag — all signal-driven, no reconcile.
	dropSig := reactive.UseSignal(dropPos{id: -1})
	// Stable per-row class signals for the two drop lines (above/below),
	// cached by todo id so each is created once (renders reuse them).
	barSigs := reactive.UseRef(map[int]rowBars{})

	// Derived signals (created once, then stable): the filtered view and an
	// is-empty flag. Auto-tracked — they re-run when todos or urgentOnly change.
	filtered := reactive.UseMemo(func() *reactive.Signal[[]todo] {
		return reactive.Computed(func() []todo {
			all := todos.Get()
			if !urgentOnly.Get() {
				return all
			}
			out := make([]todo, 0, len(all))
			for _, t := range all {
				if t.urgent {
					out = append(out, t)
				}
			}
			return out
		})
	}, "filtered")
	isEmpty := reactive.UseMemo(func() *reactive.Signal[bool] {
		return reactive.Computed(func() bool { return len(filtered.Get()) == 0 })
	}, "isEmpty")

	// Chip classes bound to the filter signal — the active chip updates with
	// zero reconcile, right alongside the list re-sync.
	allChip := reactive.UseMemo(func() *reactive.Signal[string] {
		return reactive.Map(urgentOnly, func(u bool) string { return chipClass(!u) })
	}, "allChip")
	urgentChip := reactive.UseMemo(func() *reactive.Signal[string] {
		return reactive.Map(urgentOnly, func(u bool) string { return chipClass(u) })
	}, "urgentChip")

	// A signal-driven clock: BindText pushes it straight to the element.
	clock := reactive.UseSignal("0.0s")
	reactive.UseEffectOnce(func() func() {
		start := time.Now()
		stop := make(chan struct{})
		go func() {
			t := time.NewTicker(1 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					clock.Set(fmt.Sprintf("%.1fs", time.Since(start).Seconds()))
				}
			}
		}()
		return func() { close(stop) }
	})

	// OS → app file drop. GLFW fires SetOnFileDrop's callback on the main
	// thread when files are dragged from Finder/Explorer onto the window; we
	// stat each path and push the result into a signal, so the list below
	// re-syncs with no render pass. Cleared on unmount.
	drops := reactive.UseSignal([]droppedFile{})
	reactive.UseEffectOnce(func() func() {
		window.SetOnFileDrop(func(paths []string, _, _ float32) {
			items := make([]droppedFile, 0, len(paths))
			for _, p := range paths {
				df := droppedFile{path: p, name: filepath.Base(p)}
				if info, err := os.Stat(p); err == nil {
					df.size = info.Size()
					df.dir = info.IsDir()
				}
				items = append(items, df)
			}
			drops.Set(items)
		})
		return func() { window.SetOnFileDrop(nil) }
	})
	noDrops := reactive.UseMemo(func() *reactive.Signal[bool] {
		return reactive.Computed(func() bool { return len(drops.Get()) == 0 })
	}, "noDrops")

	// --- mutations (all operate on the todos signal) ---
	nextID := func(cur []todo) int {
		id := 1
		for _, t := range cur {
			if t.id >= id {
				id = t.id + 1
			}
		}
		return id
	}
	addTodo := func() {
		if draft == "" {
			return
		}
		todos.Update(func(cur []todo) []todo {
			return append(append([]todo{}, cur...),
				todo{id: nextID(cur), text: draft, urgent: urgent, prio: priority})
		})
		setDraft("")
		setUrgent(false)
		setPriority(1)
	}
	deleteID := func(id int) {
		todos.Update(func(cur []todo) []todo {
			out := make([]todo, 0, len(cur))
			for _, t := range cur {
				if t.id != id {
					out = append(out, t)
				}
			}
			return out
		})
	}
	duplicateID := func(id int) {
		todos.Update(func(cur []todo) []todo {
			out := make([]todo, 0, len(cur)+1)
			for _, t := range cur {
				out = append(out, t)
				if t.id == id {
					dup := t
					dup.id = nextID(cur)
					dup.text = t.text + " (copy)"
					out = append(out, dup)
				}
			}
			return out
		})
	}
	// moveRelative removes src and reinserts it just before (after=false) or
	// just after (after=true) dst — the drag-reorder primitive, keyed by id
	// so it survives filtering. "after" lets a row reach the very last slot.
	moveRelative := func(srcID, dstID int, after bool) {
		if srcID == dstID {
			return
		}
		todos.Update(func(cur []todo) []todo {
			var src todo
			found := false
			for _, t := range cur {
				if t.id == srcID {
					src, found = t, true
				}
			}
			if !found {
				return cur
			}
			out := make([]todo, 0, len(cur))
			for _, t := range cur {
				if t.id == srcID {
					continue
				}
				if t.id == dstID && !after {
					out = append(out, src)
				}
				out = append(out, t)
				if t.id == dstID && after {
					out = append(out, src)
				}
			}
			return out
		})
	}

	// One todo row, wrapped in an .item (a flex column) with a drop line ABOVE
	// and BELOW the row. Each line's class is bound to dropSig so it lights up
	// (no reconcile) when the drag targets this row on that side — the "below"
	// line is what lets a row be dropped into the final slot.
	renderRow := func(_ int, t todo) h.Node {
		id := t.id
		bars, ok := (*barSigs)[id]
		if !ok {
			bars = rowBars{
				top: reactive.Map(dropSig, func(p dropPos) string {
					return barClass(p.id == id && !p.after)
				}),
				bot: reactive.Map(dropSig, func(p dropPos) string {
					return barClass(p.id == id && p.after)
				}),
			}
			(*barSigs)[id] = bars
		}

		rowClass := "todo"
		if dark {
			rowClass = "todo todo-dark"
		}
		rowKids := []h.Node{
			// Only the grip is the drag source: DragHandle restricts drag
			// initiation to this span, and lifts the whole row as the ghost.
			h.Span("⠿").Class("grip").ID("grip-" + strconv.Itoa(id)).
				DragHandle().
				Draggable(strconv.Itoa(id)).
				OnDragEnd(func() {
					if p := dropSig.Peek(); p.id >= 0 && p.id != id {
						moveRelative(id, p.id, p.after)
					}
					dropSig.Set(dropPos{id: -1})
				}),
			h.Span(t.text).Class("todo-text"),
			h.Span(priorities[t.prio]).Class("prio prio-" + strconv.Itoa(t.prio)),
		}
		if t.urgent {
			rowKids = append(rowKids, h.Span("urgent").Class("tag"))
		}
		rowKids = append(rowKids,
			h.Button("").Icon(icons.Delete).Class("del").OnClick(func() { setConfirmID(id) }))

		return h.Div(
			h.Div().Class("drop-bar").BindClass(bars.top),
			h.Div(rowKids).Class(rowClass),
			h.Div().Class("drop-bar").BindClass(bars.bot),
		).
			Class("item").
			Key(strconv.Itoa(id)).
			// The row is the drop target; "after" (from OnDragOver) says the
			// cursor is in the row's lower half → insert below. Reorder runs in
			// the grip's OnDragEnd using this indicator.
			OnDragOver(func(_ string, after bool) { dropSig.Set(dropPos{id: id, after: after}) }).
			OnContextMenu(func(x, y float32) {
				h.ContextMenu(window, x, y, []h.MenuItem{
					{Label: "Duplicate", OnClick: func() { duplicateID(id) }},
					{Separator: true},
					{Label: "Delete…", OnClick: func() { setConfirmID(id) }},
				})
			})
	}

	// Confirm-delete dialog (Esc / backdrop / Cancel all dismiss).
	dialog := h.Nothing()
	if confirmID >= 0 {
		id := confirmID
		text := ""
		for _, t := range todos.Peek() {
			if t.id == id {
				text = t.text
			}
		}
		if text != "" {
			cancel := func() { setConfirmID(-1) }
			del := func() {
				deleteID(id)
				setConfirmID(-1)
			}
			// The h.Dialog shell: header / body / action row with stable
			// classes, Enter = Delete, Esc / backdrop / ✕ = cancel.
			dialog = h.Dialog(h.DialogProps{
				Title: "Delete this todo?",
				Body:  h.P(text),
				Actions: []h.Node{
					h.Button("Cancel").Class("btn ghost").OnClick(cancel),
					h.Button("Delete").Class("btn danger").OnClick(del),
				},
				OnDismiss:   cancel,
				OnConfirm:   del,
				CloseButton: true,
				Class:       "confirm",
			})
		}
	}

	appClass := "app"
	darkIcon := icons.DarkMode
	darkLabel := "Dark"
	if dark {
		appClass = "app dark"
		darkIcon = icons.LightMode
		darkLabel = "Light"
	}

	return h.Div(
		// Top bar: title, live clock (signal), theme toggle.
		h.Div(
			h.H1("Reactive + htmlcss").Class("title"),
			h.Span("").Class("clock").BindText(clock),
			h.Button("").
				Children(h.Icon(darkIcon).Class("btn-icon"), h.Span(darkLabel)).
				Class("btn ghost toggle").ID("theme-toggle").
				OnClick(func() { setDark(!dark) }),
		).Class("topbar"),
		h.P("A web-like React flow, rendered by qui's HTML/CSS engine.").Class("sub"),

		// Feature cards: CSS grid + gradients + shadows + :hover transform.
		h.Div(
			card(icons.Palette, "CSS Engine", "Styled entirely by cascading CSS classes and variables."),
			card(icons.Star, "Signals", "Fine-grained updates that skip the reconciler."),
			card(icons.ContentCopy, "Keyed Reconcile", "Add, remove and reorder reuse live widgets."),
		).Class("cards"),

		// OS file drop: a drop zone plus a signal-bound list of dropped paths.
		// Drag files from Finder/Explorer onto the window to populate it.
		h.Div(
			h.Div(
				h.Icon(icons.CloudUpload).Class("drop-icon"),
				h.Div(
					h.H3("Drop files here").Class("card-title"),
					h.P("Drag files from Finder / Explorer onto this window.").Class("card-desc"),
				).Class("drop-copy"),
			).Class("dropzone"),
			h.Div(
				h.ForWith("drops", reactive.BoundLayout{Direction: qui.Vertical, Gap: 6},
					drops, renderDrop),
				h.Show("no-drops", noDrops, func() h.Node {
					return h.P("No files dropped yet.").Class("empty")
				}),
			).Class("droplist").ID("droplist"),
		).Class("drop-section"),

		// Cross-package components (imported from ./components).
		h.Div(
			components.Badge("cross-package"),
			components.Stepper("Volume"),
		).Class("row"),

		// Composer: Enter or the Add button appends a todo.
		h.Div(
			h.Input().Class("field").Placeholder("Add a todo…  (press Enter)").
				Value(draft).OnInput(setDraft).OnSubmit(func(string) { addTodo() }),
			h.Button("").
				Children(h.Icon(icons.Add).Class("btn-icon"), h.Span("Add")).
				Class("btn primary").OnClick(addTodo),
		).Class("composer"),
		h.Div(
			h.Checkbox().Checked(urgent).OnToggle(setUrgent),
			h.Span("urgent").Class("form-label"),
			h.Span("priority").Class("form-label"),
			h.Select(priorities).Selected(priority).
				OnSelect(func(i int, _ string) { setPriority(i) }),
		).Class("row"),

		// Filter bar: clicking a chip sets a signal — the list AND the active
		// chip update with no reconcile (For re-sync + BindClass).
		h.Div(
			h.Icon(icons.FilterList).Class("filter-icon"),
			h.Span("Filter:").Class("form-label"),
			h.Button("All").BindClass(allChip).OnClick(func() { urgentOnly.Set(false) }),
			h.Button("Urgent").BindClass(urgentChip).OnClick(func() { urgentOnly.Set(true) }),
		).Class("row"),

		// The list: h.For over the filtered signal, plus a signal-gated hint.
		h.Div(
			h.ForWith("rows", reactive.BoundLayout{Direction: qui.Vertical, Gap: 8},
				filtered, renderRow),
			h.Show("empty", isEmpty, func() h.Node {
				return h.P("Nothing here — add a todo or clear the filter.").Class("empty")
			}),
		).Class("todos").ID("todos"),

		h.Span("Drag rows to reorder · right-click for a menu · Enter adds · Esc closes the dialog.").
			Class("hint"),

		dialog,
	).Class(appClass)
}

// droppedFile is one entry in the file-drop list: an absolute path plus the
// stat info we show (base name, size, whether it's a directory).
type droppedFile struct {
	path string
	name string
	size int64
	dir  bool
}

// renderDrop renders one dropped file as a row — icon + name + full path +
// a size (or "folder") badge. Keyed by path so the For re-sync is stable.
func renderDrop(_ int, f droppedFile) h.Node {
	icon := icons.FilePresent
	badge := humanSize(f.size)
	if f.dir {
		icon = icons.Folder
		badge = "folder"
	}
	return h.Div(
		h.Icon(icon).Class("drop-file-icon"),
		h.Div(
			h.Span(f.name).Class("drop-file-name"),
			h.Span(f.path).Class("drop-file-path"),
		).Class("drop-file-text"),
		h.Span(badge).Class("drop-file-badge"),
	).Class("drop-file").Key(f.path)
}

// humanSize formats a byte count as B / KB / MB / GB.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// card is a feature tile for the grid — icon + title + description. Its
// markup lives in card.html; compare with the builder-written components
// elsewhere in this file, which produce the same widget tree.
func card(icon qui.VectorSource, title, desc string) h.Node {
	return cardTemplate.Bind(h.Scope{"icon": icon, "title": title, "desc": desc})
}

func chipClass(active bool) string {
	if active {
		return "chip chip-active"
	}
	return "chip"
}

// dropPos identifies the drop target during a drag: the row id and whether
// to insert after it (cursor in the row's lower half) rather than before.
type dropPos struct {
	id    int
	after bool
}

// rowBars holds a row's two drop-line class signals (above / below).
type rowBars struct {
	top, bot *reactive.Signal[string]
}

func barClass(on bool) string {
	if on {
		return "drop-bar drop-bar-on"
	}
	return "drop-bar"
}
