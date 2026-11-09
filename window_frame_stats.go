package qui

import (
	"slices"
	"sync"
	"time"
)

// Frame timing — the baseline every rendering optimization is measured
// against. Each painted frame records how long its phases took and how
// much it repainted; the Performance protocol domain reads them back
// (Performance.getFrames), so a benchmark script can drive the UI over the
// agent channel and get numbers without instrumenting the app.

// FrameTiming describes one frame Window.Step produced.
type FrameTiming struct {
	// Start is when the frame's Step began.
	Start time.Time `json:"start"`
	// Layout is the measure + layout pass (0 when nothing was dirty).
	Layout time.Duration `json:"layoutNs"`
	// Draw is the widget-tree rasterization into the renderer's canvas.
	Draw time.Duration `json:"drawNs"`
	// Present is the renderer finishing the frame and handing it to the
	// compositor: texture upload, GPU composite, buffer swap / drawable
	// present.
	Present time.Duration `json:"presentNs"`
	// Total is Start to the end of presentation.
	Total time.Duration `json:"totalNs"`
	// Dirty is the repainted area and Full the window's, both in
	// physical pixels.
	Dirty int `json:"dirtyPx"`
	Full  int `json:"fullPx"`
	// Scrolled is the area whose pixels were moved instead of repainted
	// (Window.ScrollPixels), physical pixels.
	Scrolled int `json:"scrolledPx,omitempty"`
	// RedrawOnly marks a RequestGLRedraw frame (no rasterization).
	RedrawOnly bool `json:"redrawOnly,omitempty"`
}

// frameStatsCap bounds the ring of recent frames.
const frameStatsCap = 512

// frameStats is the ring of recent FrameTimings; written on the UI
// thread, read from protocol handlers.
type frameStats struct {
	mu     sync.Mutex
	frames []FrameTiming
	next   int
}

func (s *frameStats) add(f FrameTiming) {
	s.mu.Lock()
	if len(s.frames) < frameStatsCap {
		s.frames = append(s.frames, f)
	} else {
		s.frames[s.next] = f
		s.next = (s.next + 1) % frameStatsCap
	}
	s.mu.Unlock()
}

// recent returns up to limit frames, oldest first (limit <= 0: all).
func (s *frameStats) recent(limit int) []FrameTiming {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FrameTiming, 0, len(s.frames))
	out = append(out, s.frames[s.next:]...)
	out = append(out, s.frames[:s.next]...)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (s *frameStats) clear() {
	s.mu.Lock()
	s.frames, s.next = nil, 0
	s.mu.Unlock()
}

// RecentFrames returns the timings of up to limit recent frames, oldest
// first (limit <= 0: all that are kept). Safe from any goroutine.
func (w *Window) RecentFrames(limit int) []FrameTiming {
	if w == nil {
		return nil
	}
	return w.frameStats.recent(limit)
}

// ClearFrameTimings forgets the recorded frames. Safe from any goroutine.
func (w *Window) ClearFrameTimings() {
	if w != nil {
		w.frameStats.clear()
	}
}

// FrameSummary aggregates FrameTimings.
type FrameSummary struct {
	Frames int `json:"frames"`
	// Percentiles of Total, Draw and Present.
	TotalP50   time.Duration `json:"totalP50Ns"`
	TotalP95   time.Duration `json:"totalP95Ns"`
	TotalMax   time.Duration `json:"totalMaxNs"`
	DrawP50    time.Duration `json:"drawP50Ns"`
	DrawP95    time.Duration `json:"drawP95Ns"`
	PresentP50 time.Duration `json:"presentP50Ns"`
	PresentP95 time.Duration `json:"presentP95Ns"`
	// DirtyShare is repainted pixels over window pixels, summed over the
	// frames (1 = every frame repainted everything).
	DirtyShare float64 `json:"dirtyShare"`
}

// SummarizeFrames aggregates frames.
func SummarizeFrames(frames []FrameTiming) FrameSummary {
	s := FrameSummary{Frames: len(frames)}
	if len(frames) == 0 {
		return s
	}
	pick := func(get func(FrameTiming) time.Duration) (p50, p95, max time.Duration) {
		v := make([]time.Duration, len(frames))
		for i, f := range frames {
			v[i] = get(f)
		}
		slices.Sort(v)
		at := func(q float64) time.Duration { return v[min(len(v)-1, int(q*float64(len(v))))] }
		return at(0.5), at(0.95), v[len(v)-1]
	}
	s.TotalP50, s.TotalP95, s.TotalMax = pick(func(f FrameTiming) time.Duration { return f.Total })
	s.DrawP50, s.DrawP95, _ = pick(func(f FrameTiming) time.Duration { return f.Draw })
	s.PresentP50, s.PresentP95, _ = pick(func(f FrameTiming) time.Duration { return f.Present })
	var dirty, full float64
	for _, f := range frames {
		dirty += float64(f.Dirty)
		full += float64(f.Full)
	}
	if full > 0 {
		s.DirtyShare = dirty / full
	}
	return s
}

type perfFramesParams struct {
	Limit int `json:"limit,omitempty" desc:"most recent N frames (default: all kept, up to 512)"`
}

type perfFramesResult struct {
	Frames  []FrameTiming `json:"frames"`
	Summary FrameSummary  `json:"summary"`
}

func performanceDomain() ProtocolDomain {
	return ProtocolDomain{
		Name:        "Performance",
		Description: "Frame timings: per-frame layout / draw / present durations and repainted area, for benchmarks driven over the agent channel.",
		Commands: []ProtocolCommand{
			NewProtocolCommand("getFrames", "Recent painted frames, oldest first, with a summary (percentiles, repainted share).",
				func(ctx *ProtocolContext, p perfFramesParams) (perfFramesResult, error) {
					f := ctx.Window.RecentFrames(p.Limit)
					if f == nil {
						f = []FrameTiming{}
					}
					return perfFramesResult{Frames: f, Summary: SummarizeFrames(f)}, nil
				}),
			NewProtocolCommand("clearFrames", "Forget recorded frames (start a measurement).",
				func(ctx *ProtocolContext, _ empty) (empty, error) {
					ctx.Window.ClearFrameTimings()
					return empty{}, nil
				}),
		},
	}
}
