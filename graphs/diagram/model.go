package diagram

import (
	"errors"
	"fmt"
)

// CellType is the drawable role of a cell.
type CellType string

const (
	CellVertex CellType = "vertex"
	CellEdge   CellType = "edge"
	CellGroup  CellType = "group"
)

// Point is a diagram-space coordinate.
type Point struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

// Rect is an axis-aligned diagram-space rectangle.
type Rect struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.Y >= r.Y && p.X <= r.X+r.W && p.Y <= r.Y+r.H
}

func (r Rect) Center() Point { return Point{X: r.X + r.W/2, Y: r.Y + r.H/2} }

func (r Rect) Union(o Rect) Rect {
	if r.Empty() {
		return o
	}
	if o.Empty() {
		return r
	}
	x0 := min32(r.X, o.X)
	y0 := min32(r.Y, o.Y)
	x1 := max32(r.X+r.W, o.X+o.W)
	y1 := max32(r.Y+r.H, o.Y+o.H)
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func (r Rect) Inflate(n float32) Rect {
	return Rect{X: r.X - n, Y: r.Y - n, W: r.W + 2*n, H: r.H + 2*n}
}

// Port identifies an attachment point on a vertex. Auto asks the router
// to choose a perimeter point facing the opposite endpoint.
type Port string

const (
	PortAuto  Port = "auto"
	PortNorth Port = "n"
	PortEast  Port = "e"
	PortSouth Port = "s"
	PortWest  Port = "w"
)

// Endpoint is one end of an edge. A non-empty Cell attaches the endpoint
// to that cell; X/Y are then only used for custom relative ports. With no
// Cell, X/Y are a fixed diagram-space point.
type Endpoint struct {
	Cell string  `json:"cell,omitempty"`
	Port Port    `json:"port,omitempty"`
	X    float32 `json:"x,omitempty"`
	Y    float32 `json:"y,omitempty"`
}

// Geometry contains a vertex bounds or an edge's waypoints and fixed
// terminal points.
type Geometry struct {
	X float32 `json:"x,omitempty"`
	Y float32 `json:"y,omitempty"`
	W float32 `json:"w,omitempty"`
	H float32 `json:"h,omitempty"`

	// Points are user waypoints for edges.
	Points []Point `json:"points,omitempty"`
	// SourcePoint/TargetPoint override an unattached edge end.
	SourcePoint *Point `json:"sourcePoint,omitempty"`
	TargetPoint *Point `json:"targetPoint,omitempty"`
	// Label position is a fraction along the routed edge (default .5).
	LabelPosition float32 `json:"labelPosition,omitempty"`
}

func (g Geometry) Bounds() Rect { return Rect{X: g.X, Y: g.Y, W: g.W, H: g.H} }

// Cell is one vertex, group or edge. Cells are flat inside a Page; groups
// are expressed through Parent, layers through Layer.
type Cell struct {
	ID       string   `json:"id"`
	Type     CellType `json:"type"`
	Layer    string   `json:"layer,omitempty"`
	Parent   string   `json:"parent,omitempty"`
	Label    string   `json:"label,omitempty"`
	Geometry Geometry `json:"geometry"`
	Style    Style    `json:"style,omitempty"`
	Source   Endpoint `json:"source,omitempty"`
	Target   Endpoint `json:"target,omitempty"`
	Hidden   bool     `json:"hidden,omitempty"`
	Locked   bool     `json:"locked,omitempty"`
	Z        int      `json:"z,omitempty"`
}

func (c *Cell) Bounds() Rect {
	if c == nil {
		return Rect{}
	}
	return c.Geometry.Bounds()
}

// Layer is a named paint/visibility band inside a page.
type Layer struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Hidden  bool   `json:"hidden,omitempty"`
	Locked  bool   `json:"locked,omitempty"`
	Default bool   `json:"default,omitempty"`
}

// Grid configures the page grid.
type Grid struct {
	Visible bool    `json:"visible"`
	Size    float32 `json:"size,omitempty"`
	Color   string  `json:"color,omitempty"`
}

// Page is one diagram page.
type Page struct {
	ID         string  `json:"id"`
	Name       string  `json:"name,omitempty"`
	Width      float32 `json:"width"`
	Height     float32 `json:"height"`
	Background string  `json:"background,omitempty"`
	Grid       Grid    `json:"grid,omitempty"`
	Layers     []Layer `json:"layers,omitempty"`
	Cells      []*Cell `json:"cells,omitempty"`
}

// Document is the root serializable diagram value.
type Document struct {
	Pages      []*Page `json:"pages"`
	ActivePage int     `json:"activePage,omitempty"`
}

// NewDocument returns a one-page default document.
func NewDocument(width, height float32) *Document {
	if width <= 0 {
		width = 800
	}
	if height <= 0 {
		height = 600
	}
	p := &Page{
		ID: "page-1", Name: "Page-1", Width: width, Height: height,
		Background: "#ffffff",
		Grid:       Grid{Visible: true, Size: 10, Color: "#e5e7eb"},
		Layers:     []Layer{{ID: "layer-1", Name: "Layer-1", Default: true}},
	}
	return &Document{Pages: []*Page{p}}
}

// Active returns the active page, or nil for an empty document.
func (d *Document) Active() *Page {
	if d == nil || len(d.Pages) == 0 {
		return nil
	}
	i := d.ActivePage
	if i < 0 || i >= len(d.Pages) {
		i = 0
	}
	return d.Pages[i]
}

// PageAt returns the requested page, falling back to the active page.
func (d *Document) PageAt(i int) *Page {
	if d == nil || len(d.Pages) == 0 {
		return nil
	}
	if i < 0 || i >= len(d.Pages) {
		return d.Active()
	}
	return d.Pages[i]
}

// Cell returns a cell by ID from the active page.
func (d *Document) Cell(id string) *Cell {
	return d.CellOn(d.Active(), id)
}

// CellOn returns a cell by ID from a page.
func (d *Document) CellOn(page *Page, id string) *Cell {
	if page == nil {
		return nil
	}
	for _, c := range page.Cells {
		if c != nil && c.ID == id {
			return c
		}
	}
	return nil
}

// Cell returns a cell by ID from this page.
func (p *Page) Cell(id string) *Cell {
	if p == nil || id == "" {
		return nil
	}
	for _, c := range p.Cells {
		if c != nil && c.ID == id {
			return c
		}
	}
	return nil
}

// LayerByID returns a layer by ID, or nil.
func (p *Page) LayerByID(id string) *Layer {
	if p == nil {
		return nil
	}
	for i := range p.Layers {
		if p.Layers[i].ID == id {
			return &p.Layers[i]
		}
	}
	return nil
}

// DefaultLayer returns the default layer, creating one for legacy pages.
func (p *Page) DefaultLayer() *Layer {
	if p == nil {
		return nil
	}
	for i := range p.Layers {
		if p.Layers[i].Default {
			return &p.Layers[i]
		}
	}
	if len(p.Layers) > 0 {
		return &p.Layers[0]
	}
	p.Layers = append(p.Layers, Layer{ID: "layer-1", Name: "Layer-1", Default: true})
	return &p.Layers[0]
}

// AddCell appends a cell and assigns a default layer when missing.
func (p *Page) AddCell(c *Cell) error {
	if p == nil || c == nil {
		return errors.New("diagram: nil page or cell")
	}
	if c.ID == "" {
		return errors.New("diagram: cell id is required")
	}
	if p.Cell(c.ID) != nil {
		return fmt.Errorf("diagram: duplicate cell id %q", c.ID)
	}
	if c.Type == "" {
		c.Type = CellVertex
	}
	if c.Layer == "" {
		if l := p.DefaultLayer(); l != nil {
			c.Layer = l.ID
		}
	}
	p.Cells = append(p.Cells, c)
	return nil
}

// RemoveCell removes a cell, its descendant group subtree and all edges
// referencing any removed cell.
func (p *Page) RemoveCell(id string) bool {
	if p == nil || id == "" {
		return false
	}
	removedIDs := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, c := range p.Cells {
			if c != nil && !removedIDs[c.ID] && removedIDs[c.Parent] {
				removedIDs[c.ID] = true
				changed = true
			}
		}
	}
	removed := false
	out := p.Cells[:0]
	for _, c := range p.Cells {
		if c == nil {
			continue
		}
		if removedIDs[c.ID] || (c.Type == CellEdge && (removedIDs[c.Source.Cell] || removedIDs[c.Target.Cell])) {
			removed = true
			continue
		}
		out = append(out, c)
	}
	p.Cells = out
	return removed
}

// CellVisible reports whether c and every layer/group/endpoint it depends
// on should be painted. Edges attached to hidden vertices are hidden, as in
// draw.io.
func (p *Page) CellVisible(c *Cell) bool {
	if p == nil || c == nil || c.Hidden {
		return false
	}
	if l := p.LayerByID(c.Layer); l != nil && l.Hidden {
		return false
	}
	seen := map[string]bool{}
	for id := c.Parent; id != "" && !seen[id]; {
		seen[id] = true
		if p.LayerByID(id) != nil {
			break
		}
		parent := p.Cell(id)
		if parent == nil {
			break
		}
		if parent.Hidden {
			return false
		}
		if l := p.LayerByID(parent.Layer); l != nil && l.Hidden {
			return false
		}
		id = parent.Parent
	}
	if c.Type == CellEdge {
		if c.Source.Cell != "" {
			src := p.Cell(c.Source.Cell)
			if src != nil && !p.CellVisible(src) {
				return false
			}
		}
		if c.Target.Cell != "" {
			dst := p.Cell(c.Target.Cell)
			if dst != nil && !p.CellVisible(dst) {
				return false
			}
		}
	}
	return true
}

// ContentBounds returns the union of visible cells in page coordinates.
func (p *Page) ContentBounds() Rect {
	var out Rect
	if p == nil {
		return out
	}
	for _, c := range p.Cells {
		if !p.CellVisible(c) {
			continue
		}
		switch c.Type {
		case CellVertex, CellGroup:
			out = out.Union(c.Bounds())
		case CellEdge:
			for _, pt := range p.EdgeSamples(c, nil, 12) {
				out = out.Union(Rect{X: pt.X, Y: pt.Y, W: .001, H: .001})
			}
		}
	}
	return out
}

// ContentBounds returns the active page's content bounds.
func (d *Document) ContentBounds() Rect {
	if p := d.Active(); p != nil {
		return p.ContentBounds()
	}
	return Rect{}
}

// Validate checks the invariants consumed by renderers and editors.
func (d *Document) Validate() error {
	if d == nil {
		return errors.New("diagram: nil document")
	}
	for pi, p := range d.Pages {
		if p == nil {
			return fmt.Errorf("diagram: page %d is nil", pi)
		}
		seen := map[string]bool{}
		layers := map[string]bool{}
		for _, l := range p.Layers {
			if l.ID == "" {
				return fmt.Errorf("diagram: page %q has an empty layer id", p.ID)
			}
			if layers[l.ID] {
				return fmt.Errorf("diagram: page %q has duplicate layer %q", p.ID, l.ID)
			}
			layers[l.ID] = true
		}
		for i, c := range p.Cells {
			if c == nil {
				return fmt.Errorf("diagram: page %q cell %d is nil", p.ID, i)
			}
			if c.ID == "" {
				return fmt.Errorf("diagram: page %q cell %d has empty id", p.ID, i)
			}
			if seen[c.ID] {
				return fmt.Errorf("diagram: page %q has duplicate cell %q", p.ID, c.ID)
			}
			seen[c.ID] = true
			if c.Type == "" {
				return fmt.Errorf("diagram: cell %q has empty type", c.ID)
			}
			if c.Layer != "" && !layers[c.Layer] {
				return fmt.Errorf("diagram: cell %q references missing layer %q", c.ID, c.Layer)
			}
		}
		for _, c := range p.Cells {
			if c.Type != CellEdge {
				continue
			}
			if c.Source.Cell != "" && !seen[c.Source.Cell] {
				return fmt.Errorf("diagram: edge %q references missing source %q", c.ID, c.Source.Cell)
			}
			if c.Target.Cell != "" && !seen[c.Target.Cell] {
				return fmt.Errorf("diagram: edge %q references missing target %q", c.ID, c.Target.Cell)
			}
		}
	}
	return nil
}

// Clone returns a deep copy through the standard JSON representation.
func (d *Document) Clone() *Document {
	if d == nil {
		return nil
	}
	out, err := DecodeJSONBytes(EncodeJSON(d))
	if err != nil {
		return nil
	}
	return out
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
