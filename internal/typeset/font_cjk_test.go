package typeset

import "testing"

func TestPublicFontName(t *testing.T) {
	for in, want := range map[string]string{
		".PingFangUITextSC-Regular":    "PingFangSC-Regular",
		".PingFangUIDisplayTC-Regular": "PingFangTC-Regular",
		"HiraginoSans-W3":              "HiraginoSans-W3",
		".SFNS-Regular":                "SFNS-Regular",
	} {
		if got := publicFontName(in); got != want {
			t.Errorf("publicFontName(%q) = %q, want %q", in, got, want)
		}
	}
}
