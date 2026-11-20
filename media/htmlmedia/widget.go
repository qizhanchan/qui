package htmlmedia

import (
	"fmt"
	"image"
	"math"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/media"
)

// mediaWidget is the replaced content of a <video> / <audio>: the video
// picture (a media.VideoView child), the poster, and the native controls
// drawn on top.
type mediaWidget struct {
	*qui.Container
	m  *Element
	vv *media.VideoView

	hover     bool
	lastMove  time.Time
	scrubbing bool
	hoverPart part
	poster    image.Image
	posterSrc string
}

// part is a hit region of the controls.
type part int

const (
	partNone part = iota
	partPlay
	partTimeline
	partMute
	partFullscreen
	partSurface // the picture itself (click toggles playback)
)

// fillLayout gives the video view the whole box.
type fillLayout struct{}

func (fillLayout) Apply(children []qui.Widget, b qui.Rect) {
	for _, c := range children {
		c.Layout(b)
	}
}

func newMediaWidget(m *Element) *mediaWidget {
	w := &mediaWidget{Container: qui.NewContainer(fillLayout{}), m: m}
	w.SetSelf(w)
	// The picture fills the element's content box: no theme padding.
	qui.UpdateStyle(w, func(st *qui.Style) { st.Padding = qui.Insets{} })
	return w
}

func (w *mediaWidget) setVideo(vv *media.VideoView) {
	if w.vv != nil {
		w.RemoveChild(w.vv)
	}
	w.vv = vv
	if vv != nil {
		w.AddChild(vv)
	}
	w.InvalidateLayout()
}

// SetWindow runs jobs posted while detached, and pauses playback when the
// element leaves the window (a removed media element pauses).
func (w *mediaWidget) SetWindow(win *qui.Window) {
	w.Container.SetWindow(win)
	if win != nil {
		w.m.runPending(win)
		return
	}
	if !w.m.paused {
		w.m.Pause()
	}
}

func (w *mediaWidget) controls() bool {
	_, ok := w.m.el.Attr("controls")
	return ok
}

// Measure: the default object size when the element has no intrinsic size
// yet — 300×150 for <video>, Chrome's 300×54 control bar for <audio>.
func (w *mediaWidget) Measure(avail qui.Size) qui.Size {
	if !w.m.video {
		return qui.Size{W: 300, H: 54}
	}
	if w.m.vw > 0 && w.m.vh > 0 {
		return qui.Size{W: float32(w.m.vw), H: float32(w.m.vh)}
	}
	return qui.Size{W: 300, H: 150}
}

func (w *mediaWidget) HitTest(p qui.Point) qui.Widget {
	if !w.Bounds().Contains(p) {
		return nil
	}
	return w
}

func (w *mediaWidget) Tick(now time.Time) qui.Rect {
	dirty := w.Container.Tick(now)
	w.m.tick(now)
	if !w.m.paused && w.controls() && w.controlsVisible(now) {
		// The time readout and the timeline advance.
		dirty = dirty.Union(w.Bounds())
	}
	return dirty
}

// controlsVisible: always while paused or not yet playing; while playing
// only for 2.5 s after the pointer last moved over the element.
func (w *mediaWidget) controlsVisible(now time.Time) bool {
	if !w.m.video {
		return true
	}
	if w.m.paused || w.scrubbing || w.m.readyState < HaveMetadata {
		return true
	}
	return w.hover && now.Sub(w.lastMove) < 2500*time.Millisecond
}

// ---- Geometry ----

type layout struct {
	bar, play, mute, full, track, text qui.Rect
	trackY                             float32
}

func (w *mediaWidget) geometry() layout {
	b := w.Bounds()
	var g layout
	if !w.m.video {
		// Audio: a 54px pill: play, time, timeline, mute.
		g.bar = b
		cy := b.Y + b.H/2
		g.play = qui.Rect{X: b.X + 12, Y: cy - 16, W: 32, H: 32}
		g.text = qui.Rect{X: g.play.X + g.play.W + 4, Y: cy - 10, W: 92, H: 20}
		g.mute = qui.Rect{X: b.X + b.W - 44, Y: cy - 16, W: 32, H: 32}
		g.track = qui.Rect{X: g.text.X + g.text.W + 8, Y: cy - 8, W: max(g.mute.X-8-(g.text.X+g.text.W+8), 0), H: 16}
		g.trackY = cy
		return g
	}
	const rowH = 40
	g.bar = qui.Rect{X: b.X, Y: b.Y + b.H - 72, W: b.W, H: 72}
	row := b.Y + b.H - rowH
	g.play = qui.Rect{X: b.X + 8, Y: row + 4, W: 32, H: 32}
	g.text = qui.Rect{X: g.play.X + 40, Y: row + 10, W: 120, H: 20}
	g.full = qui.Rect{X: b.X + b.W - 40, Y: row + 4, W: 32, H: 32}
	g.mute = qui.Rect{X: g.full.X - 36, Y: row + 4, W: 32, H: 32}
	g.trackY = row - 6
	g.track = qui.Rect{X: b.X + 16, Y: g.trackY - 8, W: max(b.W-32, 0), H: 16}
	return g
}

func (w *mediaWidget) partAt(p qui.Point) part {
	if !w.controls() {
		return partNone
	}
	g := w.geometry()
	switch {
	case g.play.Contains(p):
		return partPlay
	case g.track.Contains(p):
		return partTimeline
	case g.mute.Contains(p):
		return partMute
	case w.m.video && g.full.Contains(p) && fullscreenHandler != nil:
		return partFullscreen
	}
	if w.m.video {
		return partSurface
	}
	return partNone
}

// ---- Events ----

func (w *mediaWidget) Handle(ev qui.Event) bool {
	me, ok := ev.(qui.MouseEvent)
	if !ok {
		return false
	}
	p := qui.Point{X: me.X, Y: me.Y}
	switch me.Type() {
	case qui.EventMouseEnter:
		w.hover, w.lastMove = true, time.Now()
		w.Invalidate()
	case qui.EventMouseLeave:
		w.hover = false
		w.hoverPart = partNone
		w.Invalidate()
	case qui.EventMouseMove:
		w.lastMove = time.Now()
		if hp := w.partAt(p); hp != w.hoverPart {
			w.hoverPart = hp
		}
		if w.scrubbing {
			w.seekTo(p.X)
		}
		if w.controls() {
			w.Invalidate()
		}
	case qui.EventMouseDown:
		if !w.controls() || me.Button != qui.MouseButtonLeft {
			return false
		}
		NoteUserActivation(w.m.el)
		switch w.partAt(p) {
		case partTimeline:
			w.scrubbing = true
			w.seekTo(p.X)
			return true
		case partPlay, partMute, partFullscreen:
			return true
		}
	case qui.EventMouseUp:
		if w.scrubbing {
			w.scrubbing = false
			w.Invalidate()
			return true
		}
		if !w.controls() || me.Button != qui.MouseButtonLeft || me.AfterDrag {
			return false
		}
		switch w.partAt(p) {
		case partPlay, partSurface:
			w.togglePlay()
			return w.partAt(p) == partPlay
		case partMute:
			w.m.SetMuted(!w.m.muted)
			return true
		case partFullscreen:
			fullscreenHandler(w.m)
			return true
		}
	}
	return false
}

func (w *mediaWidget) togglePlay() {
	if w.m.paused || w.m.ended {
		w.m.Play(true, nil)
	} else {
		w.m.Pause()
	}
}

func (w *mediaWidget) seekTo(x float32) {
	d := w.m.duration
	if math.IsNaN(d) || d <= 0 {
		return
	}
	g := w.geometry()
	f := (x - g.track.X) / max(g.track.W, 1)
	f = min(max(f, 0), 1)
	w.m.SetCurrentTime(float64(f) * d)
}

// fullscreenHandler is installed by the host (SetFullscreenHandler).
var fullscreenHandler func(*Element)

// SetFullscreenHandler installs what the controls' fullscreen button does
// (a browser's Fullscreen API). Without one the button is hidden.
func SetFullscreenHandler(fn func(*Element)) { fullscreenHandler = fn }

// ---- Drawing ----

var (
	white      = qui.Color{R: 1, G: 1, B: 1, A: 1}
	audioBg    = qui.Color{R: 0xf1 / 255.0, G: 0xf3 / 255.0, B: 0xf4 / 255.0, A: 1}
	audioInk   = qui.Color{R: 0x20 / 255.0, G: 0x21 / 255.0, B: 0x24 / 255.0, A: 1}
	trackBase  = qui.Color{R: 1, G: 1, B: 1, A: 0.3}
	trackBuf   = qui.Color{R: 1, G: 1, B: 1, A: 0.5}
	audioTrack = qui.Color{R: 0, G: 0, B: 0, A: 0.15}
	audioBuf   = qui.Color{R: 0, G: 0, B: 0, A: 0.3}
)

func (w *mediaWidget) Draw(c qui.Canvas) {
	b := w.Bounds()
	m := w.m
	if m.video {
		// Like Chrome, a <video> paints only its picture (or poster): before
		// the first frame and around a letterboxed one the page shows through.
		if w.vv != nil && !(m.showPoster && w.posterImage() != nil) {
			w.Container.Draw(c)
		}
		if m.showPoster {
			if img := w.posterImage(); img != nil {
				ib := img.Bounds()
				dst := fitContain(b, float32(ib.Dx()), float32(ib.Dy()))
				c.DrawImage(img, dst)
			}
		}
		if w.controls() && w.controlsVisible(time.Now()) {
			w.drawVideoControls(c)
		}
		return
	}
	if w.controls() {
		w.drawAudioControls(c)
	}
}

// posterImage loads the poster attribute through the document's loader.
func (w *mediaWidget) posterImage() image.Image {
	src, ok := w.m.el.Attr("poster")
	if !ok || src == "" {
		return nil
	}
	if src == w.posterSrc {
		return w.poster
	}
	eng := w.m.el.Engine()
	if eng == nil || eng.Resources() == nil {
		return nil
	}
	res := eng.Resources().Image(src, func() {
		w.posterSrc = ""
		w.Invalidate()
	})
	if res.Pending {
		return nil
	}
	w.posterSrc = src
	if res.Raster != nil {
		w.poster = res.Raster
	} else if res.Vector != nil {
		w.poster = res.Vector.Rasterize(int(max(res.Vector.Width, 300)), int(max(res.Vector.Height, 150)), qui.Color{})
	}
	return w.poster
}

func fitContain(b qui.Rect, iw, ih float32) qui.Rect {
	if iw <= 0 || ih <= 0 {
		return b
	}
	s := min(b.W/iw, b.H/ih)
	w, h := iw*s, ih*s
	return qui.Rect{X: b.X + (b.W-w)/2, Y: b.Y + (b.H-h)/2, W: w, H: h}
}

func (w *mediaWidget) drawVideoControls(c qui.Canvas) {
	g := w.geometry()
	// Scrim: a transparent → black gradient under the controls.
	c.DrawShape(qui.ShapeRect(g.bar), qui.Paint{Shader: qui.LinearGradient{
		Start: qui.Point{X: g.bar.X, Y: g.bar.Y},
		End:   qui.Point{X: g.bar.X, Y: g.bar.Y + g.bar.H},
		Stops: []qui.GradientStop{{Offset: 0, Color: qui.Color{}}, {Offset: 1, Color: qui.Color{A: 0.6}}},
	}})
	w.drawPlayIcon(c, g.play, white)
	w.drawTime(c, g.text, white, 13)
	w.drawSpeaker(c, g.mute, white)
	if fullscreenHandler != nil {
		drawFullscreenIcon(c, g.full, white)
	}
	w.drawTimeline(c, g, white, trackBase, trackBuf)
}

func (w *mediaWidget) drawAudioControls(c qui.Canvas) {
	g := w.geometry()
	c.FillRoundedRect(g.bar, min(g.bar.H/2, 27), audioBg)
	w.drawPlayIcon(c, g.play, audioInk)
	w.drawTime(c, g.text, audioInk, 13)
	w.drawSpeaker(c, g.mute, audioInk)
	w.drawTimeline(c, g, audioInk, audioTrack, audioBuf)
}

func (w *mediaWidget) drawTimeline(c qui.Canvas, g layout, played, base, buf qui.Color) {
	t := g.track
	if t.W <= 0 {
		return
	}
	h := float32(4)
	line := qui.Rect{X: t.X, Y: g.trackY - h/2, W: t.W, H: h}
	c.FillRoundedRect(line, h/2, base)
	d := w.m.duration
	if math.IsNaN(d) || d <= 0 {
		return
	}
	for _, r := range w.m.Buffered() {
		x0 := line.X + line.W*float32(r.Start/d)
		x1 := line.X + line.W*float32(min(r.End/d, 1))
		if x1 > x0 {
			c.FillRoundedRect(qui.Rect{X: x0, Y: line.Y, W: x1 - x0, H: h}, h/2, buf)
		}
	}
	f := float32(min(max(w.m.CurrentTime()/d, 0), 1))
	c.FillRoundedRect(qui.Rect{X: line.X, Y: line.Y, W: line.W * f, H: h}, h/2, played)
	if w.hoverPart == partTimeline || w.scrubbing || !w.m.video {
		x := line.X + line.W*f
		r := float32(6)
		c.FillRoundedRect(qui.Rect{X: x - r, Y: g.trackY - r, W: 2 * r, H: 2 * r}, r, played)
	}
}

func (w *mediaWidget) drawPlayIcon(c qui.Canvas, r qui.Rect, ink qui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	if w.m.paused || w.m.ended {
		p := qui.NewPath().MoveTo(cx-5, cy-8).LineTo(cx+8, cy).LineTo(cx-5, cy+8).Close()
		c.DrawShape(qui.ShapePath{Path: p}, qui.Paint{Color: ink})
		return
	}
	c.FillRect(qui.Rect{X: cx - 6, Y: cy - 7, W: 4, H: 14}, ink)
	c.FillRect(qui.Rect{X: cx + 2, Y: cy - 7, W: 4, H: 14}, ink)
}

func (w *mediaWidget) drawSpeaker(c qui.Canvas, r qui.Rect, ink qui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	p := qui.NewPath().MoveTo(cx-9, cy-3).LineTo(cx-5, cy-3).LineTo(cx, cy-8).
		LineTo(cx, cy+8).LineTo(cx-5, cy+3).LineTo(cx-9, cy+3).Close()
	c.DrawShape(qui.ShapePath{Path: p}, qui.Paint{Color: ink})
	if w.m.muted || w.m.volume == 0 {
		c.DrawLine(qui.Point{X: cx + 3, Y: cy - 4}, qui.Point{X: cx + 10, Y: cy + 4}, ink, 2)
		c.DrawLine(qui.Point{X: cx + 10, Y: cy - 4}, qui.Point{X: cx + 3, Y: cy + 4}, ink, 2)
		return
	}
	c.DrawLine(qui.Point{X: cx + 3, Y: cy - 3}, qui.Point{X: cx + 3, Y: cy + 3}, ink, 2)
	c.DrawLine(qui.Point{X: cx + 7, Y: cy - 6}, qui.Point{X: cx + 7, Y: cy + 6}, ink, 2)
}

func drawFullscreenIcon(c qui.Canvas, r qui.Rect, ink qui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	s, l := float32(7), float32(4)
	for _, sx := range []float32{-1, 1} {
		for _, sy := range []float32{-1, 1} {
			x, y := cx+sx*s, cy+sy*s
			c.DrawLine(qui.Point{X: x, Y: y}, qui.Point{X: x - sx*l, Y: y}, ink, 2)
			c.DrawLine(qui.Point{X: x, Y: y}, qui.Point{X: x, Y: y - sy*l}, ink, 2)
		}
	}
}

func (w *mediaWidget) drawTime(c qui.Canvas, r qui.Rect, ink qui.Color, size float32) {
	d := w.m.duration
	text := formatTime(w.m.CurrentTime())
	if !math.IsNaN(d) && !math.IsInf(d, 0) {
		text += " / " + formatTime(d)
	}
	c.DrawText(text, r, ink, qui.Font{Size: size, Features: []string{"tnum"}})
}

// formatTime is Chrome's control-bar format: m:ss, or h:mm:ss.
func formatTime(s float64) string {
	if math.IsNaN(s) || s < 0 {
		s = 0
	}
	t := int(s)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// ---- Registration ----

func init() {
	for _, tag := range []string{"video", "audio"} {
		video := tag == "video"
		htmlcss.RegisterElement(tag, htmlcss.ElementDef{
			Create: func(el *htmlcss.El) qui.Widget {
				if m := For(el); m != nil {
					return m.w // created by a script before it was styled
				}
				m := newElement(el, video)
				m.w = newMediaWidget(m)
				// Resource selection runs as a task once the element is in a
				// window, with its <source> children parsed.
				m.Load()
				return m.w
			},
			NaturalSize: func(el *htmlcss.El, w qui.Widget) (qui.Size, bool) {
				mw, ok := w.(*mediaWidget)
				if !ok {
					return qui.Size{}, false
				}
				if !mw.m.video {
					return qui.Size{W: 300, H: 54}, true
				}
				if mw.m.vw > 0 && mw.m.vh > 0 {
					return qui.Size{W: float32(mw.m.vw), H: float32(mw.m.vh)}, true
				}
				if img := mw.posterImage(); img != nil {
					ib := img.Bounds()
					return qui.Size{W: float32(ib.Dx()), H: float32(ib.Dy())}, true
				}
				return qui.Size{W: 300, H: 150}, true
			},
			AttrChanged: func(el *htmlcss.El, w qui.Widget, name string) {
				if mw, ok := w.(*mediaWidget); ok {
					mw.m.attrChanged(name)
				}
			},
		})
	}
}
