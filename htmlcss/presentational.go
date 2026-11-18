package htmlcss

import (
	"strconv"
	"strings"
)

// Presentational hints (HTML § 15 "Rendering"): legacy attributes such as
// <img width>, <td bgcolor> or <table border> map to CSS declarations that
// sit just above the UA sheet and below every author rule — exactly where
// mergedDecls emits them (tier 0, after the UA declarations).

// dimensionTags take width/height attributes as CSS lengths.
var dimensionTags = map[string]bool{
	"img": true, "video": true, // not canvas: its width/height are only its intrinsic size
	"iframe": true, "embed": true,
	"object": true, "table": true, "td": true, "th": true, "col": true, "input": true,
	"svg": true,
}

func presentationalHints(n *Node) []Declaration {
	if n == nil || n.Type != ElementNode {
		return nil
	}
	var out []Declaration
	add := func(p, v string) { out = append(out, Declaration{Property: p, Value: v}) }

	if dimensionTags[n.Tag] {
		for _, k := range [...]string{"width", "height"} {
			if n.Tag == "input" && n.Attrs["type"] != "image" {
				break
			}
			if n.Tag == "col" && k == "height" {
				continue
			}
			if v, ok := n.Attrs[k]; ok {
				if n.Tag == "svg" || n.Tag == "img" && n.Attrs["data-svg-inline"] != "" {
					// SVG width/height are presentation attributes: any
					// CSS length (1.33em, 100%), a bare number is px. An
					// <img> a host put in place of an inline <svg>
					// (data-svg-inline) keeps that reading.
					if l := svgLength(v); l != "" {
						add(k, l)
					}
				} else if l := htmlLength(v, true); l != "" {
					add(k, l)
				}
			}
		}
	}
	switch n.Tag {
	case "table", "td", "th", "tr", "body", "tbody", "thead", "tfoot":
		if v, ok := n.Attrs["bgcolor"]; ok && strings.TrimSpace(v) != "" {
			add("background-color", strings.TrimSpace(v))
		}
	}
	switch n.Tag {
	case "td", "th", "tr", "tbody", "thead", "tfoot":
		if v, ok := n.Attrs["align"]; ok {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "left", "right":
				add("text-align", "-webkit-"+strings.ToLower(strings.TrimSpace(v)))
			case "center", "middle":
				add("text-align", "-webkit-center")
			}
		}
		if v, ok := n.Attrs["valign"]; ok {
			switch s := strings.ToLower(strings.TrimSpace(v)); s {
			case "top", "middle", "bottom", "baseline":
				add("vertical-align", s)
			}
		}
		if _, ok := n.Attrs["nowrap"]; ok && (n.Tag == "td" || n.Tag == "th") {
			add("white-space", "nowrap")
		}
	case "div", "p", "h1", "h2", "h3", "h4", "h5", "h6":
		if v, ok := n.Attrs["align"]; ok {
			switch s := strings.ToLower(strings.TrimSpace(v)); s {
			case "left", "right":
				add("text-align", "-webkit-"+s)
			case "justify":
				add("text-align", s)
			case "center", "middle":
				add("text-align", "-webkit-center")
			}
		}
	case "center":
		add("text-align", "-webkit-center")
	case "table":
		if v, ok := n.Attrs["cellspacing"]; ok {
			if l := htmlLength(v, false); l != "" {
				add("border-spacing", l)
			}
		}
		if v, ok := n.Attrs["border"]; ok {
			w := strings.TrimSpace(v)
			if w == "" {
				w = "1"
			}
			if px, err := strconv.Atoi(w); err == nil && px > 0 {
				add("border", strconv.Itoa(px)+"px outset gray")
			}
		}
		if v, ok := n.Attrs["align"]; ok {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "center":
				add("margin-left", "auto")
				add("margin-right", "auto")
			}
		}
	case "font":
		if v, ok := n.Attrs["color"]; ok && strings.TrimSpace(v) != "" {
			add("color", strings.TrimSpace(v))
		}
		if v, ok := n.Attrs["face"]; ok && strings.TrimSpace(v) != "" {
			add("font-family", v)
		}
		if v, ok := n.Attrs["size"]; ok {
			if s := fontSizeAttr(v); s != "" {
				add("font-size", s)
			}
		}
	case "body":
		if v, ok := n.Attrs["text"]; ok && strings.TrimSpace(v) != "" {
			add("color", strings.TrimSpace(v))
		}
	case "hr":
		if v, ok := n.Attrs["width"]; ok {
			if l := htmlLength(v, true); l != "" {
				add("width", l)
			}
		}
	}
	// <td> inside <table cellpadding> / <table border>.
	if (n.Tag == "td" || n.Tag == "th") && n.Parent != nil {
		if t := enclosingTable(n); t != nil {
			if v, ok := t.Attrs["cellpadding"]; ok {
				if l := htmlLength(v, false); l != "" {
					add("padding", l)
				}
			}
			if v, ok := t.Attrs["border"]; ok {
				if px, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && px > 0 {
					add("border", "1px inset gray")
				}
			}
		}
	}
	return out
}

func enclosingTable(n *Node) *Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Tag == "table" {
			return p
		}
	}
	return nil
}

// htmlLength parses an HTML dimension attribute ("120", "120px", "50%")
// into a CSS length; "" when invalid.
func htmlLength(v string, allowPct bool) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, "%") {
		if !allowPct {
			return ""
		}
		if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64); err == nil && f >= 0 {
			return strconv.FormatFloat(f, 'f', -1, 64) + "%"
		}
		return ""
	}
	s = strings.TrimSuffix(s, "px")
	// HTML's "rules for parsing dimension values": leading digits count.
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '.') {
		end++
	}
	if end == 0 {
		return ""
	}
	f, err := strconv.ParseFloat(s[:end], 64)
	if err != nil || f < 0 {
		return ""
	}
	return strconv.FormatFloat(f, 'f', -1, 64) + "px"
}

// fontSizeAttr maps <font size=1..7> (and +n / -n relative to 3).
func fontSizeAttr(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return ""
	}
	n, err := strconv.Atoi(strings.TrimLeft(s, "+-"))
	if err != nil {
		return ""
	}
	switch s[0] {
	case '+':
		n = 3 + n
	case '-':
		n = 3 - n
	}
	n = max(1, min(7, n))
	return [...]string{"", "10px", "13px", "16px", "18px", "24px", "32px", "48px"}[n]
}

// svgLength reads an SVG width / height attribute as a CSS length ("" for
// auto or an invalid value).
func svgLength(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" || s == "auto" {
		return ""
	}
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '.' || end == 0 && (s[end] == '+' || s[end] == '-')) {
		end++
	}
	f, err := strconv.ParseFloat(s[:end], 64)
	if err != nil || f < 0 {
		return ""
	}
	switch unit := s[end:]; unit {
	case "":
		return strconv.FormatFloat(f, 'f', -1, 64) + "px"
	case "px", "em", "rem", "ex", "ch", "%", "pt", "pc", "cm", "mm", "in", "vw", "vh", "vmin", "vmax":
		return strconv.FormatFloat(f, 'f', -1, 64) + unit
	}
	return ""
}
