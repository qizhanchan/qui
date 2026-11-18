package mp4x

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// splitFMP4 cuts a fragmented MP4 into its initialization segment and
// its media segments (each moof with the mdat after it), the way an MSE
// player receives it.
func splitFMP4(b []byte) (init []byte, segs [][]byte) {
	pos := 0
	segStart := -1
	for pos+8 <= len(b) {
		size := int(binary.BigEndian.Uint32(b[pos:]))
		typ := string(b[pos+4 : pos+8])
		if size < 8 || pos+size > len(b) {
			break
		}
		switch typ {
		case "moof":
			if segStart < 0 && init == nil {
				init = b[:pos]
			}
			segStart = pos
		case "mdat":
			if segStart >= 0 {
				segs = append(segs, b[segStart:pos+size])
				segStart = -1
			}
		}
		pos += size
	}
	return init, segs
}

func TestParseSegmentsMatchFile(t *testing.T) {
	data, err := os.ReadFile("../../testdata/frag_p_64x64.mp4")
	if err != nil {
		t.Fatal(err)
	}
	whole, err := OpenReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	init, segs := splitFMP4(data)
	if init == nil || len(segs) == 0 {
		t.Fatalf("split: init %d bytes, %d segments", len(init), len(segs))
	}
	r, err := OpenInit(init)
	if err != nil {
		t.Fatal(err)
	}
	if r.VideoTrack() == nil || r.VideoTrack().SampleCount() != 0 {
		t.Fatal("init segment should give a video track without samples")
	}
	var got []SegSample
	for _, s := range segs {
		ss, err := r.ParseSegment(s)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ss...)
	}
	vt := whole.VideoTrack()
	if len(got) != vt.SampleCount() {
		t.Fatalf("%d samples from segments, %d from the file", len(got), vt.SampleCount())
	}
	for i, s := range got {
		want, _ := whole.ReadSample(vt, i)
		if s.PTS != whole.PTS(vt, i) || s.Sync != whole.IsSync(vt, i) || !bytes.Equal(s.Data, want) {
			t.Fatalf("sample %d: pts %v/%v sync %v/%v bytes equal %v", i, s.PTS, whole.PTS(vt, i), s.Sync, whole.IsSync(vt, i), bytes.Equal(s.Data, want))
		}
	}
}
