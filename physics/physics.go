// Package physics holds the dimension-independent vocabulary shared by
// every concrete physics world implementation. The 2D engine lives in
// physics/p2; a future 3D engine (physics/p3) must mirror p2's API
// surface (Vec3/AABB/Body/World with the same field and method names)
// and obey the same contract:
//
//   - Coordinate system matches the screen: +Y points DOWN, so gravity
//     is a positive-Y vector in the typical case. Worlds never hardcode
//     a "down" axis — gravity is whatever vector the caller passes.
//   - Contact.Normal is an axis-aligned unit vector pointing FROM the
//     blocking body TOWARD the dynamic body being resolved: landing on
//     the ground reports {0,-1}, bumping a ceiling reports {0,+1},
//     running into a wall on the right reports {-1,0}.
//   - World.Step(dt) expects a FIXED timestep. The accumulator that
//     converts variable frame time into fixed steps belongs to the
//     game layer, not the world.
//   - Solid resolution is swept per-axis (move-and-collide): X is
//     resolved before Y (p3 adds a Z round). Dynamic bodies collide
//     against Static and Kinematic bodies only; dynamic×dynamic pairs
//     are reported as sensor overlaps, never positionally resolved.
//   - Callbacks fire synchronously inside Step. AddBody/RemoveBody
//     called from a callback are deferred until the step completes.
//   - Resting contacts re-fire OnContact every step (a body standing
//     on the ground emits a {0,-1} contact each Step). Edge-triggered
//     semantics are only provided for sensor overlaps (OnSensorBegin).
//
// p3 extension notes (deliberately not implemented yet): the spatial
// hash gains a third cell axis, the per-axis sweep gains a Z round, and
// OnGround/OnCeiling keep their meaning while the wall flags generalize
// per horizontal axis. Everything in THIS package is reused as-is.
package physics

// BodyType classifies how a body participates in simulation.
type BodyType uint8

const (
	// Static bodies never move and are unaffected by forces. Terrain
	// tiles, bricks. Stored in the world's spatial hash; moving one
	// after AddBody is undefined — remove and re-add instead.
	Static BodyType = iota
	// Kinematic bodies move by their velocity under script control,
	// ignore gravity and are never blocked, but block dynamic bodies
	// (moving platforms).
	Kinematic
	// Dynamic bodies integrate gravity and are blocked by static and
	// kinematic bodies (player, enemies).
	Dynamic
)

// Layer is a collision-filtering bitmask. Two bodies A and B interact
// if and only if A.Layer&B.Mask != 0 && B.Layer&A.Mask != 0.
type Layer uint32

const (
	// LayerDefault is the layer new bodies start on.
	LayerDefault Layer = 1
	// LayerAll matches every layer; the default mask of new bodies.
	LayerAll Layer = ^Layer(0)
)
