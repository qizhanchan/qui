package htmlmedia

import (
	"image"
	"math"
	"sync"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"

	"github.com/qizhanchan/qui/media"
)

// Media Source Extensions: a page builds a MediaSource, gives the element
// its object URL (URL.createObjectURL(mediaSource)) and feeds coded media
// through SourceBuffers. The page side (MediaSource / SourceBuffer
// objects, their events) belongs to the script host; this file is the
// element's end: the object-URL registry, the attachment, and the
// switch to playback once the initialization segments are in.

// MediaSourceHandle is a page's MediaSource as an element sees it. Both
// methods run on the UI thread.
type MediaSourceHandle interface {
	// Attach is the element selecting the object URL: the MediaSource
	// opens (sourceopen) and later calls AttachMSE with its buffers.
	Attach(e *Element)
	// Detach is the element dropping it (a new load, or Destroy): the
	// MediaSource closes (sourceclose).
	Detach(e *Element)
}

var (
	objectURLs   = map[string]MediaSourceHandle{}
	objectURLsMu sync.Mutex
)

// RegisterObjectURL makes url (a blob: URL) name h for media elements.
func RegisterObjectURL(url string, h MediaSourceHandle) {
	objectURLsMu.Lock()
	objectURLs[url] = h
	objectURLsMu.Unlock()
}

// RevokeObjectURL forgets url; elements already attached keep playing.
func RevokeObjectURL(url string) {
	objectURLsMu.Lock()
	delete(objectURLs, url)
	objectURLsMu.Unlock()
}

func lookupObjectURL(url string) MediaSourceHandle {
	objectURLsMu.Lock()
	defer objectURLsMu.Unlock()
	return objectURLs[url]
}

// attachMediaSource runs the resource fetch for an MSE object URL.
func (e *Element) attachMediaSource(h MediaSourceHandle) {
	e.mse = h
	e.mseDur = math.NaN()
	e.networkState = NetworkLoading
	h.Attach(e)
}

// AttachMSE switches the element to playing bufs: the MediaSource calls it
// once every SourceBuffer has its initialization segment (the "HAVE_METADATA"
// transition). duration is MediaSource.duration (NaN = not set: +Inf).
func (e *Element) AttachMSE(duration float64, bufs ...*media.MSEBuffer) error {
	if e.mse == nil {
		return media.ErrMSEInit
	}
	src, err := media.OpenMSE(bufs...)
	if err != nil {
		e.loadFailed(err, false)
		return err
	}
	e.mseDur = mseDuration(duration)
	e.install(src, nil)
	return nil
}

// MSEDurationChanged reflects a MediaSource.duration change.
func (e *Element) MSEDurationChanged(duration float64) {
	if e.mse == nil {
		return
	}
	d := mseDuration(duration)
	e.mseDur = d
	if e.src == nil || d == e.duration {
		return
	}
	e.duration = d
	e.fire("durationchange")
}

// MSEAttached reports whether the element plays a MediaSource.
func (e *Element) MSEAttached() bool { return e.mse != nil }

func mseDuration(d float64) float64 {
	if math.IsNaN(d) {
		return math.Inf(1)
	}
	return d
}

// CurrentImage is the picture the video shows now, as RGBA (for
// canvas drawImage(video)); nil before the first frame or for audio.
func (e *Element) CurrentImage() image.Image {
	if e.vv == nil {
		return nil
	}
	f := e.vv.CurrentFrame()
	if f == nil {
		return nil
	}
	defer f.Release()
	px, ok := f.(interface{ RGBA() []byte })
	if !ok {
		return nil
	}
	b := px.RGBA()
	w, h := f.Width(), f.Height()
	if len(b) < 4*w*h || w <= 0 || h <= 0 {
		return nil
	}
	return &image.RGBA{Pix: b, Stride: 4 * w, Rect: image.Rect(0, 0, w, h)}
}

// EnsureAll gives every <video> / <audio> under root its element state,
// including those that build no widget (an <audio> without controls is
// display:none), so they load and autoplay as in a browser.
func EnsureAll(root *htmlcss.El, win *qui.Window) {
	if root == nil {
		return
	}
	if t := root.Tag(); (t == "video" || t == "audio") && For(root) == nil {
		Ensure(root, win)
	}
	for _, k := range root.Kids() {
		if c, ok := k.(*htmlcss.El); ok {
			EnsureAll(c, win)
		}
	}
}
