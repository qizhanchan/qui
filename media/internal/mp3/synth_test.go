package mp3

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic bitstream tests: random but syntactically valid frames that
// exercise what LAME and FFmpeg's encoders never emit — Layer I, every
// Layer II allocation table and scfsi pattern, Layer III intensity stereo
// (MPEG-1 and LSF), mixed blocks, all Huffman tables including escapes —
// decoded by us and by FFmpeg, then compared.

type bitWriter struct {
	buf  []byte
	nbit int
}

func (w *bitWriter) write(v, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.nbit%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if (v>>uint(i))&1 != 0 {
			w.buf[len(w.buf)-1] |= 0x80 >> uint(w.nbit%8)
		}
		w.nbit++
	}
}

func makeHeader(version, layer, brIdx, srIdx, mode, modeExt int, pad bool) uint32 {
	h := uint32(0xffe00000)
	h |= uint32(version) << 19
	h |= uint32(4-layer) << 17
	h |= 1 << 16 // no CRC
	h |= uint32(brIdx) << 12
	h |= uint32(srIdx) << 10
	if pad {
		h |= 1 << 9
	}
	h |= uint32(mode) << 6
	h |= uint32(modeExt) << 4
	return h
}

func putHeader(w *bitWriter, h uint32) { w.write(int(h), 32) }

// finishFrame pads the frame to its nominal size; ok is false when the
// payload did not fit.
func finishFrame(w *bitWriter, size int) ([]byte, bool) {
	if len(w.buf) > size {
		return nil, false
	}
	out := make([]byte, size)
	copy(out, w.buf)
	return out, true
}

// ---- Layer I / II ----

func genLayer1(rng *rand.Rand, h Header, hdr uint32) ([]byte, bool) {
	var w bitWriter
	putHeader(&w, hdr)
	nch := h.Channels()
	bound := 32
	if h.Mode == ModeJointStereo {
		bound = (h.ModeExt + 1) * 4
	}
	var alloc [2][32]int
	for sb := 0; sb < 32; sb++ {
		if sb < bound {
			for ch := 0; ch < nch; ch++ {
				if rng.Intn(3) != 0 {
					alloc[ch][sb] = rng.Intn(9)
				}
				w.write(alloc[ch][sb], 4)
			}
		} else {
			a := rng.Intn(6)
			alloc[0][sb], alloc[1][sb] = a, a
			w.write(a, 4)
		}
	}
	for sb := 0; sb < 32; sb++ {
		for ch := 0; ch < nch; ch++ {
			if alloc[ch][sb] != 0 {
				w.write(rng.Intn(63), 6)
			}
		}
	}
	for s := 0; s < 12; s++ {
		for sb := 0; sb < 32; sb++ {
			n := nch
			if sb >= bound {
				n = 1
			}
			for ch := 0; ch < n; ch++ {
				if a := alloc[ch][sb]; a != 0 {
					nb := a + 1
					w.write(rng.Intn(1<<uint(nb)-1), nb)
				}
			}
		}
	}
	return finishFrame(&w, h.FrameSize())
}

func genLayer2(rng *rand.Rand, h Header, hdr uint32) ([]byte, bool) {
	var w bitWriter
	putHeader(&w, hdr)
	nch := h.Channels()
	tab := &layer2Tables[layer2Table(h)]
	bound := tab.sblimit
	if h.Mode == ModeJointStereo {
		bound = (h.ModeExt + 1) * 4
		if bound > tab.sblimit {
			bound = tab.sblimit
		}
	}
	var alloc [2][32]int
	for sb := 0; sb < tab.sblimit; sb++ {
		b := &tab.bands[sb]
		n := nch
		if sb >= bound {
			n = 1
		}
		for ch := 0; ch < n; ch++ {
			a := 0
			if rng.Intn(3) != 0 {
				a = rng.Intn(1 << uint(b.nbal))
				if a > 6 {
					a = rng.Intn(7)
				}
			}
			alloc[ch][sb] = a
			w.write(a, b.nbal)
		}
		if sb >= bound {
			alloc[1][sb] = alloc[0][sb]
		}
	}
	var scfsi [2][32]int
	for sb := 0; sb < tab.sblimit; sb++ {
		for ch := 0; ch < nch; ch++ {
			if alloc[ch][sb] != 0 {
				scfsi[ch][sb] = rng.Intn(4)
				w.write(scfsi[ch][sb], 2)
			}
		}
	}
	for sb := 0; sb < tab.sblimit; sb++ {
		for ch := 0; ch < nch; ch++ {
			if alloc[ch][sb] == 0 {
				continue
			}
			n := [4]int{3, 2, 1, 2}[scfsi[ch][sb]]
			for i := 0; i < n; i++ {
				w.write(rng.Intn(63), 6)
			}
		}
	}
	for gr := 0; gr < 12; gr++ {
		for sb := 0; sb < tab.sblimit; sb++ {
			n := nch
			if sb >= bound {
				n = 1
			}
			for ch := 0; ch < n; ch++ {
				a := alloc[ch][sb]
				if a == 0 {
					continue
				}
				q := int(tab.bands[sb].quants[a-1])
				steps, bits := quantSteps[q], quantBits[q]
				if bits < 0 {
					c := rng.Intn(steps) + steps*(rng.Intn(steps)+steps*rng.Intn(steps))
					w.write(c, -bits)
				} else {
					for i := 0; i < 3; i++ {
						w.write(rng.Intn(steps), bits)
					}
				}
			}
		}
	}
	return finishFrame(&w, h.FrameSize())
}

// ---- Layer III ----

func huffEncode(w *bitWriter, tab, sym int) {
	for _, src := range huffSrc {
		if src.id == tab {
			w.write(int(src.code[sym]), int(src.len[sym]))
			return
		}
	}
	panic("no table")
}

type genGranule struct {
	granule
	side bitWriter // side info (part2_3_length patched in later)
	bits bitWriter // part2 + part3
	vals [576]int
}

// genGranuleData fills g with random side info and spectral values and
// encodes its main data. lowOnly limits nonzero values to the low lines
// (used for the intensity-stereo channel so upper bands become IS bands).
func genGranuleData(rng *rand.Rand, g *genGranule, h Header, ch, gr int, modeExt int, lowOnly bool, bt int, mixed bool) {
	lsf := h.LSF()
	g.globalGain = 100 + rng.Intn(50)
	g.blockType = bt
	g.switchPoint = bt == 2 && mixed
	if lsf {
		g.sfCompress = rng.Intn(512)
	} else {
		g.sfCompress = rng.Intn(16)
	}
	g.sfScale = rng.Intn(2)
	g.count1Table = rng.Intn(2)
	if !lsf {
		g.preflag = rng.Intn(2)
	}
	for i := range g.subGain {
		g.subGain[i] = rng.Intn(8)
	}
	validTab := func() int {
		for {
			t := rng.Intn(32)
			if t != 4 && t != 14 {
				return t
			}
		}
	}
	maxBig := 120
	if lowOnly {
		maxBig = 20
	}
	g.bigValues = rng.Intn(maxBig)
	for i := range g.tableSelect {
		g.tableSelect[i] = validTab()
	}
	ra1, ra2 := rng.Intn(16), rng.Intn(8)
	if g.blockType == 0 {
		g.tableSelect[2] = validTab()
	}
	// Derive region sizes / band layout exactly as the decoder will.
	var sw bitWriter
	writeSide := func(w *bitWriter) {
		w.write(0, 12) // part2_3_length placeholder
		w.write(g.bigValues, 9)
		w.write(g.globalGain, 8)
		if lsf {
			w.write(g.sfCompress, 9)
		} else {
			w.write(g.sfCompress, 4)
		}
		if g.blockType != 0 {
			w.write(1, 1)
			w.write(g.blockType, 2)
			sp := 0
			if g.switchPoint {
				sp = 1
			}
			w.write(sp, 1)
			w.write(g.tableSelect[0], 5)
			w.write(g.tableSelect[1], 5)
			for i := 0; i < 3; i++ {
				w.write(g.subGain[i], 3)
			}
		} else {
			w.write(0, 1)
			for i := 0; i < 3; i++ {
				w.write(g.tableSelect[i], 5)
			}
			w.write(ra1, 4)
			w.write(ra2, 3)
		}
		if !lsf {
			w.write(g.preflag, 1)
		}
		w.write(g.sfScale, 1)
		w.write(g.count1Table, 1)
	}
	writeSide(&sw)
	var br bitReader
	br.init(sw.buf)
	var parsed granule
	if err := readGranuleInfo(&br, &parsed, h, 0); err != nil {
		panic(err)
	}
	g.regionSize = parsed.regionSize
	g.longEnd, g.shortStart = parsed.longEnd, parsed.shortStart
	g.side = sw

	// Scale factors.
	var w bitWriter
	if lsf {
		var dummy Decoder
		var sfw bitWriter
		// Find slen layout by letting the decoder parse an all-ones
		// stream: the count of bits read per band tells the widths.
		ones := bytes.Repeat([]byte{0xff}, 64)
		var rd bitReader
		rd.init(ones)
		tmp := g.granule
		dummy.lsfScaleFactors(&rd, &tmp, ch, modeExt)
		for i := 0; i < 40; i++ {
			// tmp.sf[i] = 2^slen-1 reveals slen.
			slen := 0
			for (1<<uint(slen))-1 < tmp.sf[i] {
				slen++
			}
			if slen > 0 {
				v := rng.Intn(1 << uint(slen))
				sfw.write(v, slen)
			}
		}
		w = sfw
	} else {
		slen1, slen2 := slenTable[0][g.sfCompress], slenTable[1][g.sfCompress]
		if g.blockType == 2 {
			n := 18
			if g.switchPoint {
				n = 17
			}
			for i := 0; i < n; i++ {
				w.write(rng.Intn(1<<uint(slen1)), slen1)
			}
			for i := 0; i < 18; i++ {
				w.write(rng.Intn(1<<uint(slen2)), slen2)
			}
		} else {
			for k := 0; k < 4; k++ {
				n := 5
				if k == 0 {
					n = 6
				}
				if gr == 1 && g.scfsi&(8>>uint(k)) != 0 {
					continue
				}
				slen := slen2
				if k < 2 {
					slen = slen1
				}
				for i := 0; i < n; i++ {
					w.write(rng.Intn(1<<uint(slen)), slen)
				}
			}
		}
	}
	// Big values.
	s := 0
	for i := 0; i < 3; i++ {
		sel := huffSelect[g.tableSelect[i]]
		for j := 0; j < g.regionSize[i]; j++ {
			if sel.tab == 0 {
				s += 2
				continue
			}
			xlen := huffTrees[sel.tab].xlen
			var v [2]int
			for k := range v {
				switch r := rng.Intn(10); {
				case r < 3:
					v[k] = 0
				case sel.linbits > 0 && r == 9:
					v[k] = 15 + rng.Intn(1<<uint(min(sel.linbits, 8)))
				default:
					v[k] = rng.Intn(xlen)
				}
			}
			x, y := v[0], v[1]
			if x > 15 {
				x = 15
			}
			if y > 15 {
				y = 15
			}
			huffEncode(&w, sel.tab, x*xlen+y)
			for k := range v {
				if v[k] >= 15 && sel.linbits > 0 {
					w.write(v[k]-15, sel.linbits)
				}
				if v[k] != 0 {
					w.write(rng.Intn(2), 1)
				}
			}
			s += 2
		}
	}
	// Count1.
	n1 := rng.Intn(24)
	if lowOnly {
		n1 = rng.Intn(4)
	}
	for q := 0; q < n1 && s <= 572; q++ {
		sym := rng.Intn(16)
		huffEncode(&w, 32+g.count1Table, sym)
		for k := 0; k < 4; k++ {
			if sym&(8>>uint(k)) != 0 {
				w.write(rng.Intn(2), 1)
			}
		}
		s += 4
	}
	g.bits = w
	g.part23 = w.nbit
}

// l3Gen generates Layer III frames. With legal set, block types follow the
// standard window sequence per channel (long -> start -> short -> stop ->
// long) and a run of short blocks is either all mixed or all pure: FFmpeg's
// short-block IMDCT drops the last 6 overlap samples of the previous
// granule, which are zero only on such sequences. Arbitrary sequences
// (mixed <-> pure, long <-> short without start/stop windows) follow the
// standard, and are checked against mpg123 instead.
type l3Gen struct {
	legal    bool
	prevBT   [2]int
	mixedRun [2]bool // legal mode: whether the current short run is mixed
}

func (l *l3Gen) blockType(rng *rand.Rand, ch int) (bt int, mixed bool) {
	if !l.legal {
		return rng.Intn(4), rng.Intn(2) == 0
	}
	p := l.prevBT[ch]
	switch p {
	case 0, 3:
		bt = []int{0, 0, 1}[rng.Intn(3)]
	case 1:
		bt = 2
		l.mixedRun[ch] = rng.Intn(2) == 0
	case 2:
		bt = []int{2, 3}[rng.Intn(2)]
	}
	return bt, bt == 2 && l.mixedRun[ch]
}

func (l *l3Gen) gen(rng *rand.Rand, h Header, hdr uint32) (frame []byte, ok bool) {
	save := l.prevBT
	defer func() {
		if !ok {
			l.prevBT = save
		}
	}()
	nch := h.Channels()
	ngr := 2
	if h.LSF() {
		ngr = 1
	}
	modeExt := 0
	if h.Mode == ModeJointStereo {
		modeExt = h.ModeExt
	}
	var gs [2][2]genGranule
	var scfsi [2]int
	for ch := 0; ch < nch; ch++ {
		if !h.LSF() {
			scfsi[ch] = rng.Intn(16)
		}
	}
	for gr := 0; gr < ngr; gr++ {
		for ch := 0; ch < nch; ch++ {
			g := &gs[ch][gr]
			if gr == 1 {
				g.scfsi = scfsi[ch]
			}
			bt, mixed := l.blockType(rng, ch)
			genGranuleData(rng, g, h, ch, gr, modeExt, ch == 1 && modeExt&modeExtIS != 0, bt, mixed)
			l.prevBT[ch] = bt
		}
	}
	var w bitWriter
	putHeader(&w, hdr)
	if h.LSF() {
		w.write(0, 8)
		w.write(0, nch)
	} else {
		w.write(0, 9)
		if nch == 1 {
			w.write(0, 5)
		} else {
			w.write(0, 3)
		}
		for ch := 0; ch < nch; ch++ {
			w.write(scfsi[ch], 4)
		}
	}
	for gr := 0; gr < ngr; gr++ {
		for ch := 0; ch < nch; ch++ {
			g := &gs[ch][gr]
			if g.part23 > 4095 {
				return nil, false
			}
			side := g.side
			// Patch part2_3_length into the first 12 bits.
			side.buf[0] = byte(g.part23 >> 4)
			side.buf[1] = side.buf[1]&0x0f | byte(g.part23&15)<<4
			for i := 0; i < side.nbit; i++ {
				w.write(int(side.buf[i/8]>>uint(7-i%8))&1, 1)
			}
		}
	}
	for gr := 0; gr < ngr; gr++ {
		for ch := 0; ch < nch; ch++ {
			b := &gs[ch][gr].bits
			for i := 0; i < b.nbit; i++ {
				w.write(int(b.buf[i/8]>>uint(7-i%8))&1, 1)
			}
		}
	}
	return finishFrame(&w, h.FrameSize())
}

type synthCase struct {
	name                      string
	version, layer, brIdx, sr int
	mode, modeExt             int // modeExt -1 = random per frame
	gen                       func(*rand.Rand, Header, uint32) ([]byte, bool)
	ffcodec                   string
}

func TestSyntheticMatchesFFmpeg(t *testing.T) {
	cases := []synthCase{
		{"l1_stereo48k", MPEG1, 1, 14, 1, ModeStereo, 0, genLayer1, "mp1float"},
		{"l1_joint44k", MPEG1, 1, 14, 0, ModeJointStereo, -1, genLayer1, "mp1float"},
		{"l1_mono_lsf22k", MPEG2, 1, 14, 0, ModeMono, 0, genLayer1, "mp1float"},
		{"l2_tableA_48k", MPEG1, 2, 14, 1, ModeStereo, 0, genLayer2, "mp2float"},
		{"l2_tableB_joint44k", MPEG1, 2, 13, 0, ModeJointStereo, -1, genLayer2, "mp2float"},
		{"l2_tableC_mono48k", MPEG1, 2, 2, 1, ModeMono, 0, genLayer2, "mp2float"},
		{"l2_tableD_mono32k", MPEG1, 2, 2, 2, ModeMono, 0, genLayer2, "mp2float"},
		{"l2_lsf_joint16k", MPEG2, 2, 14, 2, ModeJointStereo, -1, genLayer2, "mp2float"},
		{"l3_mpeg1_stereo48k", MPEG1, 3, 14, 1, ModeStereo, 0, (&l3Gen{legal: true}).gen, "mp3float"},
		{"l3_mpeg1_stereo48k_anyseq", MPEG1, 3, 14, 1, ModeStereo, 0, (&l3Gen{}).gen, "mp3float"},
		{"l3_mpeg1_is_ms48k", MPEG1, 3, 14, 1, ModeJointStereo, -1, (&l3Gen{legal: true}).gen, "mp3float"},
		{"l3_mpeg1_is_ms48k_anyseq", MPEG1, 3, 14, 1, ModeJointStereo, -1, (&l3Gen{}).gen, "mp3float"},
		{"l3_mpeg1_mono32k", MPEG1, 3, 14, 2, ModeMono, 0, (&l3Gen{legal: true}).gen, "mp3float"},
		{"l3_mpeg1_mono32k_anyseq", MPEG1, 3, 14, 2, ModeMono, 0, (&l3Gen{}).gen, "mp3float"},
		{"l3_lsf_is_ms24k", MPEG2, 3, 14, 1, ModeJointStereo, -1, (&l3Gen{legal: true}).gen, "mp3float"},
		{"l3_lsf_is_ms24k_anyseq", MPEG2, 3, 14, 1, ModeJointStereo, -1, (&l3Gen{}).gen, "mp3float"},
		{"l3_lsf_mono16k", MPEG2, 3, 14, 2, ModeMono, 0, (&l3Gen{legal: true}).gen, "mp3float"},
		{"l3_lsf_mono16k_anyseq", MPEG2, 3, 14, 2, ModeMono, 0, (&l3Gen{}).gen, "mp3float"},
		{"l3_mpeg25_is11k", MPEG25, 3, 14, 0, ModeJointStereo, -1, (&l3Gen{legal: true}).gen, "mp3float"},
		{"l3_mpeg25_is11k_anyseq", MPEG25, 3, 14, 0, ModeJointStereo, -1, (&l3Gen{}).gen, "mp3float"},
		{"l3_mpeg25_8k", MPEG25, 3, 14, 2, ModeJointStereo, -1, (&l3Gen{legal: true}).gen, "mp3float"},
		{"l3_mpeg25_8k_anyseq", MPEG25, 3, 14, 2, ModeJointStereo, -1, (&l3Gen{}).gen, "mp3float"},
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	for ci, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(ci) + 1))
			var file []byte
			nframes := 0
			for tries := 0; nframes < 40 && tries < 4000; tries++ {
				me := c.modeExt
				if me < 0 {
					me = rng.Intn(4)
				}
				hdr := makeHeader(c.version, c.layer, c.brIdx, c.sr, c.mode, me, false)
				h, ok := parseHeader(hdr)
				if !ok {
					t.Fatal("bad header")
				}
				fr, ok := c.gen(rng, h, hdr)
				if !ok {
					continue
				}
				file = append(file, fr...)
				nframes++
			}
			if nframes < 40 {
				t.Fatalf("only %d frames fit", nframes)
			}
			path := filepath.Join(dir, c.name+".mp3")
			if err := os.WriteFile(path, file, 0o644); err != nil {
				t.Fatal(err)
			}
			var got []float32
			d := NewDecoder()
			for p := 0; p < len(file); {
				h, _ := ParseHeader(file[p:])
				var err error
				got, err = d.Decode(file[p:p+h.FrameSize()], got)
				if err != nil {
					t.Fatalf("frame at %d: %v", p, err)
				}
				p += h.FrameSize()
			}
			for i, v := range got {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || math.Abs(float64(v)) > 64 {
					t.Fatalf("sample %d = %v", i, v)
				}
			}
			anySeq := strings.HasSuffix(c.name, "_anyseq")
			ref := ffmpegDecodeFmt(t, path, c.ffcodec)
			if len(ref) != len(got) {
				t.Fatalf("length %d, ffmpeg %d", len(got), len(ref))
			}
			maxErr, snr := compare(got, ref)
			t.Logf("%s: %d frames, ffmpeg %s: max abs err %.3g, SNR %.1f dB", c.name, nframes, c.ffcodec, maxErr, snr)
			if !anySeq && snr < 90 {
				t.Errorf("SNR vs FFmpeg %.1f dB < 90 dB", snr)
			}
			if ref := mpg123Decode(t, path); ref != nil && len(ref) == len(got) {
				maxErr, snr := compare(got, ref)
				t.Logf("%s: mpg123: max abs err %.3g, SNR %.1f dB", c.name, maxErr, snr)
				// mpg123 differs from FFmpeg (and us) on intensity stereo
				// limits and the 8 kHz band tables; elsewhere it is the
				// reference for window sequences FFmpeg mishandles.
				if anySeq && !strings.Contains(c.name, "_is") && !strings.Contains(c.name, "8k") && snr < 90 {
					t.Errorf("SNR vs mpg123 %.1f dB < 90 dB", snr)
				}
			}
		})
	}
}

func ffmpegDecodeFmt(t *testing.T, path, codec string) []float32 {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "mp3", "-c:a", codec, "-i", path, "-f", "f32le", "-")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, errb.String())
	}
	if errb.Len() > 0 {
		t.Logf("ffmpeg stderr: %s", errb.String())
	}
	ref := make([]float32, out.Len()/4)
	_ = binary.Read(&out, binary.LittleEndian, ref)
	return ref
}

// mpg123Decode decodes path with the mpg123 CLI (float output, no gapless
// trimming); nil when mpg123 is not installed.
func mpg123Decode(t *testing.T, path string) []float32 {
	t.Helper()
	if _, err := exec.LookPath("mpg123"); err != nil {
		return nil
	}
	out, err := exec.Command("mpg123", "-q", "--no-gapless", "-e", "f32", "-s", path).Output()
	if err != nil {
		t.Fatalf("mpg123: %v", err)
	}
	ref := make([]float32, len(out)/4)
	_ = binary.Read(bytes.NewReader(out), binary.LittleEndian, ref)
	return ref
}
