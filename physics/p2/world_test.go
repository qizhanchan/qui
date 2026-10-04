package p2

import (
	"math"
	"testing"

	"github.com/qizhanchan/qui/physics"
)

const dt = float32(1.0 / 120.0)

func approx(a, b, eps float32) bool {
	return float32(math.Abs(float64(a-b))) <= eps
}

// addTile adds a 16x16 static solid at tile coordinates (tx, ty).
func addTile(w *World, tx, ty int) *Body {
	b := NewBody(physics.Static, Vec2{float32(tx) * 16, float32(ty) * 16}, Vec2{16, 16})
	w.AddBody(b)
	return b
}

func TestFreeFallIntegration(t *testing.T) {
	g := float32(100)
	w := NewWorld(Vec2{0, g})
	b := NewBody(physics.Dynamic, Vec2{0, 0}, Vec2{16, 16})
	w.AddBody(b)

	// Mirror the semi-implicit Euler the world performs.
	var wantVel, wantPos float32
	for i := 0; i < 100; i++ {
		wantVel += g * dt
		wantPos += wantVel * dt
		w.Step(dt)
	}
	if !approx(b.Vel.Y, wantVel, 1e-3) {
		t.Fatalf("Vel.Y = %v, want %v", b.Vel.Y, wantVel)
	}
	if !approx(b.Pos.Y, wantPos, 1e-3) {
		t.Fatalf("Pos.Y = %v, want %v", b.Pos.Y, wantPos)
	}
	if !approx(wantVel, g*float32(100)*dt, 1e-3) {
		t.Fatalf("sanity: wantVel %v != g*t %v", wantVel, g*100*dt)
	}
}

func TestLandAndRest(t *testing.T) {
	w := NewWorld(Vec2{0, 1000})
	// Ground row at y=100..116.
	for tx := -2; tx < 4; tx++ {
		b := NewBody(physics.Static, Vec2{float32(tx) * 16, 100}, Vec2{16, 16})
		w.AddBody(b)
	}
	body := NewBody(physics.Dynamic, Vec2{0, 0}, Vec2{16, 16})
	w.AddBody(body)

	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if body.Pos.Y != 84 {
		t.Fatalf("after landing Pos.Y = %v, want exactly 84 (flush on ground)", body.Pos.Y)
	}
	// Stay at rest with no sinking or jitter.
	for i := 0; i < 120; i++ {
		w.Step(dt)
		if body.Pos.Y != 84 {
			t.Fatalf("step %d: Pos.Y drifted to %v", i, body.Pos.Y)
		}
		if !body.OnGround {
			t.Fatalf("step %d: OnGround lost while resting", i)
		}
		if body.Vel.Y != 0 {
			t.Fatalf("step %d: Vel.Y = %v, want 0", i, body.Vel.Y)
		}
	}
}

func TestWallBlocksAndAllowsVerticalSlide(t *testing.T) {
	w := NewWorld(Vec2{0, 1000})
	for tx := 0; tx < 6; tx++ {
		addTile(w, tx, 7) // ground at y=112
	}
	wall := addTile(w, 5, 6) // wall column at x=80..96, on the ground
	addTile(w, 5, 5)
	_ = wall

	body := NewBody(physics.Dynamic, Vec2{0, 96}, Vec2{16, 16}) // on the ground
	w.AddBody(body)

	// Hold "right" every step, like a player holding the key. Contact
	// flags mean "blocked this step", so they stay set only while the
	// body keeps pushing into the wall.
	for i := 0; i < 120; i++ {
		body.Vel.X = 200
		w.Step(dt)
	}
	if body.Pos.X != 64 {
		t.Fatalf("Pos.X = %v, want exactly 64 (flush against wall)", body.Pos.X)
	}
	if !body.OnWallRight {
		t.Fatal("OnWallRight not set while pushing into wall")
	}
	if body.Vel.X != 0 {
		t.Fatalf("Vel.X = %v, want 0 after clamp", body.Vel.X)
	}

	// Vertical sliding while flush against the wall still works.
	body.Vel.Y = -300
	prevY := body.Pos.Y
	w.Step(dt)
	if body.Pos.Y >= prevY {
		t.Fatalf("body did not slide up against wall: %v -> %v", prevY, body.Pos.Y)
	}
}

func TestHeadBumpContact(t *testing.T) {
	w := NewWorld(Vec2{0, 1000})
	for tx := 0; tx < 4; tx++ {
		addTile(w, tx, 10) // ground at y=160
	}
	brick := addTile(w, 1, 6) // brick at y=96..112, above the body
	brick.UserData = "brick"

	body := NewBody(physics.Dynamic, Vec2{16, 144}, Vec2{16, 16}) // standing under the brick
	w.AddBody(body)

	var bumps []Contact
	w.OnContact(func(c Contact) {
		if c.Normal.Y == 1 {
			bumps = append(bumps, c)
		}
	})

	body.Vel.Y = -400 // jump
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if len(bumps) != 1 {
		t.Fatalf("head bump contacts = %d, want exactly 1", len(bumps))
	}
	c := bumps[0]
	if c.A != body || c.B != brick {
		t.Fatalf("contact bodies wrong: A=%p B=%p", c.A, c.B)
	}
	if c.B.UserData != "brick" {
		t.Fatalf("UserData = %v", c.B.UserData)
	}
}

func TestNoTunnelingAtHighSpeed(t *testing.T) {
	w := NewWorld(Vec2{0, 0})
	// Thin 16px floor at y=200..216.
	for tx := -4; tx < 8; tx++ {
		b := NewBody(physics.Static, Vec2{float32(tx) * 16, 200}, Vec2{16, 16})
		w.AddBody(b)
	}
	body := NewBody(physics.Dynamic, Vec2{0, 0}, Vec2{16, 16})
	body.Vel.Y = 5000
	w.AddBody(body)

	// Single huge step: 5000 * 0.1 = 500px of travel, far past the floor.
	w.Step(0.1)
	if body.Pos.Y != 184 {
		t.Fatalf("Pos.Y = %v, want 184 (stopped on floor, no tunneling)", body.Pos.Y)
	}
	if !body.OnGround {
		t.Fatal("OnGround not set after swept landing")
	}
}

func TestOneWayPlatform(t *testing.T) {
	newWorld := func() (*World, *Body) {
		w := NewWorld(Vec2{0, 1000})
		plat := NewBody(physics.Static, Vec2{0, 100}, Vec2{64, 4})
		plat.OneWay = true
		w.AddBody(plat)
		return w, plat
	}

	// (a) Falling from above lands on it.
	w, _ := newWorld()
	body := NewBody(physics.Dynamic, Vec2{16, 0}, Vec2{16, 16})
	w.AddBody(body)
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if body.Pos.Y != 84 || !body.OnGround {
		t.Fatalf("falling onto one-way: Pos.Y=%v OnGround=%v, want 84/true", body.Pos.Y, body.OnGround)
	}

	// (b) Jumping up from below passes through, no contact.
	w, _ = newWorld()
	contacts := 0
	w.OnContact(func(Contact) { contacts++ })
	body = NewBody(physics.Dynamic, Vec2{16, 130}, Vec2{16, 16})
	body.GravityScale = 0
	body.Vel.Y = -400
	w.AddBody(body)
	for i := 0; i < 30; i++ {
		w.Step(dt)
	}
	if body.Pos.Y >= 100 {
		t.Fatalf("jump through one-way blocked: Pos.Y=%v", body.Pos.Y)
	}
	if contacts != 0 || body.OnCeiling {
		t.Fatalf("one-way produced contact from below: contacts=%d OnCeiling=%v", contacts, body.OnCeiling)
	}

	// (c) Horizontal motion at platform level is never blocked.
	w, _ = newWorld()
	body = NewBody(physics.Dynamic, Vec2{-40, 96}, Vec2{16, 16}) // overlapping platform's Y band
	body.GravityScale = 0
	body.Vel.X = 300
	w.AddBody(body)
	for i := 0; i < 60; i++ {
		w.Step(dt)
	}
	if body.OnWallLeft || body.OnWallRight {
		t.Fatal("one-way blocked horizontal motion")
	}
	if body.Pos.X < 64 {
		t.Fatalf("body did not pass platform horizontally: Pos.X=%v", body.Pos.X)
	}
}

func TestSensorBeginEdge(t *testing.T) {
	w := NewWorld(Vec2{0, 0})
	coin := NewBody(physics.Static, Vec2{100, 0}, Vec2{16, 16})
	coin.Sensor = true
	w.AddBody(coin)

	body := NewBody(physics.Dynamic, Vec2{0, 0}, Vec2{16, 16})
	w.AddBody(body)

	var events [][2]*Body
	w.OnSensorBegin(func(a, b *Body) { events = append(events, [2]*Body{a, b}) })

	// Drive across the sensor and far past it.
	body.Vel.X = 200
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if len(events) != 1 {
		t.Fatalf("sensor begin fired %d times during one crossing, want 1", len(events))
	}
	if events[0][0] != body || events[0][1] != coin {
		t.Fatal("sensor event pair order wrong: want (dynamic, sensor)")
	}
	if body.Pos.X <= 116 {
		t.Fatalf("sensor blocked motion: Pos.X=%v", body.Pos.X)
	}

	// Re-entering fires again.
	body.Vel.X = -200
	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if len(events) != 2 {
		t.Fatalf("re-entry: events = %d, want 2", len(events))
	}
}

func TestLayerFiltering(t *testing.T) {
	const (
		layerA physics.Layer = 1 << 0
		layerB physics.Layer = 1 << 1
	)
	w := NewWorld(Vec2{0, 0})

	wall := NewBody(physics.Static, Vec2{100, -50}, Vec2{16, 116})
	wall.Layer = layerB
	w.AddBody(wall)

	sensor := NewBody(physics.Static, Vec2{200, 0}, Vec2{16, 16})
	sensor.Sensor = true
	sensor.Layer = layerB
	sensor.Mask = layerB // does not match the body's layer
	w.AddBody(sensor)

	body := NewBody(physics.Dynamic, Vec2{0, 0}, Vec2{16, 16})
	body.Layer = layerA
	body.Mask = layerA // mask excludes layerB
	body.Vel.X = 300
	w.AddBody(body)

	events := 0
	w.OnSensorBegin(func(a, b *Body) { events++ })

	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if body.OnWallRight || body.Pos.X < 250 {
		t.Fatalf("mask-mismatched wall blocked body: Pos.X=%v", body.Pos.X)
	}
	if events != 0 {
		t.Fatalf("mask-mismatched sensor fired %d events", events)
	}
}

func TestTileSeamWalk(t *testing.T) {
	w := NewWorld(Vec2{0, 1000})
	for tx := 0; tx < 30; tx++ {
		addTile(w, tx, 7) // continuous floor at y=112
	}
	body := NewBody(physics.Dynamic, Vec2{0, 96}, Vec2{16, 16})
	body.Vel.X = 100
	w.AddBody(body)

	for i := 0; i < 200; i++ {
		w.Step(dt)
		body.Vel.X = 100 // hold running speed
		if body.OnWallLeft || body.OnWallRight {
			t.Fatalf("step %d: snagged on tile seam at Pos.X=%v", i, body.Pos.X)
		}
		if body.Pos.Y != 96 {
			t.Fatalf("step %d: Y jumped to %v while walking", i, body.Pos.Y)
		}
	}
}

func TestDeterminism(t *testing.T) {
	run := func() []Vec2 {
		w := NewWorld(Vec2{0, 1000})
		for tx := 0; tx < 40; tx++ {
			addTile(w, tx, 7)
		}
		addTile(w, 12, 6)
		addTile(w, 12, 5)
		body := NewBody(physics.Dynamic, Vec2{0, 96}, Vec2{16, 16})
		w.AddBody(body)
		other := NewBody(physics.Dynamic, Vec2{300, 0}, Vec2{16, 16})
		w.AddBody(other)

		var trace []Vec2
		for i := 0; i < 300; i++ {
			// Scripted input pattern.
			if i%40 < 20 {
				body.Vel.X = 120
			} else {
				body.Vel.X = -60
			}
			if i%97 == 0 && body.OnGround {
				body.Vel.Y = -350
			}
			w.Step(dt)
			trace = append(trace, body.Pos, other.Pos)
		}
		return trace
	}

	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("trace diverged at index %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestDynamicDynamicOverlapEvent(t *testing.T) {
	w := NewWorld(Vec2{0, 0})
	a := NewBody(physics.Dynamic, Vec2{0, 0}, Vec2{16, 16})
	a.Vel.X = 200
	w.AddBody(a)
	b := NewBody(physics.Dynamic, Vec2{100, 0}, Vec2{16, 16})
	w.AddBody(b)

	var events [][2]*Body
	w.OnSensorBegin(func(x, y *Body) { events = append(events, [2]*Body{x, y}) })

	for i := 0; i < 120; i++ {
		w.Step(dt)
	}
	if len(events) != 1 {
		t.Fatalf("dyn×dyn overlap events = %d, want 1", len(events))
	}
	if events[0] != [2]*Body{a, b} {
		t.Fatal("dyn×dyn event pair should be (lower-index, higher-index) dynamic")
	}
	// Dynamic pairs are never positionally resolved: a passes through b.
	if a.Pos.X <= b.Pos.X {
		t.Fatalf("dynamic body was blocked by another dynamic body: a.X=%v b.X=%v", a.Pos.X, b.Pos.X)
	}
}

func TestRemoveBodyDuringCallback(t *testing.T) {
	w := NewWorld(Vec2{0, 0})
	coin := NewBody(physics.Static, Vec2{50, 0}, Vec2{16, 16})
	coin.Sensor = true
	w.AddBody(coin)

	body := NewBody(physics.Dynamic, Vec2{0, 0}, Vec2{16, 16})
	body.Vel.X = 200
	w.AddBody(body)

	picked := 0
	w.OnSensorBegin(func(a, b *Body) {
		picked++
		w.RemoveBody(b) // deferred until the step completes
	})

	for i := 0; i < 60; i++ {
		w.Step(dt)
	}
	if picked != 1 {
		t.Fatalf("pickup fired %d times, want 1 (removed after first overlap)", picked)
	}
	// Re-crossing the removed sensor produces nothing.
	body.Pos = Vec2{0, 0}
	body.Vel.X = 200
	for i := 0; i < 60; i++ {
		w.Step(dt)
	}
	if picked != 1 {
		t.Fatalf("removed sensor fired again: %d", picked)
	}
}
