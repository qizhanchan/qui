package diagram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/qizhanchan/qui"
)

// Mode selects editing versus read-only viewing.
type Mode int

const (
	ModeView Mode = iota
	ModeEdit
)

// Option configures a Diagram widget.
type Option struct {
	Document *Document
	Mode     Mode
	Theme    *Theme
	Grid     bool
	Snap     bool
	SnapSize float32
	// Guides snaps moved and resized shapes to the edges and centre lines
	// of other shapes and draws the alignment lines (hold Alt to bypass).
	Guides   bool
	Fit      bool
	Padding  float32
	ReadOnly bool
	Shapes   *ShapeRegistry
}

// Diagram is a retained diagram viewer/editor widget.
type Diagram struct {
	qui.BaseWidget

	doc    *Document
	theme  Theme
	shapes *ShapeRegistry
	mode   Mode
	grid   bool
	snap   bool
	snapSz float32
	guides bool
	margin float32
	vp     Viewport

	selected map[string]bool
	hoverID  string
	editing  string
	editBuf  string

	interact interaction
	history  history
	fitNext  bool

	OnChange func(*Document)
	OnSelect func([]*Cell)
}

type interactionMode int

const (
	modeIdle interactionMode = iota
	modeDrag
	modeResize
	modeConnect
	modePan
	modeMarquee
)

type interaction struct {
	mode interactionMode

	dragStartWorld Point
	dragOrigins    map[string]Point
	dragBefore     []byte
	dragMoved      bool

	resizeHandle int
	resizeOrigin Rect
	resizeBefore []byte

	guides []Guide // alignment lines for the move/resize in progress

	connectFromCell string
	connectFromPort Port
	connectToCell   string
	connectToPort   Port
	connectToPoint  Point
	connectBefore   []byte

	panStartLocal Point
	panStartVP    Viewport

	marqueeStartLocal Point
	marqueeNowLocal   Point
	marqueeAdditive   bool
	marqueeBase       map[string]bool

	lastDownAt   time.Time
	lastDownPos  Point
	hasLastDown  bool
	spaceHeld    bool
	lastMousePos Point
}

type historyEntry struct {
	before []byte
	after  []byte
}

type history struct {
	undo  []historyEntry
	redo  []historyEntry
	limit int
}

// New constructs a Diagram. A nil document creates a blank page.
func New(opt Option) *Diagram {
	doc := opt.Document
	if doc == nil {
		doc = NewDocument(800, 600)
	}
	normalizeDocument(doc)
	theme := DefaultTheme()
	if opt.Theme != nil {
		theme = *opt.Theme
	}
	shapes := opt.Shapes
	if shapes == nil {
		shapes = DefaultShapeRegistry()
	}
	snapSz := opt.SnapSize
	if snapSz <= 0 {
		snapSz = 10
	}
	d := &Diagram{
		BaseWidget: qui.NewBaseWidget(),
		doc:        doc,
		theme:      theme,
		shapes:     shapes,
		mode:       opt.Mode,
		grid:       opt.Grid,
		snap:       opt.Snap,
		snapSz:     snapSz,
		guides:     opt.Guides,
		margin:     opt.Padding,
		vp:         Viewport{Scale: 1},
		selected:   map[string]bool{},
		fitNext:    opt.Fit,
	}
	if opt.ReadOnly {
		d.mode = ModeView
	}
	d.interact.resizeHandle = -1
	d.SetSelf(d)
	return d
}

// NewEditor is shorthand for New with edit mode enabled.
func NewEditor(doc *Document) *Diagram {
	return New(Option{Document: doc, Mode: ModeEdit, Grid: true, Snap: true, Guides: true, Fit: true})
}

// NewViewer is shorthand for a read-only fitted viewer.
func NewViewer(doc *Document) *Diagram {
	return New(Option{Document: doc, Mode: ModeView, Fit: true})
}

func (d *Diagram) Measure(_ qui.Size) qui.Size { return qui.Size{W: 720, H: 480} }

func (d *Diagram) HitTest(p qui.Point) qui.Widget {
	if d == nil || !d.Bounds().Contains(p) {
		return nil
	}
	if s := d.Self(); s != nil {
		return s
	}
	return d
}

func (d *Diagram) Focusable() bool { return d.mode == ModeEdit && d.Enabled() }
func (d *Diagram) SetFocused(bool) {}

func (d *Diagram) Layout(r qui.Rect) {
	d.BaseWidget.Layout(r)
	if d.fitNext {
		d.fitNext = false
		d.Fit()
	}
}

// Document returns the retained document.
func (d *Diagram) Document() *Document { return d.doc }

// SetDocument replaces the document and clears transient interaction state.
func (d *Diagram) SetDocument(doc *Document, fit bool) {
	if doc == nil {
		doc = NewDocument(800, 600)
	}
	normalizeDocument(doc)
	d.doc = doc
	d.selected = map[string]bool{}
	d.hoverID = ""
	d.editing = ""
	d.interact = interaction{resizeHandle: -1}
	d.history = history{}
	d.fitNext = fit
	if fit {
		d.Fit()
	}
	d.Invalidate()
	d.notifySelect()
}

// Mode returns the current interaction mode.
func (d *Diagram) Mode() Mode { return d.mode }

// SetMode changes between view and edit modes.
func (d *Diagram) SetMode(mode Mode) {
	d.mode = mode
	if mode == ModeView {
		d.cancelEditing(false)
		d.interact.mode = modeIdle
	}
	d.Invalidate()
}

// Theme returns the active visual defaults.
func (d *Diagram) Theme() Theme { return d.theme }

// SetTheme changes cell defaults and chrome colors.
func (d *Diagram) SetTheme(theme Theme) {
	d.theme = theme
	d.Invalidate()
}

// SetGrid toggles grid painting.
func (d *Diagram) SetGrid(on bool) {
	d.grid = on
	d.Invalidate()
}

// SetSnap toggles grid snapping for editing operations.
func (d *Diagram) SetSnap(on bool, size float32) {
	d.snap = on
	if size > 0 {
		d.snapSz = size
	}
}

// Viewport returns the current pan/zoom transform.
func (d *Diagram) Viewport() Viewport { return d.vp }

// SetViewport replaces pan/zoom and clamps the scale to a usable range.
func (d *Diagram) SetViewport(vp Viewport) {
	if vp.Scale <= 0 {
		vp.Scale = 1
	}
	vp.Scale = clamp32(vp.Scale, .05, 8)
	d.vp = vp
	d.Invalidate()
}

// Fit centers and scales the active page's visible content.
func (d *Diagram) Fit() {
	if d == nil || d.doc == nil {
		return
	}
	page := d.doc.Active()
	if page == nil {
		return
	}
	b := d.Bounds()
	if b.W <= 1 || b.H <= 1 {
		return
	}
	pad := d.margin
	if pad <= 0 {
		pad = 28
	}
	content := page.ContentBounds()
	if content.Empty() {
		content = Rect{X: 0, Y: 0, W: page.Width, H: page.Height}
	}
	d.vp = fitViewport(content, Rect{X: 0, Y: 0, W: b.W, H: b.H}, pad)
	d.Invalidate()
}

// ZoomAt scales around a widget-local point.
func (d *Diagram) ZoomAt(local Point, factor float32) {
	if factor <= 0 {
		return
	}
	world := d.vp.localToWorld(local)
	next := d.vp.Scale * factor
	next = clamp32(next, .05, 8)
	d.vp.Scale = next
	d.vp.Pan = Point{X: local.X - world.X*next, Y: local.Y - world.Y*next}
	d.Invalidate()
}

// ZoomIn / ZoomOut are convenience controls for toolbars.
func (d *Diagram) ZoomIn()  { d.ZoomAt(Point{X: d.Bounds().W / 2, Y: d.Bounds().H / 2}, 1.2) }
func (d *Diagram) ZoomOut() { d.ZoomAt(Point{X: d.Bounds().W / 2, Y: d.Bounds().H / 2}, 1/1.2) }

// SelectedIDs returns a copy of the current selection.
func (d *Diagram) SelectedIDs() []string {
	out := make([]string, 0, len(d.selected))
	for id, on := range d.selected {
		if on {
			out = append(out, id)
		}
	}
	return out
}

// SelectedCells returns the selected cells in page paint order.
func (d *Diagram) SelectedCells() []*Cell {
	page := d.doc.Active()
	var out []*Cell
	if page == nil {
		return nil
	}
	for _, c := range page.Cells {
		if d.selected[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

// Select replaces the selection with one cell. Empty clears it.
func (d *Diagram) Select(id string) {
	d.selected = map[string]bool{}
	if id != "" && d.doc.Cell(id) != nil {
		d.selected[id] = true
	}
	d.Invalidate()
	d.notifySelect()
}

// SelectAll selects every visible cell on the active page.
func (d *Diagram) SelectAll() {
	d.selected = map[string]bool{}
	if p := d.doc.Active(); p != nil {
		for _, c := range p.Cells {
			if c != nil && !c.Hidden && d.cellVisible(c) {
				d.selected[c.ID] = true
			}
		}
	}
	d.Invalidate()
	d.notifySelect()
}

// ClearSelection removes all selected cells.
func (d *Diagram) ClearSelection() { d.Select("") }

// CanUndo / CanRedo report history availability.
func (d *Diagram) CanUndo() bool { return len(d.history.undo) > 0 }
func (d *Diagram) CanRedo() bool { return len(d.history.redo) > 0 }

// Undo restores the document snapshot before the most recent mutation.
func (d *Diagram) Undo() bool {
	if len(d.history.undo) == 0 {
		return false
	}
	entry := d.history.undo[len(d.history.undo)-1]
	d.history.undo = d.history.undo[:len(d.history.undo)-1]
	doc, err := DecodeJSONBytes(entry.before)
	if err != nil {
		return false
	}
	d.doc = doc
	d.history.redo = append(d.history.redo, entry)
	d.pruneSelection()
	d.Invalidate()
	d.notifySelect()
	d.notifyChange()
	return true
}

// Redo reapplies the most recently undone mutation.
func (d *Diagram) Redo() bool {
	if len(d.history.redo) == 0 {
		return false
	}
	entry := d.history.redo[len(d.history.redo)-1]
	d.history.redo = d.history.redo[:len(d.history.redo)-1]
	doc, err := DecodeJSONBytes(entry.after)
	if err != nil {
		return false
	}
	d.doc = doc
	d.history.undo = append(d.history.undo, entry)
	d.pruneSelection()
	d.Invalidate()
	d.notifySelect()
	d.notifyChange()
	return true
}

// Mutate applies a document change through the history channel. Returning
// false aborts without recording.
func (d *Diagram) Mutate(fn func(*Document) bool) bool {
	if d.mode != ModeEdit || fn == nil {
		return false
	}
	before := EncodeJSON(d.doc)
	if !fn(d.doc) {
		return false
	}
	normalizeDocument(d.doc)
	if err := d.doc.Validate(); err != nil {
		d.doc, _ = DecodeJSONBytes(before)
		return false
	}
	d.record(before)
	d.pruneSelection()
	d.Invalidate()
	d.notifyChange()
	return true
}

// AddVertex appends a vertex using the active page's default layer.
func (d *Diagram) AddVertex(label string, shape ShapeKind, rect Rect) *Cell {
	page := d.doc.Active()
	if page == nil || d.mode != ModeEdit {
		return nil
	}
	before := EncodeJSON(d.doc)
	c := &Cell{
		ID: d.nextCellID(page, "v"), Type: CellVertex, Label: label,
		Geometry: Geometry{X: rect.X, Y: rect.Y, W: rect.W, H: rect.H},
		Style:    Style{Shape: shape},
	}
	if err := page.AddCell(c); err != nil {
		return nil
	}
	d.record(before)
	d.selected = map[string]bool{c.ID: true}
	d.Invalidate()
	d.notifyChange()
	d.notifySelect()
	return c
}

// Connect adds an edge between two endpoints.
func (d *Diagram) Connect(source, target Endpoint, route RouteKind, label string) *Cell {
	page := d.doc.Active()
	if page == nil || d.mode != ModeEdit {
		return nil
	}
	before := EncodeJSON(d.doc)
	c := &Cell{
		ID: d.nextCellID(page, "e"), Type: CellEdge, Label: label,
		Source: source, Target: target, Style: Style{Route: route, EndArrow: ArrowTriangle},
	}
	if err := page.AddCell(c); err != nil {
		return nil
	}
	d.record(before)
	d.selected = map[string]bool{c.ID: true}
	d.Invalidate()
	d.notifyChange()
	d.notifySelect()
	return c
}

// CursorAt provides resize, port and selection cursors.
func (d *Diagram) CursorAt(p qui.Point) (qui.CursorShape, bool) {
	if d.mode != ModeEdit {
		return qui.CursorDefault, false
	}
	local := Point{X: p.X - d.Bounds().X, Y: p.Y - d.Bounds().Y}
	if h := d.resizeHandleAt(local); h >= 0 {
		switch h {
		case 0, 4:
			return qui.CursorResizeNWSE, true
		case 2, 6:
			return qui.CursorResizeNESW, true
		case 1, 5:
			return qui.CursorResizeNS, true
		default:
			return qui.CursorResizeEW, true
		}
	}
	world := d.vp.localToWorld(local)
	if d.portAt(world) != nil {
		return qui.CursorCrosshair, true
	}
	if d.hitCell(world, 6/d.vp.Scale) != nil {
		return qui.CursorHand, true
	}
	return qui.CursorDefault, false
}

func (d *Diagram) Handle(event qui.Event) bool {
	switch e := event.(type) {
	case qui.MouseEvent:
		return d.handleMouse(e)
	case qui.KeyEvent:
		return d.handleKey(e)
	case qui.CharEvent:
		return d.handleChar(e)
	case qui.GestureEvent:
		if e.Type() == qui.EventGesturePinch && d.Bounds().Contains(qui.Point{X: e.X, Y: e.Y}) {
			local := Point{X: e.X - d.Bounds().X, Y: e.Y - d.Bounds().Y}
			d.ZoomAt(local, 1+e.DScale)
			return true
		}
	}
	return false
}

func (d *Diagram) Draw(canvas qui.Canvas) {
	b := d.Bounds()
	selected := make(map[string]bool, len(d.selected))
	for id, on := range d.selected {
		if on {
			selected[id] = true
		}
	}
	cfg := sceneConfig{
		Origin:       Point{X: b.X, Y: b.Y},
		Viewport:     d.vp,
		Clip:         quiRectToDiagram(b),
		Theme:        d.theme,
		Shapes:       d.shapes,
		Grid:         d.grid,
		Selected:     selected,
		EditingID:    d.editing,
		EditingLabel: d.editBuf,
		ShowControls: d.mode == ModeEdit,
		ReadOnly:     d.mode == ModeView,
	}
	if d.hoverID != "" && d.mode == ModeEdit {
		cfg.ShowPortsFor = d.hoverID
	}
	drawScene(canvas, d.doc, cfg)

	// Live connector preview shares the scene transform.
	if d.interact.mode == modeConnect {
		depth := canvas.Save()
		canvas.ClipRect(b)
		canvas.Translate(b.X+d.vp.Pan.X, b.Y+d.vp.Pan.Y)
		canvas.Scale(d.vp.Scale, d.vp.Scale)
		p := &scenePainter{doc: d.doc, cfg: cfg}
		e := d.previewEdge()
		p.drawEdge(canvas, e)
		p.drawEdgeArrows(canvas, e)
		canvas.RestoreTo(depth)
	}

	if d.interact.mode == modeMarquee {
		d.drawMarquee(canvas)
	}
	if d.interact.mode == modeDrag || d.interact.mode == modeResize {
		d.drawGuides(canvas)
	}
}

func (d *Diagram) drawMarquee(canvas qui.Canvas) {
	a, b := d.interact.marqueeStartLocal, d.interact.marqueeNowLocal
	r := qui.Rect{X: min32(a.X, b.X), Y: min32(a.Y, b.Y), W: abs32(b.X - a.X), H: abs32(b.Y - a.Y)}
	r.X += d.Bounds().X
	r.Y += d.Bounds().Y
	sel, _ := ParseColor(d.theme.Selection)
	fill, _ := ParseColor(d.theme.SelectionFill)
	canvas.FillRect(r, fill)
	canvas.StrokeRect(r, sel, 1)
}

func (d *Diagram) previewEdge() *Cell {
	src := Endpoint{Cell: d.interact.connectFromCell, Port: d.interact.connectFromPort}
	dst := Endpoint{Cell: d.interact.connectToCell, Port: d.interact.connectToPort}
	if dst.Cell == "" {
		dst.X = d.interact.connectToPoint.X
		dst.Y = d.interact.connectToPoint.Y
	}
	return &Cell{
		ID: "__preview", Type: CellEdge, Source: src, Target: dst,
		Style: Style{Route: RouteOrthogonal, Stroke: d.theme.Selection, EndArrow: ArrowTriangle},
	}
}

func (d *Diagram) handleMouse(e qui.MouseEvent) bool {
	b := d.Bounds()
	if !b.Contains(qui.Point{X: e.X, Y: e.Y}) && e.Type() != qui.EventMouseUp && e.Type() != qui.EventMouseMove {
		return false
	}
	local := Point{X: e.X - b.X, Y: e.Y - b.Y}
	world := d.vp.localToWorld(local)
	d.interact.lastMousePos = local
	switch e.Type() {
	case qui.EventMouseDown:
		if e.Button == qui.MouseButtonMiddle || (e.Button == qui.MouseButtonLeft && d.interact.spaceHeld) {
			d.commitEditing()
			d.interact.mode = modePan
			d.interact.panStartLocal = local
			d.interact.panStartVP = d.vp
			return true
		}
		if e.Button != qui.MouseButtonLeft {
			return false
		}
		d.commitEditing()
		now := time.Now()
		double := d.interact.hasLastDown && now.Sub(d.interact.lastDownAt) < 350*time.Millisecond &&
			dist32(local, d.interact.lastDownPos) < 7
		d.interact.hasLastDown = true
		d.interact.lastDownAt = now
		d.interact.lastDownPos = local
		if d.mode != ModeEdit {
			if c := d.hitCell(world, 6/d.vp.Scale); c != nil {
				d.selected = map[string]bool{c.ID: true}
				d.Invalidate()
				d.notifySelect()
				return true
			}
			d.ClearSelection()
			return true
		}
		// Resize handles win over the selected shape's own ports: the side
		// handles sit exactly on the N/E/S/W ports.
		if h := d.resizeHandleAt(local); h >= 0 {
			c := d.singleSelectedVertex()
			if c != nil {
				d.interact.mode = modeResize
				d.interact.resizeHandle = h
				d.interact.resizeOrigin = c.Bounds()
				d.interact.dragStartWorld = world
				d.interact.resizeBefore = EncodeJSON(d.doc)
				return true
			}
		}
		if port := d.portAt(world); port != nil {
			d.interact.mode = modeConnect
			d.interact.connectFromCell = port.cell.ID
			d.interact.connectFromPort = port.port
			d.interact.connectToCell = ""
			d.interact.connectToPort = PortAuto
			d.interact.connectToPoint = world
			d.interact.connectBefore = EncodeJSON(d.doc)
			d.Invalidate()
			return true
		}
		if c := d.hitCell(world, 6/d.vp.Scale); c != nil {
			if double {
				d.selected = map[string]bool{c.ID: true}
				d.startEditing(c.ID)
				return true
			}
			if e.Mods&qui.ModShift != 0 {
				if d.selected[c.ID] {
					delete(d.selected, c.ID)
				} else {
					d.selected[c.ID] = true
				}
			} else if !d.selected[c.ID] {
				d.selected = map[string]bool{c.ID: true}
			}
			d.interact.mode = modeDrag
			d.interact.dragStartWorld = world
			d.interact.dragOrigins = d.selectedOrigins()
			d.interact.dragBefore = EncodeJSON(d.doc)
			d.interact.dragMoved = false
			d.Invalidate()
			d.notifySelect()
			return true
		}
		d.interact.mode = modeMarquee
		d.interact.marqueeStartLocal = local
		d.interact.marqueeNowLocal = local
		d.interact.marqueeBase = cloneSelection(d.selected)
		d.interact.marqueeAdditive = e.Mods&qui.ModShift != 0
		if !d.interact.marqueeAdditive {
			d.selected = map[string]bool{}
		}
		d.Invalidate()
		d.notifySelect()
		return true

	case qui.EventMouseMove:
		switch d.interact.mode {
		case modePan:
			d.vp.Pan = Point{
				X: d.interact.panStartVP.Pan.X + local.X - d.interact.panStartLocal.X,
				Y: d.interact.panStartVP.Pan.Y + local.Y - d.interact.panStartLocal.Y,
			}
			d.Invalidate()
			return true
		case modeDrag:
			dx, dy := world.X-d.interact.dragStartWorld.X, world.Y-d.interact.dragStartWorld.Y
			dx, dy = d.snapMove(dx, dy, e.Mods&qui.ModAlt != 0)
			if dx != 0 || dy != 0 {
				d.interact.dragMoved = true
			}
			for id, origin := range d.interact.dragOrigins {
				if c := d.doc.Cell(id); c != nil && c.Type != CellEdge {
					c.Geometry.X = origin.X + dx
					c.Geometry.Y = origin.Y + dy
				}
			}
			d.Invalidate()
			return true
		case modeResize:
			c := d.singleSelectedVertex()
			if c != nil {
				d.resizeCell(c, world, e.Mods&qui.ModAlt != 0)
				d.Invalidate()
				return true
			}
		case modeConnect:
			d.updateConnector(world)
			d.Invalidate()
			return true
		case modeMarquee:
			d.interact.marqueeNowLocal = local
			d.updateMarqueeSelection()
			d.Invalidate()
			return true
		default:
			if d.mode == ModeEdit {
				if p := d.portAt(world); p != nil {
					d.setHover(p.cell.ID)
				} else if c := d.hitCell(world, 6/d.vp.Scale); c != nil {
					d.setHover(c.ID)
				} else {
					d.setHover("")
				}
			}
		}
		return false

	case qui.EventMouseUp:
		switch d.interact.mode {
		case modeDrag:
			if d.interact.dragMoved {
				d.record(d.interact.dragBefore)
				d.notifyChange()
			}
		case modeResize:
			if c := d.singleSelectedVertex(); c != nil && c.Bounds() != d.interact.resizeOrigin {
				d.record(d.interact.resizeBefore)
				d.notifyChange()
			}
		case modeConnect:
			d.finishConnector()
		case modeMarquee:
			d.updateMarqueeSelection()
			d.notifySelect()
		}
		d.interact.mode = modeIdle
		d.interact.resizeHandle = -1
		d.interact.dragBefore = nil
		d.interact.resizeBefore = nil
		d.interact.connectBefore = nil
		d.interact.guides = nil
		d.Invalidate()
		return true

	case qui.EventScroll:
		factor := float32(math.Pow(10, float64(e.DeltaY)*.04))
		d.ZoomAt(local, factor)
		return true
	case qui.EventMouseLeave:
		d.setHover("")
	}
	return false
}

func (d *Diagram) updateConnector(world Point) {
	d.interact.connectToPoint = world
	d.interact.connectToCell = ""
	d.interact.connectToPort = PortAuto
	if c := d.hitCell(world, 18/d.vp.Scale); c != nil && c.Type != CellEdge && c.ID != d.interact.connectFromCell {
		d.interact.connectToCell = c.ID
		d.interact.connectToPort = nearestPort(c.Bounds(), world, PortAuto)
	}
}

func (d *Diagram) finishConnector() {
	page := d.doc.Active()
	if page == nil {
		return
	}
	edge := d.previewEdge()
	edge.ID = d.nextCellID(page, "e")
	edge.Style = Style{Route: RouteOrthogonal, EndArrow: ArrowTriangle}
	if d.interact.connectToCell == "" {
		if from := page.Cell(d.interact.connectFromCell); from != nil && dist32(portPoint(from.Bounds(), d.interact.connectFromPort), d.interact.connectToPoint) < 4 {
			return
		}
	}
	if err := page.AddCell(edge); err != nil {
		return
	}
	d.record(d.interact.connectBefore)
	d.selected = map[string]bool{edge.ID: true}
	d.notifyChange()
	d.notifySelect()
}

// resizeCell applies a resize-handle drag. Only the edges the handle moves
// change; each lands on an alignment guide when one is in reach, otherwise
// on the grid.
func (d *Diagram) resizeCell(c *Cell, world Point, noGuides bool) {
	const minSize float32 = 24
	dx, dy := world.X-d.interact.dragStartWorld.X, world.Y-d.interact.dragStartWorld.Y
	o := d.interact.resizeOrigin
	ex, ey := resizeEdges(d.interact.resizeHandle)
	left, right, top, bottom := o.X, o.X+o.W, o.Y, o.Y+o.H
	useGuides := d.guides && !noGuides
	var targets []Rect
	if useGuides {
		moving := map[string]bool{c.ID: true}
		for _, child := range d.descendants(c.ID) {
			moving[child.ID] = true
		}
		targets = d.guideTargets(moving, o)
	}
	xMask, yMask := 0, 0
	var guided bool
	switch ex {
	case -1:
		if left, guided = d.snapResizeEdge(left+dx, true, targets, useGuides); guided {
			xMask = featLow
		}
	case 1:
		if right, guided = d.snapResizeEdge(right+dx, true, targets, useGuides); guided {
			xMask = featHigh
		}
	}
	switch ey {
	case -1:
		if top, guided = d.snapResizeEdge(top+dy, false, targets, useGuides); guided {
			yMask = featLow
		}
	case 1:
		if bottom, guided = d.snapResizeEdge(bottom+dy, false, targets, useGuides); guided {
			yMask = featHigh
		}
	}
	if right-left < minSize {
		if ex < 0 {
			left = right - minSize
		} else {
			right = left + minSize
		}
		xMask = 0
	}
	if bottom-top < minSize {
		if ey < 0 {
			top = bottom - minSize
		} else {
			bottom = top + minSize
		}
		yMask = 0
	}
	r := Rect{X: left, Y: top, W: right - left, H: bottom - top}
	c.Geometry.X, c.Geometry.Y, c.Geometry.W, c.Geometry.H = r.X, r.Y, r.W, r.H
	d.interact.guides = nil
	if xMask|yMask != 0 {
		d.interact.guides = guidesFor(r, targets, xMask, yMask)
	}
}

func (d *Diagram) handleKey(e qui.KeyEvent) bool {
	if e.Type() != qui.EventKeyDown {
		if e.Key == qui.KeySpace {
			d.interact.spaceHeld = false
			return true
		}
		return false
	}
	if d.editing != "" {
		switch e.Key {
		case qui.KeyEnter:
			d.commitEditing()
			return true
		case qui.KeyEscape:
			d.cancelEditing(true)
			return true
		case qui.KeyBackspace:
			if d.editBuf != "" {
				_, size := utf8.DecodeLastRuneInString(d.editBuf)
				d.editBuf = d.editBuf[:len(d.editBuf)-size]
				d.Invalidate()
			}
			return true
		}
		return false
	}
	cmd := e.Mods&(qui.ModSuper|qui.ModControl) != 0
	if cmd {
		switch e.Key {
		case qui.KeyZ:
			if e.Mods&qui.ModShift != 0 {
				return d.Redo()
			}
			return d.Undo()
		case qui.KeyC:
			return d.copySelection()
		case qui.KeyV:
			return d.pasteSelection()
		case qui.KeyA:
			d.SelectAll()
			return true
		}
	}
	switch e.Key {
	case qui.KeySpace:
		d.interact.spaceHeld = true
		return true
	case qui.KeyDelete, qui.KeyBackspace:
		return d.deleteSelection()
	case qui.KeyEscape:
		d.interact.mode = modeIdle
		d.ClearSelection()
		return true
	case qui.KeyEnter:
		if len(d.selected) == 1 {
			for id := range d.selected {
				d.startEditing(id)
			}
			return true
		}
	case qui.KeyLeft, qui.KeyRight, qui.KeyUp, qui.KeyDown:
		return d.nudgeSelection(e.Key, e.Mods&qui.ModShift != 0)
	}
	return false
}

func (d *Diagram) handleChar(e qui.CharEvent) bool {
	if d.editing == "" || e.Rune < 0x20 || e.Rune == 0x7f {
		return false
	}
	d.editBuf += string(e.Rune)
	d.Invalidate()
	return true
}

func (d *Diagram) startEditing(id string) {
	c := d.doc.Cell(id)
	if c == nil || c.Type == CellGroup {
		return
	}
	d.editing = id
	d.editBuf = c.Label
	d.interact.dragBefore = EncodeJSON(d.doc)
	d.Invalidate()
}

func (d *Diagram) commitEditing() {
	if d.editing == "" {
		return
	}
	c := d.doc.Cell(d.editing)
	if c != nil && c.Label != d.editBuf {
		c.Label = d.editBuf
		d.record(d.interact.dragBefore)
		d.notifyChange()
	}
	d.editing = ""
	d.editBuf = ""
	d.interact.dragBefore = nil
	d.Invalidate()
}

func (d *Diagram) cancelEditing(record bool) {
	_ = record
	d.editing = ""
	d.editBuf = ""
	d.interact.dragBefore = nil
	d.Invalidate()
}

func (d *Diagram) deleteSelection() bool {
	if len(d.selected) == 0 || d.mode != ModeEdit {
		return false
	}
	before := EncodeJSON(d.doc)
	page := d.doc.Active()
	for id := range d.selected {
		page.RemoveCell(id)
	}
	d.record(before)
	d.selected = map[string]bool{}
	d.Invalidate()
	d.notifyChange()
	d.notifySelect()
	return true
}

func (d *Diagram) nudgeSelection(key qui.Key, large bool) bool {
	if len(d.selected) == 0 || d.mode != ModeEdit {
		return false
	}
	step := float32(1)
	if large {
		step = d.snapSz
		if step <= 0 {
			step = 10
		}
	}
	var dx, dy float32
	switch key {
	case qui.KeyLeft:
		dx = -step
	case qui.KeyRight:
		dx = step
	case qui.KeyUp:
		dy = -step
	case qui.KeyDown:
		dy = step
	}
	before := EncodeJSON(d.doc)
	for id := range d.selected {
		if c := d.doc.Cell(id); c != nil && c.Type != CellEdge {
			c.Geometry.X += dx
			c.Geometry.Y += dy
		}
	}
	d.record(before)
	d.Invalidate()
	d.notifyChange()
	return true
}

func (d *Diagram) copySelection() bool {
	page := d.doc.Active()
	if page == nil || len(d.selected) == 0 {
		return false
	}
	copyPage := &Page{ID: "clipboard", Width: page.Width, Height: page.Height, Layers: page.Layers}
	ids := map[string]bool{}
	for id := range d.selected {
		ids[id] = true
		for _, child := range d.descendants(id) {
			ids[child.ID] = true
		}
	}
	for _, c := range page.Cells {
		if !ids[c.ID] {
			continue
		}
		cp := *c
		cp.Geometry.Points = append([]Point(nil), c.Geometry.Points...)
		copyPage.Cells = append(copyPage.Cells, &cp)
	}
	data, _ := json.Marshal(&Document{Pages: []*Page{copyPage}})
	qui.SetClipboardText("qui-diagram:1\n" + string(data))
	return true
}

func (d *Diagram) pasteSelection() bool {
	text := qui.GetClipboardText()
	const prefix = "qui-diagram:1\n"
	if !strings.HasPrefix(text, prefix) || d.mode != ModeEdit {
		return false
	}
	doc, err := DecodeJSONBytes([]byte(strings.TrimPrefix(text, prefix)))
	if err != nil || len(doc.Pages) == 0 {
		return false
	}
	src := doc.Pages[0]
	page := d.doc.Active()
	if page == nil {
		return false
	}
	before := EncodeJSON(d.doc)
	idMap := map[string]string{}
	reserved := map[string]bool{}
	for _, c := range src.Cells {
		if c != nil {
			idMap[c.ID] = d.nextUniqueCellID(page, prefixForType(c.Type), reserved)
		}
	}
	selected := map[string]bool{}
	for _, c := range src.Cells {
		if c == nil {
			continue
		}
		cp := *c
		cp.ID = idMap[c.ID]
		if cp.Parent != "" {
			if mapped := idMap[cp.Parent]; mapped != "" {
				cp.Parent = mapped
			}
		}
		if cp.Layer == "" || page.LayerByID(cp.Layer) == nil {
			if l := page.DefaultLayer(); l != nil {
				cp.Layer = l.ID
			}
		}
		if cp.Type != CellEdge {
			cp.Geometry.X += 20
			cp.Geometry.Y += 20
		}
		cp.Geometry.Points = append([]Point(nil), c.Geometry.Points...)
		if c.Source.Cell != "" {
			cp.Source.Cell = idMap[c.Source.Cell]
		}
		if c.Target.Cell != "" {
			cp.Target.Cell = idMap[c.Target.Cell]
		}
		if c.Geometry.SourcePoint != nil {
			p := *c.Geometry.SourcePoint
			p.X += 20
			p.Y += 20
			cp.Geometry.SourcePoint = &p
		}
		if c.Geometry.TargetPoint != nil {
			p := *c.Geometry.TargetPoint
			p.X += 20
			p.Y += 20
			cp.Geometry.TargetPoint = &p
		}
		if err := page.AddCell(&cp); err == nil {
			selected[cp.ID] = true
		}
	}
	d.record(before)
	d.selected = selected
	d.Invalidate()
	d.notifyChange()
	d.notifySelect()
	return true
}

// Accessible children and methods are in accessibility.go.

func (d *Diagram) cellVisible(c *Cell) bool {
	return d.doc.Active().CellVisible(c)
}

func (d *Diagram) hitCell(world Point, tolerance float32) *Cell {
	page := d.doc.Active()
	if page == nil {
		return nil
	}
	cells := d.orderedCells()
	// Vertices win over edges: an edge terminates on a node's perimeter,
	// so reverse paint order alone would make the last-drawn edge steal
	// clicks from port markers and node labels.
	for i := len(cells) - 1; i >= 0; i-- {
		c := cells[i]
		if c.Type != CellVertex && c.Type != CellGroup {
			continue
		}
		style := ResolvedStyle(c.Style, func() Style {
			if c.Type == CellGroup {
				return d.theme.DefaultGroup
			}
			return d.theme.DefaultVertex
		}())
		if d.shapes.contains(style.Shape, c.Bounds(), world) {
			return c
		}
	}
	for i := len(cells) - 1; i >= 0; i-- {
		c := cells[i]
		if c.Type == CellEdge && pointPolylineDistance(world, page.RouteWithRegistry(c, d.shapes).Polyline) <= tolerance {
			return c
		}
	}
	return nil
}

func (d *Diagram) orderedCells() []*Cell {
	page := d.doc.Active()
	if page == nil {
		return nil
	}
	out := make([]*Cell, 0, len(page.Cells))
	for _, c := range page.Cells {
		if d.cellVisible(c) {
			out = append(out, c)
		}
	}
	return out
}

type portHit struct {
	cell *Cell
	port Port
}

func (d *Diagram) portAt(world Point) *portHit {
	radius := 11 / d.vp.Scale
	cells := d.orderedCells()
	for i := len(cells) - 1; i >= 0; i-- {
		c := cells[i]
		if c.Type == CellEdge {
			continue
		}
		for _, p := range []Port{PortNorth, PortEast, PortSouth, PortWest} {
			if dist32(portPoint(c.Bounds(), p), world) <= radius {
				return &portHit{cell: c, port: p}
			}
		}
	}
	return nil
}

func (d *Diagram) setHover(id string) {
	if d.hoverID == id {
		return
	}
	d.hoverID = id
	d.Invalidate()
}

func (d *Diagram) singleSelectedVertex() *Cell {
	if len(d.selected) != 1 {
		return nil
	}
	for id := range d.selected {
		c := d.doc.Cell(id)
		if c != nil && c.Type != CellEdge {
			return c
		}
	}
	return nil
}

func (d *Diagram) selectedOrigins() map[string]Point {
	out := map[string]Point{}
	for id := range d.selected {
		c := d.doc.Cell(id)
		if c == nil || c.Type == CellEdge {
			continue
		}
		out[id] = Point{X: c.Geometry.X, Y: c.Geometry.Y}
		for _, child := range d.descendants(id) {
			out[child.ID] = Point{X: child.Geometry.X, Y: child.Geometry.Y}
		}
	}
	return out
}

func (d *Diagram) descendants(id string) []*Cell {
	page := d.doc.Active()
	var out []*Cell
	if page == nil {
		return out
	}
	for _, c := range page.Cells {
		for p := c.Parent; p != ""; {
			if p == id {
				out = append(out, c)
				break
			}
			parent := page.Cell(p)
			if parent == nil {
				break
			}
			p = parent.Parent
		}
	}
	return out
}

func (d *Diagram) resizeHandleAt(local Point) int {
	c := d.singleSelectedVertex()
	if c == nil {
		return -1
	}
	r := d.vp.rectToLocal(c.Bounds())
	hit := 7 / d.vp.Scale
	for i, p := range resizePoints(r) {
		if abs32(local.X-p.X) <= hit && abs32(local.Y-p.Y) <= hit {
			return i
		}
	}
	return -1
}

func (d *Diagram) updateMarqueeSelection() {
	page := d.doc.Active()
	if page == nil {
		return
	}
	a, b := d.vp.localToWorld(d.interact.marqueeStartLocal), d.vp.localToWorld(d.interact.marqueeNowLocal)
	r := Rect{X: min32(a.X, b.X), Y: min32(a.Y, b.Y), W: abs32(a.X - b.X), H: abs32(a.Y - b.Y)}
	sel := cloneSelection(d.interact.marqueeBase)
	if !d.interact.marqueeAdditive {
		sel = map[string]bool{}
	}
	for _, c := range page.Cells {
		if !d.cellVisible(c) {
			continue
		}
		hit := false
		if c.Type == CellEdge {
			for _, p := range page.RouteWithRegistry(c, d.shapes).Polyline {
				if r.Contains(p) {
					hit = true
					break
				}
			}
		} else {
			cr := c.Bounds()
			hit = !(cr.X+cr.W < r.X || cr.X > r.X+r.W || cr.Y+cr.H < r.Y || cr.Y > r.Y+r.H)
		}
		if hit {
			sel[c.ID] = true
		}
	}
	d.selected = sel
}

func (d *Diagram) record(before []byte) {
	if len(before) == 0 {
		return
	}
	after := EncodeJSON(d.doc)
	if bytes.Equal(before, after) {
		return
	}
	if d.history.limit <= 0 {
		d.history.limit = 100
	}
	d.history.undo = append(d.history.undo, historyEntry{before: before, after: after})
	if len(d.history.undo) > d.history.limit {
		d.history.undo = d.history.undo[len(d.history.undo)-d.history.limit:]
	}
	d.history.redo = nil
}

func (d *Diagram) pruneSelection() {
	for id := range d.selected {
		if d.doc.Cell(id) == nil {
			delete(d.selected, id)
		}
	}
	if d.doc.Cell(d.hoverID) == nil {
		d.hoverID = ""
	}
}

func (d *Diagram) notifyChange() {
	if d.OnChange != nil {
		d.OnChange(d.doc)
	}
}

func (d *Diagram) notifySelect() {
	if d.OnSelect != nil {
		d.OnSelect(d.SelectedCells())
	}
}

func (d *Diagram) nextCellID(page *Page, prefix string) string {
	return d.nextUniqueCellID(page, prefix, nil)
}

func (d *Diagram) nextUniqueCellID(page *Page, prefix string, reserved map[string]bool) string {
	for i := 1; ; i++ {
		id := fmt.Sprintf("%s-%d", prefix, i)
		if page.Cell(id) == nil && (reserved == nil || !reserved[id]) {
			if reserved != nil {
				reserved[id] = true
			}
			return id
		}
	}
}

func prefixForType(t CellType) string {
	switch t {
	case CellEdge:
		return "e"
	case CellGroup:
		return "g"
	default:
		return "v"
	}
}

func pointPolylineDistance(p Point, pts []Point) float32 {
	best := float32(math.Inf(1))
	for i := 0; i < len(pts)-1; i++ {
		d := pointSegmentDist2(p, pts[i], pts[i+1])
		if d < best {
			best = d
		}
	}
	return float32(math.Sqrt(float64(best)))
}

func pointSegmentDist2(p, a, b Point) float32 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		dx, dy = p.X-a.X, p.Y-a.Y
		return dx*dx + dy*dy
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
	t = clamp32(t, 0, 1)
	x, y := a.X+t*dx-p.X, a.Y+t*dy-p.Y
	return x*x + y*y
}

func cloneSelection(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func quiRectToDiagram(r qui.Rect) Rect {
	return Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
}
