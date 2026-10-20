package diagram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParityFixturesDecodeRenderAndRoundTrip(t *testing.T) {
	paths, err := filepath.Glob("testdata/parity/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 12 {
		t.Fatalf("parity fixtures = %d, want 12", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Width  int    `json:"width"`
				Height int    `json:"height"`
				Drawio string `json:"drawio"`
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			doc, err := DecodeDrawio([]byte(fixture.Drawio))
			if err != nil {
				t.Fatal(err)
			}
			img, err := RenderImage(doc, RenderOptions{Width: fixture.Width, Height: fixture.Height, Fit: true})
			if err != nil {
				t.Fatal(err)
			}
			nonWhite := 0
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					if r != 65535 || g != 65535 || b != 65535 {
						nonWhite++
					}
				}
			}
			if nonWhite == 0 {
				t.Fatal("fixture rendered a blank page")
			}
			exported, err := EncodeDrawio(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeDrawio(exported); err != nil {
				t.Fatalf("round trip: %v", err)
			}
		})
	}
}
