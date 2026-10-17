package av1

import (
	"fmt"
)

// Decoder decodes an AV1 bitstream (low-overhead OBU format) one temporal
// unit at a time.
type Decoder struct {
	seq *SequenceHeader
	ref [numRefFrames]refSlot

	hdr *FrameHeader // current frame header
	fs  *frameState  // frame being decoded
	out []*frame     // shown frames not yet returned

	// outputInvisible also outputs frames that are not shown (as dav1d's
	// --outputinvisible does), for testing.
	outputInvisible bool

	// noPipeline runs the in-loop filters after decoding even for
	// single-tile frames (see pipeline.go), for testing.
	noPipeline bool
	pipelined  int // frames filtered by the pipeline

	// noFilmGrain skips film grain synthesis on output.
	noFilmGrain     bool
	seenFrameHeader bool
	tileNum         int // next expected tile

	// trace, when set, receives the header syntax elements (see bitReader)
	// with positions relative to the start of each OBU.
	trace func(pos, val int)

	scratch []*tileScratch // per tile-decoding goroutine

	tag   int64    // SetTag value for the frames Decode outputs
	ready []*Frame // converted output frames (Decode / Next)

	// Plane buffers of intermediate post-filter frames, for reuse.
	free8  [][]uint8
	free16 [][]uint16
}

// New returns a decoder.
func New() *Decoder { return &Decoder{} }

// Sequence returns the active sequence header, or nil.
func (d *Decoder) Sequence() *SequenceHeader { return d.seq }

// DecodeTemporalUnit decodes one temporal unit (the OBUs of an IVF frame
// or an MP4 sample).
func (d *Decoder) DecodeTemporalUnit(b []byte) error {
	obus, err := splitOBUs(b)
	if err != nil {
		return err
	}
	for i := range obus {
		if err := d.decodeOBU(&obus[i]); err != nil {
			return err
		}
	}
	return nil
}

// reader returns a bit reader over o's payload whose trace positions are
// relative to the start of the OBU, like FFmpeg's.
func (d *Decoder) reader(o *obu) *bitReader {
	return &bitReader{data: o.payload, trace: d.trace, base: 8 * o.hdrLen}
}

func (d *Decoder) decodeOBU(o *obu) error {
	if o.typ != obuSequenceHeader && o.typ != obuTemporalDelimiter && d.seq != nil && d.seq.OperatingPointIdc != 0 && o.hasExt {
		idc := d.seq.OperatingPointIdc
		if (idc>>o.temporalID)&1 == 0 || (idc>>(o.spatialID+8))&1 == 0 {
			return nil
		}
	}
	switch o.typ {
	case obuSequenceHeader:
		s, err := parseSequenceHeader(d.reader(o))
		if err != nil {
			return err
		}
		d.seq = s
	case obuTemporalDelimiter:
		d.seenFrameHeader = false
	case obuFrameHeader, obuRedundantFrameHeader, obuFrame:
		r := d.reader(o)
		if d.seenFrameHeader {
			if o.typ == obuFrame {
				return fmt.Errorf("%w: frame OBU while a frame is in progress", ErrInvalid)
			}
			return nil // frame_header_copy
		}
		h, err := d.parseFrameHeader(r, o.temporalID, o.spatialID)
		if err != nil {
			return err
		}
		d.hdr = h
		if h.ShowExistingFrame {
			return d.showExisting(h)
		}
		d.seenFrameHeader = true
		d.tileNum = 0
		if debugFrame != nil {
			debugFrame()
		}
		if err := d.startFrame(h); err != nil {
			return err
		}
		if o.typ == obuFrame {
			r.byteAlign()
			return d.tileGroup(o.payload[r.pos>>3:], r.base+r.pos)
		}
	case obuTileGroup:
		if !d.seenFrameHeader {
			return fmt.Errorf("%w: tile group without a frame header", ErrInvalid)
		}
		return d.tileGroup(o.payload, 8*o.hdrLen)
	}
	return nil
}

// tileGroup parses tile_group_obu (spec 5.11.1) and decodes its tiles.
// base is the bit offset of b in its OBU (for tracing).
func (d *Decoder) tileGroup(b []byte, base int) error {
	h := d.hdr
	t := &h.tiles
	r := &bitReader{data: b, trace: d.trace, base: base}
	numTiles := t.cols * t.rows
	start, end := 0, numTiles-1
	if numTiles > 1 && r.flag() {
		bits := t.colsLog2 + t.rowsLog2
		start = r.f(bits)
		end = r.f(bits)
	}
	r.byteAlign()
	if start != d.tileNum || end < start || end >= numTiles {
		return fmt.Errorf("%w: tile group %d..%d (expected start %d of %d)", ErrInvalid, start, end, d.tileNum, numTiles)
	}
	p := r.pos >> 3
	type job struct {
		tn   int
		data []byte
	}
	var jobs []job
	for tn := start; tn <= end; tn++ {
		size := len(b) - p
		if tn != end {
			if p+t.sizeBytes > len(b) {
				return fmt.Errorf("%w: truncated tile size", ErrInvalid)
			}
			size = 0
			for i := 0; i < t.sizeBytes; i++ {
				size |= int(b[p+i]) << (8 * i)
			}
			size++
			p += t.sizeBytes
			if size > len(b)-p {
				return fmt.Errorf("%w: tile of %d bytes exceeds the %d left", ErrInvalid, size, len(b)-p)
			}
		}
		jobs = append(jobs, job{tn, b[p : p+size]})
		p += size
	}
	// Tiles are independent (contexts, availability and entropy state
	// are per tile; each writes only its own region of the frame state),
	// so they decode concurrently.
	nw := min(workers(), len(jobs))
	for len(d.scratch) < nw {
		d.scratch = append(d.scratch, new(tileScratch))
	}
	errs := make([]error, len(jobs))
	if start == 0 && end == numTiles-1 {
		d.startPipe()
		if p := d.fs.pipe; p != nil {
			// A frame that fails (or panics) stops its filters before
			// the decoder lets go of it.
			defer func() {
				if d.fs != nil && d.fs.pipe == p {
					d.fs.pipe = nil
					p.abort()
				}
			}()
		}
	}
	parallelFor(len(jobs), func(w, i int) {
		errs[i] = d.decodeTile(jobs[i].tn, jobs[i].data, d.scratch[w])
	})
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	d.tileNum = end + 1
	if end == numTiles-1 {
		d.seenFrameHeader = false
		return d.finishFrame()
	}
	return nil
}
