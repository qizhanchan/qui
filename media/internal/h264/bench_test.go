package h264

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkDecodeVectors decodes every committed Annex-B vector. It runs
// offline (no native library) and is the reproducible throughput baseline
// behind the "Performance" section of docs/media-foundation.md; set
// H264_BENCH_CLIP to also time a specific large clip (e.g. 1080p).
func BenchmarkDecodeVectors(b *testing.B) {
	files, _ := filepath.Glob("testdata/*.264")
	files = append(files, globH264()...)
	if len(files) == 0 {
		b.Skip("no vectors")
	}
	streams := make([][]byte, 0, len(files))
	for _, f := range files {
		d, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}
		streams = append(streams, d)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range streams {
			if _, err := DecodeAnnexB(s); err != nil {
				b.Fatalf("decode: %v", err)
			}
		}
	}
}

func globH264() []string {
	f, _ := filepath.Glob("testdata/*.h264")
	return f
}

// BenchmarkDecode1080p times a single large clip given by H264_BENCH_CLIP.
func BenchmarkDecode1080p(b *testing.B) {
	path := os.Getenv("H264_BENCH_CLIP")
	if path == "" {
		b.Skip("set H264_BENCH_CLIP")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	frames, err := DecodeAnnexB(data)
	if err != nil {
		b.Fatalf("decode: %v", err)
	}
	b.Logf("decoded %d frames %dx%d", len(frames), frames[0].Y.W, frames[0].Y.H)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeAnnexB(data); err != nil {
			b.Fatal(err)
		}
	}
}
