package webm

import (
	"io"
	"sort"
	"time"
)

// qitem is one queued frame.
type qitem struct {
	t    *Track
	e    entry
	data []byte // MSE: a copy of the payload; file cursor: nil (read at pop)
}

// frameQueue holds frames in file order until their durations are
// known: a block without a duration waits for the next block of its
// track.
type frameQueue struct {
	q       []qitem
	head    int
	pend    map[*Track][]int // queue indices of a track's unresolved block
	lastDur map[*Track]int64
}

func (fq *frameQueue) reset() {
	fq.q, fq.head = fq.q[:0], 0
	clear(fq.pend)
}

func (fq *frameQueue) push(t *Track, es []entry, data [][]byte) {
	if fq.pend == nil {
		fq.pend = map[*Track][]int{}
		fq.lastDur = map[*Track]int64{}
	}
	if idx := fq.pend[t]; len(idx) > 0 {
		fq.resolve(t, idx, func(g []entry) { spread(g, es[0].ts) })
	}
	start := len(fq.q)
	for i, e := range es {
		it := qitem{t: t, e: e}
		if data != nil {
			it.data = data[i]
		}
		fq.q = append(fq.q, it)
	}
	if es[0].dur < 0 {
		idx := make([]int, len(es))
		for i := range idx {
			idx[i] = start + i
		}
		fq.pend[t] = idx
	} else {
		fq.lastDur[t] = es[len(es)-1].dur
	}
}

func (fq *frameQueue) resolve(t *Track, idx []int, fn func([]entry)) {
	g := make([]entry, len(idx))
	for i, k := range idx {
		g[i] = fq.q[k].e
	}
	fn(g)
	for i, k := range idx {
		fq.q[k].e = g[i]
	}
	fq.lastDur[t] = g[len(g)-1].dur
	delete(fq.pend, t)
}

// finishTrack gives t's unresolved block a fallback duration.
func (fq *frameQueue) finishTrack(t *Track) {
	if idx := fq.pend[t]; len(idx) > 0 {
		prev := fq.lastDur[t]
		fq.resolve(t, idx, func(g []entry) { finish(t, g, prev) })
	}
}

func (fq *frameQueue) flush() {
	for t := range fq.pend {
		fq.finishTrack(t)
	}
}

func (fq *frameQueue) ready() bool { return fq.head < len(fq.q) && fq.q[fq.head].e.dur >= 0 }

func (fq *frameQueue) pop() qitem {
	it := fq.q[fq.head]
	fq.q[fq.head] = qitem{}
	fq.head++
	if fq.head == len(fq.q) {
		fq.q, fq.head = fq.q[:0], 0
	}
	return it
}

// maxHeld bounds how many frames may queue behind one whose duration is
// unknown (a sparse track) before it gets a fallback duration.
const maxHeld = 4096

// cursor is the sequential ReadFrame state.
type cursor struct {
	pos     int64
	end     bool
	fq      frameQueue
	waitKey *Track // drop this track's frames until its first keyframe
	err     error
}

// ReadFrame returns the next frame in file (decode) order across all
// tracks, reading its payload. It returns io.EOF at the end. ReadFrame
// starts at the first Cluster; SeekTo repositions it. It does not use
// or extend the sample index, so it streams files of any size.
func (r *Reader) ReadFrame() (Frame, error) {
	r.mu.Lock()
	c := &r.cur
	var it qitem
	for {
		if !c.fq.ready() {
			if c.fq.head < len(c.fq.q) && (c.end || len(c.fq.q)-c.fq.head > maxHeld) {
				c.fq.finishTrack(c.fq.q[c.fq.head].t)
				continue
			}
			if c.end {
				err := c.err
				r.mu.Unlock()
				if err != nil {
					return Frame{}, err
				}
				return Frame{}, io.EOF
			}
			next, _, _, end, err := r.step(c.pos, -1, func(t *Track, es []entry) { c.fq.push(t, es, nil) })
			c.pos = next
			if err != nil {
				c.err = err
			}
			if end {
				c.end = true
				c.fq.flush()
			}
			continue
		}
		it = c.fq.pop()
		if c.waitKey == it.t {
			if it.e.flags&flagKey == 0 {
				continue
			}
			c.waitKey = nil
		}
		break
	}
	r.mu.Unlock()
	data, err := readEntry(r.ra, it.t, it.e, nil)
	if err != nil {
		return Frame{}, err
	}
	return frameOf(it.t, it.e, data), nil
}

func frameOf(t *Track, e entry, data []byte) Frame {
	return Frame{
		Track:          t,
		PTS:            time.Duration(e.ts),
		Duration:       time.Duration(max(e.dur, 0)),
		Keyframe:       e.flags&flagKey != 0,
		Discardable:    e.flags&flagDiscardable != 0,
		Invisible:      e.flags&flagInvisible != 0,
		DiscardPadding: time.Duration(e.pad),
		Data:           data,
	}
}

// Rewind moves ReadFrame back to the first Cluster.
func (r *Reader) Rewind() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resetCursor(r.firstCluster())
}

func (r *Reader) firstCluster() int64 {
	if len(r.clusters) > 0 {
		return r.clusters[0].pos
	}
	return r.start
}

func (r *Reader) resetCursor(pos int64) {
	r.cur.pos, r.cur.end, r.cur.err, r.cur.waitKey = pos, false, nil, nil
	r.cur.fq.reset()
}

// SeekTo positions ReadFrame at the cluster holding the last keyframe of
// the main track (video if any, else the first track) presented at or
// before t, using Cues when present and the lazy cluster index
// otherwise. Frames of the main track before that keyframe are skipped;
// other tracks' frames from the same cluster are returned, so they may
// start a little before the keyframe. It returns the keyframe's time
// (for Opus, callers that need exact audio should seek SeekPreRoll
// earlier and discard).
func (r *Reader) SeekTo(t time.Duration) (time.Duration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	main := r.Track(KindVideo)
	if main == nil {
		main = r.tracks[0]
	}
	target := int64(t)
	if pos, kt, ok := r.cueLookup(main, target); ok {
		r.resetCursor(pos)
		r.cur.waitKey = main
		return time.Duration(kt), nil
	}
	ki := r.seekKeyframe(main, target)
	if ki >= len(main.frames) {
		r.resetCursor(r.firstCluster())
		if r.scanErr != nil {
			return 0, r.scanErr
		}
		return 0, nil
	}
	e := main.frames[ki]
	r.resetCursor(r.clusters[e.cluster].pos)
	r.cur.waitKey = main
	return time.Duration(e.ts), nil
}

// cueLookup finds the cue of track t at or before target and checks
// that it points at a Cluster.
func (r *Reader) cueLookup(t *Track, target int64) (pos, ts int64, ok bool) {
	var cs []cuePoint
	for _, c := range r.cues {
		if c.track == t.Number {
			cs = append(cs, c)
		}
	}
	if len(cs) == 0 {
		cs = r.cues
	}
	if len(cs) == 0 {
		return 0, 0, false
	}
	// Cue times are raw block timestamps; compare in that space.
	raw := target + int64(t.CodecDelay)
	j := sort.Search(len(cs), func(j int) bool { return cs[j].t > raw })
	if j > 0 {
		j--
	}
	c := cs[j]
	e, err := r.src.header(c.pos)
	if err != nil || e.id != idCluster {
		return 0, 0, false
	}
	return c.pos, c.t - int64(t.CodecDelay), true
}
