package qui

import (
	"image"
	"time"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
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
	mods  Modifiers
	phase GesturePhase
	valid bool
}

type glfwApp struct{}

func init() { registerPlatformBackend("glfw", newGLFWApp) }

func newGLFWApp() (platformApp, error) {
	if err := glfw.Init(); err != nil {
		return nil, err
	}
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	return glfwApp{}, nil
}

func (glfwApp) newWindow(cfg platformWindowConfig) (platformWindow, error) {
	// GLFW can drop decorations, float a window and even request a
	// transparent framebuffer, but it has no way to stop a window taking
	// keyboard focus — and that is the property an overlay panel exists
	// for. Handing back a focus-stealing window would be worse than
	// failing: see the WindowOverlayPanel doc comment.
	if cfg.Kind == WindowOverlayPanel {
		return nil, ErrOverlayPanelUnsupported
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
	w.installCallbacks()
	// Native gestures (and exact scroll modifiers/phase) come from a
	// platform bridge that patches GLFW's own view class — see
	// platform_glfw_gesture_darwin.m. No-op where absent.
	w.installGestures()
	return w, nil
}

func (glfwApp) pumpEvents(timeout time.Duration) {
	if timeout > 0 {
		glfw.WaitEventsTimeout(timeout.Seconds())
		return
	}
	glfw.PollEvents()
}

func (glfwApp) wake() { glfw.PostEmptyEvent() }

func (glfwApp) monitors() []platformMonitor {
	handles := glfw.GetMonitors()
	out := make([]platformMonitor, 0, len(handles))
	for _, m := range handles {
		if m != nil {
			out = append(out, glfwMonitor{m})
		}
	}
	return out
}

func (glfwApp) primaryMonitor() platformMonitor {
	m := glfw.GetPrimaryMonitor()
	if m == nil {
		return nil
	}
	return glfwMonitor{m}
}

// --- monitor ---------------------------------------------------------

type glfwMonitor struct{ handle *glfw.Monitor }

func (m glfwMonitor) name() string { return m.handle.GetName() }

func (m glfwMonitor) position() (int, int) { return m.handle.GetPos() }

func (m glfwMonitor) videoMode() (int, int, int) {
	mode := m.handle.GetVideoMode()
	if mode == nil {
		return 0, 0, 0
	}
	return mode.Width, mode.Height, mode.RefreshRate
}

func (m glfwMonitor) workArea() (int, int, int, int) { return m.handle.GetWorkarea() }

func (m glfwMonitor) contentScale() (float32, float32) { return m.handle.GetContentScale() }

// --- window ----------------------------------------------------------

type glfwWindow struct {
	handle  *glfw.Window
	handler platformHandler
	cursors map[CursorShape]*glfw.Cursor
	// pending carries the modifier/phase snapshot the darwin bridge took
	// off the real NSEvent just before GLFW's own scrollWheel: ran. See
	// platform_glfw_gesture_darwin.m and scrollContext.
	pending scrollContext
}

func (w *glfwWindow) setHandler(h platformHandler) { w.handler = h }

// setVisible ignores activate: GLFW always focuses on Show. That is
// precisely why this backend declines overlay panels in newWindow, so the
// only callers here are normal windows, which want the focus anyway.
func (w *glfwWindow) setVisible(visible, activate bool) {
	if visible {
		w.handle.Show()
		return
	}
	w.handle.Hide()
}

func (w *glfwWindow) isIconified() bool {
	return w.handle.GetAttrib(glfw.Iconified) == glfw.True
}

func (w *glfwWindow) isVisible() bool {
	return w.handle.GetAttrib(glfw.Visible) == glfw.True
}

func (w *glfwWindow) caps() platformCaps {
	return platformCaps{
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
	}
}

func (w *glfwWindow) size() (int, int)                 { return w.handle.GetSize() }
func (w *glfwWindow) framebufferSize() (int, int)      { return w.handle.GetFramebufferSize() }
func (w *glfwWindow) contentScale() (float32, float32) { return w.handle.GetContentScale() }

func (w *glfwWindow) pos() (int, int, bool) {
	x, y := w.handle.GetPos()
	return x, y, true
}

func (w *glfwWindow) setPos(x, y int) bool {
	w.handle.SetPos(x, y)
	return true
}

func (w *glfwWindow) setTitle(title string) { w.handle.SetTitle(title) }

// setSize goes through GLFW's SetSize, which resizes about the window's
// existing top-left on every platform GLFW supports — the anchor the seam
// specifies.
func (w *glfwWindow) setSize(width, height int) { w.handle.SetSize(width, height) }

func (w *glfwWindow) setSizeLimits(minW, minH, maxW, maxH int) {
	limit := func(v int) int {
		if v <= 0 {
			return glfw.DontCare
		}
		return v
	}
	w.handle.SetSizeLimits(limit(minW), limit(minH), limit(maxW), limit(maxH))
}

func (w *glfwWindow) iconify()  { w.handle.Iconify() }
func (w *glfwWindow) maximize() { w.handle.Maximize() }
func (w *glfwWindow) restore()  { w.handle.Restore() }
func (w *glfwWindow) focus()    { w.handle.Focus() }

func (w *glfwWindow) setFullscreen(m platformMonitor, x, y, width, height, refreshHz int) {
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

func (w *glfwWindow) shouldClose() bool     { return w.handle.ShouldClose() }
func (w *glfwWindow) setShouldClose(v bool) { w.handle.SetShouldClose(v) }

func (w *glfwWindow) destroy() {
	w.handle.Destroy()
	w.handle = nil
}

func (w *glfwWindow) cursorPos() (float64, float64) { return w.handle.GetCursorPos() }

func (w *glfwWindow) setCursor(shape CursorShape) {
	if w.cursors == nil {
		w.cursors = map[CursorShape]*glfw.Cursor{}
	}
	cur, ok := w.cursors[shape]
	if !ok {
		cur = glfw.CreateStandardCursor(glfwCursorShape(shape))
		w.cursors[shape] = cur
	}
	w.handle.SetCursor(cur)
}

func glfwCursorShape(shape CursorShape) glfw.StandardCursor {
	switch shape {
	case CursorText:
		return glfw.IBeamCursor
	case CursorCrosshair:
		return glfw.CrosshairCursor
	case CursorHand:
		return glfw.HandCursor
	case CursorResizeEW:
		return glfw.HResizeCursor
	case CursorResizeNS:
		return glfw.VResizeCursor
	default:
		return glfw.ArrowCursor
	}
}

// currentMods polls the live modifier state. GLFW reports modifiers with
// key and button events but not with motion or scroll, and scroll needs
// them to decide zoom-versus-scroll — so poll rather than trust a value
// tracked from the last keystroke, which a wheel turn can outlive.
func (w *glfwWindow) currentMods() Modifiers {
	pressed := func(keys ...glfw.Key) bool {
		for _, k := range keys {
			if w.handle.GetKey(k) == glfw.Press {
				return true
			}
		}
		return false
	}
	var mods Modifiers
	if pressed(glfw.KeyLeftShift, glfw.KeyRightShift) {
		mods |= ModShift
	}
	if pressed(glfw.KeyLeftControl, glfw.KeyRightControl) {
		mods |= ModControl
	}
	if pressed(glfw.KeyLeftAlt, glfw.KeyRightAlt) {
		mods |= ModAlt
	}
	if pressed(glfw.KeyLeftSuper, glfw.KeyRightSuper) {
		mods |= ModSuper
	}
	return mods
}

// GLFW's clipboard API still takes a window for legacy reasons; the
// underlying OS clipboard is process-wide, so the handle is effectively a
// passkey into the GLFW runtime.
func (w *glfwWindow) clipboardText() string {
	if w.handle == nil {
		return ""
	}
	return w.handle.GetClipboardString()
}

func (w *glfwWindow) setClipboardText(text string) {
	if w.handle == nil {
		return
	}
	w.handle.SetClipboardString(text)
}

func (w *glfwWindow) makeCurrent() { w.handle.MakeContextCurrent() }

func (w *glfwWindow) surface() platformSurface { return glfwSurface{win: w} }

// wake is documented goroutine-safe by GLFW: it posts one event into the
// platform queue, unblocking the next WaitEvents/WaitEventsTimeout.
func (w *glfwWindow) wake() { glfw.PostEmptyEvent() }

// glfwSurface is the GPU-only presentation path: GLFW gives us a GL
// context and a buffer swap, and nothing else.
//
// It has no frame-ready signal (GLFW's vsync is inside SwapBuffers, which
// blocks rather than notifying) and no CPU path — presenting an
// *image.RGBA through GLFW would mean uploading it as a texture, which is
// exactly what the GL renderer already does. Both therefore decline, and
// the engine keeps its timeout-driven pacing.
type glfwSurface struct{ win *glfwWindow }

func (glfwSurface) ready() <-chan struct{}      { return nil }
func (glfwSurface) presentCPU(*image.RGBA) bool { return false }
func (s glfwSurface) present()                  { s.win.handle.SwapBuffers() }

func (w *glfwWindow) nativeWindow() unsafe.Pointer {
	return glfwNativeWindow(w.handle)
}

// --- input plumbing --------------------------------------------------

// noteScrollContext records what a platform bridge observed on the real
// scroll event it is about to let GLFW process. See scrollContext.
func (w *glfwWindow) noteScrollContext(mods Modifiers, phase GesturePhase) {
	w.pending = scrollContext{mods: mods, phase: phase, valid: true}
}

// clearScrollContext drops a snapshot GLFW never turned into a callback
// (it skips _glfwInputScroll when both deltas round to zero) so it can't
// be misattributed to a later scroll.
func (w *glfwWindow) clearScrollContext() { w.pending = scrollContext{} }

// takeScrollContext consumes the snapshot, falling back to polled
// modifiers and an unknown phase where no bridge supplied one.
func (w *glfwWindow) takeScrollContext() (Modifiers, GesturePhase) {
	if w.pending.valid {
		ctx := w.pending
		w.pending = scrollContext{}
		return ctx.mods, ctx.phase
	}
	return w.currentMods(), GesturePhaseNone
}

// noteGesture forwards a gesture from a platform bridge to the handler.
func (w *glfwWindow) noteGesture(kind EventType, phase GesturePhase, dScale, dRotation float32, mods Modifiers) {
	if w.handler == nil {
		return
	}
	w.handler.onGesture(kind, phase, dScale, dRotation, mods)
}

func (w *glfwWindow) installCallbacks() {
	h := func() platformHandler { return w.handler }

	w.handle.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		if hh := h(); hh != nil {
			// Motion carries no modifiers of its own; poll so shift-drag
			// range selection sees the real state.
			hh.onMouseMove(x, y, w.currentMods())
		}
	})

	w.handle.SetMouseButtonCallback(func(_ *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		if hh := h(); hh != nil {
			hh.onMouseButton(mapMouseButton(button), action != glfw.Release, mapMods(mods))
		}
	})

	w.handle.SetScrollCallback(func(_ *glfw.Window, xoff, yoff float64) {
		hh := h()
		if hh == nil {
			return
		}
		mods, phase := w.takeScrollContext()
		x, y := w.handle.GetCursorPos()
		hh.onScroll(x, y, xoff, yoff, mods, phase)
	})

	w.handle.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		hh := h()
		if hh == nil {
			return
		}
		down := action == glfw.Press || action == glfw.Repeat
		hh.onKey(mapKey(key), scancode, down, action == glfw.Repeat, mapMods(mods))
	})

	w.handle.SetCharCallback(func(_ *glfw.Window, char rune) {
		if hh := h(); hh != nil {
			hh.onChar(char)
		}
	})

	w.handle.SetFocusCallback(func(_ *glfw.Window, focused bool) {
		if hh := h(); hh != nil {
			hh.onFocus(focused)
		}
	})

	w.handle.SetSizeCallback(func(_ *glfw.Window, width, height int) {
		if hh := h(); hh != nil {
			hh.onResize(width, height)
		}
	})

	w.handle.SetFramebufferSizeCallback(func(_ *glfw.Window, width, height int) {
		if hh := h(); hh != nil {
			hh.onFramebufferResize(width, height)
		}
	})

	w.handle.SetRefreshCallback(func(_ *glfw.Window) {
		if hh := h(); hh != nil {
			hh.onRefresh()
		}
	})
}

// setDropEnabled wires (or clears) the OS file-drop callback. GLFW
// reports the paths without coordinates, so the current cursor position
// stands in for the drop point.
func (w *glfwWindow) setDropEnabled(enabled bool) {
	if !enabled {
		w.handle.SetDropCallback(nil)
		return
	}
	w.handle.SetDropCallback(func(_ *glfw.Window, names []string) {
		if hh := w.handler; hh != nil {
			x, y := w.handle.GetCursorPos()
			hh.onFileDrop(names, x, y)
		}
	})
}

// --- GLFW → engine enum translation --------------------------------

func mapMouseButton(button glfw.MouseButton) MouseButton {
	switch button {
	case glfw.MouseButtonRight:
		return MouseButtonRight
	case glfw.MouseButtonMiddle:
		return MouseButtonMiddle
	default:
		return MouseButtonLeft
	}
}

func mapKey(key glfw.Key) Key {
	switch key {
	case glfw.KeySpace:
		return KeySpace
	case glfw.KeyEnter:
		return KeyEnter
	case glfw.KeyEscape:
		return KeyEscape
	case glfw.KeyLeft:
		return KeyLeft
	case glfw.KeyRight:
		return KeyRight
	case glfw.KeyUp:
		return KeyUp
	case glfw.KeyDown:
		return KeyDown
	case glfw.KeyHome:
		return KeyHome
	case glfw.KeyEnd:
		return KeyEnd
	case glfw.KeyBackspace:
		return KeyBackspace
	case glfw.KeyDelete:
		return KeyDelete
	case glfw.KeyInsert:
		return KeyInsert
	case glfw.KeyPageUp:
		return KeyPageUp
	case glfw.KeyPageDown:
		return KeyPageDown
	case glfw.KeyF1:
		return KeyF1
	case glfw.KeyF2:
		return KeyF2
	case glfw.KeyF3:
		return KeyF3
	case glfw.KeyF4:
		return KeyF4
	case glfw.KeyF5:
		return KeyF5
	case glfw.KeyF6:
		return KeyF6
	case glfw.KeyF7:
		return KeyF7
	case glfw.KeyF8:
		return KeyF8
	case glfw.KeyF9:
		return KeyF9
	case glfw.KeyF10:
		return KeyF10
	case glfw.KeyF11:
		return KeyF11
	case glfw.KeyF12:
		return KeyF12
	case glfw.KeyTab:
		return KeyTab
	case glfw.KeyA:
		return KeyA
	case glfw.KeyB:
		return KeyB
	case glfw.KeyC:
		return KeyC
	case glfw.KeyD:
		return KeyD
	case glfw.KeyE:
		return KeyE
	case glfw.KeyF:
		return KeyF
	case glfw.KeyG:
		return KeyG
	case glfw.KeyH:
		return KeyH
	case glfw.KeyI:
		return KeyI
	case glfw.KeyJ:
		return KeyJ
	case glfw.KeyK:
		return KeyK
	case glfw.KeyL:
		return KeyL
	case glfw.KeyM:
		return KeyM
	case glfw.KeyN:
		return KeyN
	case glfw.KeyO:
		return KeyO
	case glfw.KeyP:
		return KeyP
	case glfw.KeyQ:
		return KeyQ
	case glfw.KeyR:
		return KeyR
	case glfw.KeyS:
		return KeyS
	case glfw.KeyT:
		return KeyT
	case glfw.KeyU:
		return KeyU
	case glfw.KeyV:
		return KeyV
	case glfw.KeyW:
		return KeyW
	case glfw.KeyX:
		return KeyX
	case glfw.KeyY:
		return KeyY
	case glfw.KeyZ:
		return KeyZ
	case glfw.Key0:
		return Key0
	case glfw.Key1:
		return Key1
	case glfw.Key2:
		return Key2
	case glfw.Key3:
		return Key3
	case glfw.Key4:
		return Key4
	case glfw.Key5:
		return Key5
	case glfw.Key6:
		return Key6
	case glfw.Key7:
		return Key7
	case glfw.Key8:
		return Key8
	case glfw.Key9:
		return Key9
	case glfw.KeyMinus:
		return KeyMinus
	case glfw.KeyEqual:
		return KeyEqual
	case glfw.KeySlash:
		return KeySlash
	case glfw.KeyComma:
		return KeyComma
	case glfw.KeyPeriod:
		return KeyPeriod
	case glfw.KeySemicolon:
		return KeySemicolon
	case glfw.KeyApostrophe:
		return KeyApostrophe
	case glfw.KeyLeftBracket:
		return KeyLeftBracket
	case glfw.KeyRightBracket:
		return KeyRightBracket
	case glfw.KeyBackslash:
		return KeyBackslash
	default:
		return KeyUnknown
	}
}

func mapMods(mods glfw.ModifierKey) Modifiers {
	var out Modifiers
	if mods&glfw.ModShift != 0 {
		out |= ModShift
	}
	if mods&glfw.ModControl != 0 {
		out |= ModControl
	}
	if mods&glfw.ModAlt != 0 {
		out |= ModAlt
	}
	if mods&glfw.ModSuper != 0 {
		out |= ModSuper
	}
	return out
}
