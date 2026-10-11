package av1

import (
	"os"
	"testing"
)

// BenchmarkDecodeFile (opt-in: AV1_BENCH=file.ivf) decodes a whole IVF
// file per iteration and reports the time per frame.
func BenchmarkDecodeFile(b *testing.B) {
	path := os.Getenv("AV1_BENCH")
	if path == "" {
		b.Skip("set AV1_BENCH")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	tus, _, _, err := ReadIVF(data)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	frames := 0
	for i := 0; i < b.N; i++ {
		d := New()
		for _, tu := range tus {
			if err := d.DecodeTemporalUnit(tu); err != nil {
				b.Fatal(err)
			}
			frames += len(d.out)
			d.out = d.out[:0]
		}
	}
	b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(frames), "ms/frame")
}
