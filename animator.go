package qui

import "time"

// Animator is a generalized frame-driven entity that the Window polls
// once per frame. It reports the pixels it wants repainted (usually
// the bounds of the widget it animates) and whether it has finished.
//
// Animators live in Window.animators and are pruned when Tick reports
// done=true. They differ from Tickable in that they don't need to
// live in the widget tree — arbitrary user code can register them,
// e.g. animating a floating overlay or a camera orbit.
//
// Implementations (Tween, Spring, Timeline) live in the `anim`
// subpackage; this interface stays in root so Window can reference it
// without creating an import cycle.
type Animator interface {
	Tick(now time.Time) (dirty Rect, done bool)
	Stop()
}
