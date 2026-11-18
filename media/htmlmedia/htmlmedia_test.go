//go:build cgo

package htmlmedia

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
)

var clip, _ = filepath.Abs("../testdata/bframes_aac_160x90.mp4")

type fixture struct {
	t      *testing.T
	win    *qui.Window
	doc    htmlcss.RenderResult
	events []string
}

func mount(t *testing.T, body string) *fixture {
	t.Helper()
	f := &fixture{t: t}
	f.win = qui.NewTestWindow(qui.Size{W: 800, H: 600})
	f.doc = htmlcss.RenderDoc(body, "", htmlcss.Options{Window: f.win, DocumentRoot: true})
	f.win.SetRoot(f.doc.Root)
	f.layout()
	return f
}

func (f *fixture) layout() {
	f.doc.Engine.FlushPendingStyles()
	f.doc.Root.Measure(f.win.Size())
	f.doc.Root.Layout(qui.Rect{W: 800, H: 600})
}

func (f *fixture) el(id string) (*htmlcss.El, *Element) {
	f.t.Helper()
	el, _ := f.doc.ByID[id].(*htmlcss.El)
	if el == nil {
		f.t.Fatalf("no #%s", id)
	}
	m := For(el)
	if m == nil {
		f.t.Fatalf("#%s has no media element state", id)
	}
	return el, m
}

func (f *fixture) listen(m *Element) {
	m.OnEvent(func(n string) {
		if n != "timeupdate" && n != "progress" {
			f.events = append(f.events, n)
		}
	})
}

// waitFor drains posted jobs until cond holds.
func (f *fixture) waitFor(what string, cond func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s (events %v)", what, f.events)
		}
		f.win.DrainJobsForTest()
		time.Sleep(5 * time.Millisecond)
	}
}

func TestVideoLoadsMetadataAndSizes(t *testing.T) {
	f := mount(t, `<video id=v muted src="`+clip+`"></video>`)
	el, m := f.el("v")
	f.listen(m)
	f.win.DrainJobsForTest()
	if m.NetworkState() != NetworkLoading || m.ReadyState() != HaveNothing {
		t.Fatalf("network %d ready %d before load", m.NetworkState(), m.ReadyState())
	}
	if !math.IsNaN(m.Duration()) {
		t.Fatal("duration known before metadata")
	}
	if b := el.Bounds(); b.W != 300 || b.H != 150 {
		t.Fatalf("pre-metadata box %v, want 300×150", b)
	}
	f.waitFor("canplaythrough", func() bool { return m.ReadyState() == HaveEnoughData })
	f.win.DrainJobsForTest()
	want := "loadstart durationchange resize loadedmetadata loadeddata canplay canplaythrough"
	if got := strings.Join(f.events, " "); got != want {
		t.Fatalf("events %q, want %q", got, want)
	}
	if m.VideoWidth() != 160 || m.VideoHeight() != 90 {
		t.Fatalf("video size %d×%d", m.VideoWidth(), m.VideoHeight())
	}
	if d := m.Duration(); math.Abs(d-1) > 0.05 {
		t.Fatalf("duration %v", d)
	}
	f.layout()
	if b := el.Bounds(); b.W != 160 || b.H != 90 {
		t.Fatalf("box %v after metadata, want 160×90", b)
	}
	m.Destroy()
}

func TestVideoWidthAttrKeepsRatio(t *testing.T) {
	f := mount(t, `<video id=v muted width=320 src="`+clip+`"></video>`)
	el, m := f.el("v")
	f.waitFor("metadata", func() bool { return m.ReadyState() >= HaveMetadata })
	f.layout()
	if b := el.Bounds(); b.W != 320 || b.H != 180 {
		t.Fatalf("box %v, want 320×180", b)
	}
	if m.vv.Bounds() != el.Bounds() {
		t.Fatalf("picture %v does not fill the element %v", m.vv.Bounds(), el.Bounds())
	}
	m.Destroy()
}

func TestPlayPauseSeekEvents(t *testing.T) {
	f := mount(t, `<video id=v muted src="`+clip+`"></video>`)
	_, m := f.el("v")
	f.listen(m)
	f.waitFor("ready", func() bool { return m.ReadyState() == HaveEnoughData })
	f.win.DrainJobsForTest()
	f.events = nil
	var res error = errSentinel
	m.Play(false, func(err error) { res = err })
	if res != nil {
		t.Fatalf("muted play rejected: %v", res)
	}
	if m.Paused() {
		t.Fatal("paused after play")
	}
	m.Pause()
	m.SetCurrentTime(0.5)
	if !m.Seeking() || m.CurrentTime() != 0.5 {
		t.Fatalf("seeking %v t %v", m.Seeking(), m.CurrentTime())
	}
	f.waitFor("seeked", func() bool { return !m.Seeking() })
	f.win.DrainJobsForTest()
	want := "play playing pause seeking seeked"
	if got := strings.Join(f.events, " "); got != want {
		t.Fatalf("events %q, want %q", got, want)
	}
	m.Destroy()
}

var errSentinel = &MediaErrorErr{}

type MediaErrorErr struct{}

func (*MediaErrorErr) Error() string { return "unset" }

func TestUnmutedAutoplayBlocked(t *testing.T) {
	f := mount(t, `<video id=v autoplay src="`+clip+`"></video>`)
	_, m := f.el("v")
	f.waitFor("ready", func() bool { return m.ReadyState() == HaveEnoughData })
	if !m.Paused() {
		t.Fatal("unmuted autoplay started without a gesture")
	}
	var res error
	m.Play(false, func(err error) { res = err })
	if res != ErrNotAllowed {
		t.Fatalf("play() = %v, want NotAllowedError", res)
	}
	m.Destroy()

	g := mount(t, `<video id=v autoplay muted src="`+clip+`"></video>`)
	_, m2 := g.el("v")
	g.waitFor("ready", func() bool { return m2.ReadyState() == HaveEnoughData })
	if m2.Paused() {
		t.Fatal("muted autoplay did not start")
	}
	m2.Destroy()
}

func TestBadSourceErrors(t *testing.T) {
	f := mount(t, `<video id=v src="/nonexistent/clip.mp4"></video>`)
	_, m := f.el("v")
	f.listen(m)
	f.waitFor("error", func() bool { return m.Error() != nil })
	if m.Error().Code != ErrCodeSrcUnsupported || m.NetworkState() != NetworkNoSource {
		t.Fatalf("error %+v network %d", m.Error(), m.NetworkState())
	}
	var res error
	m.Play(true, func(err error) { res = err })
	if res != ErrNotSupported {
		t.Fatalf("play() = %v", res)
	}
}

func TestSourceChildrenFallback(t *testing.T) {
	f := mount(t, `<video id=v muted>
		<source src="x.webm" type="video/webm">
		<source src="/nonexistent.mp4" type="video/mp4">
		<source src="`+clip+`" type="video/mp4; codecs=&quot;avc1.64001e, mp4a.40.2&quot;">
	</video>`)
	_, m := f.el("v")
	f.waitFor("ready", func() bool { return m.ReadyState() == HaveEnoughData || m.Error() != nil })
	if m.Error() != nil || m.CurrentSrc() != clip {
		t.Fatalf("error %+v currentSrc %q", m.Error(), m.CurrentSrc())
	}
	m.Destroy()
}

func TestAudioElement(t *testing.T) {
	f := mount(t, `<audio id=a muted controls src="`+clip+`"></audio><audio id=b src="`+clip+`"></audio>`)
	el, m := f.el("a")
	f.waitFor("ready", func() bool { return m.ReadyState() == HaveEnoughData })
	f.layout()
	if b := el.Bounds(); b.W != 300 || b.H != 54 {
		t.Fatalf("audio box %v, want 300×54", b)
	}
	hidden, _ := f.doc.ByID["b"].(*htmlcss.El)
	if b := hidden.Bounds(); b.W != 0 || b.H != 0 {
		t.Fatalf("audio without controls has a box %v", b)
	}
	// Without controls it builds no widget, yet still loads (EnsureAll),
	// as Chrome loads and autoplays a hidden <audio>.
	root, _ := f.doc.Root.(*htmlcss.El)
	EnsureAll(root, f.win)
	mb := For(hidden)
	if mb == nil {
		t.Fatal("EnsureAll did not create the hidden audio's state")
	}
	f.waitFor("hidden ready", func() bool { return mb.ReadyState() == HaveEnoughData })
	m.Destroy()
	mb.Destroy()
}

func TestCanPlayType(t *testing.T) {
	for in, want := range map[string]string{
		"video/mp4":                                             "maybe",
		`video/mp4; codecs="avc1.42E01E"`:                       "probably",
		`video/mp4; codecs="avc1.4d401f,mp4a.40.2"`:             "probably",
		`video/mp4; codecs="av01.0.05M.08"`:                     "probably",
		`video/mp4; codecs="vp09.00.10.08"`:                     "",
		"video/webm":                                            "maybe",
		`video/webm; codecs="av01.0.05M.08, opus"`:              "probably",
		`video/webm; codecs="vp9, opus"`:                        "probably",
		`video/webm; codecs="vp09.00.40.08, opus"`:              "probably",
		`video/webm; codecs="vp09.02.10.10"`:                    "",
		`video/webm; codecs="vp8"`:                              "",
		`video/webm; codecs="avc1.42E01E"`:                      "",
		`audio/webm; codecs=vorbis`:                             "",
		"audio/mpeg":                                            "probably",
		"audio/ogg; codecs=opus":                                "probably",
		"audio/ogg; codecs=vorbis":                              "",
		`video/x-matroska; codecs="hvc1.1.6.L93.B0, mp4a.40.2"`: "probably",
		`video/mp4; codecs="mp4a.69"`:                           "",
		"application/x-mpegurl":                                 "",
		"":                                                      "",
	} {
		if got := CanPlayType(in); got != want {
			t.Errorf("CanPlayType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTime(t *testing.T) {
	for in, want := range map[float64]string{0: "0:00", 5.9: "0:05", 83: "1:23", 3723: "1:02:03", math.NaN(): "0:00"} {
		if got := formatTime(in); got != want {
			t.Errorf("formatTime(%v) = %q, want %q", in, got, want)
		}
	}
}
