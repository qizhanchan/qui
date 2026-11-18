//go:build cgo

package media

import (
	"os"
	"testing"
	"time"
)

// A WebM file appended in arbitrary chunks plays like the file: the
// initialization segment is found in the byte stream, and the decoder
// waits for clusters as they arrive.
func TestMSEWebMChunksMatchFFmpeg(t *testing.T) {
	for _, c := range containerClips {
		t.Run(c.codec, func(t *testing.T) {
			data, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatal(err)
			}
			b := NewMSEBuffer()
			const chunk = 700
			var i int
			for !b.IsWebM() && i < len(data) {
				if _, err := b.AppendWebM(data[i:min(i+chunk, len(data))], 0); err != nil {
					t.Fatal(err)
				}
				i += chunk
			}
			if !b.HasVideo() {
				t.Fatal("init segment not recognized")
			}
			src, err := OpenMSE(b)
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for ; i < len(data); i += chunk {
					time.Sleep(time.Millisecond)
					if _, err := b.AppendWebM(data[i:min(i+chunk, len(data))], 0); err != nil {
						t.Error(err)
					}
				}
				b.EndOfStream()
			}()
			checkMatchesFFmpeg(t, src, c.codec, c.hash)
		})
	}
}

func TestMSEWebMBuffered(t *testing.T) {
	data, err := os.ReadFile("testdata/av1_opus_160x90.webm")
	if err != nil {
		t.Fatal(err)
	}
	b := NewMSEBuffer()
	if got, err := b.AppendWebM(data, 0); err != nil || !got {
		t.Fatalf("append: init %v err %v", got, err)
	}
	b.EndOfStream()
	r := b.Buffered()
	if len(r) != 1 || r[0].Start > 0.01 || r[0].End < 0.99 {
		t.Fatalf("buffered %v", r)
	}
	if !b.HasAudio() || b.audio.count() < 40 {
		t.Fatalf("audio frames %d", b.audio.count())
	}
}
