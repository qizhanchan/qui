//go:build cgo

package media

import (
	"bytes"
	"errors"
	"time"

	"github.com/qizhanchan/qui/media/internal/webm"
)

// Media Source Extensions over WebM byte streams ("video/webm",
// "audio/webm"). Unlike fMP4, whose boxes the page-side code splits, a
// WebM SourceBuffer takes raw appended bytes: AppendWebM finds the
// initialization segment (EBML header + Segment + Info + Tracks) and then
// feeds everything to the demuxer's streaming parser, which yields frames
// as Clusters complete.

// clusterID is the Matroska Cluster element ID.
var clusterID = []byte{0x1F, 0x43, 0xB6, 0x75}

var errWebMInitPartial = errors.New("media: WebM initialization segment incomplete")

// tracksComplete reports whether b holds a whole Tracks element (the end
// of a WebM initialization segment), walking the EBML header and the
// Segment's top-level elements.
func tracksComplete(b []byte) bool {
	p := 0
	for p < len(b) {
		id, n := ebmlVint(b[p:], false)
		if n == 0 {
			return false
		}
		size, m := ebmlVint(b[p+n:], true)
		if m == 0 {
			return false
		}
		body := p + n + m
		switch {
		case id == 0x18538067: // Segment: descend
			p = body
			continue
		case id == 0x1654AE6B: // Tracks
			return size >= 0 && int64(len(b)-body) >= size
		case size < 0:
			return false
		}
		if int64(len(b)-body) < size {
			return false
		}
		p = body + int(size)
	}
	return false
}

// ebmlVint reads an element ID (marker kept) or a size (marker removed;
// -1 = unknown) and its length, or 0 when b is too short or invalid.
func ebmlVint(b []byte, isSize bool) (int64, int) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0
	}
	n := 1
	for b[0]&(0x80>>(n-1)) == 0 {
		n++
	}
	if n > 8 || n > len(b) || !isSize && n > 4 {
		return 0, 0
	}
	v := uint64(b[0])
	if isSize {
		v &= 0xFF >> n
	}
	all := v == uint64(0xFF>>n)
	for k := 1; k < n; k++ {
		v = v<<8 | uint64(b[k])
		all = all && b[k] == 0xFF
	}
	if isSize && all {
		return -1, n
	}
	return int64(v), n
}

// maxWebMInit bounds the bytes held while waiting for a complete
// initialization segment.
const maxWebMInit = 16 << 20

// AppendWebM takes the next bytes of a WebM byte stream (split anywhere).
// offset is the SourceBuffer's timestampOffset. gotInit reports that this
// append completed the initialization segment.
func (b *MSEBuffer) AppendWebM(data []byte, offset time.Duration) (gotInit bool, err error) {
	b.mu.Lock()
	wm := b.wm
	if b.init != nil {
		b.mu.Unlock()
		return false, ErrMSEInit // an fMP4 buffer
	}
	if wm == nil {
		b.wbuf = append(b.wbuf, data...)
		head := b.wbuf
		if i := bytes.Index(head, clusterID); i >= 0 {
			head = head[:i]
		}
		var r *webm.Reader
		oerr := errWebMInitPartial
		if len(head) >= 4 && !bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}) {
			oerr = webm.ErrInvalid
		} else if tracksComplete(head) {
			r, oerr = webm.OpenInit(head)
		}
		if oerr != nil || (r.VideoTrack() == nil && r.AudioTrack() == nil) {
			// Not complete yet (or not WebM).
			if len(b.wbuf) > maxWebMInit || oerr != errWebMInitPartial {
				b.wbuf = nil
				b.mu.Unlock()
				if oerr == nil {
					oerr = ErrNoTrack
				}
				return false, oerr
			}
			b.mu.Unlock()
			return false, nil
		}
		b.wm, wm = r, r
		if vt := r.VideoTrack(); vt != nil {
			b.video = &mseTrack{b: b, wt: vt, wlast: -1 << 62}
		}
		if at := r.AudioTrack(); at != nil {
			b.audio = &mseTrack{b: b, wt: at, wlast: -1 << 62}
		}
		// The parser skips the EBML header, Info and Tracks itself, and
		// picks up whatever follows them.
		data = b.wbuf
		b.wbuf = nil
		gotInit = true
	}
	b.mu.Unlock()
	if len(data) == 0 {
		return gotInit, nil
	}
	frames, err := wm.AppendBytes(data)
	b.addWebMFrames(frames, offset)
	return gotInit, err
}

// flushWebM releases the frames the WebM parser still holds (the last
// block of each track waits for its successor to learn its duration);
// EndOfStream calls it.
func (b *MSEBuffer) flushWebM() {
	b.mu.Lock()
	wm := b.wm
	b.mu.Unlock()
	if wm != nil {
		b.addWebMFrames(wm.Flush(), 0)
	}
}

// ResetWebMParser drops a partial element (SourceBuffer.abort()).
func (b *MSEBuffer) ResetWebMParser() {
	b.mu.Lock()
	wm := b.wm
	b.wbuf = nil
	b.mu.Unlock()
	if wm != nil {
		wm.ResetParser()
	}
}

// IsWebM reports whether the buffer holds a WebM byte stream.
func (b *MSEBuffer) IsWebM() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.wm != nil }

func (b *MSEBuffer) addWebMFrames(frames []webm.Frame, offset time.Duration) {
	if len(frames) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	byTrack := map[*mseTrack][]mseSample{}
	for _, f := range frames {
		var t *mseTrack
		switch {
		case b.video != nil && f.Track == b.video.wt:
			t = b.video
		case b.audio != nil && f.Track == b.audio.wt:
			t = b.audio
		default:
			continue
		}
		pts := f.PTS + offset
		// Matroska stores presentation times only. Decode times must
		// increase in block order (reordered frames get the previous
		// one's plus 1 ns); a keyframe well before the last frame (the
		// page appending an earlier range) starts over.
		dts := pts
		if dts <= t.wlast && !(f.Keyframe && pts < t.wlast-time.Second) {
			dts = t.wlast + 1
		}
		t.wlast = dts
		run := byTrack[t]
		byTrack[t] = append(run, mseSample{
			dts: dts, pts: pts, dur: f.Duration, sync: f.Keyframe, data: f.Data, pad: f.DiscardPadding,
		})
	}
	for t, add := range byTrack {
		t.insert(add)
	}
	b.ended = false
	b.cond.Broadcast()
}

// newMSEWebMAudio is the renderer for a WebM SourceBuffer's audio track.
func newMSEWebMAudio(t *mseTrack, open sinkOpener) (*golangAudio, error) {
	at := t.wt
	var dec audioDecoder
	var err error
	st := &trackStream{tr: t}
	switch at.Codec {
	case "opus":
		dec, err = newOpusDecoder(at.CodecPrivate)
		st.preRoll = at.SeekPreRoll
		if st.preRoll == 0 {
			st.preRoll = opusSeekPreRoll
		}
	case "aac":
		dec, err = newAACDecoder(at.CodecPrivate)
		st.preRollPackets = 1
	default:
		return nil, ErrCodecUnsupported
	}
	if err != nil {
		return nil, err
	}
	st.rate = dec.SampleRate()
	st.padding = func(i int) time.Duration { s, _ := t.at(i); return s.pad }
	return newGolangAudioStream(at.Codec, dec, st, 0, open)
}
