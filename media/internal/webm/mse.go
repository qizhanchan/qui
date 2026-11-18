package webm

import (
	"bytes"
	"errors"
	"fmt"
)

// Media Source Extensions: a WebM byte stream is an initialization
// segment (EBML header + Segment header + Info + Tracks, no Clusters)
// followed by media segments, which are Clusters (often of unknown size
// in live streams). OpenInit parses the former; ParseCluster extracts the
// frames of complete media segments and AppendBytes accepts the stream in
// arbitrary chunks.

// mseState is the streaming parser state of AppendBytes.
type mseState struct {
	buf       []byte
	inCluster bool
	remain    int64 // bytes left in a known-size cluster; -1 = unknown size
	tsTicks   int64
	fq        frameQueue
}

// OpenInit parses an initialization segment. The returned Reader has the
// tracks' configuration and no frames; its Segment may be of unknown
// size or extend past data.
func OpenInit(data []byte) (*Reader, error) {
	if len(data) == 0 {
		return nil, errors.New("webm: empty initialization segment")
	}
	return open(memSource(data), bytes.NewReader(data))
}

// ParseCluster returns the frames of one or more complete media
// segments (Clusters; a leading EBML header or Segment header, Cues,
// Void and similar elements are skipped). Frame data aliases data,
// except for header-stripped tracks. Frames whose duration cannot be
// derived (the last block of a track without BlockDuration or
// DefaultDuration) get the previous frame's duration. Damaged elements
// are skipped by resynchronising on the next Cluster; the error then
// reports the damage alongside the frames that were recovered.
func (r *Reader) ParseCluster(data []byte) ([]Frame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := memSource(data)
	var fq frameQueue
	fq.lastDur = map[*Track]int64{}
	fq.pend = map[*Track][]int{}
	for t, d := range r.mse.fq.lastDur {
		fq.lastDur[t] = d
	}
	push := func(t *Track, es []entry) {
		fq.push(t, es, payloads(data, t, es))
	}
	var firstErr error
	note := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	end := int64(len(data))
	off := int64(0)
	for off < end {
		e, err := src.header(off)
		if err != nil {
			note(fmt.Errorf("webm: bad element at %d: %w", off, ErrInvalid))
			if off = src.find(idCluster, off+1, end); off < 0 {
				break
			}
			continue
		}
		switch e.id {
		case idSegment:
			off = e.dataOff
			continue
		case idCluster:
			next, _, cerr := r.scanCluster(src, off, end, -1, push)
			if cerr != nil {
				note(fmt.Errorf("webm: cluster at %d: %w", off, cerr))
				if errors.Is(cerr, ErrInvalid) {
					if p := src.find(idCluster, max(next, off+1), end); p >= 0 {
						off = p
						continue
					}
				}
				off = end
				continue
			}
			if next <= off {
				off = end
				continue
			}
			off = next
			continue
		}
		if e.size < 0 || e.dataOff+e.size > end {
			break
		}
		off = e.dataOff + e.size
	}
	fq.flush()
	out := make([]Frame, 0, len(fq.q))
	for _, it := range fq.q {
		out = append(out, frameOf(it.t, it.e, it.data))
	}
	if r.mse.fq.lastDur == nil {
		r.mse.fq.lastDur = map[*Track]int64{}
		r.mse.fq.pend = map[*Track][]int{}
	}
	for t, d := range fq.lastDur {
		r.mse.fq.lastDur[t] = d
	}
	return out, firstErr
}

// payloads slices each entry's payload out of data (copying when a
// stripped header must be re-attached).
func payloads(data []byte, t *Track, es []entry) [][]byte {
	out := make([][]byte, len(es))
	for i, e := range es {
		end := e.off + int64(e.size)
		if e.off < 0 || end > int64(len(data)) {
			continue
		}
		p := data[e.off:end:end]
		if len(t.stripPrefix) > 0 {
			p = append(append([]byte(nil), t.stripPrefix...), p...)
		}
		out[i] = p
	}
	return out
}

func copyPayloads(data []byte, t *Track, es []entry) [][]byte {
	out := payloads(data, t, es)
	for i, p := range out {
		if len(t.stripPrefix) == 0 {
			out[i] = append([]byte(nil), p...)
		}
	}
	return out
}

const maxAppendElem = 64 << 20 // largest non-cluster element AppendBytes buffers

// AppendBytes feeds the next chunk of a WebM byte stream (as delivered
// to SourceBuffer.appendBuffer: split anywhere, clusters of known or
// unknown size) and returns the frames that are now complete. A frame
// without a BlockDuration or DefaultDuration is held until its track's
// next block arrives; Flush releases held frames at the end of the
// stream. Frame data is copied and owned by the caller.
func (r *Reader) AppendBytes(data []byte) ([]Frame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := &r.mse
	m.buf = append(m.buf, data...)
	src := memSource(m.buf)
	n := int64(len(m.buf))
	var p int64
	var err error
loop:
	for p < n {
		if m.inCluster && m.remain == 0 {
			m.inCluster = false
		}
		e, herr := src.header(p)
		if herr != nil {
			if n-p < 12 {
				break // maybe an incomplete header
			}
			err = fmt.Errorf("webm: bad element at stream offset: %w", ErrInvalid)
			m.inCluster = false
			q := src.find(idCluster, p+1, n)
			if q < 0 {
				p = max(p, n-3)
				break
			}
			p = q
			continue
		}
		hdr := e.dataOff - e.off
		if m.inCluster {
			if m.remain < 0 && isTopLevel(e.id) {
				m.inCluster = false
				continue
			}
			if e.size < 0 || (m.remain >= 0 && hdr+e.size > m.remain) {
				err = fmt.Errorf("webm: bad cluster child: %w", ErrInvalid)
				m.inCluster = false
				p = e.dataOff
				continue
			}
			if e.dataOff+e.size > n {
				break // wait for the rest of the element
			}
			r.mseChild(src, e)
			if m.remain >= 0 {
				m.remain -= hdr + e.size
			}
			p = e.dataOff + e.size
			continue
		}
		switch e.id {
		case idSegment:
			p = e.dataOff
			continue
		case idCluster:
			m.inCluster, m.remain, m.tsTicks = true, e.size, 0
			p = e.dataOff
			continue
		}
		if e.size < 0 {
			err = fmt.Errorf("webm: unknown-size element %#x: %w", e.id, ErrInvalid)
			p = e.dataOff
			continue
		}
		if e.size > maxAppendElem {
			err = fmt.Errorf("webm: element %#x too large: %w", e.id, ErrInvalid)
			p = e.dataOff
			continue
		}
		if e.dataOff+e.size > n {
			break loop
		}
		p = e.dataOff + e.size
	}
	m.buf = append([]byte(nil), m.buf[p:]...)
	return r.drainMSE(false), err
}

// mseChild handles one complete Cluster child for AppendBytes.
func (r *Reader) mseChild(src *source, e elem) {
	m := &r.mse
	push := func(t *Track, es []entry) {
		m.fq.push(t, es, copyPayloads(src.mem, t, es))
	}
	switch e.id {
	case idTimestamp:
		b, _ := src.bytes(e.dataOff, int(min(e.size, 8)))
		if v := readUint(b); v <= (1<<62)/uint64(r.scale) {
			m.tsTicks = int64(v)
		}
	case idSimpleBlock:
		if t, es, err := r.parseBlock(src, e.dataOff, e.size, m.tsTicks, true, -1); err == nil && t != nil {
			push(t, es)
		}
	case idBlockGroup:
		if t, es, err := r.parseBlockGroup(src, e.dataOff, e.dataOff+e.size, m.tsTicks, -1); err == nil && t != nil {
			push(t, es)
		}
	}
}

func (r *Reader) drainMSE(all bool) []Frame {
	fq := &r.mse.fq
	if all {
		fq.flush()
	}
	var out []Frame
	for {
		if !fq.ready() {
			if fq.head < len(fq.q) && len(fq.q)-fq.head > maxHeld {
				fq.finishTrack(fq.q[fq.head].t)
				continue
			}
			break
		}
		it := fq.pop()
		out = append(out, frameOf(it.t, it.e, it.data))
	}
	return out
}

// Flush returns the frames AppendBytes is still holding, giving the
// last block of each track a fallback duration (DefaultDuration or the
// track's previous frame duration), and discards any partial element.
func (r *Reader) Flush() []Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mse.buf = nil
	r.mse.inCluster = false
	return r.drainMSE(true)
}

// ResetParser discards AppendBytes state (SourceBuffer.abort()).
func (r *Reader) ResetParser() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mse.buf = nil
	r.mse.inCluster = false
	r.mse.fq.reset()
}
