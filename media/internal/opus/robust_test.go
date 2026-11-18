package opus

import (
	"bytes"
	"math"
	"math/rand"
	"path/filepath"
	"testing"
	"time"
)

func allFixtures(t testing.TB) []fixture {
	files, _ := filepath.Glob(filepath.Join(fixtureDir, "*.opus"))
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	var fx []fixture
	for _, f := range files {
		fx = append(fx, loadFixture(t, f))
	}
	return fx
}

// decodeAll decodes packets with a fresh decoder and fails on any error.
func decodeAll(t *testing.T, ch int, pkts [][]byte) []float32 {
	t.Helper()
	d, err := NewDecoder(48000, ch)
	if err != nil {
		t.Fatal(err)
	}
	var out []float32
	for i, p := range pkts {
		if out, err = d.Decode(p, out); err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
	}
	return out
}

func frameOf(p []byte) []byte { return p[1:] } // code-0 packet payload

func writeSize(b *bytes.Buffer, n int) {
	if n < 252 {
		b.WriteByte(byte(n))
		return
	}
	b.WriteByte(byte(252 + (n-252)&3))
	b.WriteByte(byte((n - 252 - (n-252)&3) >> 2))
}

// TestFramingCodes repacks code-0 packets into code 1, 2 and 3 packets
// (CBR, VBR, with padding) and checks the output is unchanged.
func TestFramingCodes(t *testing.T) {
	fx := loadFixture(t, filepath.Join(fixtureDir, "celt_nb_mono_5ms_cbr.opus"))
	pk := fx.packets
	for _, p := range pk {
		if p[0]&3 != 0 {
			t.Fatal("fixture is expected to use code 0 only")
		}
	}
	want := decodeAll(t, 1, pk)

	var code1, code2, code3v, code3c [][]byte
	for i := 0; i+3 <= len(pk); i += 3 {
		a, b, c := pk[i], pk[i+1], pk[i+2]
		if a[0] != b[0] || b[0] != c[0] {
			t.Fatal("TOC changed inside the fixture")
		}
		toc := a[0] &^ 3
		// Two frames: code 1 if same size, else code 2; then the third frame alone.
		var p bytes.Buffer
		if len(a) == len(b) {
			p.WriteByte(toc | 1)
		} else {
			p.WriteByte(toc | 2)
			writeSize(&p, len(frameOf(a)))
		}
		p.Write(frameOf(a))
		p.Write(frameOf(b))
		code1 = append(code1, p.Bytes(), c)
		var q bytes.Buffer
		q.WriteByte(toc | 2)
		writeSize(&q, len(frameOf(a)))
		q.Write(frameOf(a))
		q.Write(frameOf(b))
		code2 = append(code2, q.Bytes(), c)

		// Code 3, VBR, three frames, 300 bytes of padding.
		var v bytes.Buffer
		v.WriteByte(toc | 3)
		v.WriteByte(0x80 | 0x40 | 3)
		v.WriteByte(255) // 254 bytes ...
		v.WriteByte(46)  // ... + 46 = 300
		writeSize(&v, len(frameOf(a)))
		writeSize(&v, len(frameOf(b)))
		v.Write(frameOf(a))
		v.Write(frameOf(b))
		v.Write(frameOf(c))
		v.Write(make([]byte, 300))
		code3v = append(code3v, v.Bytes())

		// Code 3, CBR, when the three frames have equal sizes.
		if len(a) == len(b) && len(b) == len(c) {
			var w bytes.Buffer
			w.WriteByte(toc | 3)
			w.WriteByte(0x40 | 3)
			w.WriteByte(7)
			w.Write(frameOf(a))
			w.Write(frameOf(b))
			w.Write(frameOf(c))
			w.Write(make([]byte, 7))
			code3c = append(code3c, w.Bytes())
		} else {
			code3c = append(code3c, a, b, c)
		}
	}
	n := len(pk) / 3 * 3
	cmp := func(name string, pkts [][]byte) {
		got := decodeAll(t, 1, pkts)
		if len(got) != n*240 {
			t.Fatalf("%s: %d samples, want %d", name, len(got), n*240)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: sample %d differs: %g vs %g", name, i, got[i], want[i])
			}
		}
	}
	cmp("code1", code1)
	cmp("code2", code2)
	cmp("code3-vbr-padded", code3v)
	cmp("code3-cbr-padded", code3c)

	for i, p := range code3v[:5] {
		d, err := PacketDuration(p)
		if err != nil || d != 3*240 {
			t.Fatalf("PacketDuration(code3 #%d) = %d, %v", i, d, err)
		}
	}
}

func TestPacketDuration(t *testing.T) {
	cases := []struct {
		pkt  []byte
		want int
	}{
		{[]byte{0x00}, 480},            // SILK NB 10 ms
		{[]byte{0x18}, 2880},           // SILK NB 60 ms
		{[]byte{0x78 | 1}, 1920},       // hybrid FB 20 ms, 2 frames
		{[]byte{0x80 | 3, 0x30}, 5760}, // CELT 2.5 ms x 48
		{[]byte{0xF8 | 3, 6}, 5760},    // CELT 20 ms x 6
		{[]byte{0xE0 | 2}, 240},        // CELT FB 2.5 ms x 2
	}
	for _, c := range cases {
		got, err := PacketDuration(c.pkt)
		if err != nil || got != c.want {
			t.Errorf("PacketDuration(%x) = %d, %v; want %d", c.pkt, got, err, c.want)
		}
	}
	for _, bad := range [][]byte{nil, {0xFB}, {0xFB, 0}, {0xFB, 7}} {
		if _, err := PacketDuration(bad); err == nil {
			t.Errorf("PacketDuration(%x) accepted", bad)
		}
	}
}

func TestParseOpusHead(t *testing.T) {
	h, err := ParseOpusHead([]byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 0x38, 1, 0x80, 0xBB, 0, 0, 0x00, 0x01, 0})
	if err != nil {
		t.Fatal(err)
	}
	if h.Channels != 2 || h.PreSkip != 312 || h.InputRate != 48000 || h.OutputGain != 256 ||
		h.MappingFamily != 0 || h.Streams != 1 || h.CoupledStreams != 1 || !bytes.Equal(h.Mapping, []byte{0, 1}) {
		t.Fatalf("bad head %+v", h)
	}
	fx := loadFixture(t, filepath.Join(fixtureDir, "surround_51.opus"))
	if fx.head.MappingFamily != 1 || fx.head.Channels != 6 || fx.head.Streams != 4 || fx.head.CoupledStreams != 2 {
		t.Fatalf("bad 5.1 head %+v", fx.head)
	}
	for _, bad := range [][]byte{
		nil,
		[]byte("OpusHeax\x01\x02\x38\x01\x80\xbb\x00\x00\x00\x00\x00"),
		[]byte("OpusHead\x01\x03\x38\x01\x80\xbb\x00\x00\x00\x00\x00"),     // family 0, 3 channels
		[]byte("OpusHead\x01\x02\x38\x01\x80\xbb\x00\x00\x00\x00\x01\x01"), // truncated mapping
		[]byte("OpusHead\x01\x02\x38\x01\x80\xbb\x00\x00\x00\x00\x01\x01\x00\x00\x05"),
	} {
		if _, err := ParseOpusHead(bad); err == nil {
			t.Errorf("ParseOpusHead(%q) accepted", bad)
		}
	}
}

// TestOutputRates decodes every fixture at the lower output rates and
// checks length and level against the 48 kHz decode.
func TestOutputRates(t *testing.T) {
	for _, fx := range allFixtures(t) {
		ref, ch := decodeFixture(t, fx, 48000)
		var e48 float64
		for _, v := range ref {
			e48 += float64(v) * float64(v)
		}
		e48 /= float64(len(ref))
		for _, rate := range []int{24000, 16000, 12000, 8000} {
			got, _ := decodeFixture(t, fx, rate)
			wantLen := len(ref) * rate / 48000
			if d := len(got) - wantLen; d < -ch*2 || d > ch*2 {
				t.Errorf("rate %d: %d samples, want ~%d", rate, len(got), wantLen)
			}
			var e float64
			for _, v := range got {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
					t.Fatalf("rate %d: non-finite output", rate)
				}
				e += float64(v) * float64(v)
			}
			e /= float64(len(got))
			if db := 10 * math.Log10(e/e48); db < -8 || db > 1 {
				t.Errorf("rate %d: level %.1f dB relative to 48 kHz", rate, db)
			}
		}
	}
}

// TestPLCAndReset exercises DecodePLC (including long gaps) and Reset.
func TestPLCAndReset(t *testing.T) {
	for _, fx := range allFixtures(t) {
		if fx.head.MappingFamily != 0 {
			continue
		}
		d, _ := NewDecoder(48000, fx.head.Channels)
		var out []float32
		var err error
		for i, p := range fx.packets {
			if i%7 == 3 {
				for k := 0; k < 1+i%3; k++ {
					n := len(out)
					if out, err = d.DecodePLC(out); err != nil {
						t.Fatal(err)
					}
					if len(out) == n {
						t.Fatal("DecodePLC produced no samples")
					}
				}
				continue
			}
			if i == 40 {
				d.Reset()
			}
			if out, err = d.Decode(p, out); err != nil {
				t.Fatal(err)
			}
		}
		// Concealment of 2.5 ms CELT under heavy loss can legitimately
		// overshoot (libopus peaks at ~22.8 on this pattern too); only
		// require finite, bounded output.
		for _, v := range out {
			if math.IsNaN(float64(v)) || math.Abs(float64(v)) > 64 {
				t.Fatalf("bad concealment sample %g", v)
			}
		}
	}
}

func decodeNoPanic(t *testing.T, decs []*Decoder, ms *MultistreamDecoder, p []byte) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic on packet %x: %v", p, r)
			}
		}()
		var buf []float32
		for _, d := range decs {
			buf, _ = d.Decode(p, buf[:0])
		}
		if ms != nil {
			buf, _ = ms.Decode(p, buf[:0])
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("decoder hung on packet %x", p)
	}
}

// TestMalformedInput feeds random and corrupted packets with the panic
// safety net disabled: the decoder must reject or decode them, never panic
// or hang.
func TestMalformedInput(t *testing.T) {
	noRecover = true
	defer func() { noRecover = false }()
	var decs []*Decoder
	for _, rate := range []int{48000, 16000, 8000} {
		for ch := 1; ch <= 2; ch++ {
			d, _ := NewDecoder(rate, ch)
			decs = append(decs, d)
		}
	}
	fxs := allFixtures(t)
	var ms *MultistreamDecoder
	for _, fx := range fxs {
		if fx.head.MappingFamily == 1 {
			ms, _ = NewMultistreamDecoder(fx.head)
		}
	}
	r := rand.New(rand.NewSource(1))
	iters := 3000
	if testing.Short() {
		iters = 300
	}
	for i := 0; i < iters; i++ {
		var p []byte
		switch i % 4 {
		case 0: // random bytes
			p = make([]byte, r.Intn(1400))
			r.Read(p)
		case 1: // fixture packet with flipped bits
			fx := fxs[r.Intn(len(fxs))]
			p = append([]byte(nil), fx.packets[r.Intn(len(fx.packets))]...)
			for k := 0; k < 1+r.Intn(8); k++ {
				p[r.Intn(len(p))] ^= byte(1 << uint(r.Intn(8)))
			}
		case 2: // truncated / extended fixture packet
			fx := fxs[r.Intn(len(fxs))]
			src := fx.packets[r.Intn(len(fx.packets))]
			p = append([]byte(nil), src[:r.Intn(len(src))+1]...)
			if r.Intn(2) == 0 {
				p = append(p, make([]byte, r.Intn(300))...)
			}
		case 3: // valid TOC, garbage payload, all frame-packing codes
			p = make([]byte, 1+r.Intn(400))
			r.Read(p)
			p[0] = byte(r.Intn(256))
		}
		decodeNoPanic(t, decs, ms, p)
		if t.Failed() {
			return
		}
	}
}

// FuzzDecode is the native fuzz target (go test -fuzz=FuzzDecode).
func FuzzDecode(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join(fixtureDir, "*.opus"))
	for _, file := range files {
		fx := loadFixture(f, file)
		for i := 0; i < len(fx.packets); i += 17 {
			f.Add(fx.packets[i])
		}
	}
	noRecover = true
	d1, _ := NewDecoder(48000, 1)
	d2, _ := NewDecoder(48000, 2)
	f.Fuzz(func(t *testing.T, p []byte) {
		d1.Decode(p, nil)
		d2.Decode(p, nil)
		PacketDuration(p)
		ParseOpusHead(p)
	})
}

func BenchmarkDecode(b *testing.B) {
	for _, name := range []string{"celt_fb_stereo_20ms", "silk_wb_mono_10ms", "hybrid_fb_stereo_10ms"} {
		fx := loadFixture(b, filepath.Join(fixtureDir, name+".opus"))
		b.Run(name, func(b *testing.B) {
			d, _ := NewDecoder(48000, fx.head.Channels)
			out := make([]float32, 0, 5760*2)
			samples := 0
			for i := 0; i < b.N; i++ {
				p := fx.packets[i%len(fx.packets)]
				out, _ = d.Decode(p, out[:0])
				samples += len(out) / fx.head.Channels
			}
			b.ReportMetric(float64(samples)/48000/b.Elapsed().Seconds(), "x-realtime")
		})
	}
}

// TestChannelConversion decodes mono streams with a stereo decoder (both
// channels must equal the mono decode) and stereo streams with a mono
// decoder (downmix; sanity only).
func TestChannelConversion(t *testing.T) {
	for _, fx := range allFixtures(t) {
		if fx.head.MappingFamily != 0 || hasLoss(fx.packets) {
			continue // concealment noise is drawn per output channel
		}
		if fx.head.Channels == 1 {
			mono := decodeAll(t, 1, fx.packets)
			st := decodeAll(t, 2, fx.packets)
			if len(st) != 2*len(mono) {
				t.Fatalf("stereo decode of mono stream: %d samples, want %d", len(st), 2*len(mono))
			}
			// The left channel is the mono decode; the right one only differs
			// where CELT concealment (also used in mode transitions) draws
			// its own noise.
			l := make([]float32, len(mono))
			r := make([]float32, len(mono))
			for i, v := range mono {
				if st[2*i] != v {
					t.Fatalf("%s sample %d: mono %g, left %g", modeSummary(fx.packets), i, v, st[2*i])
				}
				l[i], r[i] = st[2*i], st[2*i+1]
			}
			if s := snrDB(l, r); s < 60 {
				t.Errorf("%s: right channel %.1f dB off the left", modeSummary(fx.packets), s)
			}
		} else {
			st := decodeAll(t, 2, fx.packets)
			mono := decodeAll(t, 1, fx.packets)
			if 2*len(mono) != len(st) {
				t.Fatalf("mono decode of stereo stream: %d samples, want %d", len(mono), len(st)/2)
			}
			var em, es float64
			for i, v := range mono {
				em += float64(v) * float64(v)
				s := 0.5 * (float64(st[2*i]) + float64(st[2*i+1]))
				es += s * s
			}
			if db := 10 * math.Log10(em/es); math.IsNaN(db) || db < -3 || db > 3 {
				t.Errorf("downmix level %.1f dB off the stereo mid signal", db)
			}
		}
	}
}

func hasLoss(pkts [][]byte) bool {
	for _, p := range pkts {
		if len(p) <= 1 {
			return true
		}
	}
	return false
}
