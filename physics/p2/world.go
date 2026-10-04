package p2

import (
	"sort"

	"github.com/qizhanchan/qui/physics"
)

const (
	// cellSize is the static spatial hash bucket size in world pixels.
	cellSize = 32
	// skin is the contact tolerance: a body resting flush on a surface
	// has gap 0 and stays blocked, while perpendicular overlaps smaller
	// than skin don't count — so the seam between two adjacent floor
	// tiles never snags a body sliding across it.
	skin = 0.001
)

// Contact reports a solid collision resolved this step. A is always the
// dynamic body being moved; Normal points from B toward A (see the
// physics package doc for the axis conventions).
type Contact struct {
	A, B   *Body
	Normal Vec2
}

type cellKey struct{ x, y int32 }

// pairKey is a canonical (id-ordered) sensor-overlap pair.
type pairKey struct{ a, b *Body }

type sensorPair struct{ a, b *Body } // emission order: dynamic / lower body first

// World owns bodies and steps the simulation at a fixed timestep.
// Not safe for concurrent use; step it from the game loop goroutine.
type World struct {
	gravity    Vec2
	dynamics   []*Body
	kinematics []*Body
	grid       map[cellKey][]*Body // static bodies, solid and sensor alike

	contactFn func(Contact)
	sensorFn  func(a, b *Body)

	overlaps map[pairKey]bool // sensor pairs overlapping as of last step
	nextID   uint64
	stepping bool
	deferred []func()

	// scratch buffers reused across queries to avoid per-step allocs
	candBuf []*Body
	seenBuf map[*Body]bool
}

// NewWorld creates an empty world with the given gravity (positive Y
// is down in screen space).
func NewWorld(gravity Vec2) *World {
	return &World{
		gravity:  gravity,
		grid:     map[cellKey][]*Body{},
		overlaps: map[pairKey]bool{},
		seenBuf:  map[*Body]bool{},
	}
}

// OnContact registers the solid-contact callback. Resting contacts
// re-fire every step; use Body.OnGround etc. for level-triggered state
// and this callback for impact-style reactions (head-bumping a brick).
func (w *World) OnContact(fn func(Contact)) { w.contactFn = fn }

// OnSensorBegin registers the overlap-begin callback. It fires once per
// pair when a dynamic body starts overlapping a sensor body or another
// dynamic body (layer-filtered), and again only after they separate.
func (w *World) OnSensorBegin(fn func(a, b *Body)) { w.sensorFn = fn }

// AddBody inserts a body. Safe to call from inside a Step callback —
// the insert is deferred until the step completes.
func (w *World) AddBody(b *Body) {
	if w.stepping {
		w.deferred = append(w.deferred, func() { w.AddBody(b) })
		return
	}
	if b.id != 0 {
		return // already added
	}
	w.nextID++
	b.id = w.nextID
	switch b.Type {
	case physics.Static:
		w.eachCell(b.AABB(), func(k cellKey) {
			w.grid[k] = append(w.grid[k], b)
		})
	case physics.Kinematic:
		w.kinematics = append(w.kinematics, b)
	default:
		w.dynamics = append(w.dynamics, b)
	}
}

// RemoveBody removes a body. Safe to call from inside a Step callback.
func (w *World) RemoveBody(b *Body) {
	if w.stepping {
		w.deferred = append(w.deferred, func() { w.RemoveBody(b) })
		return
	}
	if b.id == 0 {
		return
	}
	switch b.Type {
	case physics.Static:
		w.eachCell(b.AABB(), func(k cellKey) {
			w.grid[k] = removeBody(w.grid[k], b)
			if len(w.grid[k]) == 0 {
				delete(w.grid, k)
			}
		})
	case physics.Kinematic:
		w.kinematics = removeBody(w.kinematics, b)
	default:
		w.dynamics = removeBody(w.dynamics, b)
	}
	for k := range w.overlaps {
		if k.a == b || k.b == b {
			delete(w.overlaps, k)
		}
	}
	b.id = 0
}

// Step advances the simulation by a fixed dt (seconds). The caller is
// responsible for accumulating variable frame time into fixed steps.
func (w *World) Step(dt float32) {
	w.stepping = true

	for _, b := range w.kinematics {
		b.Pos = b.Pos.Add(b.Vel.Mul(dt))
	}
	for _, b := range w.dynamics {
		b.OnGround, b.OnCeiling, b.OnWallLeft, b.OnWallRight = false, false, false, false
		b.Vel = b.Vel.Add(w.gravity.Mul(b.GravityScale * dt))
		if b.Sensor {
			b.Pos = b.Pos.Add(b.Vel.Mul(dt))
			continue
		}
		w.moveX(b, b.Vel.X*dt)
		w.moveY(b, b.Vel.Y*dt)
	}

	w.sensorPass()

	w.stepping = false
	for _, fn := range w.deferred {
		fn()
	}
	w.deferred = w.deferred[:0]
}

// OverlapsSolid reports whether any non-sensor static or kinematic body
// whose Layer matches mask overlaps area. Useful for ledge probes.
func (w *World) OverlapsSolid(area AABB, mask physics.Layer) bool {
	hit := false
	w.eachCell(area, func(k cellKey) {
		for _, c := range w.grid[k] {
			if !c.Sensor && c.Layer&mask != 0 && area.Overlaps(c.AABB()) {
				hit = true
			}
		}
	})
	if hit {
		return true
	}
	for _, c := range w.kinematics {
		if !c.Sensor && c.Layer&mask != 0 && area.Overlaps(c.AABB()) {
			return true
		}
	}
	return false
}

// moveX sweeps b horizontally by dx, clamping at the nearest blocking
// face. One-way platforms never block horizontal motion.
func (w *World) moveX(b *Body, dx float32) {
	if dx == 0 {
		return
	}
	box := b.AABB()
	swept := box
	if dx > 0 {
		swept.Max.X += dx
	} else {
		swept.Min.X += dx
	}
	allowed := dx
	var hit *Body
	var hitOver float32
	for _, c := range w.solidCandidates(b, swept) {
		if c.OneWay {
			continue
		}
		cb := c.AABB()
		over := minf(box.Max.Y, cb.Max.Y) - maxf(box.Min.Y, cb.Min.Y)
		if over <= skin {
			continue // no real overlap on Y: tile seams don't snag
		}
		var gap float32
		if dx > 0 {
			gap = cb.Min.X - box.Max.X
		} else {
			gap = cb.Max.X - box.Min.X
		}
		blocked, tie := clampGap(&gap, dx, allowed)
		if blocked {
			allowed, hit, hitOver = gap, c, over
		} else if tie && hit != nil && over > hitOver {
			hit, hitOver = c, over
		}
	}
	if hit == nil {
		b.Pos.X += allowed
	} else {
		// Snap exactly flush against the blocking face — adding the gap
		// would accumulate float error and break resting-contact checks.
		if dx > 0 {
			b.Pos.X = hit.Pos.X - b.Size.X
		} else {
			b.Pos.X = hit.Pos.X + hit.Size.X
		}
	}
	if hit != nil {
		b.Vel.X = 0
		if dx > 0 {
			b.OnWallRight = true
			w.emitContact(Contact{A: b, B: hit, Normal: Vec2{-1, 0}})
		} else {
			b.OnWallLeft = true
			w.emitContact(Contact{A: b, B: hit, Normal: Vec2{1, 0}})
		}
	}
}

// moveY sweeps b vertically by dy. Downward clamps set OnGround,
// upward clamps set OnCeiling (the head-bump callback source).
func (w *World) moveY(b *Body, dy float32) {
	if dy == 0 {
		return
	}
	box := b.AABB()
	swept := box
	if dy > 0 {
		swept.Max.Y += dy
	} else {
		swept.Min.Y += dy
	}
	allowed := dy
	var hit *Body
	var hitOver float32
	for _, c := range w.solidCandidates(b, swept) {
		cb := c.AABB()
		if c.OneWay {
			// Only blocks a body falling onto it from above.
			if dy <= 0 || box.Max.Y > cb.Min.Y+skin {
				continue
			}
		}
		over := minf(box.Max.X, cb.Max.X) - maxf(box.Min.X, cb.Min.X)
		if over <= skin {
			continue
		}
		var gap float32
		if dy > 0 {
			gap = cb.Min.Y - box.Max.Y
		} else {
			gap = cb.Max.Y - box.Min.Y
		}
		blocked, tie := clampGap(&gap, dy, allowed)
		if blocked {
			allowed, hit, hitOver = gap, c, over
		} else if tie && hit != nil && over > hitOver {
			hit, hitOver = c, over
		}
	}
	if hit == nil {
		b.Pos.Y += allowed
	} else {
		// Snap exactly flush against the blocking face — adding the gap
		// would accumulate float error and break resting-contact checks.
		if dy > 0 {
			b.Pos.Y = hit.Pos.Y - b.Size.Y
		} else {
			b.Pos.Y = hit.Pos.Y + hit.Size.Y
		}
	}
	if hit != nil {
		b.Vel.Y = 0
		if dy > 0 {
			b.OnGround = true
			w.emitContact(Contact{A: b, B: hit, Normal: Vec2{0, -1}})
		} else {
			b.OnCeiling = true
			w.emitContact(Contact{A: b, B: hit, Normal: Vec2{0, 1}})
		}
	}
}

// clampGap normalizes a candidate gap along the motion axis and decides
// whether it blocks earlier than the current best (blocked) or exactly
// ties it (tie, for perpendicular-overlap tie-breaking). gap is clamped
// to 0 for flush contacts. Candidates behind the body or unreachable
// this step report neither.
func clampGap(gap *float32, delta, allowed float32) (blocked, tie bool) {
	if delta > 0 {
		if *gap < -skin {
			return false, false // behind / interpenetrating: ignore
		}
		if *gap < 0 {
			*gap = 0
		}
		if *gap < allowed {
			return true, false
		}
		return false, *gap == allowed
	}
	if *gap > skin {
		return false, false
	}
	if *gap > 0 {
		*gap = 0
	}
	if *gap > allowed {
		return true, false
	}
	return false, *gap == allowed
}

// sensorPass detects layer-filtered overlaps (dynamic×sensor and
// dynamic×dynamic) and fires OnSensorBegin on freshly begun pairs, in
// deterministic detection order.
func (w *World) sensorPass() {
	current := make(map[pairKey]bool, len(w.overlaps))
	var began []sensorPair
	mark := func(a, b *Body) {
		k := makePair(a, b)
		if current[k] {
			return
		}
		current[k] = true
		if !w.overlaps[k] {
			began = append(began, sensorPair{a, b})
		}
	}
	for _, b := range w.dynamics {
		box := b.AABB()
		for _, c := range w.sensorCandidates(b, box) {
			if box.Overlaps(c.AABB()) {
				mark(b, c)
			}
		}
	}
	for i, a := range w.dynamics {
		for _, b := range w.dynamics[i+1:] {
			if !layerMatch(a, b) {
				continue
			}
			if a.AABB().Overlaps(b.AABB()) {
				mark(a, b)
			}
		}
	}
	w.overlaps = current
	if w.sensorFn != nil {
		for _, p := range began {
			w.sensorFn(p.a, p.b)
		}
	}
}

func (w *World) emitContact(c Contact) {
	if w.contactFn != nil {
		w.contactFn(c)
	}
}

// solidCandidates returns the non-sensor static and kinematic bodies
// near area that pass layer filtering, sorted by insertion id so the
// sweep is deterministic regardless of map iteration order.
func (w *World) solidCandidates(b *Body, area AABB) []*Body {
	w.candBuf = w.candBuf[:0]
	clear(w.seenBuf)
	w.eachCell(area, func(k cellKey) {
		for _, c := range w.grid[k] {
			if c.Sensor || w.seenBuf[c] || !layerMatch(b, c) {
				continue
			}
			w.seenBuf[c] = true
			w.candBuf = append(w.candBuf, c)
		}
	})
	for _, c := range w.kinematics {
		if c == b || c.Sensor || !layerMatch(b, c) {
			continue
		}
		if area.Overlaps(c.AABB()) {
			w.candBuf = append(w.candBuf, c)
		}
	}
	sort.Slice(w.candBuf, func(i, j int) bool { return w.candBuf[i].id < w.candBuf[j].id })
	return w.candBuf
}

// sensorCandidates returns static sensor bodies near area passing layer
// filtering, sorted by insertion id.
func (w *World) sensorCandidates(b *Body, area AABB) []*Body {
	w.candBuf = w.candBuf[:0]
	clear(w.seenBuf)
	w.eachCell(area, func(k cellKey) {
		for _, c := range w.grid[k] {
			if !c.Sensor || w.seenBuf[c] || !layerMatch(b, c) {
				continue
			}
			w.seenBuf[c] = true
			w.candBuf = append(w.candBuf, c)
		}
	})
	sort.Slice(w.candBuf, func(i, j int) bool { return w.candBuf[i].id < w.candBuf[j].id })
	return w.candBuf
}

func (w *World) eachCell(a AABB, fn func(cellKey)) {
	x0 := int32(floorDiv(a.Min.X, cellSize))
	y0 := int32(floorDiv(a.Min.Y, cellSize))
	x1 := int32(floorDiv(a.Max.X, cellSize))
	y1 := int32(floorDiv(a.Max.Y, cellSize))
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			fn(cellKey{x, y})
		}
	}
}

func floorDiv(v, cell float32) int {
	q := v / cell
	i := int(q)
	if q < 0 && float32(i) != q {
		i--
	}
	return i
}

func layerMatch(a, b *Body) bool {
	return a.Layer&b.Mask != 0 && b.Layer&a.Mask != 0
}

func makePair(a, b *Body) pairKey {
	if a.id > b.id {
		a, b = b, a
	}
	return pairKey{a, b}
}

func removeBody(s []*Body, b *Body) []*Body {
	for i, c := range s {
		if c == b {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}
