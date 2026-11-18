package webm

import (
	"fmt"
	"io"
	"time"
)

const maxLaces = 256

// scanCluster walks the Cluster element at pos (its payload bounded by
// limit; an unknown size runs to the next top-level element) and calls
// block for every block of a known track, with one entry per laced
// frame. Entry timestamps are final; durations are -1 where neither the
// block nor the track gives one (resolved by the caller from the next
// block). It returns the offset just past the cluster and the cluster
// timestamp. A damaged or truncated cluster returns the frames parsed
// so far together with an error; next is then where scanning stopped.
func (r *Reader) scanCluster(src *source, pos, limit int64, ci int32, block func(t *Track, es []entry)) (next, ts int64, err error) {
	e, err := src.header(pos)
	if err != nil {
		return pos, 0, err
	}
	if e.id != idCluster {
		return pos, 0, ErrInvalid
	}
	end := e.end(limit)
	unknown := e.size < 0
	if !unknown && e.dataOff+e.size > src.size {
		err = io.ErrUnexpectedEOF // reported after the blocks we have
		end = src.size
	}
	var tsTicks int64
	off := e.dataOff
	for off < end {
		c, herr := src.header(off)
		if herr != nil {
			if off+2 > src.size {
				return off, tsTicks * r.scale, io.ErrUnexpectedEOF
			}
			return off, tsTicks * r.scale, herr
		}
		if unknown && isTopLevel(c.id) {
			return off, tsTicks * r.scale, err
		}
		if c.size < 0 {
			return off, tsTicks * r.scale, ErrInvalid
		}
		cend := c.dataOff + c.size
		if cend > end {
			if end == src.size || cend > src.size {
				return end, tsTicks * r.scale, io.ErrUnexpectedEOF
			}
			return end, tsTicks * r.scale, ErrInvalid
		}
		switch c.id {
		case idTimestamp:
			b, perr := src.bytes(c.dataOff, int(min(c.size, 8)))
			if perr != nil {
				return off, tsTicks * r.scale, perr
			}
			v := readUint(b)
			if v > (1<<62)/uint64(r.scale) {
				return off, 0, ErrInvalid
			}
			tsTicks = int64(v)
		case idSimpleBlock:
			t, es, berr := r.parseBlock(src, c.dataOff, c.size, tsTicks, true, ci)
			if berr != nil {
				return cend, tsTicks * r.scale, berr
			}
			if t != nil {
				block(t, es)
			}
		case idBlockGroup:
			t, es, berr := r.parseBlockGroup(src, c.dataOff, cend, tsTicks, ci)
			if berr != nil {
				return cend, tsTicks * r.scale, berr
			}
			if t != nil {
				block(t, es)
			}
		}
		off = cend
	}
	return end, tsTicks * r.scale, err
}

func (r *Reader) parseBlockGroup(src *source, off, end, tsTicks int64, ci int32) (*Track, []entry, error) {
	var (
		blkOff, blkSize = int64(-1), int64(0)
		dur             = int64(-1)
		ref             bool
		pad             int64
	)
	err := src.children(off, end, func(c elem) bool {
		if c.dataOff+c.size > end {
			return false
		}
		switch c.id {
		case idBlock:
			blkOff, blkSize = c.dataOff, c.size
		case idBlockDuration, idDiscardPadding:
			b, err := src.bytes(c.dataOff, int(min(c.size, 8)))
			if err != nil {
				return false
			}
			if c.id == idBlockDuration {
				if v := readUint(b); v < (1<<62)/uint64(r.scale) {
					dur = int64(v) * r.scale
				}
			} else {
				pad = max(readInt(b), 0)
			}
		case idReferenceBlock:
			ref = true
		}
		return true
	})
	if err != nil {
		return nil, nil, err
	}
	if blkOff < 0 {
		return nil, nil, nil
	}
	t, es, err := r.parseBlock(src, blkOff, blkSize, tsTicks, false, ci)
	if err != nil || t == nil {
		return t, es, err
	}
	n := int64(len(es))
	if dur >= 0 {
		base := es[0].ts
		for i := range es {
			es[i].ts = base + dur*int64(i)/n
			es[i].dur = base + dur*int64(i+1)/n - es[i].ts
		}
	}
	for i := range es {
		if !ref {
			es[i].flags |= flagKey
		}
	}
	es[len(es)-1].pad = pad
	return t, es, nil
}

// parseBlock decodes a (Simple)Block header and its lacing into entries.
func (r *Reader) parseBlock(src *source, off, size, tsTicks int64, simple bool, ci int32) (*Track, []entry, error) {
	if size < 4 {
		return nil, nil, ErrInvalid
	}
	hdrMax := int64(16 + 8*maxLaces + size/255 + maxLaces)
	b, err := src.bytes(off, int(min(size, hdrMax)))
	if err != nil {
		return nil, nil, err
	}
	num, n, ok := readVint(b)
	if !ok || num <= 0 || len(b) < n+3 {
		return nil, nil, ErrInvalid
	}
	t := r.byNum[uint64(num)]
	if t == nil {
		return nil, nil, nil // a block for an undeclared track
	}
	rel := int64(int16(uint16(b[n])<<8 | uint16(b[n+1])))
	flags := b[n+2]
	p := n + 3
	var sizes []int64
	nl := 1
	lacing := (flags >> 1) & 3
	if lacing != 0 {
		if p >= len(b) {
			return nil, nil, ErrInvalid
		}
		nl = int(b[p]) + 1
		p++
	}
	switch lacing {
	case 1: // Xiph
		sizes = make([]int64, 0, nl)
		var sum int64
		for i := 0; i < nl-1; i++ {
			var v int64
			for {
				if p >= len(b) {
					return nil, nil, ErrInvalid
				}
				c := b[p]
				p++
				v += int64(c)
				if c != 255 {
					break
				}
			}
			sizes = append(sizes, v)
			sum += v
		}
		last := size - int64(p) - sum
		if last < 0 {
			return nil, nil, ErrInvalid
		}
		sizes = append(sizes, last)
	case 3: // EBML
		sizes = make([]int64, 0, nl)
		var sum int64
		if nl > 1 {
			v, m, ok := readVint(b[p:])
			if !ok || v < 0 {
				return nil, nil, ErrInvalid
			}
			p += m
			sizes = append(sizes, v)
			sum = v
			for i := 1; i < nl-1; i++ {
				d, m, ok := readSvint(b[p:])
				if !ok {
					return nil, nil, ErrInvalid
				}
				p += m
				v += d
				if v < 0 {
					return nil, nil, ErrInvalid
				}
				sizes = append(sizes, v)
				sum += v
			}
		}
		last := size - int64(p) - sum
		if last < 0 {
			return nil, nil, ErrInvalid
		}
		sizes = append(sizes, last)
	case 2: // fixed size
		rem := size - int64(p)
		if rem < 0 || rem%int64(nl) != 0 {
			return nil, nil, ErrInvalid
		}
		sizes = make([]int64, nl)
		for i := range sizes {
			sizes[i] = rem / int64(nl)
		}
	default:
		sizes = []int64{size - int64(p)}
	}
	tick := tsTicks + rel
	ts := tick*r.scale - int64(t.CodecDelay)
	var fl uint8
	if simple && flags&0x80 != 0 {
		fl |= flagKey
	}
	if flags&0x08 != 0 {
		fl |= flagInvisible
	}
	if simple && flags&0x01 != 0 {
		fl |= flagDiscardable
	}
	es := make([]entry, len(sizes))
	pos := off + int64(p)
	dd := int64(t.DefaultDuration)
	for i, sz := range sizes {
		if sz > 1<<31-1-int64(len(t.stripPrefix)) {
			return nil, nil, ErrInvalid
		}
		es[i] = entry{off: pos, size: int32(sz), cluster: ci, ts: ts, dur: -1, flags: fl}
		if dd > 0 {
			es[i].ts = ts + dd*int64(i)
			es[i].dur = dd
		}
		pos += sz
	}
	if pos > off+size {
		return nil, nil, ErrInvalid
	}
	return t, es, nil
}

// spread assigns timestamps and durations to one block's laces whose
// durations were unknown, given the timestamp of the track's next block.
func spread(es []entry, next int64) {
	n := int64(len(es))
	base := es[0].ts
	total := max(next-base, 0)
	for i := range es {
		es[i].ts = base + total*int64(i)/n
		es[i].dur = base + total*int64(i+1)/n - es[i].ts
	}
}

// fallbackDur is the per-frame duration for a final block whose duration
// can't be derived from a following block.
func fallbackDur(t *Track, prev int64) int64 {
	if t.DefaultDuration > 0 {
		return int64(t.DefaultDuration)
	}
	return max(prev, 0)
}

func finish(t *Track, es []entry, prev int64) {
	d := fallbackDur(t, prev)
	base := es[0].ts
	for i := range es {
		es[i].ts = base + d*int64(i)
		es[i].dur = d
	}
}

func (e *entry) sample(t *Track) Sample {
	return Sample{
		PTS:            durNS(e.ts),
		Duration:       durNS(max(e.dur, 0)),
		Keyframe:       e.flags&flagKey != 0,
		Discardable:    e.flags&flagDiscardable != 0,
		Invisible:      e.flags&flagInvisible != 0,
		DiscardPadding: durNS(e.pad),
		Offset:         e.off,
		Size:           int(e.size) + len(t.stripPrefix),
	}
}

func durNS(v int64) time.Duration { return time.Duration(v) }

// readEntry reads a frame payload, re-attaching a stripped header.
func readEntry(ra io.ReaderAt, t *Track, e entry, buf []byte) ([]byte, error) {
	if t.Compressed {
		return nil, ErrUnsupported
	}
	n := len(t.stripPrefix) + int(e.size)
	if cap(buf) < n {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	copy(buf, t.stripPrefix)
	m, err := ra.ReadAt(buf[len(t.stripPrefix):], e.off)
	if m < int(e.size) {
		if err == nil || err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("webm: frame at %d: %w", e.off, err)
	}
	return buf, nil
}
