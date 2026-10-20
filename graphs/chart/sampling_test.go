package chart

import (
	"fmt"
	"image"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestSamplePointsPreservesEndpointsAndTarget(t *testing.T) {
	points := make([]Point2D, 1000)
	for i := range points {
		points[i] = Point2D{X: float32(i), Y: float32((i*37)%101 + i%7)}
	}
	sampled := samplePoints(points, 100, "lttb")
	if len(sampled) > 100 {
		t.Fatalf("sampled length = %d, want <= 100", len(sampled))
	}
	if sampled[0] != points[0] || sampled[len(sampled)-1] != points[len(points)-1] {
		t.Fatalf("endpoints not preserved: first %+v last %+v", sampled[0], sampled[len(sampled)-1])
	}
}

func TestSampledLineDrawReducesPolylineSize(t *testing.T) {
	animation := false
	data := make([]DataPoint, 10000)
	for i := range data {
		data[i] = XYPoint(float64(i), float64((i*13)%97))
	}
	c := New(Option{
		Animation: &animation,
		XAxis:     AxisOption{Type: AxisValue},
		YAxis:     AxisOption{Type: AxisValue},
		Series: []SeriesOption{{
			Type: SeriesLine, Sampling: "lttb", LargeThreshold: 100, Data: data,
		}},
	})
	c.Layout(qui.Rect{W: 640, H: 360})
	rec := &qui.RecordingCanvas{}
	c.Draw(rec)
	if len(rec.Polylines) == 0 {
		t.Fatal("sampled line was not drawn")
	}
	if got := len(rec.Polylines[0]); got > 1300 {
		t.Fatalf("sampled polyline has %d points, want at most two per plot pixel", got)
	}
}

func TestSamplingModesSelectBucketExtrema(t *testing.T) {
	points := []Point2D{{0, 5}, {1, 1}, {2, 9}, {3, 3}, {4, 7}, {5, 2}, {6, 8}, {7, 4}, {8, 6}}
	min := samplePoints(points, 5, "min")
	max := samplePoints(points, 5, "max")
	if len(min) > 5 || len(max) > 5 {
		t.Fatalf("bucket lengths = %d/%d", len(min), len(max))
	}
	if min[0] != points[0] || min[len(min)-1] != points[len(points)-1] {
		t.Fatalf("min endpoints = %+v", min)
	}
}

func BenchmarkChartLineDraw(b *testing.B) {
	for _, size := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("points-%d", size), func(b *testing.B) {
			data := make([]DataPoint, size)
			for i := range data {
				data[i] = XYPoint(float64(i), float64((i*13)%97))
			}
			animation := false
			c := New(Option{
				Animation: &animation,
				XAxis:     AxisOption{Type: AxisValue},
				YAxis:     AxisOption{Type: AxisValue},
				Series: []SeriesOption{{
					Type: SeriesLine, Sampling: "lttb", LargeThreshold: 2000, Data: data,
				}},
			})
			c.Layout(qui.Rect{W: 800, H: 400})
			img := image.NewRGBA(image.Rect(0, 0, 800, 400))
			canvas := qui.NewImageCanvas(img)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.Draw(canvas)
			}
		})
	}
}
