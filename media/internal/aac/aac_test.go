package aac

import "testing"

func TestParseASC_LC44100Stereo(t *testing.T) {
	// 0x12 0x10 = AAC-LC, sampling index 4 (44100), channel config 2.
	c, err := ParseASC([]byte{0x12, 0x10})
	if err != nil {
		t.Fatal(err)
	}
	if c.ObjectType != ObjectLC {
		t.Errorf("ObjectType = %d, want %d", c.ObjectType, ObjectLC)
	}
	if c.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", c.SampleRate)
	}
	if c.Channels != 2 {
		t.Errorf("Channels = %d, want 2", c.Channels)
	}
	if c.FrameLength != 1024 {
		t.Errorf("FrameLength = %d, want 1024", c.FrameLength)
	}
}

func TestParseADTS(t *testing.T) {
	// FF F1 50 80 0C 9F FC: LC, index 4 (44100), stereo, frame length 100.
	h, hl, err := ParseADTS([]byte{0xFF, 0xF1, 0x50, 0x80, 0x0C, 0x9F, 0xFC})
	if err != nil {
		t.Fatal(err)
	}
	if hl != 7 {
		t.Errorf("header length = %d, want 7", hl)
	}
	if h.ObjectType != ObjectLC {
		t.Errorf("ObjectType = %d, want %d", h.ObjectType, ObjectLC)
	}
	if h.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", h.SampleRate)
	}
	if h.Channels != 2 {
		t.Errorf("Channels = %d, want 2", h.Channels)
	}
	if h.FrameLength != 100 {
		t.Errorf("FrameLength = %d, want 100", h.FrameLength)
	}
}

func TestRawBlocks(t *testing.T) {
	// Two ADTS frames, each length 9 (7-byte header + 2-byte payload).
	f1 := []byte{0xFF, 0xF1, 0x50, 0x80, 0x01, 0x1F, 0xFC, 0xAA, 0xBB}
	f2 := []byte{0xFF, 0xF1, 0x50, 0x80, 0x01, 0x1F, 0xFC, 0xCC, 0xDD}
	// Patch the frame length to 9: bits [3]&0x03<<11 | [4]<<3 | [5]>>5 == 9.
	for _, f := range [][]byte{f1, f2} {
		f[4] = 0x01 // (9>>3)=1
		f[5] = (9 & 7) << 5
	}
	blocks, err := RawBlocks(append(append([]byte{}, f1...), f2...))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	if len(blocks[0]) != 2 || blocks[0][0] != 0xAA || blocks[1][1] != 0xDD {
		t.Errorf("blocks = %v", blocks)
	}
}
