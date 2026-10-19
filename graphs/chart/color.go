package chart

import (
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// CSSColor is the JSON-friendly color representation used by the
// declarative Option API. Supported forms are #RGB, #RGBA, #RRGGBB,
// #RRGGBBAA, rgb(r,g,b), rgba(r,g,b,a) and "transparent".
type CSSColor string

// ParseColor parses a supported CSS color string. Invalid strings return
// the zero color and false.
func ParseColor(value string) (qui.Color, bool) {
	s := strings.ToLower(strings.TrimSpace(value))
	if s == "" {
		return qui.Color{}, false
	}
	if s == "transparent" {
		return qui.Color{A: 0}, true
	}
	if strings.HasPrefix(s, "#") {
		return parseHexColor(s[1:])
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		return parseRGBColor(s[4:len(s)-1], false)
	}
	if strings.HasPrefix(s, "rgba(") && strings.HasSuffix(s, ")") {
		return parseRGBColor(s[5:len(s)-1], true)
	}
	return qui.Color{}, false
}

func parseHexColor(s string) (qui.Color, bool) {
	expand := func(v byte) byte {
		n, err := strconv.ParseUint(string([]byte{v}), 16, 8)
		if err != nil {
			return 0
		}
		return byte(n*16 + n)
	}
	switch len(s) {
	case 3, 4:
		parts := make([]byte, len(s))
		for i := range s {
			if _, err := strconv.ParseUint(string(s[i]), 16, 8); err != nil {
				return qui.Color{}, false
			}
			parts[i] = expand(s[i])
		}
		a := byte(255)
		if len(parts) == 4 {
			a = parts[3]
		}
		return rgba(parts[0], parts[1], parts[2], a), true
	case 6, 8:
		parts := make([]byte, len(s)/2)
		for i := range parts {
			n, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
			if err != nil {
				return qui.Color{}, false
			}
			parts[i] = byte(n)
		}
		a := byte(255)
		if len(parts) == 4 {
			a = parts[3]
		}
		return rgba(parts[0], parts[1], parts[2], a), true
	default:
		return qui.Color{}, false
	}
}

func parseRGBColor(body string, hasAlpha bool) (qui.Color, bool) {
	parts := strings.Split(body, ",")
	want := 3
	if hasAlpha {
		want = 4
	}
	if len(parts) != want {
		return qui.Color{}, false
	}
	r, ok := parseByteChannel(parts[0])
	if !ok {
		return qui.Color{}, false
	}
	g, ok := parseByteChannel(parts[1])
	if !ok {
		return qui.Color{}, false
	}
	b, ok := parseByteChannel(parts[2])
	if !ok {
		return qui.Color{}, false
	}
	a := byte(255)
	if hasAlpha {
		alpha, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if err != nil || alpha < 0 || alpha > 1 {
			return qui.Color{}, false
		}
		a = byte(alpha*255 + 0.5)
	}
	return rgba(r, g, b, a), true
}

func parseByteChannel(value string) (byte, bool) {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || n < 0 || n > 255 {
		return 0, false
	}
	return byte(n + 0.5), true
}

func rgba(r, g, b, a byte) qui.Color {
	return qui.Color{
		R: float32(r) / 255,
		G: float32(g) / 255,
		B: float32(b) / 255,
		A: float32(a) / 255,
	}
}
