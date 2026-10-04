package qui

import "sync/atomic"

// AppActivationPolicy says how the process presents itself to the OS as an
// application: whether it has a Dock icon, and whether it may become the
// active app.
//
// The default (ActivationRegular) is right for anything a user launches and
// looks at. It is wrong — actively harmful — for a process whose whole job
// is to serve some OTHER app's foreground work: an input method, a
// clipboard HUD, a status-bar utility. Such a process must never take
// activation away from whatever the user is typing into.
type AppActivationPolicy int

const (
	// ActivationRegular is an ordinary app: Dock icon, menu bar, becomes
	// active on launch. The default.
	ActivationRegular AppActivationPolicy = iota

	// ActivationAccessory has no Dock icon but may still show windows and
	// become active when one of them is clicked. The usual choice for a
	// menu-bar utility.
	ActivationAccessory

	// ActivationProhibited never becomes the active app at all. This is
	// what an input method needs: its candidate panel appears over the
	// host app, and the host app stays active the entire time.
	ActivationProhibited
)

// activationPolicy is read when the platform backend initializes, which is
// why SetActivationPolicy has to be called first. Atomic because the
// setter is legitimately callable before the UI thread is established.
var activationPolicy atomic.Int32

// SetActivationPolicy selects the process's activation policy. It must be
// called BEFORE NewApp: the policy is applied when the windowing backend
// initializes, and on macOS an app that has already activated cannot
// un-activate itself without visibly flashing to the front first.
//
// Honored by the cocoa backend. GLFW installs its own policy and activates
// on startup with no hook to prevent it, so a process that must not steal
// focus has to run on QUI_PLATFORM=cocoa — which is also the only backend
// that can create the WindowOverlayPanel such a process needs, so the
// constraint shows up as an error there rather than as silent focus theft.
func SetActivationPolicy(p AppActivationPolicy) {
	activationPolicy.Store(int32(p))
}

// ActivationPolicy reports the policy set by SetActivationPolicy.
func ActivationPolicy() AppActivationPolicy {
	return AppActivationPolicy(activationPolicy.Load())
}
