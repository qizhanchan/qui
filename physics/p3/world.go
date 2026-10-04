package p3

import (
	"sort"

	"github.com/qizhanchan/qui/physics"
)

const (
	// cellSize is the static spatial hash bucket size in world units
	// (p2 uses 32 for pixel scale; p3 assumes meter-ish scale).
	cellSize = 4
	// skin is the contact tolerance: a body resting flush on a surface
	// has gap 0 and stays blocked, while perpendicular overlaps smaller
	// than skin don't count — so seams between adjacent tiles never
	// snag a sliding body.
	skin = 0.001
)

// Contact reports a solid collision resolved this step. A is always the
// dynamic body being moved; Normal points from B toward A and is axis
// aligned (see the physics package doc).
type Contact struct {
	A, B   *Body
	Normal Vec3
}

type cellKey struct{ x, y, z int32 }

// pairKey is a canonical (id-ordered) sensor-overlap pair.
type pairKey struct{ a, b *Body }

type sensorPair struct{ a, b *Body } // emission order: dynamic / lower body first

// World owns bodies and steps the simulation at a fixed timestep.
// Not safe for concurrent use; step it from the game loop goroutine.
type World struct {
	gravity    Vec3
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

// NewWorld creates an empty world with the given gravity (+Y is down).
func NewWorld(gravity Vec3) *World {
	return &World{
		gravity:  gravity,
		grid:     map[cellKey][]*Body{},
		overlaps: map[pairKey]bool{},
		seenBuf:  map[*Body]bool{},
	}
}

// OnContact registers the solid-contact callback. Resting contacts
// re-fire every step; use the Body contact flags for level-triggered
// state and this callback for impact-style reactions.
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
// Sweep order per dynamic body: X → Z → Y.
func (w *World) Step(dt float32) {
	w.stepping = true

	for _, b := range w.kinematics {
		b.Pos = b.Pos.Add(b.Vel.Mul(dt))
	}
	for _, b := range w.dynamics {
		b.OnGround, b.OnCeiling = false, false
		b.OnWallLeft, b.OnWallRight = false, false
		b.OnWallBack, b.OnWallFront = false, false
		b.Vel = b.Vel.Add(w.gravity.Mul(b.GravityScale * dt))
		if b.Sensor {
			b.Pos = b.Pos.Add(b.Vel.Mul(dt))
			continue
		}
		w.moveX(b, b.Vel.X*dt)
		w.moveZ(b, b.Vel.Z*dt)
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
// whose Layer matches mask overlaps area.
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

// overlap1D returns the overlap length of [a0,a1] and [b0,b1].
func overlap1D(a0, a1, b0, b1 float32) float32 {
	return minf(a1, b1) - maxf(a0, b0)
}

// moveX sweeps b along X by dx, clamping at the nearest blocking face.
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
			continue // one-way platforms only act on the Y axis
		}
		cb := c.AABB()
		overY := overlap1D(box.Min.Y, box.Max.Y, cb.Min.Y, cb.Max.Y)
		overZ := overlap1D(box.Min.Z, box.Max.Z, cb.Min.Z, cb.Max.Z)
		if overY <= skin || overZ <= skin {
			continue // no real overlap on the perpendicular axes
		}
		over := minf(overY, overZ)
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
			w.emitContact(Contact{A: b, B: hit, Normal: Vec3{X: -1}})
		} else {
			b.OnWallLeft = true
			w.emitContact(Contact{A: b, B: hit, Normal: Vec3{X: 1}})
		}
	}
}

// moveZ sweeps b along Z by dz, clamping at the nearest blocking face.
func (w *World) moveZ(b *Body, dz float32) {
	if dz == 0 {
		return
	}
	box := b.AABB()
	swept := box
	if dz > 0 {
		swept.Max.Z += dz
	} else {
		swept.Min.Z += dz
	}
	allowed := dz
	var hit *Body
	var hitOver float32
	for _, c := range w.solidCandidates(b, swept) {
		if c.OneWay {
			continue
		}
		cb := c.AABB()
		overX := overlap1D(box.Min.X, box.Max.X, cb.Min.X, cb.Max.X)
		overY := overlap1D(box.Min.Y, box.Max.Y, cb.Min.Y, cb.Max.Y)
		if overX <= skin || overY <= skin {
			continue
		}
		over := minf(overX, overY)
		var gap float32
		if dz > 0 {
			gap = cb.Min.Z - box.Max.Z
		} else {
			gap = cb.Max.Z - box.Min.Z
		}
		blocked, tie := clampGap(&gap, dz, allowed)
		if blocked {
			allowed, hit, hitOver = gap, c, over
		} else if tie && hit != nil && over > hitOver {
			hit, hitOver = c, over
		}
	}
	if hit == nil {
		b.Pos.Z += allowed
	} else {
		// Snap exactly flush against the blocking face — adding the gap
		// would accumulate float error and break resting-contact checks.
		if dz > 0 {
			b.Pos.Z = hit.Pos.Z - b.Size.Z
		} else {
			b.Pos.Z = hit.Pos.Z + hit.Size.Z
		}
	}
	if hit != nil {
		b.Vel.Z = 0
		if dz > 0 {
			b.OnWallFront = true
			w.emitContact(Contact{A: b, B: hit, Normal: Vec3{Z: -1}})
		} else {
			b.OnWallBack = true
			w.emitContact(Contact{A: b, B: hit, Normal: Vec3{Z: 1}})
		}
	}
}

// moveY sweeps b along Y by dy. Downward clamps set OnGround, upward
// clamps set OnCeiling. One-way platforms participate here only.
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
		overX := overlap1D(box.Min.X, box.Max.X, cb.Min.X, cb.Max.X)
		overZ := overlap1D(box.Min.Z, box.Max.Z, cb.Min.Z, cb.Max.Z)
		if overX <= skin || overZ <= skin {
			continue
		}
		over := minf(overX, overZ)
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
			w.emitContact(Contact{A: b, B: hit, Normal: Vec3{Y: -1}})
		} else {
			b.OnCeiling = true
			w.emitContact(Contact{A: b, B: hit, Normal: Vec3{Y: 1}})
		}
	}
}

// clampGap normalizes a candidate gap along the motion axis and decides
// whether it blocks earlier than the current best (blocked) or exactly
// ties it (tie, for perpendicular-overlap tie-breaking). Identical to
// p2's clampGap.
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
	z0 := int32(floorDiv(a.Min.Z, cellSize))
	x1 := int32(floorDiv(a.Max.X, cellSize))
	y1 := int32(floorDiv(a.Max.Y, cellSize))
	z1 := int32(floorDiv(a.Max.Z, cellSize))
	for z := z0; z <= z1; z++ {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				fn(cellKey{x, y, z})
			}
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
