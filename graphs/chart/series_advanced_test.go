package chart

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestHeatmapOptionAndHitTest(t *testing.T) {
	var opt Option
	if err := json.Unmarshal([]byte(`{
		"xAxis":{"type":"category","data":["A","B"]},
		"yAxis":{"type":"category","data":["X","Y"]},
		"visualMap":{"min":0,"max":10,"show":false,"inRange":{"color":["#0000ff","#ff0000"]}},
		"series":[{"type":"heatmap","data":[[0,0,2],[1,0,8],[0,1,5],[1,1,10]]}]
	}`), &opt); err != nil {
		t.Fatal(err)
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 400, H: 300})
	if len(c.Series) != 1 {
		t.Fatalf("series count = %d", len(c.Series))
	}
	hm := c.Series[0].(*HeatmapSeries)
	if len(hm.Points) != 4 || len(hm.Colors) != 2 {
		t.Fatalf("heatmap = %+v", hm)
	}
	rects := hm.cellRects(c.PlotRect(), c.XAxis, c.YAxis)
	if got := hm.HitTest(qui.Point{X: rects[0].X + 2, Y: rects[0].Y + 2}, c.PlotRect(), c.XAxis, c.YAxis); got != 0 {
		t.Fatalf("heatmap hit = %d, want 0", got)
	}
	rec := &qui.RecordingCanvas{}
	hm.Draw(rec, c.PlotRect(), c.XAxis, c.YAxis, NoHotSpot)
	if len(rec.Fills) != 4 {
		t.Fatalf("heatmap fills = %d, want 4", len(rec.Fills))
	}
}

func TestBoxplotGeometryAndHitTest(t *testing.T) {
	s := NewBoxplotSeries("score", []BoxplotPoint{{Min: 10, Q1: 25, Median: 40, Q3: 60, Max: 80}})
	x := &CategoryAxis{Categories: []string{"A"}}
	y := &ValueAxis{InvertedAxis: true, Min: 0, Max: 100}
	plot := qui.Rect{W: 200, H: 200}
	if got := s.HitTest(qui.Point{X: 100, Y: 100}, plot, x, y); got != 0 {
		t.Fatalf("boxplot hit = %d, want 0", got)
	}
	rec := &qui.RecordingCanvas{}
	s.Draw(rec, plot, x, y, NoHotSpot)
	if len(rec.Paths) != 2 {
		t.Fatalf("boxplot paths = %d, want fill + stroke", len(rec.Paths))
	}
}

func TestGraphCircularLayoutAndEndpointMapping(t *testing.T) {
	s := NewGraphSeries("g", []GraphNode{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}})
	s.Links = []GraphLinkOption{{Source: "A", Target: "C"}, {Source: GraphEndpoint("1"), Target: "3"}}
	points := s.positions(qui.Rect{W: 400, H: 300})
	if len(points) != 4 {
		t.Fatalf("positions = %d", len(points))
	}
	cx, cy := float32(200), float32(150)
	for i, p := range points {
		r := float32(math.Hypot(float64(p.X-cx), float64(p.Y-cy)))
		if math.Abs(float64(r-120)) > .2 {
			t.Fatalf("node %d radius = %v, want 120", i, r)
		}
	}
	if s.endpointIndex("C") != 2 || s.endpointIndex("3") != 3 {
		t.Fatalf("endpoint mapping failed")
	}
}

func TestTreeNestedDataAndLayout(t *testing.T) {
	var point DataPoint
	if err := json.Unmarshal([]byte(`{"name":"root","children":[{"name":"A","children":[{"name":"A1"}]},{"name":"B"}]}`), &point); err != nil {
		t.Fatal(err)
	}
	s := NewTreeSeries("tree", []TreeSeriesNode{treeNodeFromDataPoint(point)})
	nodes := s.layout(qui.Rect{W: 600, H: 400})
	if len(nodes) != 4 {
		t.Fatalf("tree nodes = %d, want 4", len(nodes))
	}
	root := nodes[0]
	if root.parent != -1 || root.point.X >= nodes[1].point.X {
		t.Fatalf("root layout = %+v", root)
	}
	rec := &qui.RecordingCanvas{}
	s.Draw(rec, qui.Rect{W: 600, H: 400}, nil, nil, NoHotSpot)
	if len(rec.Paths) < 2 || len(rec.Texts) != 4 {
		t.Fatalf("tree draw paths/texts = %d/%d", len(rec.Paths), len(rec.Texts))
	}
}

func TestTreemapSquarifiedLayout(t *testing.T) {
	s := NewTreemapSeries("t", []TreemapNode{
		{Name: "A", Value: 40}, {Name: "B", Value: 30}, {Name: "C", Value: 20}, {Name: "D", Value: 10},
	})
	cells := s.layout(qui.Rect{W: 760, H: 460})
	if len(cells) != 4 {
		t.Fatalf("treemap cells = %d", len(cells))
	}
	if cells[0].node.Name != "A" || math.Abs(float64(cells[0].rect.W-504)) > 1 {
		t.Fatalf("first cell = %+v, want width ~504", cells[0])
	}
	for i := range cells {
		for j := i + 1; j < len(cells); j++ {
			a, b := cells[i].rect, cells[j].rect
			overlapW := minF32(a.X+a.W, b.X+b.W) - maxF32(a.X, b.X)
			overlapH := minF32(a.Y+a.H, b.Y+b.H) - maxF32(a.Y, b.Y)
			if overlapW > .1 && overlapH > .1 {
				t.Fatalf("treemap cells overlap: %+v %+v", a, b)
			}
		}
	}
}

func TestSunburstAnglesAndHitTest(t *testing.T) {
	s := NewSunburstSeries("s", []TreemapNode{{
		Name: "root", Children: []TreemapNode{{Name: "A", Value: 2}, {Name: "B", Value: 1}},
	}})
	cells := s.layout(qui.Rect{W: 400, H: 400})
	if len(cells) != 3 {
		t.Fatalf("sunburst cells = %d", len(cells))
	}
	if cells[0].inner != 0 || math.Abs(float64(cells[1].end-cells[1].start-4*math.Pi/3)) > .01 {
		t.Fatalf("sunburst sectors = %+v", cells)
	}
	if got := s.HitTest(qui.Point{X: 200, Y: 50}, qui.Rect{W: 400, H: 400}, nil, nil); got < 0 {
		t.Fatalf("sunburst hit = %d", got)
	}
}

func TestSankeyLayoutAndColorMapping(t *testing.T) {
	opt := Option{
		Palette: []CSSColor{"#5470c6", "#91cc75", "#fac858", "#ee6666"},
		Series: []SeriesOption{{
			Type: SeriesSankey, Name: "flow",
			Data: []DataPoint{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}},
			Links: []GraphLinkOption{
				{Source: "A", Target: "B", Value: 30}, {Source: "A", Target: "C", Value: 20},
				{Source: "B", Target: "D", Value: 20}, {Source: "C", Target: "D", Value: 10},
			},
		}},
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 760, H: 460})
	s := c.Series[0].(*SankeySeries)
	nodes, links := s.layout(qui.Rect{W: 760, H: 460})
	if len(nodes) != 4 || len(links) != 4 {
		t.Fatalf("sankey layout nodes/links = %d/%d", len(nodes), len(links))
	}
	if nodes[0].depth >= nodes[1].depth || nodes[1].depth >= nodes[3].depth {
		t.Fatalf("sankey depths = %d,%d,%d", nodes[0].depth, nodes[1].depth, nodes[3].depth)
	}
	if s.Nodes[0].Color != c.Palette[3] || s.Nodes[2].Color != c.Palette[0] {
		t.Fatalf("sankey value color mapping = %+v", s.Nodes)
	}
}

func TestParallelOptionAndHitTest(t *testing.T) {
	var opt Option
	if err := json.Unmarshal([]byte(`{
		"parallelAxis":[{"name":"A","min":0,"max":10},{"name":"B","min":0,"max":10}],
		"series":[{"type":"parallel","data":[[1,9],[9,1]]}]
	}`), &opt); err != nil {
		t.Fatal(err)
	}
	c := New(opt)
	s := c.Series[0].(*ParallelSeries)
	axes := s.axisLayouts(qui.Rect{W: 500, H: 300})
	if len(axes) != 2 {
		t.Fatalf("parallel axes = %d", len(axes))
	}
	mid := qui.Point{X: (axes[0].x + axes[1].x) / 2, Y: 150}
	if got := s.HitTest(mid, qui.Rect{W: 500, H: 300}, nil, nil); got < 0 {
		t.Fatalf("parallel hit = %d", got)
	}
}

func TestThemeRiverOptionAndLayout(t *testing.T) {
	var opt Option
	if err := json.Unmarshal([]byte(`{
		"singleAxis":{"type":"value","min":0,"max":2},
		"series":[{"type":"themeRiver","data":[[0,1,"A"],[1,2,"A"],[2,1,"A"],[0,2,"B"],[1,1,"B"],[2,3,"B"]]}]
	}`), &opt); err != nil {
		t.Fatal(err)
	}
	c := New(opt)
	s := c.Series[0].(*ThemeRiverSeries)
	frames, _, _ := s.layout(qui.Rect{W: 600, H: 400})
	if len(frames) != 2 || len(frames[0].points) != 3 {
		t.Fatalf("theme river frames = %+v", frames)
	}
	if s.layerNames()[0] != "A" || s.layerNames()[1] != "B" {
		t.Fatalf("theme river layer order = %v", s.layerNames())
	}
	rec := &qui.RecordingCanvas{}
	s.Draw(rec, qui.Rect{W: 600, H: 400}, nil, nil, NoHotSpot)
	if len(rec.Paths) != 2 {
		t.Fatalf("theme river paths = %d, want 2", len(rec.Paths))
	}
}

func TestEffectScatterOptionAndRipples(t *testing.T) {
	c := New(Option{Series: []SeriesOption{{
		Type: SeriesEffectScatter,
		Data: []DataPoint{XYPoint(1, 2), XYPoint(3, 4)},
	}}})
	c.Layout(qui.Rect{W: 400, H: 300})
	s := c.Series[0].(*ScatterSeries)
	if !s.Effect || s.Size != 10 || s.RippleNumber != 3 {
		t.Fatalf("effect scatter = %+v", s)
	}
	rec := &qui.RecordingCanvas{}
	s.Draw(rec, c.PlotRect(), c.XAxis, c.YAxis, NoHotSpot)
	if len(rec.Rounds) != 2 || len(rec.Strokes) != 6 {
		t.Fatalf("effect scatter rounds/strokes = %d/%d, want 2/6", len(rec.Rounds), len(rec.Strokes))
	}
}

func TestPictorialBarSymbolArrayAndDraw(t *testing.T) {
	var opt Option
	if err := json.Unmarshal([]byte(`{
		"xAxis":{"type":"category","data":["A","B"]},
		"yAxis":{"type":"value"},
		"series":[{"type":"pictorialBar","symbol":"roundRect","symbolSize":[20,8],"symbolRepeat":true,"symbolMargin":2,"data":[3,5]}]
	}`), &opt); err != nil {
		t.Fatal(err)
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 400, H: 300})
	s := c.Series[0].(*PictorialBarSeries)
	if s.SymbolWidth != 20 || s.SymbolHeight != 8 || !s.Repeat || s.Margin != 2 {
		t.Fatalf("pictorial option = %+v", s)
	}
	rec := &qui.RecordingCanvas{}
	s.Draw(rec, c.PlotRect(), c.XAxis, c.YAxis, NoHotSpot)
	if len(rec.Rounds) < 2 {
		t.Fatalf("pictorial rounds = %d", len(rec.Rounds))
	}
}
