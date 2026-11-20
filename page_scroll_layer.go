package qui

import (
	"image"
	"sort"
)

// Scroll layers (RFC 0005 §3.3, §8.2): the part of a page commit that lets
// the UI thread scroll without the page thread.
//
// A scroller whose content is kept as rasterized tiles (a ScrollView with a
// content cache: PixelScroll + FixedViewport) commits those tiles. Tiles are
// copy-on-write — once shown they are never drawn into again — so the UI
// thread can hold and composite them while the page thread goes on. A wheel
// over such a layer moves the tiles on the UI thread at once and tells the
// page the new offset; the page catches up on its next frame.
//
// Every other scroller that can scroll commits only its viewport, as a
// region the UI thread must leave to the page: a wheel there is forwarded,
// because only the page knows what it scrolls.

// ScrollLayerFrame is one scroller's committed state. Rectangles are in the
// page window's device pixels.
type ScrollLayerFrame struct {
	// Key identifies the scroller across commits. The host only compares
	// it and hands it back in SetScrollLayerOffset.
	Key any
	// Depth orders nested scrollers: an ancestor has a smaller depth.
	Depth int
	// Viewport is where the scroller shows its content.
	Viewport image.Rectangle
	// Offset is the scroll offset (logical points) the frame shows, Max the
	// largest offsets it can scroll to.
	Offset, Max Point
	// Composited reports that Tiles hold the content: the UI thread can
	// scroll it. A scroller that is not composited is a slow region.
	Composited bool
	// Tiles is the content, TileSize device pixels square, by tile index
	// in content device space. Origin maps it to the window: content
	// device point p shows at window device point p - Origin at Offset.
	Tiles    map[image.Point]*image.RGBA
	TileSize int
	Origin   image.Point
	// Covers are what paints over the content at fixed window places
	// (fixed / sticky boxes, animated layers, scrollbars): the UI thread
	// copies them from the frame while it shows the content moved.
	Covers []image.Rectangle
	// Background fills the viewport under the content.
	Background Color
	// Scale is device pixels per logical point; WheelStep and PreciseScale
	// turn notched and precise wheel deltas into logical points, as the
	// scroller itself would.
	Scale        float32
	WheelStep    float32
	PreciseScale float32
	// Applied is the last SetScrollLayerOffset sequence number the page
	// had applied when it committed this frame.
	Applied uint64
}

// ScrollLayerSource is implemented by scrollers that take part in page
// commits (widgets.ScrollView).
type ScrollLayerSource interface {
	Widget
	// ScrollLayer reports the scroller's state for a commit; ok is false
	// when it cannot scroll (or is not showing).
	ScrollLayer() (f ScrollLayerFrame, ok bool)
	// ScrollLayerTo scrolls to the given logical offsets, as a user scroll
	// on the UI thread already did.
	ScrollLayerTo(x, y float32)
}

// scrollLayerState is the page window's view of its scrollers.
type scrollLayerState struct {
	sources map[ScrollLayerSource]struct{}
	applied map[any]uint64
}

// WantsScrollLayers reports whether w commits scroll layers: true for a
// page window (PageHost). Scrollers check it before doing commit work.
func (w *Window) WantsScrollLayers() bool { return w != nil && w.plat != nil && isEmbeddedPlatform(w.plat) }

// RegisterScrollLayer adds s to the scrollers w commits. Scrollers call it
// when laid out in a page window; detached ones drop out by themselves.
// UI thread of w.
func (w *Window) RegisterScrollLayer(s ScrollLayerSource) {
	if !w.WantsScrollLayers() || s == nil {
		return
	}
	if w.scrollLayers.sources == nil {
		w.scrollLayers.sources = map[ScrollLayerSource]struct{}{}
	}
	w.scrollLayers.sources[s] = struct{}{}
}

// applyScrollLayerOffset applies a UI-thread scroll to the page.
func (w *Window) applyScrollLayerOffset(key any, x, y float32, seq uint64) {
	src, ok := key.(ScrollLayerSource)
	if !ok {
		return
	}
	if _, live := w.scrollLayers.sources[src]; !live || widgetWindow(src) != w {
		return
	}
	src.ScrollLayerTo(x, y)
	if w.scrollLayers.applied == nil {
		w.scrollLayers.applied = map[any]uint64{}
	}
	if seq > w.scrollLayers.applied[key] {
		w.scrollLayers.applied[key] = seq
	}
}

// collectScrollLayers snapshots the scrollers for a commit, ancestors
// first.
func (w *Window) collectScrollLayers() []ScrollLayerFrame {
	if len(w.scrollLayers.sources) == 0 {
		return nil
	}
	var out []ScrollLayerFrame
	for s := range w.scrollLayers.sources {
		if widgetWindow(s) != w || !widgetShowing(s) {
			delete(w.scrollLayers.sources, s)
			delete(w.scrollLayers.applied, any(s))
			continue
		}
		f, ok := s.ScrollLayer()
		if !ok {
			continue
		}
		f.Key = s
		f.Depth = widgetDepth(s)
		f.Applied = w.scrollLayers.applied[any(s)]
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		a, b := out[i].Viewport, out[j].Viewport
		if a.Min.Y != b.Min.Y {
			return a.Min.Y < b.Min.Y
		}
		return a.Min.X < b.Min.X
	})
	return out
}

func widgetDepth(w Widget) int {
	d := 0
	for p := w.Parent(); p != nil; p = p.Parent() {
		d++
	}
	return d
}

// widgetShowing reports whether w has a size and no ancestor (or w) is
// collapsed or visibility-hidden.
func widgetShowing(w Widget) bool {
	if b := w.Bounds(); b.W <= 0 || b.H <= 0 {
		return false
	}
	for cur := w; cur != nil; cur = cur.Parent() {
		if c, ok := cur.(interface{ Collapsed() bool }); ok && c.Collapsed() {
			return false
		}
		if h, ok := cur.(VisibilityHider); ok && h.VisibilityHidden() {
			return false
		}
	}
	return true
}
