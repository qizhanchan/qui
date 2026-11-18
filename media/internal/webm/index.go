package webm

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

// step scans the top-level element at pos with the reader's source. For
// a Cluster it calls block for each block. It returns where the next
// element starts, the cluster timestamp (isCluster), and end when the
// stream is exhausted. ioErr reports a failure of the underlying
// ReaderAt (not malformed data, which is skipped by resynchronising on
// the next Cluster ID).
func (r *Reader) step(pos int64, ci int32, block func(*Track, []entry)) (next int64, isCluster bool, ts int64, end bool, ioErr error) {
	if pos >= r.segEnd {
		return pos, false, 0, true, nil
	}
	e, err := r.src.header(pos)
	if err != nil && !errors.Is(err, ErrInvalid) {
		return pos, false, 0, true, err
	}
	resync := func(from int64) (int64, bool, int64, bool, error) {
		p := r.src.find(idCluster, from, r.segEnd)
		if p < 0 {
			return r.segEnd, false, 0, true, nil
		}
		return p, false, 0, false, nil
	}
	if err != nil {
		return resync(pos + 1)
	}
	if e.id != idCluster {
		if e.size < 0 || e.dataOff+e.size > r.segEnd {
			return resync(pos + 1)
		}
		return e.dataOff + e.size, false, 0, false, nil
	}
	next, ts, err = r.scanCluster(r.src, pos, r.segEnd, ci, block)
	switch {
	case err == nil:
		if next <= pos {
			return resync(pos + 1)
		}
		return next, true, ts, false, nil
	case errors.Is(err, io.ErrUnexpectedEOF):
		return r.segEnd, true, ts, true, nil
	case errors.Is(err, ErrInvalid):
		n, _, _, end, _ := resync(max(next, pos+1))
		return n, true, ts, end, nil
	default:
		return next, true, ts, true, err
	}
}

// indexNext scans one more top-level element into the index.
func (r *Reader) indexNext() {
	ci := int32(len(r.clusters))
	next, isCluster, ts, end, err := r.step(r.frontier, ci, r.indexBlock)
	if isCluster {
		r.clusters = append(r.clusters, clusterRec{pos: r.frontier, ts: ts})
	}
	r.frontier = next
	if err != nil {
		r.scanErr = err
	}
	if end {
		r.finishIndex()
	}
}

func (r *Reader) indexBlock(t *Track, es []entry) {
	if t.pending < len(t.frames) {
		spread(t.frames[t.pending:], es[0].ts)
	}
	t.frames = append(t.frames, es...)
	if es[0].dur < 0 {
		t.pending = len(t.frames) - len(es)
	} else {
		t.pending = len(t.frames)
	}
}

func (r *Reader) finishIndex() {
	r.done = true
	for _, t := range r.tracks {
		if t.pending < len(t.frames) {
			var prev int64
			if t.pending > 0 {
				prev = t.frames[t.pending-1].dur
			}
			finish(t, t.frames[t.pending:], prev)
			t.pending = len(t.frames)
		}
	}
}

// indexUntil scans clusters until stop reports true or the stream ends.
func (r *Reader) indexUntil(stop func() bool) {
	for !r.done && !stop() {
		r.indexNext()
	}
}

func (r *Reader) owns(t *Track) bool { return t != nil && r.byNum[t.Number] == t }

// IndexAll scans every cluster. It returns the first ReaderAt error.
func (r *Reader) IndexAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.indexUntil(func() bool { return false })
	return r.scanErr
}

// Indexed reports whether every cluster has been scanned.
func (r *Reader) Indexed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

// SampleCount is the number of frames of t. It indexes the whole file.
func (r *Reader) SampleCount(t *Track) int {
	if !r.owns(t) {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.indexUntil(func() bool { return false })
	return len(t.frames)
}

// IndexedCount is the number of frames of t known so far, without
// scanning further.
func (r *Reader) IndexedCount(t *Track) int {
	if !r.owns(t) {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return len(t.frames)
	}
	return t.pending
}

// entryAt indexes far enough to know frame i of t (locked).
func (r *Reader) entryAt(t *Track, i int) (entry, error) {
	if !r.owns(t) {
		return entry{}, ErrNoTrack
	}
	if i < 0 {
		return entry{}, fmt.Errorf("webm: sample %d out of range", i)
	}
	r.indexUntil(func() bool { return i < t.pending })
	if i >= len(t.frames) || (!r.done && i >= t.pending) {
		if r.scanErr != nil {
			return entry{}, r.scanErr
		}
		return entry{}, io.EOF
	}
	return t.frames[i], nil
}

// Sample describes frame i of t, scanning clusters as needed. Past the
// last frame it returns io.EOF.
func (r *Reader) Sample(t *Track, i int) (Sample, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.entryAt(t, i)
	if err != nil {
		return Sample{}, err
	}
	return e.sample(t), nil
}

// ReadSample returns the payload of frame i of t.
func (r *Reader) ReadSample(t *Track, i int) ([]byte, error) {
	return r.ReadSampleInto(nil, t, i)
}

// ReadSampleInto is ReadSample reusing buf's storage when large enough.
func (r *Reader) ReadSampleInto(buf []byte, t *Track, i int) ([]byte, error) {
	r.mu.Lock()
	e, err := r.entryAt(t, i)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return readEntry(r.ra, t, e, buf)
}

// PTS is the presentation time of frame i (0 when unavailable).
func (r *Reader) PTS(t *Track, i int) time.Duration {
	s, _ := r.Sample(t, i)
	return s.PTS
}

// SampleDuration is the duration of frame i (0 when unavailable).
func (r *Reader) SampleDuration(t *Track, i int) time.Duration {
	s, _ := r.Sample(t, i)
	return s.Duration
}

// IsSync reports whether frame i is a keyframe.
func (r *Reader) IsSync(t *Track, i int) bool {
	s, _ := r.Sample(t, i)
	return s.Keyframe
}

// keyframes extends and returns t's keyframe list over the indexed frames.
func (t *Track) keyframes() []int {
	for i := t.keyfrLen; i < len(t.frames); i++ {
		if t.frames[i].flags&flagKey != 0 {
			t.keyfr = append(t.keyfr, i)
		}
	}
	t.keyfrLen = len(t.frames)
	return t.keyfr
}

// Keyframes returns the indices of all keyframes of t (indexes the whole
// file).
func (r *Reader) Keyframes(t *Track) []int {
	if !r.owns(t) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.indexUntil(func() bool { return false })
	return append([]int(nil), t.keyframes()...)
}

// indexPast scans until a cluster starting after target has been read
// (so every frame presented at or before target is indexed).
func (r *Reader) indexPast(target int64) {
	r.indexUntil(func() bool {
		n := len(r.clusters)
		return n > 0 && r.clusters[n-1].ts > target
	})
}

// SeekKeyframe returns the index of the last keyframe of t presented at
// or before target (the first keyframe when target precedes it, and 0
// when t has none). Clusters are scanned only up to target.
func (r *Reader) SeekKeyframe(t *Track, target time.Duration) int {
	if !r.owns(t) {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seekKeyframe(t, int64(target))
}

func (r *Reader) seekKeyframe(t *Track, target int64) int {
	r.indexPast(target)
	ks := t.keyframes()
	if len(ks) == 0 {
		return 0
	}
	j := sort.Search(len(ks), func(j int) bool { return t.frames[ks[j]].ts > target })
	if j == 0 {
		return ks[0]
	}
	return ks[j-1]
}

// SampleAtTime returns the index of the first frame of t (decode order)
// whose presentation interval ends after target, for tracks without
// reordering (audio). It returns the frame count when target is past the
// end.
func (r *Reader) SampleAtTime(t *Track, target time.Duration) int {
	if !r.owns(t) {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.indexPast(int64(target))
	if !r.done {
		// Durations of the last block may still be pending.
		n := len(r.clusters)
		r.indexUntil(func() bool { return len(r.clusters) > n })
	}
	fs := t.frames[:min(len(t.frames), max(t.pending, 0))]
	if r.done {
		fs = t.frames
	}
	return sort.Search(len(fs), func(i int) bool { return fs[i].ts+max(fs[i].dur, 0) > int64(target) })
}
