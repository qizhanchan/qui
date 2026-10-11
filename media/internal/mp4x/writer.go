package mp4x

import (
	"encoding/binary"
	"errors"
	"io"
	"time"
)

// MP4 writer: progressive files (ftyp, mdat, moov at the end) and
// fragmented files (ftyp, moov with mvex, then moof+mdat fragments), for
// H.264 video (avcC) and AAC audio (esds).

// TrackConfig describes a track to write.
type TrackConfig struct {
	Kind      Kind
	Timescale uint32

	// Video (H.264): display size and parameter sets for avcC.
	Width, Height int
	SPS, PPS      [][]byte

	// Audio (AAC): AudioSpecificConfig for esds.
	SampleRate, Channels int
	ASC                  []byte

	// Delay is the composition delay of the first sample (timescale
	// units), written as the edit list's media_time so presentation starts
	// at zero. Progressive files default to the first sample's CTS offset;
	// fragmented files need it up front.
	Delay uint32
}

// Sample is one access unit. DTS and Duration are in track timescale
// units; CTSOffset is PTS - DTS (non-negative). Video data is
// length-prefixed (4-byte lengths).
type Sample struct {
	Data      []byte
	DTS       int64
	Duration  uint32
	CTSOffset int32
	Sync      bool
}

type wSample struct {
	size     uint32
	dur      uint32
	cts      int32
	sync     bool
	offset   uint64
	dts      int64
	pendData []byte // fragmented: data not yet written
}

type wTrack struct {
	cfg     TrackConfig
	id      uint32
	samples []wSample
	// Fragmented: samples of the fragment being built.
	frag    []wSample
	fragDTS int64
	nextDTS int64
	anyCTS  bool
	allSync bool
}

// Writer muxes tracks into an MP4 file.
type Writer struct {
	w          io.Writer
	ws         io.WriteSeeker
	fragmented bool
	fragDur    time.Duration
	tracks     []*wTrack
	started    bool
	pos        uint64 // bytes written
	mdatStart  uint64 // progressive: offset of the mdat header
	seq        uint32
	err        error
	closed     bool
}

// Create starts a progressive MP4 on ws (the moov is written by Close,
// after seeking back to fix the mdat size).
func Create(ws io.WriteSeeker) *Writer {
	return &Writer{w: ws, ws: ws}
}

// CreateFragmented starts a fragmented MP4 on w; a fragment is cut at the
// first video sync sample after fragDur (or every fragDur without video).
func CreateFragmented(w io.Writer, fragDur time.Duration) *Writer {
	if fragDur <= 0 {
		fragDur = 2 * time.Second
	}
	return &Writer{w: w, fragmented: true, fragDur: fragDur}
}

// AddTrack adds a track before the first sample and returns its index.
func (w *Writer) AddTrack(c TrackConfig) (int, error) {
	if w.started {
		return 0, errors.New("mp4x: AddTrack after the first sample")
	}
	if c.Timescale == 0 {
		return 0, errors.New("mp4x: track timescale is zero")
	}
	t := &wTrack{cfg: c, id: uint32(len(w.tracks) + 1), allSync: true}
	w.tracks = append(w.tracks, t)
	return len(w.tracks) - 1, nil
}

func (w *Writer) write(b []byte) {
	if w.err != nil {
		return
	}
	n, err := w.w.Write(b)
	w.pos += uint64(n)
	w.err = err
}

func (w *Writer) start() {
	w.started = true
	if w.fragmented {
		w.write(ftyp([]string{"iso6", "iso5", "avc1", "mp41"}, "iso5"))
		w.write(w.moov(true))
		return
	}
	w.write(ftyp([]string{"isom", "iso2", "avc1", "mp41"}, "isom"))
	w.mdatStart = w.pos
	// mdat with a 64-bit size, patched by Close.
	w.write([]byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0, 0, 0, 0, 0, 0, 0})
}

// WriteSample appends a sample to track ti.
func (w *Writer) WriteSample(ti int, s Sample) error {
	if w.closed {
		return errors.New("mp4x: write after Close")
	}
	if ti < 0 || ti >= len(w.tracks) {
		return errors.New("mp4x: bad track index")
	}
	if s.CTSOffset < 0 {
		return errors.New("mp4x: negative composition offset")
	}
	if !w.started {
		w.start()
	}
	t := w.tracks[ti]
	ws := wSample{size: uint32(len(s.Data)), dur: s.Duration, cts: s.CTSOffset, sync: s.Sync, dts: s.DTS}
	if s.CTSOffset != 0 {
		t.anyCTS = true
	}
	if !s.Sync {
		t.allSync = false
	}
	if w.fragmented {
		if w.shouldCut(t, s) {
			w.flushFragment()
		}
		if len(t.frag) == 0 {
			t.fragDTS = s.DTS
		}
		ws.pendData = append([]byte(nil), s.Data...)
		t.frag = append(t.frag, ws)
		t.nextDTS = s.DTS + int64(s.Duration)
		return w.err
	}
	ws.offset = w.pos
	w.write(s.Data)
	t.samples = append(t.samples, ws)
	t.nextDTS = s.DTS + int64(s.Duration)
	return w.err
}

// shouldCut reports whether a fragment ends before sample s of track t.
func (w *Writer) shouldCut(t *wTrack, s Sample) bool {
	var video *wTrack
	for _, x := range w.tracks {
		if x.cfg.Kind == KindVideo {
			video = x
			break
		}
	}
	lead := video
	if lead == nil {
		lead = w.tracks[0]
	}
	if t != lead || len(t.frag) == 0 {
		return false
	}
	if video != nil && !s.Sync {
		return false
	}
	d := time.Duration(s.DTS-t.fragDTS) * time.Second / time.Duration(t.cfg.Timescale)
	return d >= w.fragDur
}

// Close finishes the file.
func (w *Writer) Close() error {
	if w.closed {
		return w.err
	}
	w.closed = true
	if !w.started {
		w.start()
	}
	if w.fragmented {
		w.flushFragment()
		return w.err
	}
	end := w.pos
	if w.err == nil {
		if _, err := w.ws.Seek(int64(w.mdatStart+8), io.SeekStart); err != nil {
			return err
		}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], end-w.mdatStart)
		if _, err := w.ws.Write(b[:]); err != nil {
			return err
		}
		if _, err := w.ws.Seek(int64(end), io.SeekStart); err != nil {
			return err
		}
	}
	w.write(w.moov(false))
	return w.err
}

// boxBuf is a big-endian box builder.
type boxBuf struct{ b []byte }

func (x *boxBuf) u8(v uint8)   { x.b = append(x.b, v) }
func (x *boxBuf) u16(v uint16) { x.b = binary.BigEndian.AppendUint16(x.b, v) }
func (x *boxBuf) u32(v uint32) { x.b = binary.BigEndian.AppendUint32(x.b, v) }
func (x *boxBuf) u64(v uint64) { x.b = binary.BigEndian.AppendUint64(x.b, v) }
func (x *boxBuf) raw(v []byte) { x.b = append(x.b, v...) }
func (x *boxBuf) zeros(n int) {
	for i := 0; i < n; i++ {
		x.b = append(x.b, 0)
	}
}

// open starts a boxBuf and returns its start for close.
func (x *boxBuf) open(typ string) int {
	s := len(x.b)
	x.u32(0)
	x.raw([]byte(typ))
	return s
}

// full starts a full boxBuf.
func (x *boxBuf) full(typ string, version uint8, flags uint32) int {
	s := x.open(typ)
	x.u32(uint32(version)<<24 | flags&0xffffff)
	return s
}

func (x *boxBuf) close(s int) {
	binary.BigEndian.PutUint32(x.b[s:], uint32(len(x.b)-s))
}

func ftyp(brands []string, major string) []byte {
	var x boxBuf
	s := x.open("ftyp")
	x.raw([]byte(major))
	x.u32(0x200)
	for _, b := range brands {
		x.raw([]byte(b))
	}
	x.close(s)
	return x.b
}

var unityMatrix = [9]uint32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000}

const movieTimescale = 1000

func (t *wTrack) duration() int64 {
	if len(t.samples) == 0 {
		return 0
	}
	return t.nextDTS - t.samples[0].dts
}

func (w *Writer) moov(fragmented bool) []byte {
	var x boxBuf
	m := x.open("moov")
	var movieDur uint64
	for _, t := range w.tracks {
		d := uint64(t.duration()) * movieTimescale / uint64(t.cfg.Timescale)
		movieDur = max(movieDur, d)
	}
	mv := x.full("mvhd", 0, 0)
	x.u32(0)
	x.u32(0)
	x.u32(movieTimescale)
	x.u32(uint32(movieDur))
	x.u32(0x00010000) // rate
	x.u16(0x0100)     // volume
	x.zeros(10)
	for _, v := range unityMatrix {
		x.u32(v)
	}
	x.zeros(24)
	x.u32(uint32(len(w.tracks) + 1))
	x.close(mv)
	for _, t := range w.tracks {
		w.trak(&x, t, fragmented, movieDur)
	}
	if fragmented {
		mx := x.open("mvex")
		for _, t := range w.tracks {
			tr := x.full("trex", 0, 0)
			x.u32(t.id)
			x.u32(1) // default_sample_description_index
			x.u32(0)
			x.u32(0)
			x.u32(0)
			x.close(tr)
		}
		x.close(mx)
	}
	x.close(m)
	return x.b
}

func (w *Writer) trak(x *boxBuf, t *wTrack, fragmented bool, movieDur uint64) {
	c := &t.cfg
	tk := x.open("trak")
	th := x.full("tkhd", 0, 3)
	x.u32(0)
	x.u32(0)
	x.u32(t.id)
	x.u32(0)
	x.u32(uint32(uint64(t.duration()) * movieTimescale / uint64(c.Timescale)))
	x.zeros(8)
	x.u16(0) // layer
	x.u16(0) // alternate_group
	if c.Kind == KindAudio {
		x.u16(0x0100)
	} else {
		x.u16(0)
	}
	x.u16(0)
	for _, v := range unityMatrix {
		x.u32(v)
	}
	x.u32(uint32(c.Width) << 16)
	x.u32(uint32(c.Height) << 16)
	x.close(th)
	// Edit list: present from the first sample's composition time.
	delay := c.Delay
	if delay == 0 && !fragmented && len(t.samples) > 0 {
		delay = uint32(t.samples[0].cts)
	}
	if delay > 0 {
		ed := x.open("edts")
		el := x.full("elst", 0, 0)
		x.u32(1)
		if fragmented {
			x.u32(0) // the whole (still unknown) duration
		} else {
			x.u32(uint32(uint64(t.duration()) * movieTimescale / uint64(c.Timescale)))
		}
		x.u32(delay)
		x.u32(0x00010000)
		x.close(el)
		x.close(ed)
	}
	md := x.open("mdia")
	mh := x.full("mdhd", 0, 0)
	x.u32(0)
	x.u32(0)
	x.u32(c.Timescale)
	x.u32(uint32(t.duration()))
	x.u16(0x55c4) // "und"
	x.u16(0)
	x.close(mh)
	hd := x.full("hdlr", 0, 0)
	x.u32(0)
	if c.Kind == KindAudio {
		x.raw([]byte("soun"))
	} else {
		x.raw([]byte("vide"))
	}
	x.zeros(12)
	if c.Kind == KindAudio {
		x.raw([]byte("SoundHandler\x00"))
	} else {
		x.raw([]byte("VideoHandler\x00"))
	}
	x.close(hd)
	mi := x.open("minf")
	if c.Kind == KindAudio {
		sm := x.full("smhd", 0, 0)
		x.u32(0)
		x.close(sm)
	} else {
		vm := x.full("vmhd", 0, 1)
		x.zeros(8)
		x.close(vm)
	}
	di := x.open("dinf")
	dr := x.full("dref", 0, 0)
	x.u32(1)
	u := x.full("url ", 0, 1)
	x.close(u)
	x.close(dr)
	x.close(di)
	st := x.open("stbl")
	w.stsd(x, t)
	if fragmented {
		for _, typ := range []string{"stts", "stsc", "stco"} {
			b := x.full(typ, 0, 0)
			x.u32(0)
			x.close(b)
		}
		b := x.full("stsz", 0, 0)
		x.u32(0)
		x.u32(0)
		x.close(b)
	} else {
		w.sampleTables(x, t)
	}
	x.close(st)
	x.close(mi)
	x.close(md)
	x.close(tk)
}

func (w *Writer) stsd(x *boxBuf, t *wTrack) {
	c := &t.cfg
	sd := x.full("stsd", 0, 0)
	x.u32(1)
	if c.Kind == KindAudio {
		e := x.open("mp4a")
		x.zeros(6)
		x.u16(1)
		x.zeros(8)
		x.u16(uint16(c.Channels))
		x.u16(16)
		x.u16(0)
		x.u16(0)
		x.u32(uint32(c.SampleRate) << 16)
		es := x.full("esds", 0, 0)
		// ES_Descriptor
		dsi := len(c.ASC)
		dcd := 13 + 2 + dsi
		esd := 3 + 2 + dcd + 3
		x.u8(3)
		x.u8(uint8(esd))
		x.u16(uint16(t.id))
		x.u8(0)
		x.u8(4) // DecoderConfigDescriptor
		x.u8(uint8(dcd))
		x.u8(0x40) // MPEG-4 audio
		x.u8(0x15) // audio stream
		x.u8(0)
		x.u16(0)       // bufferSizeDB
		x.u32(0)       // maxBitrate
		x.u32(0)       // avgBitrate
		x.u8(5)        // DecoderSpecificInfo
		x.u8(uint8(dsi))
		x.raw(c.ASC)
		x.u8(6) // SLConfigDescriptor
		x.u8(1)
		x.u8(2)
		x.close(es)
		x.close(e)
	} else {
		e := x.open("avc1")
		x.zeros(6)
		x.u16(1)
		x.zeros(16)
		x.u16(uint16(c.Width))
		x.u16(uint16(c.Height))
		x.u32(0x00480000)
		x.u32(0x00480000)
		x.u32(0)
		x.u16(1)
		x.zeros(32)
		x.u16(0x0018)
		x.u16(0xffff)
		ac := x.open("avcC")
		x.raw(AVCDecoderConfig(c.SPS, c.PPS))
		x.close(ac)
		x.close(e)
	}
	x.close(sd)
}

// AVCDecoderConfig builds an AVCDecoderConfigurationRecord (4-byte NAL
// lengths) from SPS and PPS NAL units.
func AVCDecoderConfig(sps, pps [][]byte) []byte {
	var x boxBuf
	x.u8(1)
	if len(sps) > 0 && len(sps[0]) >= 4 {
		x.u8(sps[0][1])
		x.u8(sps[0][2])
		x.u8(sps[0][3])
	} else {
		x.zeros(3)
	}
	x.u8(0xfc | 3)
	x.u8(0xe0 | uint8(len(sps)))
	for _, s := range sps {
		x.u16(uint16(len(s)))
		x.raw(s)
	}
	x.u8(uint8(len(pps)))
	for _, p := range pps {
		x.u16(uint16(len(p)))
		x.raw(p)
	}
	if len(sps) > 0 && len(sps[0]) >= 2 {
		switch sps[0][1] {
		case 100, 110, 122, 144:
			x.u8(0xfc | 1) // chroma_format_idc 4:2:0
			x.u8(0xf8)     // bit_depth_luma_minus8
			x.u8(0xf8)     // bit_depth_chroma_minus8
			x.u8(0)        // numOfSequenceParameterSetExt
		}
	}
	return x.b
}

func (w *Writer) sampleTables(x *boxBuf, t *wTrack) {
	s := t.samples
	// stts
	b := x.full("stts", 0, 0)
	cnt := len(x.b)
	x.u32(0)
	n := 0
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && s[j].dur == s[i].dur {
			j++
		}
		x.u32(uint32(j - i))
		x.u32(s[i].dur)
		n++
		i = j
	}
	binary.BigEndian.PutUint32(x.b[cnt:], uint32(n))
	x.close(b)
	// ctts
	if t.anyCTS {
		b = x.full("ctts", 0, 0)
		cnt = len(x.b)
		x.u32(0)
		n = 0
		for i := 0; i < len(s); {
			j := i
			for j < len(s) && s[j].cts == s[i].cts {
				j++
			}
			x.u32(uint32(j - i))
			x.u32(uint32(s[i].cts))
			n++
			i = j
		}
		binary.BigEndian.PutUint32(x.b[cnt:], uint32(n))
		x.close(b)
	}
	// stss
	if !t.allSync {
		b = x.full("stss", 0, 0)
		cnt = len(x.b)
		x.u32(0)
		n = 0
		for i, v := range s {
			if v.sync {
				x.u32(uint32(i + 1))
				n++
			}
		}
		binary.BigEndian.PutUint32(x.b[cnt:], uint32(n))
		x.close(b)
	}
	// Chunks: runs of samples contiguous in the file.
	var chunkOff []uint64
	var chunkLen []int
	for i, v := range s {
		if i > 0 && s[i-1].offset+uint64(s[i-1].size) == v.offset {
			chunkLen[len(chunkLen)-1]++
			continue
		}
		chunkOff = append(chunkOff, v.offset)
		chunkLen = append(chunkLen, 1)
	}
	b = x.full("stsc", 0, 0)
	cnt = len(x.b)
	x.u32(0)
	n = 0
	for i, l := range chunkLen {
		if i > 0 && chunkLen[i-1] == l {
			continue
		}
		x.u32(uint32(i + 1))
		x.u32(uint32(l))
		x.u32(1)
		n++
	}
	binary.BigEndian.PutUint32(x.b[cnt:], uint32(n))
	x.close(b)
	b = x.full("stsz", 0, 0)
	x.u32(0)
	x.u32(uint32(len(s)))
	for _, v := range s {
		x.u32(v.size)
	}
	x.close(b)
	large := len(chunkOff) > 0 && chunkOff[len(chunkOff)-1] > 0xffffffff
	if large {
		b = x.full("co64", 0, 0)
	} else {
		b = x.full("stco", 0, 0)
	}
	x.u32(uint32(len(chunkOff)))
	for _, o := range chunkOff {
		if large {
			x.u64(o)
		} else {
			x.u32(uint32(o))
		}
	}
	x.close(b)
}

// flushFragment writes the buffered samples of every track as one
// moof + mdat.
func (w *Writer) flushFragment() {
	any := false
	for _, t := range w.tracks {
		if len(t.frag) > 0 {
			any = true
		}
	}
	if !any {
		return
	}
	w.seq++
	var x boxBuf
	mf := x.open("moof")
	h := x.full("mfhd", 0, 0)
	x.u32(w.seq)
	x.close(h)
	type patch struct {
		at   int
		data int // offset of the track's data within mdat payload
	}
	var patches []patch
	dataOff := 0
	for _, t := range w.tracks {
		if len(t.frag) == 0 {
			continue
		}
		tf := x.open("traf")
		th := x.full("tfhd", 0, 0x020000) // default-base-is-moof
		x.u32(t.id)
		x.close(th)
		td := x.full("tfdt", 1, 0)
		x.u64(uint64(t.fragDTS))
		x.close(td)
		flags := uint32(0x1 | 0x100 | 0x200 | 0x400)
		if t.anyCTS {
			flags |= 0x800
		}
		tr := x.full("trun", 0, flags)
		x.u32(uint32(len(t.frag)))
		patches = append(patches, patch{len(x.b), dataOff})
		x.u32(0) // data_offset, patched below
		for _, s := range t.frag {
			x.u32(s.dur)
			x.u32(s.size)
			if s.sync {
				x.u32(0x02000000)
			} else {
				x.u32(0x01010000)
			}
			if t.anyCTS {
				x.u32(uint32(s.cts))
			}
			dataOff += int(s.size)
		}
		x.close(tr)
		x.close(tf)
	}
	x.close(mf)
	moofLen := len(x.b)
	for _, p := range patches {
		binary.BigEndian.PutUint32(x.b[p.at:], uint32(moofLen+8+p.data))
	}
	md := x.open("mdat")
	for _, t := range w.tracks {
		for _, s := range t.frag {
			x.raw(s.pendData)
		}
	}
	x.close(md)
	w.write(x.b)
	for _, t := range w.tracks {
		for i := range t.frag {
			t.frag[i].pendData = nil
		}
		t.samples = append(t.samples, t.frag...)
		t.frag = t.frag[:0]
	}
}
