package mp4x

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The tests build a tiny but valid progressive MP4 from raw bytes. This
// keeps the fixture self-contained (no committed binary, no encoder) and
// exercises exactly the boxes mp4x reads: ftyp, moov(mvhd, trak(tkhd,
// mdia(mdhd, minf(vmhd, stbl(stsd(avc1(avcC)), stts, stsc, stsz, stco))))),
// mdat. hdlr/dinf are omitted because neither go-mp4's Probe nor mp4x needs them.

func be32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func be16(v uint16) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, v); return b }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// box wraps a payload with the 8-byte ISO box header.
func box(typ string, payload ...[]byte) []byte {
	body := cat(payload...)
	return cat(be32(uint32(8+len(body))), []byte(typ), body)
}

// full is a box whose payload starts with a FullBox header (version+flags).
func full(typ string, version byte, payload ...[]byte) []byte {
	return box(typ, cat([]byte{version, 0, 0, 0}, cat(payload...)))
}

func u32arr(vals ...uint32) []byte {
	var out []byte
	for _, v := range vals {
		out = append(out, be32(v)...)
	}
	return out
}

var (
	testSPS = []byte{0x67, 0x42, 0x00, 0x1e}
	testPPS = []byte{0x68, 0xce, 0x3c, 0x80}

	// Three samples as AVC length-prefixed NAL units. Sample 0 is an IDR
	// (NAL type 5 => header 0x65); samples 1 and 2 are non-IDR (type 1).
	sample0 = cat(be32(3), []byte{0x65, 0x88, 0x84})
	sample1 = cat(be32(2), []byte{0x41, 0x9a})
	sample2 = cat(be32(2), []byte{0x41, 0x9b})
)

// buildTinyMP4 returns the bytes of a one-video-track MP4 with three
// samples, 9000 timescale, 3000-tick sample duration (3 fps, 1 s).
func buildTinyMP4() []byte {
	ftyp := box("ftyp", []byte("isom"), be32(0), []byte("isom"), []byte("mp42"))

	mdatPayload := cat(sample0, sample1, sample2)
	// mdat immediately follows ftyp, so the first sample sits at
	// len(ftyp) + 8 (mdat box header).
	chunkOffset := uint32(len(ftyp) + 8)
	mdat := box("mdat", mdatPayload)

	// avcC (baseline profile so no HighProfile trailing fields are read).
	avcC := box("avcC",
		[]byte{0x01, 0x42, 0x00, 0x1e, // version, profile, compat, level
			0xFF,  // reserved(6)=0x3F | lengthSizeMinusOne(2)=3
			0xE1}, // reserved(3)=7 | numSPS(5)=1
		be16(uint16(len(testSPS))), testSPS,
		[]byte{0x01}, // numPPS
		be16(uint16(len(testPPS))), testPPS,
	)

	// VisualSampleEntry payload: 78 bytes.
	compressor := make([]byte, 32) // [0]=0 => empty string
	vse := cat(
		[]byte{0, 0, 0, 0, 0, 0}, // reserved[6]
		be16(1),                  // data_reference_index
		be16(0),                  // pre_defined
		be16(0),                  // reserved
		u32arr(0, 0, 0),          // pre_defined2[3]
		be16(640), be16(360),     // width, height
		be32(0x00480000), be32(0x00480000), // horiz/vert resolution
		be32(0),      // reserved
		be16(1),      // frame_count
		compressor,   // compressorname[32]
		be16(0x0018), // depth
		be16(0xffff), // pre_defined3 = -1
	)
	avc1 := box("avc1", vse, avcC)

	stsd := full("stsd", 0, be32(1), avc1)
	stts := full("stts", 0, be32(1), be32(3), be32(3000))       // 3 samples, 3000 ticks each
	stsc := full("stsc", 0, be32(1), be32(1), be32(3), be32(1)) // 1 chunk of 3 samples
	stsz := full("stsz", 0, be32(0), be32(3), be32(7), be32(6), be32(6))
	stco := full("stco", 0, be32(1), be32(chunkOffset))
	stbl := box("stbl", stsd, stts, stsc, stsz, stco)

	vmhd := full("vmhd", 0, be16(0), be16(0), be16(0), be16(0))
	minf := box("minf", vmhd, stbl)

	mdhd := full("mdhd", 0, be32(0), be32(0), be32(9000), be32(9000), be16(0x55c4), be16(0))
	mdia := box("mdia", mdhd, minf)

	matrix := u32arr(0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000)
	tkhd := full("tkhd", 0,
		be32(0), be32(0), // creation, modification
		be32(1),          // track_ID
		be32(0),          // reserved
		be32(9000),       // duration
		be32(0), be32(0), // reserved[2]
		be16(0), be16(0), be16(0), be16(0), // layer, alt group, volume, reserved
		matrix,
		be32(640<<16), be32(360<<16),
	)
	trak := box("trak", tkhd, mdia)

	mvhd := full("mvhd", 0,
		be32(0), be32(0),
		be32(1000),            // timescale
		be32(1000),            // duration
		be32(0x00010000),      // rate
		be16(0x0100), be16(0), // volume, reserved
		be32(0), be32(0), // reserved[2]
		matrix,
		u32arr(0, 0, 0, 0, 0, 0), // pre_defined[6]
		be32(2),                  // next_track_ID
	)
	moov := box("moov", mvhd, trak)

	return cat(ftyp, mdat, moov)
}

func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tiny.mp4")
	if err := os.WriteFile(path, buildTinyMP4(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenSyntheticMP4(t *testing.T) {
	r, err := Open(writeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	if r.Fragmented() {
		t.Error("Fragmented() = true, want false")
	}
	if r.AudioTrack() != nil {
		t.Error("AudioTrack() != nil for a video-only file")
	}

	vt := r.VideoTrack()
	if vt == nil {
		t.Fatal("VideoTrack() = nil")
	}
	if vt.Kind != KindVideo || vt.Codec != "h264" {
		t.Errorf("kind/codec = %v/%q, want video/h264", vt.Kind, vt.Codec)
	}
	if vt.Width != 640 || vt.Height != 360 {
		t.Errorf("dimensions = %dx%d, want 640x360", vt.Width, vt.Height)
	}
	if vt.Timescale != 9000 {
		t.Errorf("timescale = %d, want 9000", vt.Timescale)
	}
	if vt.Duration != time.Second {
		t.Errorf("duration = %v, want 1s", vt.Duration)
	}
	if vt.SampleCount() != 3 {
		t.Errorf("sample count = %d, want 3", vt.SampleCount())
	}
	if vt.FPS < 2.9 || vt.FPS > 3.1 {
		t.Errorf("fps = %v, want ~3", vt.FPS)
	}
}

func TestReadSampleAndTimeline(t *testing.T) {
	r, err := Open(writeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	vt := r.VideoTrack()

	got, err := r.ReadSample(vt, 0)
	if err != nil {
		t.Fatalf("ReadSample(0): %v", err)
	}
	if !bytes.Equal(got, sample0) {
		t.Errorf("sample0 = % x, want % x", got, sample0)
	}
	got2, _ := r.ReadSample(vt, 1)
	if !bytes.Equal(got2, sample1) {
		t.Errorf("sample1 = % x, want % x", got2, sample1)
	}

	if d := r.DTS(vt, 0); d != 0 {
		t.Errorf("DTS(0) = %v, want 0", d)
	}
	if d := r.DTS(vt, 1); d < 330*time.Millisecond || d > 336*time.Millisecond {
		t.Errorf("DTS(1) = %v, want ~333ms", d)
	}
}

func TestKeyframesAndSeek(t *testing.T) {
	r, err := Open(writeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	vt := r.VideoTrack()

	ks := r.Keyframes(vt)
	if len(ks) != 1 || ks[0] != 0 {
		t.Errorf("Keyframes = %v, want [0]", ks)
	}
	if idx := r.SeekKeyframe(vt, 700*time.Millisecond); idx != 0 {
		t.Errorf("SeekKeyframe(700ms) = %d, want 0", idx)
	}
}

func TestAVCCParameterSets(t *testing.T) {
	r, err := Open(writeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	sps, pps, lengthSize, err := r.AVCCParameterSets()
	if err != nil {
		t.Fatalf("AVCCParameterSets: %v", err)
	}
	if lengthSize != 4 {
		t.Errorf("lengthSize = %d, want 4", lengthSize)
	}
	if len(sps) != 1 || !bytes.Equal(sps[0], testSPS) {
		t.Errorf("sps = % x, want [% x]", sps, testSPS)
	}
	if len(pps) != 1 || !bytes.Equal(pps[0], testPPS) {
		t.Errorf("pps = % x, want [% x]", pps, testPPS)
	}
}

func TestOpenMissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "nope.mp4")); err == nil {
		t.Fatal("Open(missing) = nil error, want error")
	}
}
