package mp3

import (
	"bytes"
	"errors"
	"io"
	"math"
	"math/rand"
	"os"
	"testing"
)

// decodeAllRobust runs the full demux + decode path on arbitrary bytes
// and reports panics (with recovery disabled) and runaway loops.
func decodeAllRobust(t testing.TB, data []byte) {
	testNoRecover = true
	defer func() { testNoRecover = false }()
	s, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return
	}
	_ = s.Duration()
	r := NewPCMReader(s)
	var pcm []float32
	maxReads := len(data)/4 + 16 // every Read consumes at least one frame
	for i := 0; ; i++ {
		if i > maxReads {
			t.Fatalf("reader did not terminate")
		}
		pcm, err = r.Read(pcm[:0])
		for _, v := range pcm {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("non-finite sample")
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Read: %v", err)
			}
			break
		}
	}
	_, _ = r.Seek(s.Duration() / 2)
	_, _ = r.Read(nil)
}

func TestCorruptStreams(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for _, f := range fixtures {
		orig, err := os.ReadFile(fixture(f.name))
		if err != nil {
			t.Fatal(err)
		}
		for iter := 0; iter < 60; iter++ {
			data := append([]byte(nil), orig...)
			switch iter % 4 {
			case 0: // bit flips
				for i := 0; i < 1+rng.Intn(40); i++ {
					p := rng.Intn(len(data))
					data[p] ^= 1 << uint(rng.Intn(8))
				}
			case 1: // random byte runs
				p := rng.Intn(len(data))
				n := min(rng.Intn(500), len(data)-p)
				rng.Read(data[p : p+n])
			case 2: // truncation at a random point
				data = data[:rng.Intn(len(data))]
			case 3: // overwrite side info / main_data_begin of many frames
				for i := 0; i < 200; i++ {
					p := rng.Intn(len(data) - 8)
					if data[p] == 0xff && data[p+1]&0xe0 == 0xe0 {
						for k := 4; k < 8; k++ {
							data[p+k] = byte(rng.Intn(256))
						}
					}
				}
			}
			decodeAllRobust(t, data)
		}
	}
}

// TestDecodeArbitraryFrames feeds random payloads behind every kind of
// valid header straight to the decoder.
func TestDecodeArbitraryFrames(t *testing.T) {
	testNoRecover = true
	defer func() { testNoRecover = false }()
	rng := rand.New(rand.NewSource(9))
	d := NewDecoder()
	for i := 0; i < 20000; i++ {
		var hb [4]byte
		for {
			rng.Read(hb[:])
			hb[0] = 0xff
			hb[1] |= 0xe0
			if _, ok := ParseHeader(hb[:]); ok {
				break
			}
		}
		h, _ := ParseHeader(hb[:])
		frame := make([]byte, h.FrameSize()+rng.Intn(3))
		copy(frame, hb[:])
		rng.Read(frame[4:])
		if rng.Intn(4) == 0 {
			// Mostly-zero payloads take the deep paths (long count1 runs).
			for k := 8; k < len(frame); k++ {
				if rng.Intn(8) != 0 {
					frame[k] = 0
				}
			}
		}
		out, _ := d.Decode(frame, nil)
		if n := len(out); n != 0 && n != h.SamplesPerFrame()*h.Channels() {
			t.Fatalf("%d samples for %+v", n, h)
		}
		for _, v := range out {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("non-finite sample for %+v", h)
			}
		}
		if rng.Intn(50) == 0 {
			d.Reset()
		}
	}
	if _, err := d.Decode(nil, nil); err != ErrSync {
		t.Errorf("Decode(nil) = %v", err)
	}
	if _, err := d.Decode([]byte{0, 1, 2, 3, 4}, nil); err != ErrSync {
		t.Errorf("Decode(junk) = %v", err)
	}
}

func FuzzStream(f *testing.F) {
	for _, fx := range fixtures {
		data, err := os.ReadFile(fixture(fx.name))
		if err == nil {
			f.Add(data[:min(len(data), 3000)])
		}
	}
	f.Add([]byte("ID3\x04\x00\x10\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		decodeAllRobust(t, data)
		Sniff(data)
	})
}

func BenchmarkDecode(b *testing.B) {
	data, err := os.ReadFile(fixture("mp3_stereo_cbr128.mp3"))
	if err != nil {
		b.Fatal(err)
	}
	var frames [][]byte
	s, _ := Open(bytes.NewReader(data), int64(len(data)))
	for {
		fr, err := s.ReadFrame()
		if err != nil {
			break
		}
		frames = append(frames, append([]byte(nil), fr...))
	}
	d := NewDecoder()
	var out []float32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, _ = d.Decode(frames[i%len(frames)], out[:0])
	}
	b.ReportMetric(float64(b.Elapsed().Seconds())/float64(b.N)/(1152.0/44100), "realtime-fraction")
}
