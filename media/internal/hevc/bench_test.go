package hevc

import (
	"os"
	"testing"
	"time"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// BenchmarkDecodeFile decodes HEVC_BENCH (an Annex-B file) per iteration
// and reports ms per frame.
func BenchmarkDecodeFile(b *testing.B) {
	f := os.Getenv("HEVC_BENCH")
	if f == "" {
		b.Skip("HEVC_BENCH not set")
	}
	data, err := os.ReadFile(f)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	frames := 0
	start := time.Now()
	for i := 0; i < b.N; i++ {
		d := New()
		for _, nal := range SplitAnnexB(data) {
			_ = d.DecodeNAL(nal)
			for fr := d.Next(); fr != nil; fr = d.Next() {
				h264.Recycle(fr)
				frames++
			}
		}
		d.Flush()
		for fr := d.Next(); fr != nil; fr = d.Next() {
			h264.Recycle(fr)
			frames++
		}
	}
	if frames > 0 {
		b.ReportMetric(float64(time.Since(start).Microseconds())/1000/float64(frames), "ms/frame")
	}
}
