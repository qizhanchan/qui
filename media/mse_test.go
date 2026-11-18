//go:build cgo

package media

import (
	"encoding/binary"
	"os"
	"testing"
	"time"
)

// splitFMP4 cuts a fragmented MP4 into its initialization segment and its
// media segments (moof + mdat), as an MSE player receives it.
func splitFMP4(b []byte) (init []byte, segs [][]byte) {
	pos, segStart := 0, -1
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

// fragClip is bframes_aac_160x90.mp4 remuxed by FFmpeg into 5 fragments
// (-movflags empty_moov+default_base_moof -frag_duration 250000): the same
// bitstream, so it decodes to the same FFmpeg hash.
const fragClip = "testdata/bframes_aac_160x90_frag.mp4"

func loadFrag(t *testing.T) (init []byte, segs [][]byte) {
	t.Helper()
	b, err := os.ReadFile(fragClip)
	if err != nil {
		t.Fatal(err)
	}
	init, segs = splitFMP4(b)
	if init == nil || len(segs) < 3 {
		t.Fatalf("split: %d init bytes, %d segments", len(init), len(segs))
	}
	return init, segs
}

// The decoder waits where the buffer ends and resumes as segments arrive;
// the result matches FFmpeg's decode of the file.
func TestMSEStreamedSegmentsMatchFFmpeg(t *testing.T) {
	init, segs := loadFrag(t)
	b := NewMSEBuffer()
	if err := b.AppendMedia(segs[0], 0); err != ErrMSEInit {
		t.Fatalf("media before init: %v", err)
	}
	if err := b.AppendInit(init); err != nil {
		t.Fatal(err)
	}
	if !b.HasVideo() || !b.HasAudio() {
		t.Fatal("init should declare video and audio")
	}
	// The remux has no edit list, so the B-frame delay shows: shift it
	// away as a page would with timestampOffset.
	const lead = -time.Second / 12
	if err := b.AppendMedia(segs[0], lead); err != nil {
		t.Fatal(err)
	}
	src, err := OpenMSE(b)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for _, s := range segs[1:] {
			time.Sleep(20 * time.Millisecond)
			if err := b.AppendMedia(s, lead); err != nil {
				t.Error(err)
			}
		}
		b.EndOfStream()
	}()
	checkMatchesFFmpeg(t, src, "h264", e2eClips[0].hash)
}

func TestMSEBufferedAndRemove(t *testing.T) {
	init, segs := loadFrag(t)
	b := NewMSEBuffer()
	if err := b.AppendInit(init); err != nil {
		t.Fatal(err)
	}
	// Append out of order: the last segments first, then the first.
	for _, s := range segs[2:] {
		if err := b.AppendMedia(s, 0); err != nil {
			t.Fatal(err)
		}
	}
	r := b.Buffered()
	if len(r) != 1 || r[0].Start < 0.4 {
		t.Fatalf("buffered after tail = %+v", r)
	}
	if err := b.AppendMedia(segs[0], 0); err != nil {
		t.Fatal(err)
	}
	if r := b.Buffered(); len(r) != 2 || r[0].Start > 0.1 {
		t.Fatalf("buffered with a gap = %+v", r)
	}
	if err := b.AppendMedia(segs[1], 0); err != nil {
		t.Fatal(err)
	}
	r = b.Buffered()
	if len(r) != 1 || r[0].Start > 0.1 || r[0].End < 0.95 {
		t.Fatalf("buffered whole = %+v", r)
	}
	// Re-appending a segment replaces, not duplicates.
	n := b.video.count()
	if err := b.AppendMedia(segs[1], 0); err != nil {
		t.Fatal(err)
	}
	if b.video.count() != n {
		t.Fatalf("re-append changed frame count %d → %d", n, b.video.count())
	}
	g := b.video.gen()
	// Removing the tail keeps the head.
	b.Remove(500*time.Millisecond, 2*time.Second)
	if b.video.gen() == g {
		t.Error("Remove should bump the generation")
	}
	r = b.Buffered()
	if len(r) != 1 || r[0].End > 0.6 || r[0].End < 0.4 {
		t.Fatalf("buffered after tail remove = %+v", r)
	}
	// The clip's only keyframe is the first frame, so removing the head
	// drops everything that depends on it (to the next keyframe: none).
	b.Remove(0, 100*time.Millisecond)
	if r := b.Buffered(); len(r) != 0 {
		t.Fatalf("buffered after head remove = %+v", r)
	}
}

func TestMSETimestampOffsetAndDuration(t *testing.T) {
	init, segs := loadFrag(t)
	b := NewMSEBuffer()
	if err := b.AppendInit(init); err != nil {
		t.Fatal(err)
	}
	if err := b.AppendMedia(segs[0], 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if r := b.Buffered(); len(r) != 1 || r[0].Start < 9.99 {
		t.Fatalf("offset buffered = %+v", r)
	}
	src, err := OpenMSE(b)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	b.SetDuration(42 * time.Second)
	if d := src.(liveDuration).CurrentDuration(); d != 42*time.Second {
		t.Fatalf("CurrentDuration = %v", d)
	}
	if _, ok := src.(BufferedSource); !ok {
		t.Fatal("MSE source should report buffered ranges")
	}
}
