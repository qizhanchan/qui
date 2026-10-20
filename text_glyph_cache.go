package qui

// Glyph coverage-mask cache.
//
// Shaped text used to be drawn by re-interpreting every glyph's outline
// into one Path per line, every frame, and filling that Path with
// fillPolygon — which tests every edge of every glyph on every subscan of
// the line's bounding box (O(glyphs² × rows) per line). A 600-cell
// spreadsheet frame spent ~70 ms (1×) / ~125 ms (2×) there and allocated
// ~80 MB.
//
// Under a pure positive uniform scale + translate matrix the device-space
// shape of a glyph depends only on (face, glyph, size, device scale,
// synthetic italic, sub-pixel origin). So each glyph is rasterised ONCE
// into a float32 coverage mask — with exactly the scan conversion
// fillPolygon uses (4 subscans, non-zero winding, addSubpixelSpan) — and
// later frames just sum cached masks into a line buffer and composite it
// once. Summing per-glyph coverage before the single min(cov/4, 1) clamp
// keeps fillPolygon's sum-then-clamp compositing (a pixel two glyphs
// both partially cover is blended once, not twice).
//
// Key precision: the horizontal sub-pixel origin is bucketed to 1/16 px
// (invisible; keeps a column of differently-positioned glyphs from
// producing a mask per float). The vertical fraction is keyed EXACTLY:
// with 4 vertical point samples per pixel, a tiny vertical shift can move
// a whole subscan across a horizontal stem edge and change a pixel by up
// to 64 levels, so quantising y is visibly wrong.
//
// Rotated / skewed / mirrored / non-uniformly scaled matrices, and
// backends that do not implement glyphMaskBlender, keep the original
// Path + DrawShape route untouched. QUI_GLYPH_CACHE=0 disables the cache
// entirely; QUI_GLYPH_CACHE=exact keys the x origin exactly too (used by
// tests to prove pixel equivalence with the legacy path).

import (
	"container/list"
	"image"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

const (
	glyphCacheOff      int32 = 0 // legacy Path + fillPolygon per line
	glyphCacheExact    int32 = 1 // x and y origin fractions keyed exactly
	glyphCacheBucketed int32 = 2 // x fraction bucketed to 1/glyphSubpixelXBuckets px (default)

	glyphSubpixelXBuckets   = 16
	glyphCacheDefaultBudget = 16 << 20
	// glyphMaskOverhead approximates the per-entry bookkeeping (list
	// element, map slot, mask header) charged against the byte budget.
	glyphMaskOverhead = 128
)

var glyphCacheMode atomic.Int32

func init() {
	glyphCacheMode.Store(parseGlyphCacheMode(os.Getenv("QUI_GLYPH_CACHE")))
}

func parseGlyphCacheMode(v string) int32 {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "off", "false", "no":
		return glyphCacheOff
	case "exact":
		return glyphCacheExact
	default:
		return glyphCacheBucketed
	}
}

// glyphMaskBlender is implemented by backends that can composite a
// device-space coverage buffer (values 0..4 subsample units) in a solid
// color. bounds is the buffer's device rect (row-major, bounds.Dx() wide);
// clip is the pixel clip from the canvas state.
type glyphMaskBlender interface {
	blendCoverage(cov []float32, bounds, clip image.Rectangle, color Color)
}

type glyphMaskKey struct {
	face   *gtfont.Face
	gid    gtfont.GID
	size   fixed.Int26_6
	scale  uint32 // math.Float32bits(device scale)
	italic bool   // synthetic italic shear
	fx     uint32 // x bucket index (bucketed) or Float32bits of the fraction (exact)
	fy     uint32 // Float32bits of the y fraction — always exact
}

// glyphMask is immutable once published to the cache.
type glyphMask struct {
	key    glyphMaskKey
	x0, y0 int // mask origin relative to the integer glyph origin
	w, h   int
	cov    []float32 // 0..4 subsample units, row-major w×h
}

func (g *glyphMask) cost() int { return len(g.cov)*4 + glyphMaskOverhead }

type glyphMaskCache struct {
	mu     sync.Mutex
	ll     *list.List // front = most recently used
	m      map[glyphMaskKey]*list.Element
	bytes  int
	budget int
	// maxEntry: masks costing more are rasterised but not cached (huge
	// display text would evict hundreds of body-text glyphs).
	maxEntry int
}

var glyphCache = newGlyphMaskCache(glyphCacheDefaultBudget)

func newGlyphMaskCache(budget int) *glyphMaskCache {
	return &glyphMaskCache{ll: list.New(), m: map[glyphMaskKey]*list.Element{}, budget: budget, maxEntry: budget / 8}
}

func (c *glyphMaskCache) get(k glyphMaskKey) *glyphMask {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[k]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(*glyphMask)
	}
	return nil
}

// put publishes g, evicting least-recently-used masks over budget. A mask
// over maxEntry is returned uncached. If another goroutine raced us to the
// same key, its mask wins and is returned.
func (c *glyphMaskCache) put(g *glyphMask) *glyphMask {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[g.key]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(*glyphMask)
	}
	if g.cost() > c.maxEntry {
		return g
	}
	c.m[g.key] = c.ll.PushFront(g)
	c.bytes += g.cost()
	c.evictLocked()
	return g
}

func (c *glyphMaskCache) evictLocked() {
	for c.bytes > c.budget && c.ll.Len() > 0 {
		e := c.ll.Back()
		old := e.Value.(*glyphMask)
		c.ll.Remove(e)
		delete(c.m, old.key)
		c.bytes -= old.cost()
	}
}

func (c *glyphMaskCache) reset() {
	c.mu.Lock()
	c.ll.Init()
	c.m = map[glyphMaskKey]*list.Element{}
	c.bytes = 0
	c.mu.Unlock()
}

// setBudget changes the byte budget (tests) and returns the previous one.
func (c *glyphMaskCache) setBudget(n int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev := c.budget
	c.budget = n
	c.evictLocked()
	return prev
}

func (c *glyphMaskCache) stats() (entries, bytes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len(), c.bytes
}

func (b *cpuBackend) blendCoverage(cov []float32, bounds, clip image.Rectangle, col Color) {
	img := b.activeImg()
	if img == nil {
		return
	}
	r := bounds.Intersect(clip).Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	rgba := toRGBA(col)
	w := bounds.Dx()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := cov[(y-bounds.Min.Y)*w : (y-bounds.Min.Y+1)*w]
		for x := r.Min.X; x < r.Max.X; x++ {
			cv := row[x-bounds.Min.X]
			if cv <= 0 {
				continue
			}
			a := cv / 4
			if a > 1 {
				a = 1
			}
			blendPixel(img, x, y, rgba, a)
		}
	}
}

// splitSubpixel splits a device coordinate into its integer origin, the
// fraction the mask is rasterised at, and the fraction's key.
func splitSubpixel(v float32, exact bool) (int, float32, uint32) {
	fl := float32(math.Floor(float64(v)))
	frac := v - fl
	if exact {
		return int(fl), frac, math.Float32bits(frac)
	}
	q := int(math.Round(float64(frac * glyphSubpixelXBuckets)))
	if q >= glyphSubpixelXBuckets {
		return int(fl) + 1, 0, 0
	}
	return int(fl), float32(q) / glyphSubpixelXBuckets, uint32(q)
}

// rasterGlyphMask rasterises one glyph into a coverage mask.
//
// The outline goes through exactly the arithmetic of the Path route —
// appendGlyphOutline builds the logical-space path, flatten projects it
// through the canvas matrix — and the flattened device points are then
// shifted by (shiftX, shiftY) into mask space. shiftY is always the
// integer origin row, so the shift is exact and every subscan sample sees
// the same edge positions fillPolygon would (edges lying exactly on a
// sample line resolve the same way). shiftX is the integer origin column
// in exact mode, or devX minus the bucket fraction in bucketed mode.
func rasterGlyphMask(run *shaping.Output, glyph shaping.Glyph, penX, baseline float32, m Matrix, shiftX, shiftY float32, fontSpec Font, key glyphMaskKey) *glyphMask {
	g := &glyphMask{key: key}
	path := NewPath()
	appendGlyphOutline(path, run, glyph, penX, baseline, fontSpec)
	if path.IsEmpty() {
		return g
	}
	subs := path.flatten(m.TransformPoint)
	if len(subs) == 0 {
		return g
	}
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, sub := range subs {
		for i := range sub {
			p := &sub[i]
			p.X -= shiftX
			p.Y -= shiftY
			minX, maxX = min(minX, p.X), max(maxX, p.X)
			minY, maxY = min(minY, p.Y), max(maxY, p.Y)
		}
	}
	b := image.Rect(int(math.Floor(float64(minX))), int(math.Floor(float64(minY))),
		int(math.Ceil(float64(maxX))), int(math.Ceil(float64(maxY))))
	if b.Empty() {
		return g
	}
	g.x0, g.y0, g.w, g.h = b.Min.X, b.Min.Y, b.Dx(), b.Dy()
	g.cov = rasterizeCoverage(subs, b)
	return g
}

// rasterizeCoverage is fillPolygon's AA scan conversion (4 subscans per
// row, half-open edge rule, non-zero winding, addSubpixelSpan) storing
// the raw per-pixel coverage (0..4) instead of compositing it.
func rasterizeCoverage(subs [][]Point, bounds image.Rectangle) []float32 {
	const subSamples = 4
	type edge struct {
		x0, y0, x1, y1 float32
		lo, hi         float32
		d              int8
	}
	edges := make([]edge, 0, 64)
	for _, poly := range subs {
		for i := 0; i < len(poly)-1; i++ {
			p0, p1 := poly[i], poly[i+1]
			if p1.Y == p0.Y {
				continue
			}
			e := edge{x0: p0.X, y0: p0.Y, x1: p1.X, y1: p1.Y, lo: p0.Y, hi: p1.Y, d: 1}
			if p1.Y < p0.Y {
				e.lo, e.hi, e.d = p1.Y, p0.Y, -1
			}
			edges = append(edges, e)
		}
	}
	w := bounds.Dx()
	out := make([]float32, w*bounds.Dy())
	type signedX struct {
		x float32
		d int8
	}
	crossings := make([]signedX, 0, 16)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		coverage := out[(y-bounds.Min.Y)*w : (y-bounds.Min.Y+1)*w]
		for sub := 0; sub < subSamples; sub++ {
			fy := float32(y) + (float32(sub)+0.5)/float32(subSamples)
			crossings = crossings[:0]
			for i := range edges {
				e := &edges[i]
				// Half-open: lo <= fy < hi, as in fillPolygon.
				if fy < e.lo || fy >= e.hi {
					continue
				}
				t := (fy - e.y0) / (e.y1 - e.y0)
				crossings = append(crossings, signedX{x: e.x0 + t*(e.x1-e.x0), d: e.d})
			}
			if len(crossings) < 2 {
				continue
			}
			for i := 1; i < len(crossings); i++ {
				v := crossings[i]
				j := i - 1
				for j >= 0 && crossings[j].x > v.x {
					crossings[j+1] = crossings[j]
					j--
				}
				crossings[j+1] = v
			}
			winding := 0
			prevX := crossings[0].x
			for i := range crossings {
				curX := crossings[i].x
				if winding != 0 {
					sx0, sx1 := max(prevX, float32(bounds.Min.X)), min(curX, float32(bounds.Max.X))
					if sx1 > sx0 {
						addSubpixelSpan(coverage, bounds.Min.X, sx0, sx1)
					}
				}
				winding += int(crossings[i].d)
				prevX = curX
			}
		}
	}
	return out
}

type placedGlyphMask struct {
	g      *glyphMask
	ox, oy int
}

// glyphLineScratch is pooled per-draw scratch: the placed-mask list and
// the line accumulation buffer.
type glyphLineScratch struct {
	placed []placedGlyphMask
	acc    []float32
}

var glyphLineScratchPool = sync.Pool{New: func() any { return new(glyphLineScratch) }}

// glyphMaskTarget reports whether this canvas can take the mask-cache
// route: cache enabled, backend can composite coverage, matrix is a pure
// positive uniform scale + translate.
func (c imageCanvas) glyphMaskTarget() (glyphMaskBlender, int32, bool) {
	mode := glyphCacheMode.Load()
	if mode == glyphCacheOff {
		return nil, mode, false
	}
	bl, ok := c.backend.(glyphMaskBlender)
	if !ok {
		return nil, mode, false
	}
	m := c.topCopy().matrix
	if m.B != 0 || m.C != 0 || !(m.A > 0) || m.A != m.D {
		return nil, mode, false
	}
	return bl, mode, true
}

// drawShapedLineMasks draws the outline glyphs of line (emoji are drawn
// by the caller) through the mask cache.
func (c imageCanvas) drawShapedLineMasks(bl glyphMaskBlender, mode int32, line *shapedLine, visual []int, x, baseline float32, color Color, fontSpec Font) {
	top := c.topCopy()
	m := top.matrix
	pxClip := rectToPixelRect(top.clip)
	if pxClip.Empty() {
		return
	}
	scaleBits := math.Float32bits(m.A)
	exactX := mode == glyphCacheExact
	sc := glyphLineScratchPool.Get().(*glyphLineScratch)
	defer glyphLineScratchPool.Put(sc)
	placed := sc.placed[:0]
	var acc image.Rectangle
	penX := x
	for _, ri := range visual {
		run := &line.runs[ri]
		for _, glyph := range run.Glyphs {
			if glyph.GlyphID != gtfont.EmptyGlyph {
				// Device position of the glyph origin, computed as the
				// Path route projects an outline point at font-unit (0,0).
				dev := m.TransformPoint(Point{X: penX + fixedToPx(glyph.XOffset), Y: baseline - fixedToPx(glyph.YOffset)})
				ix, fx, kx := splitSubpixel(dev.X, exactX)
				iy, _, ky := splitSubpixel(dev.Y, true)
				key := glyphMaskKey{face: run.Face, gid: glyph.GlyphID, size: run.Size, scale: scaleBits,
					italic: fontSpec.Italic, fx: kx, fy: ky}
				g := glyphCache.get(key)
				if g == nil {
					// Rasterise outside the cache lock; masks are immutable.
					shiftX := float32(ix)
					if !exactX {
						shiftX = dev.X - fx
					}
					g = glyphCache.put(rasterGlyphMask(run, glyph, penX, baseline, m, shiftX, float32(iy), fontSpec, key))
				}
				if g.w > 0 {
					r := image.Rect(ix+g.x0, iy+g.y0, ix+g.x0+g.w, iy+g.y0+g.h)
					if r.Overlaps(pxClip) {
						placed = append(placed, placedGlyphMask{g, ix, iy})
						acc = acc.Union(r)
					}
				}
			}
			penX += fixedToPx(glyph.Advance)
		}
	}
	sc.placed = placed[:0]
	if len(placed) == 0 {
		return
	}
	// Sum glyph coverage into one line buffer, then composite once.
	n := acc.Dx() * acc.Dy()
	if cap(sc.acc) < n {
		sc.acc = make([]float32, n)
	}
	buf := sc.acc[:n]
	clear(buf)
	aw := acc.Dx()
	for _, p := range placed {
		g := p.g
		bx, by := p.ox+g.x0-acc.Min.X, p.oy+g.y0-acc.Min.Y
		for row := 0; row < g.h; row++ {
			dst := buf[(by+row)*aw+bx : (by+row)*aw+bx+g.w]
			src := g.cov[row*g.w : (row+1)*g.w]
			for i, v := range src {
				dst[i] += v
			}
		}
	}
	bl.blendCoverage(buf, acc, pxClip, color)
}
