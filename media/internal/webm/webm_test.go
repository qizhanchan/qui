package webm

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testdata = "../../testdata"

// refPacket is one packet of an ffprobe reference listing
// (testdata/<file>.packets.csv, from
// `ffprobe -show_entries packet=stream_index,pts,duration,size,flags,data_hash -show_data_hash sha256`).
type refPacket struct {
	stream   int
	pts, dur int64 // stream time base
	hasDur   bool
	size     int
	key      bool
	hash     string
}

type refFile struct {
	codecs  []string
	tbNum   []int64
	tbDen   []int64
	packets []refPacket
}

func loadRef(t *testing.T, name string) *refFile {
	t.Helper()
	f, err := os.Open(filepath.Join(testdata, name+".packets.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ref := &refFile{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), ",")
		switch p[0] {
		case "S":
			tb := strings.Split(p[3], "/")
			num, _ := strconv.ParseInt(tb[0], 10, 64)
			den, _ := strconv.ParseInt(tb[1], 10, 64)
			ref.codecs = append(ref.codecs, p[2])
			ref.tbNum = append(ref.tbNum, num)
			ref.tbDen = append(ref.tbDen, den)
		case "P":
			var rp refPacket
			rp.stream, _ = strconv.Atoi(p[1])
			rp.pts, _ = strconv.ParseInt(p[2], 10, 64)
			if p[3] != "N/A" {
				rp.dur, _ = strconv.ParseInt(p[3], 10, 64)
				rp.hasDur = true
			}
			rp.size, _ = strconv.Atoi(p[4])
			rp.key = strings.HasPrefix(p[5], "K")
			rp.hash = strings.TrimPrefix(p[6], "SHA256:")
			ref.packets = append(ref.packets, rp)
		}
	}
	return ref
}

func (ref *refFile) toDur(stream int, v int64) time.Duration {
	return time.Duration(v * ref.tbNum[stream] * int64(time.Second) / ref.tbDen[stream])
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
	if !Sniff(data[:min(64, len(data))]) {
		t.Fatalf("%s: Sniff = false", name)
	}
	r, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("%s: Open: %v", name, err)
	}
	return r, data
}

// near compares a time against an ffprobe value rounded to the stream
// time base.
func near(got, want time.Duration, tb time.Duration) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= tb
}

// checkAgainstRef compares every track's index with ffprobe.
func checkAgainstRef(t *testing.T, name string) *Reader {
	t.Helper()
	r, _ := openFile(t, name)
	ref := loadRef(t, name)
	if len(r.Tracks()) != len(ref.codecs) {
		t.Fatalf("%s: %d tracks, ffprobe %d", name, len(r.Tracks()), len(ref.codecs))
	}
	for si, tr := range r.Tracks() {
		if tr.Codec != ref.codecs[si] {
			t.Errorf("%s: track %d codec %q, ffprobe %q", name, si, tr.Codec, ref.codecs[si])
		}
		var want []refPacket
		for _, p := range ref.packets {
			if p.stream == si {
				want = append(want, p)
			}
		}
		if n := r.SampleCount(tr); n != len(want) {
			t.Fatalf("%s: track %d: %d frames, ffprobe %d", name, si, n, len(want))
		}
		tb := ref.toDur(si, 1)
		for i, w := range want {
			s, err := r.Sample(tr, i)
			if err != nil {
				t.Fatalf("%s: track %d sample %d: %v", name, si, i, err)
			}
			data, err := r.ReadSample(tr, i)
			if err != nil {
				t.Fatalf("%s: track %d read %d: %v", name, si, i, err)
			}
			if len(data) != w.size || s.Size != w.size {
				t.Errorf("%s: track %d frame %d: size %d/%d, ffprobe %d", name, si, i, len(data), s.Size, w.size)
			}
			if sha(data) != w.hash {
				t.Errorf("%s: track %d frame %d: hash mismatch", name, si, i)
			}
			if !near(s.PTS, ref.toDur(si, w.pts), tb) {
				t.Errorf("%s: track %d frame %d: pts %v, ffprobe %v", name, si, i, s.PTS, ref.toDur(si, w.pts))
			}
			if w.hasDur && !near(s.Duration, ref.toDur(si, w.dur), tb) {
				t.Errorf("%s: track %d frame %d: duration %v, ffprobe %v", name, si, i, s.Duration, ref.toDur(si, w.dur))
			}
			if s.Keyframe != w.key {
				t.Errorf("%s: track %d frame %d: key %v, ffprobe %v", name, si, i, s.Keyframe, w.key)
			}
		}
	}
	// ReadFrame returns the same frames in file order.
	r.Rewind()
	for i, w := range ref.packets {
		f, err := r.ReadFrame()
		if err != nil {
			t.Fatalf("%s: ReadFrame %d: %v", name, i, err)
		}
		if f.Track != r.Tracks()[w.stream] || sha(f.Data) != w.hash || f.Keyframe != w.key {
			t.Fatalf("%s: ReadFrame %d: track %d key %v, want stream %d key %v (hash match %v)",
				name, i, f.Track.Number, f.Keyframe, w.stream, w.key, sha(f.Data) == w.hash)
		}
		if !near(f.PTS, ref.toDur(w.stream, w.pts), ref.toDur(w.stream, 1)) {
			t.Errorf("%s: ReadFrame %d: pts %v want %v", name, i, f.PTS, ref.toDur(w.stream, w.pts))
		}
	}
	if _, err := r.ReadFrame(); err != io.EOF {
		t.Errorf("%s: ReadFrame at end: %v, want io.EOF", name, err)
	}
	return r
}

func TestAV1Opus(t *testing.T) {
	r := checkAgainstRef(t, "av1_opus_160x90.webm")
	v, a := r.VideoTrack(), r.AudioTrack()
	if v.Codec != "av1" || v.Width != 160 || v.Height != 90 || v.DisplayWidth != 160 {
		t.Errorf("video track: %+v", v)
	}
	if len(v.CodecPrivate) < 4 || v.CodecPrivate[0] != 0x81 {
		t.Errorf("av1C CodecPrivate: % x", v.CodecPrivate)
	}
	if a.Codec != "opus" || a.SampleRate != 48000 || a.Channels != 1 {
		t.Errorf("audio track: %+v", a)
	}
	if a.CodecDelay != 6500*time.Microsecond || a.SeekPreRoll != 80*time.Millisecond {
		t.Errorf("opus delay %v preroll %v", a.CodecDelay, a.SeekPreRoll)
	}
	if !bytes.HasPrefix(a.CodecPrivate, []byte("OpusHead")) {
		t.Errorf("OpusHead missing")
	}
	if d := r.Duration(); d < 990*time.Millisecond || d > 1100*time.Millisecond {
		t.Errorf("duration %v", d)
	}
	if !r.HasCues() {
		t.Errorf("expected Cues")
	}
}

func TestVP9Opus(t *testing.T) {
	r := checkAgainstRef(t, "vp9_opus_160x90.webm")
	v := r.VideoTrack()
	if v.Codec != "vp9" || v.Width != 160 || v.Height != 90 {
		t.Errorf("video track: %+v", v)
	}
	if !r.HasCues() || len(r.cues) < 3 {
		t.Fatalf("cues: %d", len(r.cues))
	}
	if a := r.AudioTrack(); a.Channels != 2 {
		t.Errorf("audio channels %d", a.Channels)
	}
}

func TestLiveUnknownSegment(t *testing.T) {
	r := checkAgainstRef(t, "vp8_opus_live.webm")
	if r.VideoTrack().Codec != "vp8" {
		t.Errorf("codec %q", r.VideoTrack().Codec)
	}
	if r.HasCues() {
		t.Errorf("live file should have no cues")
	}
	if d := r.Duration(); d < 950*time.Millisecond || d > 1100*time.Millisecond {
		t.Errorf("duration %v", d)
	}
}

func TestH264AAC(t *testing.T) {
	r := checkAgainstRef(t, "h264_aac_160x90.mkv")
	v := r.VideoTrack()
	sps, pps, ls, err := AVCC(v.CodecPrivate)
	if err != nil || len(sps) != 1 || len(pps) != 1 || ls != 4 {
		t.Errorf("AVCC: %d sps %d pps ls %d err %v", len(sps), len(pps), ls, err)
	}
	if a := r.AudioTrack(); a.Codec != "aac" || len(a.CodecPrivate) < 2 {
		t.Errorf("aac track %+v", a)
	}
}

func TestHEVC(t *testing.T) {
	r := checkAgainstRef(t, "hevc_160x90.mkv")
	vps, sps, pps, ls, err := HVCC(r.VideoTrack().CodecPrivate)
	if err != nil || len(vps) != 1 || len(sps) != 1 || len(pps) != 1 || ls != 4 {
		t.Errorf("HVCC: %d/%d/%d ls %d err %v", len(vps), len(sps), len(pps), ls, err)
	}
}

func TestVorbisMKA(t *testing.T) {
	r := checkAgainstRef(t, "vorbis_audio.mka")
	a := r.AudioTrack()
	if a.Codec != "vorbis" || a.SampleRate != 44100 || a.Channels != 2 || len(a.CodecPrivate) < 30 {
		t.Errorf("vorbis track %+v", a)
	}
}

func TestSeekKeyframe(t *testing.T) {
	r, _ := openFile(t, "vp9_opus_160x90.webm")
	v := r.VideoTrack()
	n := r.SampleCount(v)
	ks := r.Keyframes(v)
	if len(ks) < 3 {
		t.Fatalf("keyframes %v", ks)
	}
	for i := 0; i < n; i++ {
		target := r.PTS(v, i) + time.Millisecond
		k := r.SeekKeyframe(v, target)
		want := 0
		for _, kk := range ks {
			if r.PTS(v, kk) <= target {
				want = kk
			}
		}
		if k != want {
			t.Errorf("SeekKeyframe(%v) = %d, want %d", target, k, want)
		}
	}
	if k := r.SeekKeyframe(v, -time.Second); k != ks[0] {
		t.Errorf("seek before start: %d", k)
	}
	a := r.AudioTrack()
	if i := r.SampleAtTime(a, 500*time.Millisecond); r.PTS(a, i) > 500*time.Millisecond || r.PTS(a, i)+r.SampleDuration(a, i) <= 500*time.Millisecond {
		t.Errorf("SampleAtTime: %d at %v", i, r.PTS(a, i))
	}
}

// TestLazyIndex checks that SeekKeyframe near the start doesn't scan the
// whole file.
func TestLazyIndex(t *testing.T) {
	r, _ := openFile(t, "vp9_opus_160x90.webm")
	r.SeekKeyframe(r.VideoTrack(), 10*time.Millisecond)
	if r.Indexed() || len(r.clusters) >= 5 {
		t.Errorf("scanned %d clusters (done=%v) for a seek near the start", len(r.clusters), r.Indexed())
	}
	if _, err := r.Sample(r.VideoTrack(), 0); err != nil {
		t.Fatal(err)
	}
	if r.Indexed() {
		t.Errorf("Sample(0) indexed everything")
	}
}

// TestSeekTo checks cue-based (and index-based) cursor seeking.
func TestSeekTo(t *testing.T) {
	for _, name := range []string{"vp9_opus_160x90.webm", "vp8_opus_live.webm"} {
		r, _ := openFile(t, name)
		v := r.VideoTrack()
		ks := r.Keyframes(v)
		for _, target := range []time.Duration{0, 300 * time.Millisecond, 620 * time.Millisecond, 5 * time.Second} {
			got, err := r.SeekTo(target)
			if err != nil {
				t.Fatal(err)
			}
			want := r.PTS(v, r.SeekKeyframe(v, target))
			if name == "vp9_opus_160x90.webm" {
				// Cues point at clusters; every keyframe here starts one.
				_ = ks
			}
			if got > target && target > 0 || got > want {
				t.Errorf("%s: SeekTo(%v) = %v, keyframe %v", name, target, got, want)
			}
			// The first video frame read is a keyframe at the returned time.
			for {
				f, err := r.ReadFrame()
				if err != nil {
					t.Fatalf("%s: ReadFrame after seek: %v", name, err)
				}
				if f.Track != v {
					continue
				}
				if !f.Keyframe || f.PTS != got {
					t.Errorf("%s: first video frame after SeekTo(%v): key %v pts %v, want %v", name, target, f.Keyframe, f.PTS, got)
				}
				break
			}
		}
	}
}

// patchUnknownClusters rewrites every Cluster size vint to the
// same-length "unknown size" value.
func patchUnknownClusters(t *testing.T, data []byte) []byte {
	out := append([]byte(nil), data...)
	n := 0
	for i := 0; i+5 <= len(out); i++ {
		if out[i] == 0x1F && out[i+1] == 0x43 && out[i+2] == 0xB6 && out[i+3] == 0x75 {
			_, l, ok := readVint(out[i+4:])
			if !ok {
				continue
			}
			out[i+4] = 0xFF >> (l - 1) // marker + all ones: (1<<(8-l))-1 with marker
			if l > 1 {
				out[i+4] = byte(0xFF >> (l - 1))
			}
			for j := 1; j < l; j++ {
				out[i+4+j] = 0xFF
			}
			n++
		}
	}
	if n == 0 {
		t.Fatal("no clusters patched")
	}
	return out
}

func TestUnknownSizeClusters(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testdata, "vp8_opus_live.webm"))
	if err != nil {
		t.Fatal(err)
	}
	patched := patchUnknownClusters(t, data)
	r, err := Open(bytes.NewReader(patched), int64(len(patched)))
	if err != nil {
		t.Fatal(err)
	}
	ref := loadRef(t, "vp8_opus_live.webm")
	for i, w := range ref.packets {
		f, err := r.ReadFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if sha(f.Data) != w.hash || f.Track != r.Tracks()[w.stream] {
			t.Fatalf("frame %d mismatch", i)
		}
	}
	if _, err := r.ReadFrame(); err != io.EOF {
		t.Errorf("end: %v", err)
	}
	for si, tr := range r.Tracks() {
		cnt := 0
		for _, p := range ref.packets {
			if p.stream == si {
				cnt++
			}
		}
		if n := r.SampleCount(tr); n != cnt {
			t.Errorf("track %d: %d frames, want %d", si, n, cnt)
		}
	}
}

// splitInit cuts a file into its initialization segment (up to the
// first Cluster) and the rest.
func splitInit(t *testing.T, data []byte) (init, media []byte) {
	i := bytes.Index(data, []byte{0x1F, 0x43, 0xB6, 0x75})
	if i < 0 {
		t.Fatal("no cluster")
	}
	return data[:i], data[i:]
}

func TestMSE(t *testing.T) {
	for _, name := range []string{"vp8_opus_live.webm", "vp9_opus_160x90.webm", "av1_opus_160x90.webm"} {
		data, err := os.ReadFile(filepath.Join(testdata, name))
		if err != nil {
			t.Fatal(err)
		}
		ref := loadRef(t, name)
		init, media := splitInit(t, data)
		r, err := OpenInit(init)
		if err != nil {
			t.Fatalf("%s: OpenInit: %v", name, err)
		}
		if len(r.Tracks()) != len(ref.codecs) {
			t.Fatalf("%s: tracks %d", name, len(r.Tracks()))
		}
		// Whole media segments at once.
		frames, err := r.ParseCluster(media)
		if err != nil {
			t.Fatalf("%s: ParseCluster: %v", name, err)
		}
		compareFrames(t, name+"/ParseCluster", r, ref, frames)

		// The same stream split at awkward points.
		r2, _ := OpenInit(init)
		var got []Frame
		for off := 0; off < len(media); {
			n := min(len(media)-off, 1+off%977)
			fs, err := r2.AppendBytes(media[off : off+n])
			if err != nil {
				t.Fatalf("%s: AppendBytes: %v", name, err)
			}
			got = append(got, fs...)
			off += n
		}
		got = append(got, r2.Flush()...)
		compareFrames(t, name+"/AppendBytes", r2, ref, got)
	}
}

func compareFrames(t *testing.T, name string, r *Reader, ref *refFile, frames []Frame) {
	t.Helper()
	if len(frames) != len(ref.packets) {
		t.Fatalf("%s: %d frames, ffprobe %d", name, len(frames), len(ref.packets))
	}
	for i, w := range ref.packets {
		f := frames[i]
		if f.Track != r.Tracks()[w.stream] || sha(f.Data) != w.hash || f.Keyframe != w.key {
			t.Fatalf("%s: frame %d mismatch", name, i)
		}
		tb := ref.toDur(w.stream, 1)
		if !near(f.PTS, ref.toDur(w.stream, w.pts), tb) {
			t.Errorf("%s: frame %d pts %v want %v", name, i, f.PTS, ref.toDur(w.stream, w.pts))
		}
		if w.hasDur && !near(f.Duration, ref.toDur(w.stream, w.dur), tb) {
			t.Errorf("%s: frame %d dur %v want %v", name, i, f.Duration, ref.toDur(w.stream, w.dur))
		}
	}
}

func TestTruncated(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testdata, "vp9_opus_160x90.webm"))
	if err != nil {
		t.Fatal(err)
	}
	full, _ := Open(bytes.NewReader(data), int64(len(data)))
	total := 0
	for _, tr := range full.Tracks() {
		total += full.SampleCount(tr)
	}
	prev := -1
	for cut := len(data); cut > 0; cut -= 97 {
		d := data[:cut]
		r, err := Open(bytes.NewReader(d), int64(len(d)))
		if err != nil {
			continue
		}
		n := 0
		for _, tr := range r.Tracks() {
			n += r.SampleCount(tr)
			for i := 0; i < r.SampleCount(tr); i++ {
				if _, err := r.ReadSample(tr, i); err != nil {
					t.Fatalf("cut %d: ReadSample(%d): %v", cut, i, err)
				}
			}
		}
		if n > total || (prev >= 0 && n > prev) {
			t.Errorf("cut %d: %d frames (full %d, previous cut %d)", cut, n, total, prev)
		}
		prev = n
		r.Rewind()
		for {
			if _, err := r.ReadFrame(); err != nil {
				if err != io.EOF {
					t.Fatalf("cut %d: ReadFrame: %v", cut, err)
				}
				break
			}
		}
	}
	if prev > total/4 {
		t.Errorf("heavily truncated file still has %d frames", prev)
	}
}

func TestCorruptCluster(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testdata, "vp9_opus_160x90.webm"))
	if err != nil {
		t.Fatal(err)
	}
	full, _ := Open(bytes.NewReader(data), int64(len(data)))
	v := full.VideoTrack()
	total := full.SampleCount(v)
	// Smash the middle of the second cluster: frames of later clusters
	// must still be found by resynchronising.
	idx := bytes.Index(data[bytes.Index(data, []byte{0x1F, 0x43, 0xB6, 0x75})+4:], []byte{0x1F, 0x43, 0xB6, 0x75})
	if idx < 0 {
		t.Fatal("no second cluster")
	}
	bad := append([]byte(nil), data...)
	at := idx + 200
	for i := 0; i < 40; i++ {
		bad[at+i] = 0
	}
	r, err := Open(bytes.NewReader(bad), int64(len(bad)))
	if err != nil {
		t.Fatal(err)
	}
	n := r.SampleCount(r.VideoTrack())
	if n == 0 || n > total {
		t.Errorf("corrupt file: %d frames (full %d)", n, total)
	}
	last, _ := r.Sample(r.VideoTrack(), n-1)
	fl, _ := full.Sample(v, total-1)
	if last.PTS != fl.PTS {
		t.Errorf("resync lost the tail: last pts %v, want %v", last.PTS, fl.PTS)
	}
}

func TestGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, {0x1A}, {0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{0xFF}, 100), []byte("not a webm file at all")} {
		if _, err := Open(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Errorf("Open(% x) succeeded", b)
		}
		if Sniff(b) {
			t.Errorf("Sniff(% x) = true", b)
		}
	}
	if _, err := OpenInit([]byte{1, 2, 3}); err == nil {
		t.Error("OpenInit garbage succeeded")
	}
}

func TestSampleOutOfRange(t *testing.T) {
	r, _ := openFile(t, "av1_opus_160x90.webm")
	v := r.VideoTrack()
	if _, err := r.Sample(v, 1<<20); !errors.Is(err, io.EOF) {
		t.Errorf("Sample past end: %v", err)
	}
	if _, err := r.Sample(v, -1); err == nil {
		t.Error("Sample(-1) succeeded")
	}
	if _, err := r.Sample(&Track{}, 0); !errors.Is(err, ErrNoTrack) {
		t.Errorf("foreign track: %v", err)
	}
}
