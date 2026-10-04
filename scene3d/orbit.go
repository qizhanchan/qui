package scene3d

import (
	"math"

	"github.com/qizhanchan/qui"
)

// OrbitController is a reusable turntable/arcball camera controller.
// It drives a *Camera by maintaining spherical coordinates (yaw,
// pitch, distance) around a Target point:
//
//   - left-button drag  → orbit (rotate yaw/pitch)
//   - middle/right drag → pan (translate the target in screen plane)
//   - scroll wheel      → dolly (change distance)
//
// It does not own event dispatch — a host widget forwards mouse
// events via HandleMouse. This keeps Viewport free of any specific
// camera policy while giving CAD-style navigation for free:
//
//	orbit := scene3d.NewOrbitController(scene.Camera)
//	// in the host widget's Handle:
//	if me, ok := ev.(qui.MouseEvent); ok && orbit.HandleMouse(me) {
//	    viewport.InvalidateScene()
//	    return true
//	}
//
// Angles are in radians. Pitch is clamped away from the poles so the
// up vector never flips.
type OrbitController struct {
	Camera *Camera
	Target Vec3

	Yaw      float32 // rotation around world +Y, radians
	Pitch    float32 // elevation, radians (clamped to (Min,Max)Pitch)
	Distance float32 // eye-to-target distance

	RotateSpeed float32 // radians per pixel (default 0.01)
	PanSpeed    float32 // fraction of distance per pixel (default 0.0015)
	ZoomSpeed   float32 // multiplicative step per wheel notch (default 0.1)

	MinPitch, MaxPitch       float32
	MinDistance, MaxDistance float32

	// drag state
	mode         orbitMode
	lastX, lastY float32
}

type orbitMode int

const (
	orbitIdle orbitMode = iota
	orbitRotate
	orbitPan
)

// NewOrbitController seeds itself from the camera's current
// Position/Target so the view doesn't jump on the first frame.
func NewOrbitController(cam *Camera) *OrbitController {
	o := &OrbitController{
		Camera:      cam,
		RotateSpeed: 0.01,
		PanSpeed:    0.0015,
		ZoomSpeed:   0.1,
		MinPitch:    -1.5533, // ~-89°
		MaxPitch:    1.5533,  // ~+89°
		MinDistance: 0.05,
		MaxDistance: 100000,
	}
	if cam != nil {
		o.Target = cam.Target
		off := cam.Position.Sub(cam.Target)
		o.Distance = off.Length()
		if o.Distance < 1e-4 {
			o.Distance = 5
			off = Vec3{0, 0, o.Distance}
		}
		o.Pitch = float32(math.Asin(float64(clampf(off.Y/o.Distance, -1, 1))))
		o.Yaw = float32(math.Atan2(float64(off.X), float64(off.Z)))
	}
	o.Apply()
	return o
}

// Apply recomputes Camera.Position/Target from the spherical state.
// Call after mutating Yaw/Pitch/Distance/Target directly.
func (o *OrbitController) Apply() {
	if o == nil || o.Camera == nil {
		return
	}
	o.Pitch = clampf(o.Pitch, o.MinPitch, o.MaxPitch)
	o.Distance = clampf(o.Distance, o.MinDistance, o.MaxDistance)
	cp := float32(math.Cos(float64(o.Pitch)))
	sp := float32(math.Sin(float64(o.Pitch)))
	sy := float32(math.Sin(float64(o.Yaw)))
	cy := float32(math.Cos(float64(o.Yaw)))
	dir := Vec3{cp * sy, sp, cp * cy} // eye direction from target
	o.Camera.Target = o.Target
	o.Camera.Position = o.Target.Add(dir.Mul(o.Distance))
	o.Camera.Up = Vec3{0, 1, 0}
}

// Rotate orbits by pixel deltas (dx: horizontal, dy: vertical).
func (o *OrbitController) Rotate(dx, dy float32) {
	o.Yaw -= dx * o.RotateSpeed
	o.Pitch += dy * o.RotateSpeed
	o.Apply()
}

// Pan slides the target in the camera's screen plane by pixel deltas.
func (o *OrbitController) Pan(dx, dy float32) {
	if o.Camera == nil {
		return
	}
	forward := o.Target.Sub(o.Camera.Position).Normalize()
	right := forward.Cross(Vec3{0, 1, 0}).Normalize()
	up := right.Cross(forward)
	scale := o.PanSpeed * o.Distance
	o.Target = o.Target.
		Add(right.Mul(-dx * scale)).
		Add(up.Mul(dy * scale))
	o.Apply()
}

// Zoom dollies the camera. notches>0 zooms in (closer).
func (o *OrbitController) Zoom(notches float32) {
	o.Distance *= float32(math.Pow(float64(1-o.ZoomSpeed), float64(notches)))
	o.Apply()
}

// HandleMouse consumes a mouse event and returns true if it changed
// the camera (so the host can InvalidateScene). Non-mouse events and
// no-op moves return false.
func (o *OrbitController) HandleMouse(ev qui.MouseEvent) bool {
	switch ev.Type() {
	case qui.EventMouseDown:
		switch ev.Button {
		case qui.MouseButtonLeft:
			o.mode = orbitRotate
		case qui.MouseButtonMiddle, qui.MouseButtonRight:
			o.mode = orbitPan
		}
		o.lastX, o.lastY = ev.X, ev.Y
		return false
	case qui.EventMouseUp:
		o.mode = orbitIdle
		return false
	case qui.EventMouseMove:
		if o.mode == orbitIdle {
			return false
		}
		dx, dy := ev.X-o.lastX, ev.Y-o.lastY
		o.lastX, o.lastY = ev.X, ev.Y
		if dx == 0 && dy == 0 {
			return false
		}
		switch o.mode {
		case orbitRotate:
			o.Rotate(dx, dy)
		case orbitPan:
			o.Pan(dx, dy)
		}
		return true
	case qui.EventScroll:
		if ev.DeltaY == 0 {
			return false
		}
		o.Zoom(ev.DeltaY)
		return true
	}
	return false
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
