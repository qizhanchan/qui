package qui

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"sync/atomic"
	"time"
)

// The Page domain: the window as a page — screenshots, viewport metrics,
// bringing it to the front. Frame events (frameCommitted, windowResized)
// are emitted from the frame loop; see protocol_events.go.
//
// Load lifecycle, as Chrome reports it. A window has no document to parse
// or subresources to fetch, so the two milestones map onto the frame loop:
//
//	DOMContentLoaded — the first layout pass over a root at a non-empty
//	                   size: the tree exists and every node has bounds.
//	load             — the first frame painted after that.
//
// Both fire once per window, as Page.domContentEventFired / loadEventFired
// to sessions with Page enabled, and as Page.lifecycleEvent to those that
// also called Page.setLifecycleEventsEnabled. Enabling lifecycle events
// replays the milestones already reached, as Chrome does, so a client that
// attaches late still learns the window loaded. Agent.waitForReady is the
// same state for one-shot HTTP callers, which hear no events.

// pageLifecycle holds the milestone timestamps (protocol monotonic
// seconds as math.Float64bits; 0 = not reached).
type pageLifecycle struct {
	domContentLoaded atomic.Uint64
	load             atomic.Uint64
}

// protocolClock is the origin of the protocol's MonotonicTime.
var protocolClock = time.Now()

// monotonicSeconds is CDP's MonotonicTime: seconds on a clock that never
// goes back. Never 0, which pageLifecycle reserves for "not reached".
func monotonicSeconds() float64 {
	return math.Max(time.Since(protocolClock).Seconds(), 1e-9)
}

func lifecycleTime(v *atomic.Uint64) float64 { return math.Float64frombits(v.Load()) }

// ReadyState is the window's load state in document.readyState's terms:
// "loading" until the first layout, "interactive" until the first painted
// frame, then "complete". Safe from any goroutine.
func (w *Window) ReadyState() string {
	switch {
	case w == nil || w.lifecycle.domContentLoaded.Load() == 0:
		return "loading"
	case w.lifecycle.load.Load() == 0:
		return "interactive"
	default:
		return "complete"
	}
}

// noteLayoutLifecycle runs after each layout pass: the first one over a
// root at a real size is DOMContentLoaded.
func (w *Window) noteLayoutLifecycle() {
	if w.lifecycle.domContentLoaded.Load() != 0 || w.root == nil || w.lastSize.W <= 0 || w.lastSize.H <= 0 {
		return
	}
	t := monotonicSeconds()
	w.lifecycle.domContentLoaded.Store(math.Float64bits(t))
	w.emitLifecycle("DOMContentLoaded", t)
}

// notePaintLifecycle runs after each painted frame: the first one after
// DOMContentLoaded is load.
func (w *Window) notePaintLifecycle() {
	if w.lifecycle.load.Load() != 0 || w.lifecycle.domContentLoaded.Load() == 0 {
		return
	}
	t := monotonicSeconds()
	w.lifecycle.load.Store(math.Float64bits(t))
	w.emitLifecycle("load", t)
}

func (w *Window) emitLifecycle(name string, t float64) {
	if !ProtocolEventsWanted(w, "Page") {
		return
	}
	if name == "load" {
		EmitProtocolEvent(w, "Page.loadEventFired", pageTimestamp{Timestamp: t})
	} else {
		EmitProtocolEvent(w, "Page.domContentEventFired", pageTimestamp{Timestamp: t})
	}
	EmitProtocolEvent(w, "Page.lifecycleEvent", w.lifecycleEvent(name, t))
}

func (w *Window) lifecycleEvent(name string, t float64) pageLifecycleEvent {
	id := w.TargetID()
	return pageLifecycleEvent{FrameID: id, LoaderID: id, Name: name, Timestamp: t}
}

// LifecycleEventsToggle names the per-session switch
// Page.setLifecycleEventsEnabled flips (see ProtocolSessionToggles).
const LifecycleEventsToggle = "Page.lifecycleEvents"

type pageTimestamp struct {
	Timestamp float64 `json:"timestamp" desc:"MonotonicTime, seconds"`
}

type pageLifecycleEvent struct {
	FrameID   string  `json:"frameId" desc:"the window's targetId"`
	LoaderID  string  `json:"loaderId" desc:"the window's targetId (a window loads once)"`
	Name      string  `json:"name" desc:"DOMContentLoaded | load"`
	Timestamp float64 `json:"timestamp" desc:"MonotonicTime, seconds"`
}

type pageSetLifecycleParams struct {
	Enabled bool `json:"enabled"`
}

// maxScreenshotScale caps a screenshot's scale: the resample allocates
// W*scale × H*scale on the UI thread, so an unbounded scale is a one-request
// out-of-memory.
const maxScreenshotScale = 8

type pageClip struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
	Scale  float32 `json:"scale,omitempty" desc:"output scale for the clip; 1 = logical pixels"`
}

type pageAnnotate struct {
	OnlyLeaves    bool   `json:"onlyLeaves,omitempty"`
	OnlyFocusable bool   `json:"onlyFocusable,omitempty"`
	Highlight     string `json:"highlight,omitempty" desc:"selector whose matches get a heavier outline"`
}

type pageScreenshotParams struct {
	Format  string    `json:"format,omitempty" desc:"png (default) | jpeg"`
	Quality int       `json:"quality,omitempty" desc:"jpeg quality 0-100"`
	Clip    *pageClip `json:"clip,omitempty" desc:"capture a region, in logical window coordinates"`
	// qui extensions.
	Scale    float32       `json:"scale,omitempty" desc:"qui: whole-window scale, 0.5 halves token cost; 1 = logical, 2 = Retina physical. Default 1"`
	Annotate *pageAnnotate `json:"annotate,omitempty" desc:"qui: overlay node bounds and @ref/role/name labels, so one image shows the UI and how to address it"`
}

type pageScreenshotResult struct {
	Data   string `json:"data" desc:"base64-encoded image"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type pageViewport struct {
	PageX        float32 `json:"pageX"`
	PageY        float32 `json:"pageY"`
	ClientWidth  float32 `json:"clientWidth"`
	ClientHeight float32 `json:"clientHeight"`
}

type pageVisualViewport struct {
	OffsetX      float32 `json:"offsetX"`
	OffsetY      float32 `json:"offsetY"`
	PageX        float32 `json:"pageX"`
	PageY        float32 `json:"pageY"`
	ClientWidth  float32 `json:"clientWidth"`
	ClientHeight float32 `json:"clientHeight"`
	Scale        float32 `json:"scale"`
}

type pageRect struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

type pageLayoutMetrics struct {
	CSSLayoutViewport pageViewport       `json:"cssLayoutViewport"`
	CSSVisualViewport pageVisualViewport `json:"cssVisualViewport"`
	CSSContentSize    pageRect           `json:"cssContentSize"`
	LayoutViewport    pageViewport       `json:"layoutViewport"`
	VisualViewport    pageVisualViewport `json:"visualViewport"`
	ContentSize       pageRect           `json:"contentSize"`
	DevicePixelRatio  float32            `json:"devicePixelRatio" desc:"qui: physical pixels per logical pixel"`
	Zoom              float32            `json:"zoom" desc:"qui: window viewport zoom"`
}

// CaptureScreenshot is the Page.captureScreenshot primitive: it returns the
// encoded image bytes. Safe from any goroutine.
func (w *Window) captureScreenshot(p pageScreenshotParams) ([]byte, image.Rectangle, error) {
	scale := p.Scale
	if scale == 0 {
		scale = 1
	}
	if !(scale > 0 && scale <= maxScreenshotScale) {
		return nil, image.Rectangle{}, NewProtocolError("invalid_params", "scale must be in (0, %d]", maxScreenshotScale)
	}
	var img *image.RGBA
	switch {
	case p.Clip != nil:
		cs := p.Clip.Scale
		if cs == 0 {
			cs = scale
		}
		if !(cs > 0 && cs <= maxScreenshotScale) {
			return nil, image.Rectangle{}, NewProtocolError("invalid_params", "clip.scale must be in (0, %d]", maxScreenshotScale)
		}
		img = w.SnapshotRegion(Rect{X: p.Clip.X, Y: p.Clip.Y, W: p.Clip.Width, H: p.Clip.Height}, cs)
	case p.Annotate != nil:
		img = w.SnapshotAnnotated(AnnotateOptions{
			Scale:         scale,
			OnlyLeaves:    p.Annotate.OnlyLeaves,
			OnlyFocusable: p.Annotate.OnlyFocusable,
			Highlight:     p.Annotate.Highlight,
		})
	default:
		img = w.SnapshotScaled(scale)
	}
	if img == nil {
		return nil, image.Rectangle{}, NewProtocolError("error", "no frame available yet")
	}
	var buf bytes.Buffer
	var err error
	switch p.Format {
	case "", "png":
		err = png.Encode(&buf, img)
	case "jpeg", "jpg":
		q := p.Quality
		if q <= 0 || q > 100 {
			q = 80
		}
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: q})
	default:
		return nil, image.Rectangle{}, NewProtocolError("invalid_params", "format must be png or jpeg, got %q", p.Format)
	}
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	return buf.Bytes(), img.Bounds(), nil
}

func pageDomain() ProtocolDomain {
	return ProtocolDomain{
		Name:        "Page",
		Description: "The window as a page: screenshots, viewport metrics, focus, frame events and the load lifecycle (events go only to sessions that Page.enable).",
		Commands: []ProtocolCommand{
			NewProtocolCommand("captureScreenshot", "Capture the window (or a clip of it) at the end of the next frame, so it reflects every queued change.",
				func(ctx *ProtocolContext, p pageScreenshotParams) (pageScreenshotResult, error) {
					data, b, err := ctx.Window.captureScreenshot(p)
					if err != nil {
						return pageScreenshotResult{}, err
					}
					return pageScreenshotResult{Data: base64.StdEncoding.EncodeToString(data), Width: b.Dx(), Height: b.Dy()}, nil
				}),
			NewProtocolCommand("getLayoutMetrics", "Viewport and content size in logical pixels.",
				func(ctx *ProtocolContext, _ empty) (pageLayoutMetrics, error) {
					var m pageLayoutMetrics
					err := ctx.Window.synchronously(func() error {
						b := ctx.Window.Bounds()
						vp := pageViewport{ClientWidth: b.W, ClientHeight: b.H}
						vv := pageVisualViewport{ClientWidth: b.W, ClientHeight: b.H, Scale: 1}
						cs := pageRect{Width: b.W, Height: b.H}
						m = pageLayoutMetrics{
							CSSLayoutViewport: vp, CSSVisualViewport: vv, CSSContentSize: cs,
							LayoutViewport: vp, VisualViewport: vv, ContentSize: cs,
							DevicePixelRatio: ctx.Window.DevicePixelRatio(),
							Zoom:             ctx.Window.Zoom(),
						}
						return nil
					})
					return m, err
				}),
			NewProtocolCommand("bringToFront", "Raise the window and give it key focus.",
				func(ctx *ProtocolContext, _ empty) (empty, error) {
					return empty{}, ctx.Window.synchronously(func() error { ctx.Window.Raise(); return nil })
				}),
			NewProtocolCommand("setLifecycleEventsEnabled", "Send Page.lifecycleEvent to this session. Enabling replays the milestones already reached, as Chrome does.",
				func(ctx *ProtocolContext, p pageSetLifecycleParams) (empty, error) {
					if t, ok := ctx.Session.(ProtocolSessionToggles); ok {
						t.SetToggle(LifecycleEventsToggle, p.Enabled)
					}
					if !p.Enabled || ctx.Session == nil {
						return empty{}, nil
					}
					w := ctx.Window
					if t := lifecycleTime(&w.lifecycle.domContentLoaded); t != 0 {
						ctx.Session.Emit("Page.lifecycleEvent", w.lifecycleEvent("DOMContentLoaded", t))
					}
					if t := lifecycleTime(&w.lifecycle.load); t != 0 {
						ctx.Session.Emit("Page.lifecycleEvent", w.lifecycleEvent("load", t))
					}
					return empty{}, nil
				}),
		},
		Events: []ProtocolEvent{
			NewProtocolEvent[pageFrameCommitted]("frameCommitted", "A frame was painted (only while a session has Page enabled)."),
			NewProtocolEvent[pageWindowResized]("windowResized", "The window's logical size changed."),
			NewProtocolEvent[pageTimestamp]("domContentEventFired", "The first layout over the window's root (once per window)."),
			NewProtocolEvent[pageTimestamp]("loadEventFired", "The first frame painted after DOMContentLoaded (once per window)."),
			NewProtocolEvent[pageLifecycleEvent]("lifecycleEvent", "A load milestone (DOMContentLoaded, load); only to sessions that called setLifecycleEventsEnabled."),
		},
	}
}

type pageFrameCommitted struct {
	Frame       uint64   `json:"frame" desc:"frame counter"`
	DirtyRect   pageRect `json:"dirtyRect" desc:"the repainted region"`
	FullRepaint bool     `json:"fullRepaint,omitempty"`
}

type pageWindowResized struct {
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}
