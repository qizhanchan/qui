package mp3

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand"
	"os"
	"testing"
	"time"
)

func decodeBytes(t *testing.T, data []byte) ([]float32, *Stream) {
	t.Helper()
	s, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return decodeStream(t, s), s
}

func readAllFrom(t *testing.T, r *PCMReader) []float32 {
	t.Helper()
	var pcm []float32
	for {
		var err error
		pcm, err = r.Read(pcm)
		if err == io.EOF {
			return pcm
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSniff(t *testing.T) {
	for _, f := range fixtures {
		data, err := os.ReadFile(fixture(f.name))
		if err != nil {
			t.Fatal(err)
		}
		if !Sniff(data[:min(len(data), 4096)]) {
			t.Errorf("%s: Sniff = false", f.name)
		}
		// Without the ID3 tag, from the first frame on.
		s, _ := Open(bytes.NewReader(data), int64(len(data)))
		if !Sniff(data[s.tagStart:min(int64(len(data)), s.tagStart+4096)]) {
			t.Errorf("%s: Sniff(after tags) = false", f.name)
		}
	}
	for _, name := range []string{"bframes_aac_160x90.mp4", "tone_vorbis.ogg", "tone.opus", "vp9_opus_160x90.webm"} {
		data, err := os.ReadFile(fixture(name))
		if err != nil {
			t.Fatal(err)
		}
		if Sniff(data[:min(len(data), 4096)]) {
			t.Errorf("%s: Sniff = true", name)
		}
	}
	rng := rand.New(rand.NewSource(1))
	junk := make([]byte, 4096)
	for i := 0; i < 50; i++ {
		rng.Read(junk)
		if Sniff(junk) {
			t.Errorf("random block %d sniffed as MPEG audio", i)
		}
	}
	if Sniff(nil) || Sniff([]byte{0xff, 0xfb}) {
		t.Error("tiny input sniffed")
	}
}

func TestStreamInfo(t *testing.T) {
	s, _ := openFixture(t, "mp3_stereo_cbr128.mp3")
	if n, ok := s.NumFrames(); !ok || n <= 0 {
		t.Fatalf("NumFrames = %d, %v", n, ok)
	}
	delay, pad, ok := s.Gapless()
	if !ok {
		t.Fatal("no gapless info")
	}
	t.Logf("cbr128: frames %d, delay %d, padding %d", s.frames, delay, pad)
	if s.Duration() != 2*time.Second {
		t.Errorf("Duration = %v, want 2s", s.Duration())
	}
	// Xing-less stream: estimated from the bitrate, then exact with an index.
	s, _ = openFixture(t, "mp3_noxing.mp3")
	if _, ok := s.NumFrames(); ok {
		t.Error("frame count known without Xing header")
	}
	est := s.Duration()
	if err := s.BuildIndex(); err != nil {
		t.Fatal(err)
	}
	n, _ := s.NumFrames()
	exact := time.Duration(n * 1152 * int64(time.Second) / 44100)
	if s.Duration() != exact || est < exact-30*time.Millisecond || est > exact+30*time.Millisecond {
		t.Errorf("duration estimate %v, indexed %v, want %v", est, s.Duration(), exact)
	}
	// VBR: Xing header with TOC.
	s, _ = openFixture(t, "mp3_mono22k_vbr.mp3")
	if !s.vbr || s.toc == nil {
		t.Error("VBR fixture: no Xing TOC")
	}
	if s.Duration() != 2*time.Second {
		t.Errorf("VBR Duration = %v", s.Duration())
	}
}

func TestID3Tags(t *testing.T) {
	data, err := os.ReadFile(fixture("mp3_id3_tags.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := decodeBytes(t, data)
	if string(data[:3]) != "ID3" || string(data[len(data)-128:len(data)-125]) != "TAG" {
		t.Fatal("fixture lacks ID3v2/ID3v1")
	}
	// ID3v2.4 tag with footer, plus an APE tag before the ID3v1 tag.
	id3 := []byte("ID3\x04\x00\x10\x00\x00\x00\x20")
	id3 = append(id3, make([]byte, 0x20)...)
	id3 = append(id3, []byte("3DI\x04\x00\x10\x00\x00\x00\x20")...)
	var ape bytes.Buffer
	item := []byte("hello")
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[0:], uint32(len(item)))
	body = append(body, "Title\x00"...)
	body = append(body, item...)
	footer := make([]byte, 32)
	copy(footer, "APETAGEX")
	binary.LittleEndian.PutUint32(footer[8:], 2000)
	binary.LittleEndian.PutUint32(footer[12:], uint32(len(body)+32))
	binary.LittleEndian.PutUint32(footer[16:], 1)
	ape.Write(body)
	ape.Write(footer)
	audio := data[:len(data)-128]
	v1 := data[len(data)-128:]
	var b []byte
	b = append(b, id3...)
	b = append(b, audio...)
	b = append(b, ape.Bytes()...)
	b = append(b, v1...)
	got, _ := decodeBytes(t, b)
	if len(got) != len(ref) {
		t.Fatalf("with extra tags: %d samples, want %d", len(got), len(ref))
	}
	for i := range got {
		if got[i] != ref[i] {
			t.Fatalf("sample %d differs", i)
		}
	}
}

func TestGarbageResync(t *testing.T) {
	data, err := os.ReadFile(fixture("mp3_stereo_cbr128.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	ref, s := decodeBytes(t, data)
	if err := s.BuildIndex(); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(3))
	junk := make([]byte, 1500)
	rng.Read(junk)
	for i := 0; i < len(junk); i += 97 {
		junk[i] = 0xff // plenty of false sync candidates
	}
	var b []byte
	b = append(b, junk[:300]...) // garbage before the first frame
	cut := s.index[20]
	b = append(b, data[:cut]...)
	b = append(b, junk...) // garbage between frames 19 and 20
	b = append(b, data[cut:]...)
	// The garbage is never fed to the decoder and the reservoir of frame
	// 20 lies in frame 19, so the output is unchanged.
	got, _ := decodeBytes(t, b)
	if len(got) != len(ref) {
		t.Fatalf("%d samples, want %d", len(got), len(ref))
	}
	for i := range got {
		if got[i] != ref[i] {
			t.Fatalf("sample %d differs", i)
		}
	}
}

func TestTruncated(t *testing.T) {
	data, err := os.ReadFile(fixture("mp3_stereo_cbr128.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	ref, s := decodeBytes(t, data)
	_ = s.BuildIndex()
	last := s.index[len(s.index)-1]
	for _, cut := range []int64{last + 3, last + 100, int64(len(data)) - 1} {
		got, s2 := decodeBytes(t, data[:cut])
		n, _ := s2.NumFrames()
		if len(got) == 0 || len(got) > len(ref) {
			t.Fatalf("cut %d: %d samples (full %d)", cut, len(got), len(ref))
		}
		for i := range got {
			if got[i] != ref[i] {
				t.Fatalf("cut %d: sample %d differs", cut, i)
			}
		}
		t.Logf("cut at %d: %d samples/ch (Xing frames %d)", cut, len(got)/2, n)
	}
	// A truncated frame handed to the decoder directly is an error.
	fr := data[last : last+50]
	d := NewDecoder()
	out, err := d.Decode(fr, nil)
	if err != ErrTruncated || len(out) != 0 {
		t.Fatalf("Decode(truncated) = %d samples, %v", len(out), err)
	}
}

func TestSeek(t *testing.T) {
	for _, name := range []string{"mp3_stereo_cbr128.mp3", "mp3_mono22k_vbr.mp3", "mp3_noxing.mp3", "mp3_stereo8k_mpeg25.mp3", "mp2_stereo44k.mp2"} {
		for _, indexed := range []bool{false, true} {
			s, _ := openFixture(t, name)
			full := decodeStream(t, s)
			nch := s.Channels()
			sr := s.SampleRate()
			s, _ = openFixture(t, name)
			if indexed {
				_ = s.BuildIndex()
			}
			r := NewPCMReader(s)
			for _, target := range []time.Duration{700 * time.Millisecond, 50 * time.Millisecond, 1234 * time.Millisecond, 0, 10 * time.Second} {
				at, err := r.Seek(target)
				if err != nil {
					t.Fatal(err)
				}
				got := readAllFrom(t, r)
				off := int(int64(at)*int64(sr)/int64(time.Second)) * nch
				if off > len(full) {
					if len(got) != 0 {
						t.Errorf("%s: seek past end returned %d samples", name, len(got))
					}
					continue
				}
				want := full[off:]
				exact := indexed || !s.vbr
				if exact {
					if len(got) != len(want) {
						t.Errorf("%s idx=%v seek %v: %d samples, want %d", name, indexed, target, len(got), len(want))
						continue
					}
					for i := range got {
						if d := got[i] - want[i]; d > 1e-6 || d < -1e-6 {
							t.Errorf("%s idx=%v seek %v: sample %d = %v want %v", name, indexed, target, i, got[i], want[i])
							break
						}
					}
				} else {
					// TOC seeking lands near the target frame.
					diff := (len(got) - len(want)) / nch
					if diff < -10*s.SamplesPerFrame() || diff > 10*s.SamplesPerFrame() {
						t.Errorf("%s TOC seek %v: %d samples off", name, target, diff)
					}
				}
			}
		}
	}
}

func TestDecoderReset(t *testing.T) {
	data, err := os.ReadFile(fixture("mp3_stereo_cbr128.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := Open(bytes.NewReader(data), int64(len(data)))
	d := NewDecoder()
	var frames [][]byte
	for {
		f, err := s.ReadFrame()
		if err != nil {
			break
		}
		frames = append(frames, append([]byte(nil), f...))
	}
	// Feed from frame 30 after a reset: frames whose reservoir is missing
	// produce no output, then decoding resumes.
	d.Reset()
	skipped := 0
	for i := 30; i < 35; i++ {
		out, err := d.Decode(frames[i], nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) == 0 {
			skipped++
		} else if len(out) != 1152*2 {
			t.Fatalf("frame %d: %d samples", i, len(out))
		}
	}
	t.Logf("frames skipped after reset: %d", skipped)
	if skipped > 2 {
		t.Errorf("%d frames skipped", skipped)
	}
}
