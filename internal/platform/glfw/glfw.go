//go:build cgo && !js && !qui_nogl && (windows || qui_glfw)

package glfw

import (
	"image"
	"time"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/qizhanchan/qui/internal/platform/contract"
)

// GLFW implementation of the platform seam (see platform.go).
//
// This is the ONLY file in the engine that may import glfw. If a glfw
// symbol is needed elsewhere, the seam is missing a method.
//
// GLFW's own limitations are absorbed here rather than leaked upward:
//   - its scroll callback reports neither modifiers nor gesture phase, so
//     scrollContext below carries what the macOS bridge snapshots off the
//     real NSEvent (platform_glfw_gesture_darwin.m), and currentMods polls key state as
//     the fallback everywhere else;
//   - it has no multi-touch API at all, so pinch/rotate arrive from the
//     same darwin bridge via noteGesture;
//   - it reports the drop path list without coordinates, so onFileDrop
//     gets the current cursor position.

// scrollContext is the snapshot a platform bridge takes of the modifier
// and phase state belonging to a scroll event it is about to let GLFW
// process.
//
// It exists purely because GLFW's scroll callback signature carries
// neither: the darwin bridge intercepts scrollWheel:, records the real
// NSEvent state here, then chains into GLFW's own implementation, which
// synchronously triggers the callback that consumes it. valid
// distinguishes "no bridge supplied anything" (fall back to polling) from
// "bridge saw no modifiers".
type scrollContext struct {
	mods  contract.Modifiers
	phase contract.GesturePhase
	valid bool
}

type glfwApp struct{}

func init() { contract.Register("glfw", contract.PriorityPortable, newGLFWApp) }

func newGLFWApp() (contract.App, error) {
	if err := glfw.Init(); err != nil {
		return nil, err
	}
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	return glfwApp{}, nil
}

func (glfwApp) NewWindow(cfg contract.WindowConfig) (contract.Window, error) {
	// GLFW can drop decorations, float a window and even request a
	// transparent framebuffer, but it has no way to stop a window taking
	// keyboard focus — and that is the property an overlay panel exists
	// for. Handing back a focus-stealing window would be worse than
	// failing: see the WindowOverlayPanel doc comment.
	if cfg.Kind == contract.WindowOverlayPanel {
		return nil, contract.ErrOverlayPanelUnsupported
	}
	var share *glfw.Window
	if cfg.Share != nil {
		if gw, ok := cfg.Share.(*glfwWindow); ok {
			share = gw.handle
		}
	}
	handle, err := glfw.CreateWindow(cfg.Width, cfg.Height, cfg.Title, nil, share)
	if err != nil {
		return nil, err
	}
	handle.MakeContextCurrent()
	// vsync: cap redraw rate to display refresh, dropping idle CPU to
	// near zero.
	glfw.SwapInterval(1)
	w := &glfwWindow{handle: handle}
	// Report Caps Lock / Num Lock in key callbacks' mods, so KeyEvent
	// can carry the Caps Lock state.
	handle.SetInputMode(glfw.LockKeyMods, glfw.True)
	w.installCallbacks()
	// Native gestures (and exact scroll modifiers/phase) come from a
	// platform bridge that patches GLFW's own view class — see
	// platform_glfw_gesture_darwin.m. No-op where absent.
	w.installGestures()
	return w, nil
}

func (glfwApp) PumpEvents(timeout time.Duration) {
	if timeout == contract.PumpForever {
		glfw.WaitEvents()
		return
	}
	if timeout > 0 {
		glfw.WaitEventsTimeout(timeout.Seconds())
		return
	}
	glfw.PollEvents()
}

func (glfwApp) Wake() { glfw.PostEmptyEvent() }

func (glfwApp) Monitors() []contract.Monitor {
	handles := glfw.GetMonitors()
	out := make([]contract.Monitor, 0, len(handles))
	for _, m := range handles {
		if m != nil {
			out = append(out, glfwMonitor{m})
		}
	}
	return out
}

func (glfwApp) PrimaryMonitor() contract.Monitor {
	m := glfw.GetPrimaryMonitor()
	if m == nil {
		return nil
	}
	return glfwMonitor{m}
}

// --- monitor ---------------------------------------------------------

type glfwMonitor struct{ handle *glfw.Monitor }

func (m glfwMonitor) Name() string { return m.handle.GetName() }

func (m glfwMonitor) Position() (int, int) { return m.handle.GetPos() }

func (m glfwMonitor) VideoMode() (int, int, int) {
	mode := m.handle.GetVideoMode()
	if mode == nil {
		return 0, 0, 0
	}
	return mode.Width, mode.Height, mode.RefreshRate
}

func (m glfwMonitor) WorkArea() (int, int, int, int) { return m.handle.GetWorkarea() }

func (m glfwMonitor) ContentScale() (float32, float32) { return m.handle.GetContentScale() }

// --- window ----------------------------------------------------------

type glfwWindow struct {
	handle  *glfw.Window
	handler contract.Handler
	cursors map[contract.CursorShape]*glfw.Cursor
	// cursorHidden mirrors GLFW's cursor mode (CursorHidden is a mode).
	cursorHidden bool
	// capsOn is the Caps Lock state from the last key callback's lock
	// modifiers (GLFW_LOCK_KEY_MODS is enabled in newWindow).
	capsOn bool
	// pending carries the modifier/phase snapshot the darwin bridge took
	// off the real NSEvent just before GLFW's own scrollWheel: ran. See
	// platform_glfw_gesture_darwin.m and scrollContext.
	pending scrollContext
}

func (w *glfwWindow) SetHandler(h contract.Handler) { w.handler = h }

// setVisible ignores activate: GLFW always focuses on Show. That is
// precisely why this backend declines overlay panels in newWindow, so the
// only callers here are normal windows, which want the focus anyway.
func (w *glfwWindow) SetVisible(visible, activate bool) {
	if visible {
		w.handle.Show()
		return
	}
	w.handle.Hide()
}

func (w *glfwWindow) IsIconified() bool {
	return w.handle.GetAttrib(glfw.Iconified) == glfw.True
}

// isFocused reports whether the window has keyboard focus.
func (w *glfwWindow) IsFocused() bool {
	return w.handle.GetAttrib(glfw.Focused) == glfw.True
}

func (w *glfwWindow) IsVisible() bool {
	return w.handle.GetAttrib(glfw.Visible) == glfw.True
}

func (w *glfwWindow) Caps() contract.Caps {
	return contract.Caps{
		AbsolutePosition: true,
		// GLFW always asks for a decorated window, and every platform it
		// supports honors that.
		ServerDecorations: true,
		// True only where the darwin bridge is compiled in; see
		// gesture_darwin.go / gesture_other.go.
		NativeGestures:            glfwNativeGestures,
		ClipboardNeedsInputSerial: false,
		// No non-activating window style in GLFW — newWindow declines
		// WindowOverlayPanel outright.
		OverlayPanels: false,
		GPUContext:    true,
	}
}

func (w *glfwWindow) Size() (int, int)                 { return w.handle.GetSize() }
func (w *glfwWindow) FramebufferSize() (int, int)      { return w.handle.GetFramebufferSize() }
func (w *glfwWindow) ContentScale() (float32, float32) { return w.handle.GetContentScale() }

func (w *glfwWindow) Pos() (int, int, bool) {
	x, y := w.handle.GetPos()
	return x, y, true
}

func (w *glfwWindow) SetPos(x, y int) bool {
	w.handle.SetPos(x, y)
	return true
}

func (w *glfwWindow) SetTitle(title string) { w.handle.SetTitle(title) }

// setSize goes through GLFW's SetSize, which resizes about the window's
// existing top-left on every platform GLFW supports — the anchor the seam
// specifies.
func (w *glfwWindow) SetSize(width, height int) { w.handle.SetSize(width, height) }

func (w *glfwWindow) SetSizeLimits(minW, minH, maxW, maxH int) {
	limit := func(v int) int {
		if v <= 0 {
			return glfw.DontCare
		}
		return v
	}
	w.handle.SetSizeLimits(limit(minW), limit(minH), limit(maxW), limit(maxH))
}

func (w *glfwWindow) Iconify()  { w.handle.Iconify() }
func (w *glfwWindow) Maximize() { w.handle.Maximize() }
func (w *glfwWindow) Restore()  { w.handle.Restore() }
func (w *glfwWindow) Focus()    { w.handle.Focus() }

func (w *glfwWindow) SetFullscreen(m contract.Monitor, x, y, width, height, refreshHz int) {
	var handle *glfw.Monitor
	if gm, ok := m.(glfwMonitor); ok {
		handle = gm.handle
	}
	rate := refreshHz
	if rate <= 0 {
		rate = glfw.DontCare
	}
	w.handle.SetMonitor(handle, x, y, width, height, rate)
}

func (w *glfwWindow) ShouldClose() bool     { return w.handle.ShouldClose() }
func (w *glfwWindow) SetShouldClose(v bool) { w.handle.SetShouldClose(v) }

func (w *glfwWindow) Destroy() {
	w.handle.Destroy()
	w.handle = nil
}

func (w *glfwWindow) CursorPos() (float64, float64) { return w.handle.GetCursorPos() }

func (w *glfwWindow) SetCursor(shape contract.CursorShape) {
	// Hidden is a cursor MODE in GLFW, not an image: the pointer vanishes
	// over the content area and comes back when it leaves the window.
	if shape == contract.CursorHidden {
		if !w.cursorHidden {
			w.handle.SetInputMode(glfw.CursorMode, glfw.CursorHidden)
			w.cursorHidden = true
		}
		return
	}
	if w.cursorHidden {
		w.handle.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
		w.cursorHidden = false
	}
	if w.cursors == nil {
		w.cursors = map[contract.CursorShape]*glfw.Cursor{}
	}
	cur, ok := w.cursors[shape]
	if !ok {
		if img, hx, hy, ok := contract.Host.ImageCursor(shape); ok {
			cur = glfwImageCursor(shape, img, hx, hy)
		} else {
			cur = glfw.CreateStandardCursor(glfwCursorShape(shape))
		}
		if cur == nil {
			cur = glfw.CreateStandardCursor(glfw.ArrowCursor)
		}
		w.cursors[shape] = cur
	}
	w.handle.SetCursor(cur)
}

// ReleaseCursor implements contract.CursorReleaser: GLFW cursors are
// created per window, so a released image cursor is destroyed here.
// glfwDestroyCursor resets any window still showing it to the arrow.
func (w *glfwWindow) ReleaseCursor(shape contract.CursorShape) {
	if cur, ok := w.cursors[shape]; ok {
		delete(w.cursors, shape)
		if cur != nil {
			cur.Destroy()
		}
	}
}

func glfwCursorShape(shape contract.CursorShape) glfw.StandardCursor {
	switch shape {
	case contract.CursorText:
		return glfw.IBeamCursor
	case contract.CursorCrosshair:
		return glfw.CrosshairCursor
	case contract.CursorHand:
		return glfw.HandCursor
	case contract.CursorResizeEW:
		return glfw.HResizeCursor
	case contract.CursorResizeNS:
		return glfw.VResizeCursor
	default:
		return glfw.ArrowCursor
	}
}

// currentMods polls the live modifier state. GLFW reports modifiers with
// key and button events but not with motion or scroll, and scroll needs
// them to decide zoom-versus-scroll — so poll rather than trust a value
// tracked from the last keystroke, which a wheel turn can outlive.
// capsLock implements platformLockState.
func (w *glfwWindow) CapsLock() bool { return w.capsOn }

func (w *glfwWindow) CurrentMods() contract.Modifiers {
	pressed := func(keys ...glfw.Key) bool {
		for _, k := range keys {
			if w.handle.GetKey(k) == glfw.Press {
				return true
			}
		}
		return false
	}
	var mods contract.Modifiers
	if pressed(glfw.KeyLeftShift, glfw.KeyRightShift) {
		mods |= contract.ModShift
	}
	if pressed(glfw.KeyLeftControl, glfw.KeyRightControl) {
		mods |= contract.ModControl
	}
	if pressed(glfw.KeyLeftAlt, glfw.KeyRightAlt) {
		mods |= contract.ModAlt
	}
	if pressed(glfw.KeyLeftSuper, glfw.KeyRightSuper) {
		mods |= contract.ModSuper
	}
	return mods
}

// GLFW's clipboard API still takes a window for legacy reasons; the
// underlying OS clipboard is process-wide, so the handle is effectively a
// passkey into the GLFW runtime.
func (w *glfwWindow) ClipboardText() string {
	if w.handle == nil {
		return ""
	}
	return w.handle.GetClipboardString()
}

func (w *glfwWindow) SetClipboardText(text string) {
	if w.handle == nil {
		return
	}
	w.handle.SetClipboardString(text)
}

func (w *glfwWindow) MakeCurrent() { w.handle.MakeContextCurrent() }

func (w *glfwWindow) Surface() contract.Surface { return glfwSurface{win: w} }

// wake is documented goroutine-safe by GLFW: it posts one event into the
// platform queue, unblocking the next WaitEvents/WaitEventsTimeout.
func (w *glfwWindow) Wake() { glfw.PostEmptyEvent() }

// glfwSurface is the GPU-only presentation path: GLFW gives us a GL
// context and a buffer swap, and nothing else.
//
// It has no frame-ready signal (GLFW's vsync is inside SwapBuffers, which
// blocks rather than notifying) and no CPU path — presenting an
// *image.RGBA through GLFW would mean uploading it as a texture, which is
// exactly what the GL renderer already does. Both therefore decline, and
// the engine keeps its timeout-driven pacing.
type glfwSurface struct{ win *glfwWindow }

func (glfwSurface) Ready() <-chan struct{}                       { return nil }
func (glfwSurface) PresentCPU(*image.RGBA, image.Rectangle) bool { return false }
func (s glfwSurface) Present()                                   { s.win.handle.SwapBuffers() }

func (w *glfwWindow) NativeWindow() unsafe.Pointer {
	return glfwNativeWindow(w.handle)
}

// --- input plumbing --------------------------------------------------

// noteScrollContext records what a platform bridge observed on the real
// scroll event it is about to let GLFW process. See scrollContext.
func (w *glfwWindow) noteScrollContext(mods contract.Modifiers, phase contract.GesturePhase) {
	w.pending = scrollContext{mods: mods, phase: phase, valid: true}
}

// clearScrollContext drops a snapshot GLFW never turned into a callback
// (it skips _glfwInputScroll when both deltas round to zero) so it can't
// be misattributed to a later scroll.
func (w *glfwWindow) clearScrollContext() { w.pending = scrollContext{} }

// takeScrollContext consumes the snapshot, falling back to polled
// modifiers and an unknown phase where no bridge supplied one.
func (w *glfwWindow) takeScrollContext() (contract.Modifiers, contract.GesturePhase) {
	if w.pending.valid {
		ctx := w.pending
		w.pending = scrollContext{}
		return ctx.mods, ctx.phase
	}
	return w.CurrentMods(), contract.GesturePhaseNone
}

// noteGesture forwards a gesture from a platform bridge to the handler.
func (w *glfwWindow) noteGesture(kind contract.GestureKind, phase contract.GesturePhase, dScale, dRotation float32, mods contract.Modifiers) {
	if w.handler == nil {
		return
	}
	w.handler.OnGesture(kind, phase, dScale, dRotation, mods)
}

func (w *glfwWindow) installCallbacks() {
	h := func() contract.Handler { return w.handler }

	w.handle.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		if hh := h(); hh != nil {
			// Motion carries no modifiers of its own; poll so shift-drag
			// range selection sees the real state.
			hh.OnMouseMove(x, y, w.CurrentMods())
		}
	})

	w.handle.SetMouseButtonCallback(func(_ *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		if hh := h(); hh != nil {
			hh.OnMouseButton(mapMouseButton(button), action != glfw.Release, mapMods(mods))
		}
	})

	w.handle.SetScrollCallback(func(_ *glfw.Window, xoff, yoff float64) {
		hh := h()
		if hh == nil {
			return
		}
		mods, phase := w.takeScrollContext()
		x, y := w.handle.GetCursorPos()
		hh.OnScroll(x, y, xoff, yoff, mods, phase, phase != contract.GesturePhaseNone)
	})

	w.handle.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		hh := h()
		if hh == nil {
			return
		}
		down := action == glfw.Press || action == glfw.Repeat
		w.capsOn = mods&glfw.ModCapsLock != 0
		// GLFW reports Caps Lock as a press when it turns on and a release
		// when it turns off (from flagsChanged on macOS) — the documented
		// KeyCapsLock contract. The lock bit in mods lags that event, so
		// take the state from the action itself.
		if key == glfw.KeyCapsLock {
			w.capsOn = down
		}
		hh.OnKey(mapKey(key), scancode, down, action == glfw.Repeat, mapMods(mods))
	})

	w.handle.SetCharCallback(func(_ *glfw.Window, char rune) {
		if hh := h(); hh != nil {
			hh.OnChar(char)
		}
	})

	w.handle.SetFocusCallback(func(_ *glfw.Window, focused bool) {
		if hh := h(); hh != nil {
			hh.OnFocus(focused)
		}
	})

	w.handle.SetSizeCallback(func(_ *glfw.Window, width, height int) {
		if hh := h(); hh != nil {
			hh.OnResize(width, height)
		}
	})

	w.handle.SetFramebufferSizeCallback(func(_ *glfw.Window, width, height int) {
		if hh := h(); hh != nil {
			hh.OnFramebufferResize(width, height)
		}
	})

	w.handle.SetRefreshCallback(func(_ *glfw.Window) {
		if hh := h(); hh != nil {
			hh.OnRefresh()
		}
	})

	// State the loop polls (OnMove / OnMinimize, close requests,
	// fullscreen) can change from inside an idle wait with no input event
	// of its own — a Dock minimize, Dock "Quit", a fullscreen animation
	// settling. Cut the wait short so the loop notices.
	w.handle.SetCloseCallback(func(*glfw.Window) { contract.Host.WakeIfBlocked() })
	w.handle.SetIconifyCallback(func(*glfw.Window, bool) { contract.Host.WakeIfBlocked() })
	w.handle.SetMaximizeCallback(func(*glfw.Window, bool) { contract.Host.WakeIfBlocked() })
	w.handle.SetPosCallback(func(*glfw.Window, int, int) { contract.Host.WakeIfBlocked() })
	w.handle.SetContentScaleCallback(func(*glfw.Window, float32, float32) { contract.Host.WakeIfBlocked() })
}

// setDropEnabled wires (or clears) the OS file-drop callback. GLFW
// reports the paths without coordinates, so the current cursor position
// stands in for the drop point.
func (w *glfwWindow) SetDropEnabled(enabled bool) {
	if !enabled {
		w.handle.SetDropCallback(nil)
		return
	}
	w.handle.SetDropCallback(func(_ *glfw.Window, names []string) {
		if hh := w.handler; hh != nil {
			x, y := w.handle.GetCursorPos()
			hh.OnFileDrop(names, x, y)
		}
	})
}

// --- GLFW → engine enum translation --------------------------------

func mapMouseButton(button glfw.MouseButton) contract.MouseButton {
	switch button {
	case glfw.MouseButtonRight:
		return contract.MouseButtonRight
	case glfw.MouseButtonMiddle:
		return contract.MouseButtonMiddle
	default:
		return contract.MouseButtonLeft
	}
}

func mapKey(key glfw.Key) contract.Key {
	switch key {
	case glfw.KeySpace:
		return contract.KeySpace
	case glfw.KeyEnter:
		return contract.KeyEnter
	case glfw.KeyEscape:
		return contract.KeyEscape
	case glfw.KeyLeft:
		return contract.KeyLeft
	case glfw.KeyRight:
		return contract.KeyRight
	case glfw.KeyUp:
		return contract.KeyUp
	case glfw.KeyDown:
		return contract.KeyDown
	case glfw.KeyHome:
		return contract.KeyHome
	case glfw.KeyEnd:
		return contract.KeyEnd
	case glfw.KeyBackspace:
		return contract.KeyBackspace
	case glfw.KeyDelete:
		return contract.KeyDelete
	case glfw.KeyInsert:
		return contract.KeyInsert
	case glfw.KeyPageUp:
		return contract.KeyPageUp
	case glfw.KeyPageDown:
		return contract.KeyPageDown
	case glfw.KeyF1:
		return contract.KeyF1
	case glfw.KeyF2:
		return contract.KeyF2
	case glfw.KeyF3:
		return contract.KeyF3
	case glfw.KeyF4:
		return contract.KeyF4
	case glfw.KeyF5:
		return contract.KeyF5
	case glfw.KeyF6:
		return contract.KeyF6
	case glfw.KeyF7:
		return contract.KeyF7
	case glfw.KeyF8:
		return contract.KeyF8
	case glfw.KeyF9:
		return contract.KeyF9
	case glfw.KeyF10:
		return contract.KeyF10
	case glfw.KeyF11:
		return contract.KeyF11
	case glfw.KeyF12:
		return contract.KeyF12
	case glfw.KeyTab:
		return contract.KeyTab
	case glfw.KeyA:
		return contract.KeyA
	case glfw.KeyB:
		return contract.KeyB
	case glfw.KeyC:
		return contract.KeyC
	case glfw.KeyD:
		return contract.KeyD
	case glfw.KeyE:
		return contract.KeyE
	case glfw.KeyF:
		return contract.KeyF
	case glfw.KeyG:
		return contract.KeyG
	case glfw.KeyH:
		return contract.KeyH
	case glfw.KeyI:
		return contract.KeyI
	case glfw.KeyJ:
		return contract.KeyJ
	case glfw.KeyK:
		return contract.KeyK
	case glfw.KeyL:
		return contract.KeyL
	case glfw.KeyM:
		return contract.KeyM
	case glfw.KeyN:
		return contract.KeyN
	case glfw.KeyO:
		return contract.KeyO
	case glfw.KeyP:
		return contract.KeyP
	case glfw.KeyQ:
		return contract.KeyQ
	case glfw.KeyR:
		return contract.KeyR
	case glfw.KeyS:
		return contract.KeyS
	case glfw.KeyT:
		return contract.KeyT
	case glfw.KeyU:
		return contract.KeyU
	case glfw.KeyV:
		return contract.KeyV
	case glfw.KeyW:
		return contract.KeyW
	case glfw.KeyX:
		return contract.KeyX
	case glfw.KeyY:
		return contract.KeyY
	case glfw.KeyZ:
		return contract.KeyZ
	case glfw.Key0:
		return contract.Key0
	case glfw.Key1:
		return contract.Key1
	case glfw.Key2:
		return contract.Key2
	case glfw.Key3:
		return contract.Key3
	case glfw.Key4:
		return contract.Key4
	case glfw.Key5:
		return contract.Key5
	case glfw.Key6:
		return contract.Key6
	case glfw.Key7:
		return contract.Key7
	case glfw.Key8:
		return contract.Key8
	case glfw.Key9:
		return contract.Key9
	case glfw.KeyMinus:
		return contract.KeyMinus
	case glfw.KeyEqual:
		return contract.KeyEqual
	case glfw.KeySlash:
		return contract.KeySlash
	case glfw.KeyComma:
		return contract.KeyComma
	case glfw.KeyPeriod:
		return contract.KeyPeriod
	case glfw.KeySemicolon:
		return contract.KeySemicolon
	case glfw.KeyApostrophe:
		return contract.KeyApostrophe
	case glfw.KeyLeftBracket:
		return contract.KeyLeftBracket
	case glfw.KeyRightBracket:
		return contract.KeyRightBracket
	case glfw.KeyBackslash:
		return contract.KeyBackslash
	case glfw.KeyGraveAccent:
		return contract.KeyGrave
	case glfw.KeyLeftShift:
		return contract.KeyLeftShift
	case glfw.KeyRightShift:
		return contract.KeyRightShift
	case glfw.KeyLeftControl:
		return contract.KeyLeftControl
	case glfw.KeyRightControl:
		return contract.KeyRightControl
	case glfw.KeyLeftAlt:
		return contract.KeyLeftAlt
	case glfw.KeyRightAlt:
		return contract.KeyRightAlt
	case glfw.KeyLeftSuper:
		return contract.KeyLeftSuper
	case glfw.KeyRightSuper:
		return contract.KeyRightSuper
	case glfw.KeyCapsLock:
		return contract.KeyCapsLock
	case glfw.KeyMenu:
		return contract.KeyMenu
	case glfw.KeyNumLock:
		return contract.KeyNumLock
	case glfw.KeyKPDecimal:
		return contract.KeyKPDecimal
	case glfw.KeyKPDivide:
		return contract.KeyKPDivide
	case glfw.KeyKPMultiply:
		return contract.KeyKPMultiply
	case glfw.KeyKPSubtract:
		return contract.KeyKPSubtract
	case glfw.KeyKPAdd:
		return contract.KeyKPAdd
	case glfw.KeyKPEqual:
		return contract.KeyKPEqual
	case glfw.KeyKPEnter:
		return contract.KeyEnter
	}
	switch {
	case key >= glfw.KeyF13 && key <= glfw.KeyF20:
		return contract.KeyF13 + contract.Key(key-glfw.KeyF13)
	case key >= glfw.KeyKP0 && key <= glfw.KeyKP9:
		return contract.KeyKP0 + contract.Key(key-glfw.KeyKP0)
	}
	return contract.KeyUnknown
}

func mapMods(mods glfw.ModifierKey) contract.Modifiers {
	var out contract.Modifiers
	if mods&glfw.ModShift != 0 {
		out |= contract.ModShift
	}
	if mods&glfw.ModControl != 0 {
		out |= contract.ModControl
	}
	if mods&glfw.ModAlt != 0 {
		out |= contract.ModAlt
	}
	if mods&glfw.ModSuper != 0 {
		out |= contract.ModSuper
	}
	return out
}
