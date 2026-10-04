package p3

import (
	"math"
	"testing"

	"github.com/qizhanchan/qui/physics"
)

const dt = float32(1.0 / 120.0)

func approx(a, b, eps float32) bool {
	return float32(math.Abs(float64(a-b))) <= eps
}

func box(x0, y0, z0, x1, y1, z1 float32) AABB {
	return AABB{Min: Vec3{x0, y0, z0}, Max: Vec3{x1, y1, z1}}
}

// addFloor adds a static slab spanning [x0,x1]×[z0,z1] with top at y.
func addFloor(w *World, x0, z0, x1, z1, y float32) *Body {
	b := NewBody(physics.Static, Vec3{x0, y, z0}, Vec3{x1 - x0, 1, z1 - z0})
	w.AddBody(b)
	return b
}

func TestFreeFallIntegration(t *testing.T) {
	g := float32(9.8)
	w := NewWorld(Vec3{Y: g})
	b := NewBody(physics.Dynamic, Vec3{}, Vec3{1, 1, 1})
	w.AddBody(b)

	var wantVel, wantPos float32
	for i := 0; i < 120; i++ {
		wantVel += g * dt
		wantPos += wantVel * dt
		w.Step(dt)
	}
	if !approx(b.Vel.Y, wantVel, 1e-4) || !approx(b.Pos.Y, wantPos, 1e-4) {
		t.Fatalf("vel %v pos %v, want %v / %v", b.Vel.Y, b.Pos.Y, wantVel, wantPos)
	}
}

func TestLandAndRest(t *testing.T) {
	w := NewWorld(Vec3{Y: 9.8})
	addFloor(w, -10, -10, 10, 10, 5)
	b := NewBody(physics.Dynamic, Vec3{0, 0, 0}, Vec3{1, 1, 1})
	w.AddBody(b)

	for i := 0; i < 300; i++ {
		w.Step(dt)
	}
	if b.Pos.Y != 4 {
		t.Fatalf("Pos.Y = %v, want exactly 4 (flush on floor top)", b.Pos.Y)
	}
	for i := 0; i < 120; i++ {
		w.Step(dt)
		if b.Pos.Y != 4 || !b.OnGround || b.Vel.Y != 0 {
			t.Fatalf("rest broken at step %d: y=%v ground=%v vy=%v", i, b.Pos.Y, b.OnGround, b.Vel.Y)
		}
	}
}

func TestWallsBlockOnBothHorizontalAxes(t *testing.T) {
	w := NewWorld(Vec3{Y: 9.8})
	addFloor(w, -20, -20, 20, 20, 2)
	// Wall at x = 5..6 spanning z, sitting on the floor.
	wallX := NewBody(physics.Static, Vec3{5, -2, -20}, Vec3{1, 4, 40})
	w.AddBody(wallX)
	// Wall at z = 8..9 spanning x.
	wallZ := NewBody(physics.Static, Vec3{-20, -2, 8}, Vec3{40, 4, 1})
	w.AddBody(wallZ)

	b := NewBody(physics.Dynamic, Vec3{0, 1, 0}, Vec3{1, 1, 1})
	w.AddBody(b)
	for i := 0; i < 240; i++ {
		b.Vel.X = 10 // hold "right"
		w.Step(dt)
	}
	if b.Pos.X != 4 || !b.OnWallRight || b.Vel.X != 0 {
		t.Fatalf("+X: pos=%v wallRight=%v vx=%v", b.Pos.X, b.OnWallRight, b.Vel.X)
	}

	// While flush against the X wall, Z motion still works and the Z
	// wall blocks with its own flag.
	for i := 0; i < 240; i++ {
		b.Vel.Z = 10
		w.Step(dt)
	}
	if b.Pos.Z != 7 || !b.OnWallFront || b.Vel.Z != 0 {
		t.Fatalf("+Z: pos=%v wallFront=%v vz=%v", b.Pos.Z, b.OnWallFront, b.Vel.Z)
	}
}

func TestCeilingContact(t *testing.T) {
	w := NewWorld(Vec3{Y: 9.8})
	addFloor(w, -10, -10, 10, 10, 5)
	roof := NewBody(physics.Static, Vec3{-10, -3, -10}, Vec3{20, 1, 20})
	roof.UserData = "roof"
	w.AddBody(roof)

	b := NewBody(physics.Dynamic, Vec3{0, 3, 0}, Vec3{1, 1, 1})
	w.AddBody(b)
	var bumps []Contact
	w.OnContact(func(c Contact) {
		if c.Normal.Y == 1 {
			bumps = append(bumps, c)
		}
	})
	b.Vel.Y = -20 // jump up (−Y)
	for i := 0; i < 240; i++ {
		w.Step(dt)
	}
	if len(bumps) != 1 || bumps[0].B != roof {
		t.Fatalf("ceiling contacts = %d (want 1), B=%v", len(bumps), bumps)
	}
}

func TestNoTunnelingAtHighSpeed(t *testing.T) {
	w := NewWorld(Vec3{})
	addFloor(w, -50, -50, 50, 50, 30)
	b := NewBody(physics.Dynamic, Vec3{0, 0, 0}, Vec3{1, 1, 1})
	b.Vel.Y = 5000
	w.AddBody(b)
	w.Step(0.1) // 500 units in one step, far past the 1-unit-thick floor
	if b.Pos.Y != 29 || !b.OnGround {
		t.Fatalf("tunneled: y=%v ground=%v", b.Pos.Y, b.OnGround)
	}
}

func TestOneWayPlatform(t *testing.T) {
	newWorld := func() (*World, *Body) {
		w := NewWorld(Vec3{Y: 9.8})
		plat := NewBody(physics.Static, Vec3{-2, 5, -2}, Vec3{4, 0.2, 4})
		plat.OneWay = true
		w.AddBody(plat)
		return w, plat
	}

	// Falling from above lands.
	w, _ := newWorld()
	b := NewBody(physics.Dynamic, Vec3{-0.5, 0, -0.5}, Vec3{1, 1, 1})
	w.AddBody(b)
	for i := 0; i < 300; i++ {
		w.Step(dt)
	}
	if b.Pos.Y != 4 || !b.OnGround {
		t.Fatalf("one-way landing: y=%v ground=%v", b.Pos.Y, b.OnGround)
	}

	// Rising from below passes through with no contact.
	w, _ = newWorld()
	contacts := 0
	w.OnContact(func(Contact) { contacts++ })
	b = NewBody(physics.Dynamic, Vec3{-0.5, 8, -0.5}, Vec3{1, 1, 1})
	b.GravityScale = 0
	b.Vel.Y = -20
	w.AddBody(b)
	for i := 0; i < 60; i++ {
		w.Step(dt)
	}
	if b.Pos.Y >= 5 || contacts != 0 || b.OnCeiling {
		t.Fatalf("one-way blocked from below: y=%v contacts=%d", b.Pos.Y, contacts)
	}

	// Horizontal motion at platform level passes through on X and Z.
	w, _ = newWorld()
	b = NewBody(physics.Dynamic, Vec3{-8, 4.5, -0.5}, Vec3{1, 1, 1})
	b.GravityScale = 0
	b.Vel.X = 15
	w.AddBody(b)
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if b.OnWallLeft || b.OnWallRight || b.Pos.X < 4 {
		t.Fatalf("one-way blocked horizontal: x=%v", b.Pos.X)
	}
}

func TestSensorBeginEdge(t *testing.T) {
	w := NewWorld(Vec3{})
	gate := NewBody(physics.Static, Vec3{10, -2, -2}, Vec3{1, 4, 4})
	gate.Sensor = true
	w.AddBody(gate)

	b := NewBody(physics.Dynamic, Vec3{0, -0.5, -0.5}, Vec3{1, 1, 1})
	w.AddBody(b)
	var events [][2]*Body
	w.OnSensorBegin(func(a, x *Body) { events = append(events, [2]*Body{a, x}) })

	b.Vel.X = 20
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if len(events) != 1 || events[0][0] != b || events[0][1] != gate {
		t.Fatalf("crossing events = %v", events)
	}
	b.Vel.X = -20
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if len(events) != 2 {
		t.Fatalf("re-entry events = %d, want 2", len(events))
	}
}

func TestLayerFiltering(t *testing.T) {
	const (
		layerA physics.Layer = 1 << 0
		layerB physics.Layer = 1 << 1
	)
	w := NewWorld(Vec3{})
	wall := NewBody(physics.Static, Vec3{5, -5, -5}, Vec3{1, 10, 10})
	wall.Layer = layerB
	w.AddBody(wall)

	b := NewBody(physics.Dynamic, Vec3{0, -0.5, -0.5}, Vec3{1, 1, 1})
	b.Layer = layerA
	b.Mask = layerA
	b.Vel.X = 20
	w.AddBody(b)
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if b.OnWallRight || b.Pos.X < 10 {
		t.Fatalf("mask-mismatched wall blocked body: x=%v", b.Pos.X)
	}
}

func TestTileSeamGlide(t *testing.T) {
	// Floor built from a grid of adjacent 4×4 tiles; gliding across
	// must never snag on internal seams, on either horizontal axis.
	w := NewWorld(Vec3{Y: 9.8})
	for tx := 0; tx < 12; tx++ {
		for tz := 0; tz < 12; tz++ {
			b := NewBody(physics.Static, Vec3{float32(tx) * 4, 5, float32(tz) * 4}, Vec3{4, 1, 4})
			w.AddBody(b)
		}
	}
	b := NewBody(physics.Dynamic, Vec3{1, 3, 1}, Vec3{1, 1, 1})
	w.AddBody(b)
	for i := 0; i < 300; i++ {
		b.Vel.X = 8
		b.Vel.Z = 6
		w.Step(dt)
		if b.OnWallLeft || b.OnWallRight || b.OnWallBack || b.OnWallFront {
			t.Fatalf("step %d: snagged on seam at %v", i, b.Pos)
		}
		if i > 60 && b.Pos.Y != 4 {
			t.Fatalf("step %d: Y broke during glide: %v", i, b.Pos.Y)
		}
	}
}

func TestDeterminism(t *testing.T) {
	run := func() []Vec3 {
		w := NewWorld(Vec3{Y: 9.8})
		addFloor(w, -40, -40, 40, 40, 5)
		wall := NewBody(physics.Static, Vec3{10, 0, -40}, Vec3{1, 5, 80})
		w.AddBody(wall)
		a := NewBody(physics.Dynamic, Vec3{0, 3, 0}, Vec3{1, 1, 1})
		w.AddBody(a)
		b := NewBody(physics.Dynamic, Vec3{-5, 0, 5}, Vec3{1, 1, 1})
		w.AddBody(b)

		var trace []Vec3
		for i := 0; i < 300; i++ {
			if i%40 < 20 {
				a.Vel.X, a.Vel.Z = 6, 3
			} else {
				a.Vel.X, a.Vel.Z = -2, -4
			}
			if i%97 == 0 && a.OnGround {
				a.Vel.Y = -8
			}
			w.Step(dt)
			trace = append(trace, a.Pos, b.Pos)
		}
		return trace
	}
	x, y := run(), run()
	for i := range x {
		if x[i] != y[i] {
			t.Fatalf("trace diverged at %d: %v vs %v", i, x[i], y[i])
		}
	}
}

func TestDynamicDynamicOverlapEvent(t *testing.T) {
	w := NewWorld(Vec3{})
	a := NewBody(physics.Dynamic, Vec3{0, 0, 0}, Vec3{1, 1, 1})
	a.Vel.X = 10
	w.AddBody(a)
	b := NewBody(physics.Dynamic, Vec3{6, 0, 0}, Vec3{1, 1, 1})
	w.AddBody(b)

	events := 0
	w.OnSensorBegin(func(x, y *Body) { events++ })
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if events != 1 {
		t.Fatalf("dyn×dyn events = %d, want 1", events)
	}
	if a.Pos.X <= b.Pos.X {
		t.Fatal("dynamic body was blocked by another dynamic body")
	}
}

func TestRemoveBodyDuringCallback(t *testing.T) {
	w := NewWorld(Vec3{})
	gate := NewBody(physics.Static, Vec3{5, -2, -2}, Vec3{1, 4, 4})
	gate.Sensor = true
	w.AddBody(gate)
	b := NewBody(physics.Dynamic, Vec3{0, -0.5, -0.5}, Vec3{1, 1, 1})
	b.Vel.X = 10
	w.AddBody(b)

	hits := 0
	w.OnSensorBegin(func(x, y *Body) {
		hits++
		w.RemoveBody(y) // deferred
	})
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	b.Pos = Vec3{0, -0.5, -0.5}
	b.Vel.X = 10
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if hits != 1 {
		t.Fatalf("removed sensor fired %d times, want 1", hits)
	}
}
