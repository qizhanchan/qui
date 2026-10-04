package svg

import (
	"fmt"
	"strconv"
)

// ParsePath turns an SVG "d" attribute string into a *Path with the
// commands normalised to their absolute form. Lowercase / relative
// variants in the input collapse into absolute commands during this
// pass so downstream code (renderer, serializer, builders that walk
// Commands) only ever sees absolute coordinates.
//
// The grammar follows SVG 1.1 §8.3 — commands M/L/H/V/C/S/Q/T/A/Z
// with both cases. Implicit lineto after M, reflected control points
// for S/T, and the A flag-pair separator-less form are all handled.
func ParsePath(d string) (*Path, error) {
	p := &Path{}
	tk := &pathTokenizer{s: d}

	var cmd byte
	var rel bool
	var curX, curY float32           // current point
	var startX, startY float32       // start of current subpath
	var lastCtrlX, lastCtrlY float32 // for S/T reflection
	hadLastCubic := false
	hadLastQuad := false

	readPair := func(rx, ry float32) (float32, float32, error) {
		x, err := tk.number()
		if err != nil {
			return 0, 0, err
		}
		y, err := tk.number()
		if err != nil {
			return 0, 0, err
		}
		if rel {
			x += rx
			y += ry
		}
		return x, y, nil
	}

	for {
		c, ok := tk.peekCommandOrNumber()
		if !ok {
			break
		}
		if c.kind == tokCommand {
			cmd = c.b
			tk.advance()
			rel = cmd >= 'a' && cmd <= 'z'
		} else if cmd == 0 {
			return nil, fmt.Errorf("svg: path: leading number with no command")
		} else {
			// implicit repeat of last command; M repeats as L (per spec)
			if cmd == 'M' {
				cmd = 'L'
			} else if cmd == 'm' {
				cmd = 'l'
			}
		}

		switch cmd {
		case 'M', 'm':
			x, y, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path M: %w", err)
			}
			curX, curY = x, y
			startX, startY = x, y
			p.Commands = append(p.Commands, PathCommand{Op: OpMoveTo, Args: [7]float32{x, y}})
			hadLastCubic, hadLastQuad = false, false
		case 'L', 'l':
			x, y, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path L: %w", err)
			}
			curX, curY = x, y
			p.Commands = append(p.Commands, PathCommand{Op: OpLineTo, Args: [7]float32{x, y}})
			hadLastCubic, hadLastQuad = false, false
		case 'H', 'h':
			x, err := tk.number()
			if err != nil {
				return nil, fmt.Errorf("svg: path H: %w", err)
			}
			if rel {
				x += curX
			}
			curX = x
			p.Commands = append(p.Commands, PathCommand{Op: OpLineTo, Args: [7]float32{x, curY}})
			hadLastCubic, hadLastQuad = false, false
		case 'V', 'v':
			y, err := tk.number()
			if err != nil {
				return nil, fmt.Errorf("svg: path V: %w", err)
			}
			if rel {
				y += curY
			}
			curY = y
			p.Commands = append(p.Commands, PathCommand{Op: OpLineTo, Args: [7]float32{curX, y}})
			hadLastCubic, hadLastQuad = false, false
		case 'C', 'c':
			x1, y1, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path C: %w", err)
			}
			x2, y2, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path C: %w", err)
			}
			x, y, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path C: %w", err)
			}
			p.Commands = append(p.Commands, PathCommand{
				Op:   OpCurveTo,
				Args: [7]float32{x1, y1, x2, y2, x, y},
			})
			lastCtrlX, lastCtrlY = x2, y2
			curX, curY = x, y
			hadLastCubic = true
			hadLastQuad = false
		case 'S', 's':
			// S reflects the previous cubic's second control through
			// the current point; if previous wasn't a cubic, the
			// reflected point IS the current point.
			var x1, y1 float32
			if hadLastCubic {
				x1, y1 = 2*curX-lastCtrlX, 2*curY-lastCtrlY
			} else {
				x1, y1 = curX, curY
			}
			x2, y2, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path S: %w", err)
			}
			x, y, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path S: %w", err)
			}
			p.Commands = append(p.Commands, PathCommand{
				Op:   OpCurveTo,
				Args: [7]float32{x1, y1, x2, y2, x, y},
			})
			lastCtrlX, lastCtrlY = x2, y2
			curX, curY = x, y
			hadLastCubic = true
			hadLastQuad = false
		case 'Q', 'q':
			x1, y1, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path Q: %w", err)
			}
			x, y, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path Q: %w", err)
			}
			p.Commands = append(p.Commands, PathCommand{
				Op:   OpQuadTo,
				Args: [7]float32{x1, y1, x, y},
			})
			lastCtrlX, lastCtrlY = x1, y1
			curX, curY = x, y
			hadLastQuad = true
			hadLastCubic = false
		case 'T', 't':
			var x1, y1 float32
			if hadLastQuad {
				x1, y1 = 2*curX-lastCtrlX, 2*curY-lastCtrlY
			} else {
				x1, y1 = curX, curY
			}
			x, y, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path T: %w", err)
			}
			p.Commands = append(p.Commands, PathCommand{
				Op:   OpQuadTo,
				Args: [7]float32{x1, y1, x, y},
			})
			lastCtrlX, lastCtrlY = x1, y1
			curX, curY = x, y
			hadLastQuad = true
			hadLastCubic = false
		case 'A', 'a':
			rx, err := tk.number()
			if err != nil {
				return nil, fmt.Errorf("svg: path A: %w", err)
			}
			ry, err := tk.number()
			if err != nil {
				return nil, fmt.Errorf("svg: path A: %w", err)
			}
			rot, err := tk.number()
			if err != nil {
				return nil, fmt.Errorf("svg: path A: %w", err)
			}
			large, err := tk.flag()
			if err != nil {
				return nil, fmt.Errorf("svg: path A: %w", err)
			}
			sweep, err := tk.flag()
			if err != nil {
				return nil, fmt.Errorf("svg: path A: %w", err)
			}
			x, y, err := readPair(curX, curY)
			if err != nil {
				return nil, fmt.Errorf("svg: path A: %w", err)
			}
			la, sw := float32(0), float32(0)
			if large {
				la = 1
			}
			if sweep {
				sw = 1
			}
			p.Commands = append(p.Commands, PathCommand{
				Op:   OpArcTo,
				Args: [7]float32{rx, ry, rot, la, sw, x, y},
			})
			curX, curY = x, y
			hadLastCubic, hadLastQuad = false, false
		case 'Z', 'z':
			p.Commands = append(p.Commands, PathCommand{Op: OpClose})
			curX, curY = startX, startY
			hadLastCubic, hadLastQuad = false, false
		default:
			return nil, fmt.Errorf("svg: path: unknown command %q", cmd)
		}
	}
	return p, nil
}

// pathTokenizer splits a "d" string into commands, numbers, and the
// A-command flag tokens (single 0/1 digits, no decimal allowed).
type pathTokenizer struct {
	s string
	i int
}

type pathToken struct {
	kind tokKind
	b    byte
}

type tokKind uint8

const (
	tokNone tokKind = iota
	tokCommand
	tokNumber
)

func (t *pathTokenizer) skipSep() {
	for t.i < len(t.s) {
		c := t.s[t.i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
			t.i++
			continue
		}
		break
	}
}

// peekCommandOrNumber tells the parser what the next non-whitespace
// token looks like: a command letter (M/L/...) or a numeric value
// starting (digit / '-' / '+' / '.'). It does NOT consume the token.
func (t *pathTokenizer) peekCommandOrNumber() (pathToken, bool) {
	t.skipSep()
	if t.i >= len(t.s) {
		return pathToken{}, false
	}
	c := t.s[t.i]
	if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
		return pathToken{kind: tokCommand, b: c}, true
	}
	if isNumStart(c) {
		return pathToken{kind: tokNumber, b: c}, true
	}
	return pathToken{}, false
}

func (t *pathTokenizer) advance() { t.i++ }

// number reads one float32 token. Negative numbers may be glued to a
// preceding digit ("0-1" → 0, then -1), so we look for explicit signs
// inside the run, not just at the boundary.
func (t *pathTokenizer) number() (float32, error) {
	t.skipSep()
	if t.i >= len(t.s) {
		return 0, fmt.Errorf("unexpected end of path")
	}
	start := t.i
	c := t.s[t.i]
	if c == '+' || c == '-' {
		t.i++
	}
	// integer part
	sawDigit := false
	for t.i < len(t.s) && isDigit(t.s[t.i]) {
		t.i++
		sawDigit = true
	}
	// fractional part
	if t.i < len(t.s) && t.s[t.i] == '.' {
		t.i++
		for t.i < len(t.s) && isDigit(t.s[t.i]) {
			t.i++
			sawDigit = true
		}
	}
	// exponent
	if t.i < len(t.s) && (t.s[t.i] == 'e' || t.s[t.i] == 'E') {
		t.i++
		if t.i < len(t.s) && (t.s[t.i] == '+' || t.s[t.i] == '-') {
			t.i++
		}
		for t.i < len(t.s) && isDigit(t.s[t.i]) {
			t.i++
		}
	}
	if !sawDigit {
		return 0, fmt.Errorf("expected number at %q", t.s[start:])
	}
	v, err := strconv.ParseFloat(t.s[start:t.i], 32)
	if err != nil {
		return 0, fmt.Errorf("bad number %q: %w", t.s[start:t.i], err)
	}
	return float32(v), nil
}

// flag reads a single 0/1 character — SVG's A-command large-arc and
// sweep flags are encoded that way. Important quirk: flags don't need
// a separator after them ("A 25 25 0 011 25 25" is valid), so this
// reads exactly one character.
func (t *pathTokenizer) flag() (bool, error) {
	t.skipSep()
	if t.i >= len(t.s) {
		return false, fmt.Errorf("expected flag")
	}
	c := t.s[t.i]
	t.i++
	switch c {
	case '0':
		return false, nil
	case '1':
		return true, nil
	}
	return false, fmt.Errorf("expected 0 or 1 flag, got %q", c)
}

func isDigit(c byte) bool    { return c >= '0' && c <= '9' }
func isNumStart(c byte) bool { return isDigit(c) || c == '-' || c == '+' || c == '.' }
