package av1

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// testSource returns n source frames: the decoded frames of a committed
// vector (320x180).
func testSource(t testing.TB, name string, n int) []*image.YCbCr {
	data, err := os.ReadFile("testdata/" + name + ".ivf")
	if err != nil {
		t.Fatal(err)
	}
	tus, _, _, err := ReadIVF(data)
	if err != nil {
		t.Fatal(err)
	}
	d := New()
	var out []*image.YCbCr
	for _, tu := range tus {
		if err := d.Decode(tu); err != nil {
			t.Fatal(err)
		}
		for f := d.Next(); f != nil; f = d.Next() {
			img := image.NewYCbCr(image.Rect(0, 0, f.Width, f.Height), image.YCbCrSubsampleRatio420)
			for y := 0; y < f.Height; y++ {
				copy(img.Y[y*img.YStride:], f.Y.Pix[y*f.Y.Stride:y*f.Y.Stride+f.Width])
			}
			for y := 0; y < (f.Height+1)/2; y++ {
				copy(img.Cb[y*img.CStride:], f.Cb.Pix[y*f.Cb.Stride:y*f.Cb.Stride+f.Cb.W])
				copy(img.Cr[y*img.CStride:], f.Cr.Pix[y*f.Cr.Stride:y*f.Cr.Stride+f.Cr.W])
			}
			out = append(out, img)
			if len(out) == n {
				return out
			}
		}
	}
	return out
}

// framePlanes returns a frame's visible planes as I420 bytes.
func framePlanes(f *Frame) []byte {
	var b []byte
	for _, p := range []*struct {
		pix       []byte
		stride, w int
		h         int
	}{{f.Y.Pix, f.Y.Stride, f.Y.W, f.Y.H}, {f.Cb.Pix, f.Cb.Stride, f.Cb.W, f.Cb.H}, {f.Cr.Pix, f.Cr.Stride, f.Cr.W, f.Cr.H}} {
		for y := 0; y < p.h; y++ {
			b = append(b, p.pix[y*p.stride:y*p.stride+p.w]...)
		}
	}
	return b
}

func imagePlanes(img *image.YCbCr) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	var b []byte
	for y := 0; y < h; y++ {
		b = append(b, img.Y[y*img.YStride:y*img.YStride+w]...)
	}
	for _, p := range [][]byte{img.Cb, img.Cr} {
		for y := 0; y < h/2; y++ {
			b = append(b, p[y*img.CStride:y*img.CStride+w/2]...)
		}
	}
	return b
}

func psnr(a, b []byte) float64 {
	var se float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		se += d * d
	}
	if se == 0 {
		return 99
	}
	return 10 * math.Log10(255*255*float64(len(a))/se)
}

// writeIVF writes packets as an IVF file (with temporal delimiters).
func writeIVF(path string, w, h int, pkts []Packet) error {
	var buf bytes.Buffer
	hdr := make([]byte, 32)
	copy(hdr, "DKIF")
	binary.LittleEndian.PutUint16(hdr[6:], 32)
	copy(hdr[8:], "AV01")
	binary.LittleEndian.PutUint16(hdr[12:], uint16(w))
	binary.LittleEndian.PutUint16(hdr[14:], uint16(h))
	binary.LittleEndian.PutUint32(hdr[16:], 30)
	binary.LittleEndian.PutUint32(hdr[20:], 1)
	binary.LittleEndian.PutUint32(hdr[24:], uint32(len(pkts)))
	buf.Write(hdr)
	for i, p := range pkts {
		data := append([]byte{obuTemporalDelimiter<<3 | 2, 0}, p.Data...)
		fh := make([]byte, 12)
		binary.LittleEndian.PutUint32(fh, uint32(len(data)))
		binary.LittleEndian.PutUint64(fh[4:], uint64(i))
		buf.Write(fh)
		buf.Write(data)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// encodeCheck encodes frames with cfg and checks that our decoder and
// (when installed) the dav1d CLI reproduce the encoder's reconstruction.
func encodeCheck(t *testing.T, cfg EncoderConfig, src []*image.YCbCr) (pkts []Packet, bytesTotal int, quality float64) {
	t.Helper()
	e, err := NewEncoder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.keepRecon = true
	for _, img := range src {
		p, err := e.Encode(img)
		if err != nil {
			t.Fatal(err)
		}
		pkts = append(pkts, p)
		bytesTotal += len(p.Data)
	}
	if len(e.recon) != len(src) {
		t.Fatalf("%d reconstructed frames for %d inputs", len(e.recon), len(src))
	}
	// Our decoder, fresh.
	d := New()
	n := 0
	var ps float64
	for i, p := range pkts {
		if err := d.Decode(p.Data); err != nil {
			t.Fatalf("decode packet %d: %v", i, err)
		}
		for f := d.Next(); f != nil; f = d.Next() {
			got, want := framePlanes(f), framePlanes(e.recon[n])
			if !bytes.Equal(got, want) {
				t.Fatalf("frame %d: decoder output differs from the encoder's reconstruction", n)
			}
			ps += psnr(imagePlanes(src[n]), want)
			n++
		}
	}
	if n != len(src) {
		t.Fatalf("decoded %d frames, want %d", n, len(src))
	}
	quality = ps / float64(n)
	// libdav1d.
	if _, err := exec.LookPath("dav1d"); err == nil {
		dir := t.TempDir()
		ivf := filepath.Join(dir, "e.ivf")
		if err := writeIVF(ivf, cfg.Width, cfg.Height, pkts); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "o.yuv")
		if msg, err := exec.Command("dav1d", "-q", "-i", ivf, "-o", out).CombinedOutput(); err != nil {
			t.Fatalf("dav1d: %v %s", err, msg)
		}
		got, _ := os.ReadFile(out)
		var want []byte
		for _, f := range e.recon {
			want = append(want, framePlanes(f)...)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("libdav1d output (%d bytes) differs from the reconstruction (%d bytes)", len(got), len(want))
		}
	}
	return
}

func TestEncodeIntra(t *testing.T) {
	src := testSource(t, "ra_p4", 3)
	for _, q := range []int{40, 128, 220} {
		t.Run(fmt.Sprint(q), func(t *testing.T) {
			_, n, ps := encodeCheck(t, EncoderConfig{Width: 320, Height: 180, QIndex: q, KeyInt: 1, Preset: 2}, src)
			t.Logf("qindex %d: %d bytes/frame, PSNR %.2f dB", q, n/len(src), ps)
		})
	}
}

func TestEncodeInter(t *testing.T) {
	src := testSource(t, "ra_p4", 6)
	for _, q := range []int{60, 140, 220} {
		t.Run(fmt.Sprint(q), func(t *testing.T) {
			pkts, n, ps := encodeCheck(t, EncoderConfig{Width: 320, Height: 180, QIndex: q, Preset: 2}, src)
			sizes := []int{}
			for _, p := range pkts {
				sizes = append(sizes, len(p.Data))
			}
			t.Logf("qindex %d: %d bytes/frame %v, PSNR %.2f dB", q, n/len(src), sizes, ps)
		})
	}
}
