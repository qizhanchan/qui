package ogg

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testdata = "../../testdata"

type refPacket struct {
	stream   int
	pts, dur int64
	size     int
	hash     string
}

// loadRef reads testdata/<name>.packets.csv (ffprobe -show_entries
// packet=stream_index,pts,duration,size,flags,data_hash -show_data_hash
// sha256) and returns the packets of the given stream.
func loadRef(t *testing.T, name string, stream int) (codec string, pkts []refPacket) {
	t.Helper()
	f, err := os.Open(filepath.Join(testdata, name+".packets.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), ",")
		idx, _ := strconv.Atoi(p[1])
		if idx != stream {
			continue
		}
		if p[0] == "S" {
			codec = p[2]
			continue
		}
		var rp refPacket
		rp.stream = idx
		rp.pts, _ = strconv.ParseInt(p[2], 10, 64)
		rp.dur, _ = strconv.ParseInt(p[3], 10, 64)
		rp.size, _ = strconv.Atoi(p[4])
		rp.hash = strings.TrimPrefix(p[6], "SHA256:")
		pkts = append(pkts, rp)
	}
	return codec, pkts
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func openFile(t *testing.T, name string) (*Reader, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdata, name))
	if err != nil {
		t.Fatal(err)
	}
	if !Sniff(data[:min(len(data), 512)]) {
		t.Fatalf("%s: Sniff = false", name)
	}
	r, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return r, data
}

func readAll(t *testing.T, r *Reader) []Packet {
	t.Helper()
	var out []Packet
	for {
		p, err := r.ReadPacket()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
		if len(out) > 1e6 {
			t.Fatal("runaway")
		}
	}
}

// compare checks packets against ffprobe (time base 1/SampleRate).
func compare(t *testing.T, name string, r *Reader, got []Packet, want []refPacket) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d packets, ffprobe %d", name, len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if len(g.Data) != w.size || sha(g.Data) != w.hash {
			t.Fatalf("%s: packet %d: size %d, ffprobe %d (hash match %v)", name, i, len(g.Data), w.size, sha(g.Data) == w.hash)
		}
		if g.PTS != r.toDur(w.pts) {
			t.Errorf("%s: packet %d: pts %v, ffprobe %v", name, i, g.PTS, r.toDur(w.pts))
		}
		if g.Duration != r.toDur(w.dur) {
			t.Errorf("%s: packet %d: duration %v, ffprobe %v", name, i, g.Duration, r.toDur(w.dur))
		}
	}
}

func TestOpus(t *testing.T) {
	r, _ := openFile(t, "tone.opus")
	if r.Codec != "opus" || r.SampleRate != 48000 || r.Channels != 2 || r.PreSkip != 312 || r.SeekPreRoll != 80*time.Millisecond {
		t.Errorf("stream: %s %d Hz %d ch preskip %d", r.Codec, r.SampleRate, r.Channels, r.PreSkip)
	}
	if r.Opus.InputSampleRate != 48000 || r.Vendor == "" || len(r.Headers) != 2 {
		t.Errorf("headers: %+v vendor %q", r.Opus, r.Vendor)
	}
	if d := r.Duration(); d != 1500*time.Millisecond {
		t.Errorf("duration %v", d)
	}
	if r.StartTime() != 0 {
		t.Errorf("start %v", r.StartTime())
	}
	_, want := loadRef(t, "tone.opus", 0)
	got := readAll(t, r)
	compare(t, "tone.opus", r, got, want)
	for i, p := range got {
		if p.Samples != OpusPacketSamples(p.Data) || p.Samples != 960 {
			t.Errorf("packet %d samples %d", i, p.Samples)
		}
	}
}

func TestVorbis(t *testing.T) {
	r, _ := openFile(t, "tone_vorbis.ogg")
	if r.Codec != "vorbis" || r.SampleRate != 44100 || r.Channels != 2 || r.PreSkip != 0 || len(r.Headers) != 3 {
		t.Errorf("stream: %s %d Hz %d ch", r.Codec, r.SampleRate, r.Channels)
	}
	// ffprobe: duration 1.500590 s = 66176 samples.
	if d := r.Duration(); d != r.toDur(66176) {
		t.Errorf("duration %v", d)
	}
	_, want := loadRef(t, "tone_vorbis.ogg", 0)
	compare(t, "tone_vorbis.ogg", r, readAll(t, r), want)
}

func TestMultiplexed(t *testing.T) {
	r, _ := openFile(t, "flac_opus_multi.ogg")
	if r.Codec != "opus" || r.Channels != 1 {
		t.Fatalf("picked %s/%d ch", r.Codec, r.Channels)
	}
	codec, want := loadRef(t, "flac_opus_multi.ogg", 1)
	if codec != "opus" {
		t.Fatal(codec)
	}
	compare(t, "flac_opus_multi.ogg", r, readAll(t, r), want)
}

func TestLongOpus(t *testing.T) {
	r, _ := openFile(t, "long_6k.opus")
	_, want := loadRef(t, "long_6k.opus", 0)
	compare(t, "long_6k.opus", r, readAll(t, r), want)
	if d := r.Duration(); d != 20*time.Second {
		t.Errorf("duration %v", d)
	}
}

func TestSeek(t *testing.T) {
	for _, small := range []bool{false, true} {
		if small {
			old := bisectMin
			bisectMin = 512 // force real bisection on the small fixtures
			defer func() { bisectMin = old }()
		}
		for _, name := range []string{"long_6k.opus", "tone_vorbis.ogg", "tone.opus", "flac_opus_multi.ogg"} {
			r, _ := openFile(t, name)
			all := readAll(t, r)
			for _, target := range []time.Duration{-time.Second, 0, 1 * time.Millisecond, 333 * time.Millisecond, 777 * time.Millisecond, 1234 * time.Millisecond, 9876 * time.Millisecond, 19 * time.Second, time.Hour} {
				got, err := r.Seek(target)
				if err != nil {
					t.Fatal(err)
				}
				p, err := r.ReadPacket()
				if err == io.EOF {
					if target < r.Duration() {
						t.Errorf("%s: Seek(%v): EOF", name, target)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if p.PTS != got {
					t.Errorf("%s: Seek(%v) = %v, next packet at %v", name, target, got, p.PTS)
				}
				if target >= 0 && target < r.Duration() && p.PTS > target {
					t.Errorf("%s: Seek(%v): next packet starts after the target (%v)", name, target, p.PTS)
				}
				// The packet must be the one at that PTS in a linear read,
				// and reading on must continue in step.
				k := -1
				for i, q := range all {
					if q.PTS == p.PTS && bytes.Equal(q.Data, p.Data) {
						k = i
						break
					}
				}
				if k < 0 {
					t.Fatalf("%s: Seek(%v): packet at %v not in linear read", name, target, p.PTS)
				}
				for j := k + 1; j < min(k+20, len(all)); j++ {
					q, err := r.ReadPacket()
					if err != nil || q.PTS != all[j].PTS || q.Duration != all[j].Duration || !bytes.Equal(q.Data, all[j].Data) {
						t.Fatalf("%s: after Seek(%v), packet %d: %v / %v vs %v", name, target, j, err, q.PTS, all[j].PTS)
					}
				}
				// Bisection should land close: within ~ one page.
				if name == "long_6k.opus" && target < r.Duration() && target-p.PTS > 400*time.Millisecond {
					t.Errorf("%s: Seek(%v) landed far away at %v", name, target, p.PTS)
				}
			}
		}
	}
}

func TestOpusTOC(t *testing.T) {
	cases := []struct {
		p    []byte
		want int
	}{
		{[]byte{0 << 3}, 480},           // SILK NB 10 ms
		{[]byte{3 << 3}, 2880},          // SILK 60 ms
		{[]byte{3<<3 | 1}, 5760},        // 2 × 60 ms
		{[]byte{12 << 3}, 480},          // Hybrid 10 ms
		{[]byte{13<<3 | 2}, 1920},       // Hybrid 2 × 20 ms
		{[]byte{16 << 3}, 120},          // CELT 2.5 ms
		{[]byte{31 << 3}, 960},          // CELT 20 ms
		{[]byte{16<<3 | 3, 7}, 840},     // CELT 7 × 2.5 ms
		{[]byte{31<<3 | 3, 0x86}, 5760}, // 6 × 20 ms, VBR bit set
		{[]byte{31<<3 | 3, 7}, 0},       // 140 ms: invalid
		{[]byte{31<<3 | 3}, 0},          // missing frame count
		{nil, 0},
	}
	for _, c := range cases {
		if got := OpusPacketSamples(c.p); got != c.want {
			t.Errorf("OpusPacketSamples(% x) = %d, want %d", c.p, got, c.want)
		}
	}
}

func TestTruncated(t *testing.T) {
	for _, name := range []string{"tone.opus", "tone_vorbis.ogg"} {
		data, _ := os.ReadFile(filepath.Join(testdata, name))
		full, _ := Open(bytes.NewReader(data), int64(len(data)))
		all := readAll(t, full)
		prev := len(all)
		for cut := len(data) - 1; cut > 0; cut -= 211 {
			d := data[:cut]
			r, err := Open(bytes.NewReader(d), int64(len(d)))
			if err != nil {
				continue
			}
			got := readAll(t, r)
			if len(got) > prev {
				t.Errorf("%s cut %d: %d packets, more than a longer cut (%d)", name, cut, len(got), prev)
			}
			prev = len(got)
			for i, p := range got {
				if !bytes.Equal(p.Data, all[i].Data) || p.PTS != all[i].PTS {
					t.Fatalf("%s cut %d: packet %d differs", name, cut, i)
				}
			}
			if r.Duration() > full.Duration() {
				t.Errorf("%s cut %d: duration %v", name, cut, r.Duration())
			}
			_, _ = r.Seek(700 * time.Millisecond)
			readAll(t, r)
		}
	}
}

// TestCorruptPage: a damaged page fails its CRC; the demuxer drops the
// packets on it (and the one continuing from it) and resynchronises.
func TestCorruptPage(t *testing.T) {
	data, _ := os.ReadFile(filepath.Join(testdata, "long_6k.opus"))
	full, _ := Open(bytes.NewReader(data), int64(len(data)))
	all := readAll(t, full)
	bad := append([]byte(nil), data...)
	// Damage the body of a page in the middle.
	mid := bytes.Index(bad[len(bad)/2:], []byte("OggS")) + len(bad)/2
	bad[mid+100] ^= 0xFF
	r, err := Open(bytes.NewReader(bad), int64(len(bad)))
	if err != nil {
		t.Fatal(err)
	}
	got := readAll(t, r)
	if len(got) >= len(all) || len(got) < len(all)-40 {
		t.Fatalf("%d packets of %d", len(got), len(all))
	}
	// Every packet we return must be a real packet with its true PTS.
	byPTS := map[time.Duration][]byte{}
	for _, p := range all {
		byPTS[p.PTS] = p.Data
	}
	for i, p := range got {
		if !bytes.Equal(byPTS[p.PTS], p.Data) {
			t.Fatalf("packet %d at %v: wrong data or timestamp", i, p.PTS)
		}
	}
	if got[len(got)-1].PTS != all[len(all)-1].PTS {
		t.Error("lost the tail")
	}
}

func TestGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("OggS"), bytes.Repeat([]byte("OggS\x00"), 50), []byte("garbage garbage garbage garbage garbage")} {
		if _, err := Open(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Errorf("Open(%q) succeeded", b)
		}
		if Sniff(b) {
			t.Errorf("Sniff(%q)", b)
		}
	}
	// Leading junk before a valid stream is skipped.
	data, _ := os.ReadFile(filepath.Join(testdata, "tone.opus"))
	full, _ := Open(bytes.NewReader(data), int64(len(data)))
	want := len(readAll(t, full))
	junk := append(bytes.Repeat([]byte{0x4F, 0x67}, 5000), data...)
	r, err := Open(bytes.NewReader(junk), int64(len(junk)))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(readAll(t, r)); n != want {
		t.Errorf("%d packets after junk", n)
	}
}

// buildPage serialises a page (for synthetic tests).
func buildPage(serial, seq uint32, flags byte, granule int64, lacing, body []byte) []byte {
	b := make([]byte, headerLen, headerLen+len(lacing)+len(body))
	copy(b, "OggS")
	b[5] = flags
	binary.LittleEndian.PutUint64(b[6:], uint64(granule))
	binary.LittleEndian.PutUint32(b[14:], serial)
	binary.LittleEndian.PutUint32(b[18:], seq)
	b[26] = byte(len(lacing))
	b = append(b, lacing...)
	b = append(b, body...)
	binary.LittleEndian.PutUint32(b[22:], crcUpdate(0, b))
	return b
}

// TestPacketSpanningPages: a 600-byte packet split over three pages.
func TestPacketSpanningPages(t *testing.T) {
	head := []byte("OpusHead\x01\x01\x00\x00\x80\xbb\x00\x00\x00\x00\x00")
	tags := []byte("OpusTags\x03\x00\x00\x00abc\x00\x00\x00\x00")
	var f []byte
	f = append(f, buildPage(7, 0, flagBOS, 0, []byte{byte(len(head))}, head)...)
	f = append(f, buildPage(7, 1, 0, 0, []byte{byte(len(tags))}, tags)...)
	big := bytes.Repeat([]byte{31 << 3}, 600) // CELT 20 ms
	small := []byte{31 << 3, 1, 2}
	f = append(f, buildPage(7, 2, 0, -1, []byte{255}, big[:255])...)
	f = append(f, buildPage(7, 3, flagContinued, -1, []byte{255}, big[255:510])...)
	f = append(f, buildPage(7, 4, flagContinued|flagEOS, 1500, []byte{90, 3}, append(append([]byte(nil), big[510:]...), small...))...)
	r, err := Open(bytes.NewReader(f), int64(len(f)))
	if err != nil {
		t.Fatal(err)
	}
	got := readAll(t, r)
	if len(got) != 2 || !bytes.Equal(got[0].Data, big) || !bytes.Equal(got[1].Data, small) {
		t.Fatalf("got %d packets", len(got))
	}
	// The header pages' granule 0 fixes the start; the EOS granule 1500
	// trims the second 960-sample packet to 540 samples.
	if got[0].PTS != 0 || got[0].Duration != 20*time.Millisecond || got[1].Duration != r.toDur(540) || r.Duration() != r.toDur(1500) {
		t.Errorf("timing: %v %v", got[0].PTS, got[1].Duration)
	}
	if r.Vendor != "abc" {
		t.Errorf("vendor %q", r.Vendor)
	}
}

func TestMutations(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for _, name := range []string{"tone.opus", "tone_vorbis.ogg", "flac_opus_multi.ogg"} {
		orig, _ := os.ReadFile(filepath.Join(testdata, name))
		for it := 0; it < 300; it++ {
			b := append([]byte(nil), orig...)
			switch it % 3 {
			case 0:
				for k := 0; k < 1+rng.Intn(10); k++ {
					b[rng.Intn(len(b))] = byte(rng.Intn(256))
				}
			case 1:
				b = b[:rng.Intn(len(b))]
			case 2:
				at := rng.Intn(len(b))
				junk := make([]byte, rng.Intn(300))
				rng.Read(junk)
				b = append(b[:at], append(junk, b[at:]...)...)
			}
			done := make(chan struct{})
			go func() { defer close(done); exercise(b) }()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("%s mutation %d: hang", name, it)
			}
		}
	}
}

func exercise(b []byte) {
	_ = Sniff(b)
	r, err := Open(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return
	}
	_ = r.Duration()
	for i := 0; i < 100000; i++ {
		if _, err := r.ReadPacket(); err != nil {
			break
		}
	}
	for _, t := range []time.Duration{0, 500 * time.Millisecond, time.Hour} {
		_, _ = r.Seek(t)
		for i := 0; i < 50; i++ {
			if _, err := r.ReadPacket(); err != nil {
				break
			}
		}
	}
}

func FuzzOpen(f *testing.F) {
	for _, name := range []string{"tone.opus", "tone_vorbis.ogg"} {
		b, _ := os.ReadFile(filepath.Join(testdata, name))
		f.Add(b[:min(len(b), 4000)])
	}
	f.Fuzz(func(t *testing.T, b []byte) { exercise(b) })
}
