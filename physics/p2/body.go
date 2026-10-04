package p2

import "github.com/qizhanchan/qui/physics"

// Body is a rigid axis-aligned box. All fields may be set before
// AddBody; after that, game code typically mutates Vel (and Pos for
// teleports/respawns) on dynamic bodies only. Static bodies must not
// move — remove and re-add instead.
type Body struct {
	Type physics.BodyType

	// Pos is the AABB min corner (top-left in screen space), which
	// lines up naturally with tile coordinates.
	Pos  Vec2
	Size Vec2
	Vel  Vec2

	// GravityScale multiplies the world gravity for this body.
	// NewBody sets it to 1; a literal zero means "no gravity".
	GravityScale float32

	Layer, Mask physics.Layer

	// Sensor bodies report overlap events but never block or get
	// blocked. Dynamic sensors integrate velocity without collision.
	Sensor bool

	// OneWay marks a static platform that only blocks bodies falling
	// onto it from above; it never blocks horizontal or upward motion.
	OneWay bool

	// UserData points back at the owning game entity.
	UserData any

	// Contact state, recomputed by every World.Step.
	OnGround, OnCeiling, OnWallLeft, OnWallRight bool

	id uint64 // assigned by World.AddBody; 0 = not added
}

// NewBody returns a body with the documented defaults: GravityScale 1,
// Layer LayerDefault, Mask LayerAll.
func NewBody(t physics.BodyType, pos, size Vec2) *Body {
	return &Body{
		Type:         t,
		Pos:          pos,
		Size:         size,
		GravityScale: 1,
		Layer:        physics.LayerDefault,
		Mask:         physics.LayerAll,
	}
}

// AABB returns the body's current box.
func (b *Body) AABB() AABB {
	return AABB{Min: b.Pos, Max: b.Pos.Add(b.Size)}
}
