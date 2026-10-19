package chart

import (
	"encoding/json"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestGaugeGeometryAndHitTest(t *testing.T) {
	s := NewGaugeSeries("Speed", 72)
	plot := qui.Rect{W: 600, H: 400}
	start, end := s.screenAngles()
	if start != 135 || end != 405 {
		t.Fatalf("screen angles = %v,%v, want 135,405", start, end)
	}
	cx, cy := s.center(plot)
	r := s.radius(plot)
	if cx != 300 || cy != 200 || r != 150 {
		t.Fatalf("geometry = center(%v,%v) r=%v", cx, cy, r)
	}
	if got := s.HitTest(qui.Point{X: cx, Y: cy - r + 5}, plot, nil, nil); got != 0 {
		t.Fatalf("top arc hit = %d, want 0", got)
	}
	if got := s.HitTest(qui.Point{X: cx, Y: cy + r - 10}, plot, nil, nil); got != -1 {
		t.Fatalf("gap hit = %d, want -1", got)
	}
}

func TestGaugeDraw(t *testing.T) {
	s := NewGaugeSeries("Speed", 72)
	s.Title = "Speed"
	s.SetColor(qui.Color{R: 0.2, G: 0.4, B: 0.8, A: 1})
	rec := &qui.RecordingCanvas{}
	s.Draw(rec, qui.Rect{W: 600, H: 400}, nil, nil, NoHotSpot)
	if len(rec.Paths) != 2 {
		t.Fatalf("gauge paths = %d, want track + pointer", len(rec.Paths))
	}
	if len(rec.Texts) < 12 {
		t.Fatalf("gauge text draws = %d, want split labels + title + detail", len(rec.Texts))
	}
}

func TestGaugeOptionMapping(t *testing.T) {
	var opt Option
	err := json.Unmarshal([]byte(`{
		"series": [{
			"type": "gauge",
			"name": "Speed",
			"min": 20,
			"max": 120,
			"startAngle": 210,
			"endAngle": -30,
			"clockwise": false,
			"radius": "80%",
			"data": [{"name": "km/h", "value": 72}]
		}]
	}`), &opt)
	if err != nil {
		t.Fatal(err)
	}
	c := New(opt)
	if len(c.Series) != 1 {
		t.Fatalf("series count = %d", len(c.Series))
	}
	s := c.Series[0].(*GaugeSeries)
	if s.Min != 20 || s.Max != 120 || s.StartAngle != 210 || s.EndAngle != -30 || s.Clockwise {
		t.Fatalf("gauge option = %+v", s)
	}
	if s.RadiusPercent != .8 || s.Title != "km/h" {
		t.Fatalf("gauge radius/title = %v/%q", s.RadiusPercent, s.Title)
	}
}
