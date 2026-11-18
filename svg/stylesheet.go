package svg

import (
	"encoding/xml"
	"sort"
	"strings"
)

// An SVG's own <style> sheet, reduced to what logos and icons use: rules
// whose selectors are a type, a class, an id or a type with classes
// (`.cls-1`, `#a`, `path`, `path.st0`), in comma lists. Matching
// declarations reach an element as a style attribute placed before its
// own, so they beat presentation attributes and lose to style="…" —
// the CSS cascade for these three sources.

type styleRule struct {
	tag     string
	id      string
	classes []string
	spec    int // specificity: ids×100 + classes×10 + type
	order   int
	decls   string
}

func (r styleRule) matches(tag, id string, classes []string) bool {
	if r.tag != "" && r.tag != "*" && r.tag != tag {
		return false
	}
	if r.id != "" && r.id != id {
		return false
	}
	for _, c := range r.classes {
		found := false
		for _, have := range classes {
			if have == c {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// collectStyleRules reads every <style> element's text from dec.
func collectStyleRules(dec *xml.Decoder) []styleRule {
	var rules []styleRule
	inStyle := 0
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "style" {
				inStyle++
			}
		case xml.EndElement:
			if t.Name.Local == "style" && inStyle > 0 {
				inStyle--
				rules = append(rules, parseStyleSheet(text.String(), len(rules))...)
				text.Reset()
			}
		case xml.CharData:
			if inStyle > 0 {
				text.Write(t)
			}
		}
	}
	return rules
}

// parseStyleSheet parses `sel, sel { decls }` rules; at-rules and
// selectors it cannot match (combinators, pseudo-classes) are skipped.
func parseStyleSheet(css string, order int) []styleRule {
	for {
		i := strings.Index(css, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(css[i+2:], "*/")
		if j < 0 {
			css = css[:i]
			break
		}
		css = css[:i] + css[i+2+j+2:]
	}
	var out []styleRule
	for {
		open := strings.IndexByte(css, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(css[open:], '}')
		if end < 0 {
			break
		}
		sels, decls := strings.TrimSpace(css[:open]), css[open+1:open+end]
		css = css[open+end+1:]
		if strings.HasPrefix(sels, "@") {
			continue
		}
		for _, sel := range strings.Split(sels, ",") {
			if r, ok := parseSimpleSelector(strings.TrimSpace(sel)); ok {
				r.decls, r.order = decls, order+len(out)
				out = append(out, r)
			}
		}
	}
	return out
}

func parseSimpleSelector(sel string) (styleRule, bool) {
	if sel == "" || strings.ContainsAny(sel, " >+~:[") {
		return styleRule{}, false
	}
	var r styleRule
	cur, kind := "", byte(0)
	flush := func() bool {
		switch {
		case kind == 0 && cur != "":
			r.tag = cur
			if cur != "*" {
				r.spec++
			}
		case kind == '.' && cur != "":
			r.classes = append(r.classes, cur)
			r.spec += 10
		case kind == '#' && cur != "":
			r.id = cur
			r.spec += 100
		case kind != 0:
			return false
		}
		return true
	}
	for i := 0; i < len(sel); i++ {
		c := sel[i]
		if c == '.' || c == '#' {
			if !flush() {
				return styleRule{}, false
			}
			cur, kind = "", c
			continue
		}
		cur += string(c)
	}
	if !flush() {
		return styleRule{}, false
	}
	return r, true
}

// styleInjector is an xml.TokenReader adding each element's matching
// sheet declarations as a style attribute ahead of its own attributes.
type styleInjector struct {
	in    *xml.Decoder
	rules []styleRule
}

func (s *styleInjector) Token() (xml.Token, error) {
	tok, err := s.in.Token()
	if err != nil {
		return tok, err
	}
	start, ok := tok.(xml.StartElement)
	if !ok {
		return tok, nil
	}
	var id string
	var classes []string
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "id":
			id = a.Value
		case "class":
			classes = strings.Fields(a.Value)
		}
	}
	var hits []styleRule
	for _, r := range s.rules {
		if r.matches(start.Name.Local, id, classes) {
			hits = append(hits, r)
		}
	}
	if len(hits) == 0 {
		return tok, nil
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].spec != hits[j].spec {
			return hits[i].spec < hits[j].spec
		}
		return hits[i].order < hits[j].order
	})
	var b strings.Builder
	for _, h := range hits {
		b.WriteString(h.decls)
		b.WriteByte(';')
	}
	attrs := make([]xml.Attr, 0, len(start.Attr)+1)
	attrs = append(attrs, xml.Attr{Name: xml.Name{Local: "style"}, Value: b.String()})
	attrs = append(attrs, start.Attr...)
	start.Attr = attrs
	return start.Copy(), nil
}
