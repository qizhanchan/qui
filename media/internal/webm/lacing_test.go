package webm

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"
	"time"
)

// A tiny EBML writer for synthetic files.

func vintSize(n int) []byte {
	for l := 1; l <= 8; l++ {
		if uint64(n) < 1<<(7*l)-1 {
			b := make([]byte, l)
			v := uint64(n) | 1<<(7*l)
			for i := l - 1; i >= 0; i-- {
				b[i] = byte(v)
				v >>= 8
			}
			return b
		}
	}
	panic("too big")
}

func idBytes(id uint32) []byte {
	switch {
	case id >= 1<<24:
		return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	case id >= 1<<16:
		return []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	case id >= 1<<8:
		return []byte{byte(id >> 8), byte(id)}
	}
	return []byte{byte(id)}
}

func el(id uint32, parts ...[]byte) []byte {
	payload := bytes.Join(parts, nil)
	out := append(idBytes(id), vintSize(len(payload))...)
	return append(out, payload...)
}

func uel(id uint32, v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	i := 0
	for i < 7 && b[i] == 0 {
		i++
	}
	return el(id, b[i:])
}

func fel(id uint32, v float64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	return el(id, b[:])
}

func sel(id uint32, s string) []byte { return el(id, []byte(s)) }

func header(doc string) []byte {
	return el(idEBML, sel(idDocType, doc))
}

// lacedBlock builds a (Simple)Block payload with the given lacing
// (0 none, 1 Xiph, 2 fixed, 3 EBML).
func lacedBlock(track int, rel int16, flags byte, lacing int, frames [][]byte) []byte {
	b := append(vintSize(track), byte(uint16(rel)>>8), byte(rel))
	b = append(b, flags|byte(lacing<<1))
	if lacing != 0 {
		b = append(b, byte(len(frames)-1))
	}
	switch lacing {
	case 1:
		for _, f := range frames[:len(frames)-1] {
			n := len(f)
			for n >= 255 {
				b = append(b, 255)
				n -= 255
			}
			b = append(b, byte(n))
		}
	case 3:
		if len(frames) == 1 {
			break
		}
		b = append(b, vintSize(len(frames[0]))...)
		for i := 1; i < len(frames)-1; i++ {
			d := int64(len(frames[i]) - len(frames[i-1]))
			// signed vint, 2 bytes: bias 2^13-1
			v := uint64(d+(1<<13-1)) | 1<<14
			b = append(b, byte(v>>8), byte(v))
		}
	}
	for _, f := range frames {
		b = append(b, f...)
	}
	return b
}

func audioTrack(num int, extra ...[]byte) []byte {
	parts := [][]byte{uel(idTrackNumber, uint64(num)), uel(idTrackType, 2), sel(idCodecID, "A_VORBIS"),
		el(idAudio, fel(idSamplingFrequency, 44100), uel(idChannels, 2))}
	return el(idTrackEntry, append(parts, extra...)...)
}

func segment(tracks []byte, clusters ...[]byte) []byte {
	info := el(idInfo, uel(idTimecodeScale, 1000000))
	return el(idSegment, append([][]byte{info, el(idTracks, tracks)}, clusters...)...)
}

// vorbisFrames returns the frames and pts of the real Vorbis file.
func vorbisFrames(t *testing.T) ([][]byte, []time.Duration) {
	r, _ := openFile(t, "vorbis_audio.mka")
	a := r.AudioTrack()
	var fs [][]byte
	var ts []time.Duration
	for i := 0; i < r.SampleCount(a); i++ {
		b, err := r.ReadSample(a, i)
		if err != nil {
			t.Fatal(err)
		}
		fs = append(fs, b)
		ts = append(ts, r.PTS(a, i))
	}
	return fs, ts
}

func readAll(t *testing.T, data []byte) (*Reader, []Sample, [][]byte) {
	t.Helper()
	r, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	a := r.Tracks()[0]
	var ss []Sample
	var bs [][]byte
	for i := 0; i < r.SampleCount(a); i++ {
		s, err := r.Sample(a, i)
		if err != nil {
			t.Fatal(err)
		}
		b, err := r.ReadSample(a, i)
		if err != nil {
			t.Fatal(err)
		}
		ss = append(ss, s)
		bs = append(bs, b)
	}
	return r, ss, bs
}

// TestXiphLacingNoDuration: laced blocks without durations get evenly
// spread timestamps up to the next block.
func TestXiphLacingNoDuration(t *testing.T) {
	frames, pts := vorbisFrames(t)
	const group = 4
	var blocks [][]byte
	var blockTS []int64 // ms
	blocks = append(blocks, uel(idTimestamp, 0))
	for i := 0; i < len(frames); i += group {
		end := min(i+group, len(frames))
		ms := int64(max(pts[i], 0) / time.Millisecond)
		blockTS = append(blockTS, ms)
		blocks = append(blocks, el(idSimpleBlock, lacedBlock(1, int16(ms), 0x80, 1, frames[i:end])))
	}
	file := append(header("matroska"), segment(audioTrack(1), el(idCluster, blocks...))...)
	if p := os.Getenv("WEBM_DUMP_LACED"); p != "" {
		_ = os.WriteFile(p, file, 0o644)
	}
	_, ss, bs := readAll(t, file)
	if len(ss) != len(frames) {
		t.Fatalf("%d frames, want %d", len(ss), len(frames))
	}
	var lastDur int64
	for i := range frames {
		if !bytes.Equal(bs[i], frames[i]) {
			t.Fatalf("frame %d payload differs", i)
		}
		g, k := i/group, int64(i%group)
		base := blockTS[g] * 1e6
		var want, wantDur int64
		if g+1 < len(blockTS) {
			span := blockTS[g+1]*1e6 - base
			n := int64(group)
			want = base + span*k/n
			wantDur = base + span*(k+1)/n - want
			lastDur = wantDur
		} else {
			want = base + lastDur*k
			wantDur = lastDur
		}
		if int64(ss[i].PTS) != want || int64(ss[i].Duration) != wantDur || !ss[i].Keyframe {
			t.Errorf("frame %d: pts %v dur %v key %v, want %v %v", i, ss[i].PTS, ss[i].Duration, ss[i].Keyframe,
				time.Duration(want), time.Duration(wantDur))
		}
	}
}

// TestEBMLLacingDefaultDuration: EBML lacing, with lace times from
// DefaultDuration; also checks the 255-run Xiph sizes via large frames.
func TestEBMLLacingDefaultDuration(t *testing.T) {
	frames, _ := vorbisFrames(t)
	big := bytes.Repeat([]byte{7}, 600) // > 255: multi-byte Xiph sizes
	frames = append([][]byte{big}, frames...)
	const dd = 23219955
	for _, lacing := range []int{1, 3} {
		var blocks [][]byte
		blocks = append(blocks, uel(idTimestamp, 1000))
		for i := 0; i < len(frames); i += 5 {
			end := min(i+5, len(frames))
			rel := int16(int64(i) * dd / 1e6)
			blocks = append(blocks, el(idSimpleBlock, lacedBlock(1, rel, 0x80, lacing, frames[i:end])))
		}
		file := append(header("webm"), segment(audioTrack(1, uel(idDefaultDuration, dd)), el(idCluster, blocks...))...)
		_, ss, bs := readAll(t, file)
		if len(ss) != len(frames) {
			t.Fatalf("lacing %d: %d frames, want %d", lacing, len(ss), len(frames))
		}
		for i := range frames {
			if !bytes.Equal(bs[i], frames[i]) {
				t.Fatalf("lacing %d: frame %d payload differs", lacing, i)
			}
			g := i / 5
			base := (1000 + int64(int16(int64(g*5)*dd/1e6))) * 1e6
			want := time.Duration(base + int64(i%5)*dd)
			if ss[i].PTS != want || ss[i].Duration != dd {
				t.Errorf("lacing %d frame %d: pts %v dur %v, want %v %v", lacing, i, ss[i].PTS, ss[i].Duration, want, time.Duration(dd))
			}
		}
	}
}

// TestFixedLacingBlockGroup: fixed-size lacing in a BlockGroup with a
// BlockDuration, a ReferenceBlock (non-key) and DiscardPadding.
func TestFixedLacingBlockGroup(t *testing.T) {
	mk := func(seed byte) [][]byte {
		var fs [][]byte
		for i := 0; i < 4; i++ {
			fs = append(fs, bytes.Repeat([]byte{seed + byte(i)}, 33))
		}
		return fs
	}
	g1 := el(idBlockGroup, el(idBlock, lacedBlock(1, 0, 0, 2, mk(10))), uel(idBlockDuration, 40))
	g2 := el(idBlockGroup, el(idBlock, lacedBlock(1, 40, 0, 2, mk(20))), uel(idBlockDuration, 40),
		el(idReferenceBlock, []byte{0xD8}), uel(idDiscardPadding, 5000000))
	file := append(header("matroska"), segment(audioTrack(1), el(idCluster, uel(idTimestamp, 0), g1, g2))...)
	_, ss, bs := readAll(t, file)
	if len(ss) != 8 {
		t.Fatalf("%d frames", len(ss))
	}
	for i := range ss {
		if ss[i].PTS != time.Duration(i)*10*time.Millisecond || ss[i].Duration != 10*time.Millisecond {
			t.Errorf("frame %d: pts %v dur %v", i, ss[i].PTS, ss[i].Duration)
		}
		if ss[i].Keyframe != (i < 4) {
			t.Errorf("frame %d: key %v", i, ss[i].Keyframe)
		}
		seed := byte(10)
		if i >= 4 {
			seed = 20
		}
		if !bytes.Equal(bs[i], bytes.Repeat([]byte{seed + byte(i%4)}, 33)) {
			t.Errorf("frame %d payload", i)
		}
	}
	if ss[7].DiscardPadding != 5*time.Millisecond || ss[6].DiscardPadding != 0 {
		t.Errorf("discard padding %v %v", ss[6].DiscardPadding, ss[7].DiscardPadding)
	}
	// Fixed lacing whose payload doesn't divide evenly is rejected
	// without losing the earlier block.
	bad := lacedBlock(1, 50, 0x80, 2, mk(30))
	bad = bad[:len(bad)-1]
	file = append(header("matroska"), segment(audioTrack(1), el(idCluster, uel(idTimestamp, 0), g1, el(idSimpleBlock, bad)))...)
	_, ss, _ = readAll(t, file)
	if len(ss) != 4 {
		t.Errorf("bad fixed lacing: %d frames, want 4", len(ss))
	}
}

// TestHeaderStripping: ContentCompAlgo 3 re-attaches the stripped bytes.
func TestHeaderStripping(t *testing.T) {
	prefix := []byte{0xAA, 0xBB}
	enc := el(idContentEncodings, el(idContentEncoding, el(idContentCompression, uel(idContentCompAlgo, 3), el(idContentCompSettings, prefix))))
	frames := [][]byte{{1, 2, 3}, {4, 5}, {6}}
	var blocks [][]byte
	blocks = append(blocks, uel(idTimestamp, 0))
	for i, f := range frames {
		blocks = append(blocks, el(idSimpleBlock, lacedBlock(1, int16(i*20), 0x80, 0, [][]byte{f})))
	}
	file := append(header("matroska"), segment(audioTrack(1, enc), el(idCluster, blocks...))...)
	r, ss, bs := readAll(t, file)
	for i, f := range frames {
		want := append(append([]byte(nil), prefix...), f...)
		if !bytes.Equal(bs[i], want) || ss[i].Size != len(want) {
			t.Errorf("frame %d: % x", i, bs[i])
		}
	}
	// MSE path too.
	init, media := splitInit(t, file)
	mr, err := OpenInit(init)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := mr.ParseCluster(media)
	if err != nil || len(fs) != 3 || !bytes.Equal(fs[0].Data, []byte{0xAA, 0xBB, 1, 2, 3}) {
		t.Errorf("MSE header stripping: %v %d", err, len(fs))
	}
	// Compressed (zlib) tracks are flagged and refuse reads.
	zl := el(idContentEncodings, el(idContentEncoding, el(idContentCompression, uel(idContentCompAlgo, 0))))
	file = append(header("matroska"), segment(audioTrack(1, zl), el(idCluster, blocks...))...)
	r, err = Open(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Tracks()[0].Compressed {
		t.Error("Compressed not set")
	}
	if _, err := r.ReadSample(r.Tracks()[0], 0); err != ErrUnsupported {
		t.Errorf("zlib read: %v", err)
	}
}

// TestNoDurationVideo: frames without BlockDuration / DefaultDuration
// take the next block's timestamp; the last one repeats the previous.
func TestNoDurationVideo(t *testing.T) {
	tr := el(idTrackEntry, uel(idTrackNumber, 3), uel(idTrackType, 1), sel(idCodecID, "V_VP8"),
		el(idVideo, uel(idPixelWidth, 16), uel(idPixelHeight, 8)))
	var blocks [][]byte
	blocks = append(blocks, uel(idTimestamp, 100))
	rel := []int16{0, 33, 70, 100}
	for i, r := range rel {
		flags := byte(0)
		if i == 0 {
			flags = 0x80
		}
		blocks = append(blocks, el(idSimpleBlock, lacedBlock(3, r, flags, 0, [][]byte{{byte(i)}})))
	}
	// A second cluster so the duration crosses a cluster boundary.
	c2 := el(idCluster, uel(idTimestamp, 230), el(idSimpleBlock, lacedBlock(3, 0, 0x80, 0, [][]byte{{9}})))
	file := append(header("webm"), segment(tr, el(idCluster, blocks...), c2)...)
	_, ss, _ := readAll(t, file)
	wantPTS := []int64{100, 133, 170, 200, 230}
	wantDur := []int64{33, 37, 30, 30, 30}
	for i := range ss {
		if ss[i].PTS != time.Duration(wantPTS[i])*time.Millisecond || ss[i].Duration != time.Duration(wantDur[i])*time.Millisecond {
			t.Errorf("frame %d: pts %v dur %v", i, ss[i].PTS, ss[i].Duration)
		}
	}
	if !ss[0].Keyframe || ss[1].Keyframe || !ss[4].Keyframe {
		t.Error("keyframe flags")
	}
}
