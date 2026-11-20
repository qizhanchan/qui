package qui

// Page zoom preview (RFC 0005 §3.3): when the page zoom changes, the UI
// thread shows the committed frame scaled by the change at once, and swaps
// in the page's own frame when the page has laid out at the new zoom — as
// Chrome does. The page may take a while (a big document, a busy script);
// the view never waits for it.

type zoomPreview struct {
	tok   uint64
	ratio float32
}

// PreviewZoom shows the page scaled by ratio (relative to what it shows
// now, about the view's top-left) until the page thread has run apply —
// which should change the page zoom — and committed a frame after it.
// Successive calls compose. UI thread.
func (v *PageView) PreviewZoom(ratio float32, apply func(w *Window)) {
	if v.host == nil || ratio <= 0 {
		return
	}
	v.markSeq++
	tok := v.markSeq
	if ratio != 1 {
		v.previews = append(v.previews, zoomPreview{tok: tok, ratio: ratio})
		v.Invalidate()
	}
	v.host.Do(func(w *Window) {
		if apply != nil {
			apply(w)
		}
		w.pageMark = tok
		// Nothing may change (zoom already at its limit): the new mark
		// alone still commits (commitLayersIfChanged compares Meta).
		w.Invalidate()
	})
}

// previewRatio is the scale the view applies to the committed frame.
func (v *PageView) previewRatio() float32 {
	z := float32(1)
	for _, p := range v.previews {
		z *= p.ratio
	}
	return z
}

// settlePreviews drops the previews a commit with mark has caught up with
// and reports whether any were dropped.
func (v *PageView) settlePreviews(mark uint64) bool {
	n := 0
	for n < len(v.previews) && v.previews[n].tok <= mark {
		n++
	}
	if n == 0 {
		return false
	}
	v.previews = v.previews[n:]
	return true
}

// drawPreview draws the committed frame scaled by z. Moved scroll layers
// and overlay draws are left out: they are placed for the unscaled frame,
// and the page replaces the preview shortly.
func (v *PageView) drawPreview(c Canvas, z float32) {
	b := v.Bounds()
	s := v.pageScale()
	fb := v.front.Bounds()
	c.Save()
	c.ClipRect(b)
	c.DrawImage(v.front, Rect{X: b.X, Y: b.Y, W: float32(fb.Dx()) / s * z, H: float32(fb.Dy()) / s * z})
	c.Restore()
}

// ZoomPreviewing reports whether a zoom preview is showing.
func (v *PageView) ZoomPreviewing() bool { return len(v.previews) > 0 }
