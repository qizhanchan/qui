package diagram

import (
	"fmt"

	"github.com/qizhanchan/qui"
)

// Role identifies this widget as a diagram.
func (d *Diagram) Role() string { return qui.RoleDiagram }

// AccessibleName returns the active page title.
func (d *Diagram) AccessibleName() string {
	if d == nil || d.doc == nil || d.doc.Active() == nil {
		return ""
	}
	return d.doc.Active().Name
}

// AccessibleValue summarizes the visible cell graph.
func (d *Diagram) AccessibleValue() string {
	if d == nil || d.doc == nil || d.doc.Active() == nil {
		return ""
	}
	nodes, edges := 0, 0
	for _, c := range d.doc.Active().Cells {
		if !d.cellVisible(c) {
			continue
		}
		if c.Type == CellEdge {
			edges++
		} else {
			nodes++
		}
	}
	return fmt.Sprintf("%d nodes, %d edges", nodes, edges)
}

// AccessibleChildren publishes every visible vertex and edge with stable
// IDs, semantic roles and window-space bounds. Agent selectors therefore
// address diagram content without a separate navigation protocol.
func (d *Diagram) AccessibleChildren() []qui.AXChild {
	if d == nil || d.doc == nil || d.doc.Active() == nil {
		return nil
	}
	b := d.Bounds()
	page := d.doc.Active()
	out := make([]qui.AXChild, 0, len(page.Cells))
	for _, c := range page.Cells {
		if !d.cellVisible(c) {
			continue
		}
		r := c.Bounds()
		role := qui.RoleDiagramNode
		value := describeShape(ResolvedStyle(c.Style, d.theme.DefaultVertex).Shape)
		if c.Type == CellEdge {
			role = qui.RoleDiagramEdge
			value = describeRoute(ResolvedStyle(c.Style, d.theme.DefaultEdge).Route)
			r = polylineBounds(page.RouteWithRegistry(c, d.shapes).Polyline)
		}
		lr := d.vp.rectToLocal(r)
		lr.X += b.X
		lr.Y += b.Y
		var state qui.AccessibleState
		if d.selected[c.ID] {
			state |= qui.AXStateSelected
		}
		if c.Locked {
			state |= qui.AXStateDisabled
		}
		name := c.Label
		if name == "" {
			name = formatCell(c)
		}
		out = append(out, qui.AXChild{
			ID: c.ID, Role: role, Name: name, Value: value,
			State: state, Bounds: qui.Rect{X: lr.X, Y: lr.Y, W: lr.W, H: lr.H},
		})
	}
	return out
}

func polylineBounds(pts []Point) Rect {
	if len(pts) == 0 {
		return Rect{}
	}
	minX, minY := pts[0].X, pts[0].Y
	maxX, maxY := minX, minY
	for _, p := range pts[1:] {
		minX = min32(minX, p.X)
		minY = min32(minY, p.Y)
		maxX = max32(maxX, p.X)
		maxY = max32(maxY, p.Y)
	}
	return Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}.Inflate(2)
}
