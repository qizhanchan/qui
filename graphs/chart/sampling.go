package chart

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// samplePoints reduces a point series to at most target samples. Modes:
// lttb (shape-preserving), min/max (preserve extrema per bucket), average
// (bucket mean), and none (identity). Unknown modes fall back to lttb.
func samplePoints(points []Point2D, target int, mode string) []Point2D {
	if mode == "none" || target <= 0 || len(points) <= target {
		return points
	}
	if target < 3 {
		target = 3
	}
	if len(points) <= target {
		return points
	}
	switch mode {
	case "min", "max", "average":
		return bucketSamplePoints(points, target, mode)
	default:
		return lttbSamplePoints(points, target)
	}
}

func lttbSamplePoints(points []Point2D, threshold int) []Point2D {
	if threshold >= len(points) || threshold < 3 {
		return points
	}
	out := make([]Point2D, 0, threshold)
	out = append(out, points[0])
	every := float64(len(points)-2) / float64(threshold-2)
	a := 0
	for i := 0; i < threshold-2; i++ {
		rangeStart := int(math.Floor(float64(i)*every)) + 1
		rangeEnd := int(math.Floor(float64(i+1)*every)) + 1
		if rangeEnd >= len(points)-1 {
			rangeEnd = len(points) - 2
		}
		if rangeStart > rangeEnd {
			rangeStart = rangeEnd
		}
		avgStart := int(math.Floor(float64(i+1)*every)) + 1
		avgEnd := int(math.Floor(float64(i+2)*every)) + 1
		if avgEnd > len(points)-1 {
			avgEnd = len(points) - 1
		}
		if avgStart > avgEnd {
			avgStart = avgEnd
		}
		var avgX, avgY float64
		for j := avgStart; j <= avgEnd; j++ {
			avgX += float64(points[j].X)
			avgY += float64(points[j].Y)
		}
		count := float64(avgEnd - avgStart + 1)
		if count > 0 {
			avgX /= count
			avgY /= count
		}
		pointA := points[a]
		maxArea := float64(-1)
		selected := rangeStart
		for j := rangeStart; j <= rangeEnd; j++ {
			area := math.Abs((float64(pointA.X)-avgX)*(float64(points[j].Y)-float64(pointA.Y)) -
				(float64(pointA.X)-float64(points[j].X))*(avgY-float64(pointA.Y)))
			if area > maxArea {
				maxArea = area
				selected = j
			}
		}
		out = append(out, points[selected])
		a = selected
	}
	out = append(out, points[len(points)-1])
	return out
}

func bucketSamplePoints(points []Point2D, target int, mode string) []Point2D {
	out := make([]Point2D, 0, target)
	out = append(out, points[0])
	buckets := target - 2
	step := float64(len(points)-2) / float64(buckets)
	for i := 0; i < buckets; i++ {
		start := int(math.Floor(float64(i)*step)) + 1
		end := int(math.Floor(float64(i+1)*step)) + 1
		if end > len(points)-1 {
			end = len(points) - 1
		}
		if start >= end {
			if start < len(points)-1 {
				out = append(out, points[start])
			}
			continue
		}
		switch mode {
		case "min", "max":
			selected := start
			for j := start + 1; j < end; j++ {
				if mode == "min" && points[j].Y < points[selected].Y {
					selected = j
				}
				if mode == "max" && points[j].Y > points[selected].Y {
					selected = j
				}
			}
			out = append(out, points[selected])
		case "average":
			var x, y float64
			for j := start; j < end; j++ {
				x += float64(points[j].X)
				y += float64(points[j].Y)
			}
			count := float64(end - start)
			out = append(out, Point2D{X: float32(x / count), Y: float32(y / count)})
		}
	}
	out = append(out, points[len(points)-1])
	return out
}

func samplingTarget(plot Rect) int {
	width := int(plot.W)
	if width < 2 {
		width = 2
	}
	if width > 4096 {
		width = 4096
	}
	return width * 2
}
