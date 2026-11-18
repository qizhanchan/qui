package webm

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var fuzzSeeds = []string{"av1_opus_160x90.webm", "vp9_opus_160x90.webm", "vp8_opus_live.webm", "vorbis_audio.mka", "hevc_160x90.mkv"}

// exercise drives every entry point over data; it must neither panic
// nor loop forever.
func exercise(data []byte) {
	if r, err := Open(bytes.NewReader(data), int64(len(data))); err == nil {
		_ = r.Duration()
		for _, t := range r.Tracks() {
			n := r.SampleCount(t)
			for i := 0; i < n && i < 200; i++ {
				_, _ = r.ReadSample(t, i)
			}
			_ = r.SeekKeyframe(t, 300*time.Millisecond)
			_ = r.SampleAtTime(t, 300*time.Millisecond)
		}
		for _, target := range []time.Duration{0, 400 * time.Millisecond} {
			_, _ = r.SeekTo(target)
			for i := 0; i < 1000; i++ {
				if _, err := r.ReadFrame(); err != nil {
					break
				}
			}
		}
	}
	if i := bytes.Index(data, []byte{0x1F, 0x43, 0xB6, 0x75}); i > 0 {
		if r, err := OpenInit(data[:i]); err == nil {
			_, _ = r.ParseCluster(data[i:])
			for off := i; off < len(data); off += 300 {
				_, _ = r.AppendBytes(data[off:min(off+300, len(data))])
			}
			_ = r.Flush()
		}
	}
	_ = Sniff(data)
}

func FuzzOpen(f *testing.F) {
	for _, s := range fuzzSeeds {
		// Small seeds keep the fuzzer's input minimisation cheap.
		if b, err := os.ReadFile(filepath.Join(testdata, s)); err == nil {
			f.Add(b[:min(len(b), 3000)])
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) { exercise(data) })
}

// TestMutations is a quick deterministic fuzz pass over byte flips,
// truncations and spliced garbage of every fixture.
func TestMutations(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	iters := 300
	if testing.Short() {
		iters = 50
	}
	for _, s := range fuzzSeeds {
		orig, err := os.ReadFile(filepath.Join(testdata, s))
		if err != nil {
			t.Fatal(err)
		}
		for it := 0; it < iters; it++ {
			b := append([]byte(nil), orig...)
			switch it % 4 {
			case 0:
				for k := 0; k < 1+rng.Intn(20); k++ {
					b[rng.Intn(len(b))] = byte(rng.Intn(256))
				}
			case 1:
				b = b[:rng.Intn(len(b))]
			case 2:
				at := rng.Intn(len(b))
				junk := make([]byte, rng.Intn(64))
				rng.Read(junk)
				b = append(b[:at], append(junk, b[at:]...)...)
			case 3:
				// Overwrite a run with 0xFF: unknown sizes everywhere.
				at := rng.Intn(len(b))
				for k := at; k < min(len(b), at+1+rng.Intn(16)); k++ {
					b[k] = 0xFF
				}
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				exercise(b)
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("%s mutation %d: hang", s, it)
			}
		}
	}
}
