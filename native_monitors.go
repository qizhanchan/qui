package qui

// Monitor describes a connected display. Multi-window apps use the list
// to place windows deliberately — center on the primary display, open a
// second window on an external monitor, or clamp a restored position to a
// display that still exists.
//
// All rectangles are in the virtual-desktop LOGICAL coordinate space
// (same units as Window.Position / Window.SetPosition), so a window can be
// positioned directly against them without DPI conversion.
type Monitor struct {
	// plat is the underlying platform monitor, needed for fullscreen.
	// Kept unexported; pass the Monitor value to Window.SetFullscreen.
	plat platformMonitor

	// Name is the human-readable display name (e.g. "Built-in Retina Display").
	Name string
	// Position is the monitor's top-left corner in the virtual desktop.
	Position Point
	// Size is the monitor's current video-mode resolution.
	Size Size
	// WorkArea is the usable region excluding OS chrome (menu bar, dock,
	// taskbar). Prefer this over Position/Size when centering windows.
	WorkArea Rect
	// ContentScale is the DPI scale factor of the display (1.0 standard,
	// 2.0 on a Retina panel). X and Y can differ on unusual setups.
	ContentScale Point
}

// monitorFrom builds a Monitor snapshot from a platform monitor. Must run
// on the main goroutine (display queries are main-thread only on every
// platform we target).
func monitorFrom(m platformMonitor) Monitor {
	if m == nil {
		return Monitor{}
	}
	out := Monitor{plat: m, Name: m.name()}
	px, py := m.position()
	out.Position = Point{X: float32(px), Y: float32(py)}
	if mw, mh, _ := m.videoMode(); mw > 0 && mh > 0 {
		out.Size = Size{W: float32(mw), H: float32(mh)}
	}
	wx, wy, ww, wh := m.workArea()
	out.WorkArea = Rect{X: float32(wx), Y: float32(wy), W: float32(ww), H: float32(wh)}
	sx, sy := m.contentScale()
	out.ContentScale = Point{X: sx, Y: sy}
	return out
}

// Monitors returns a snapshot of all connected displays. The values are a
// point-in-time copy; re-query after a display is plugged/unplugged. Call
// from the main goroutine (e.g. during setup or an event handler).
func Monitors() []Monitor {
	assertProcessUIThread("Monitors")
	plat, err := activePlatform()
	if err != nil {
		return nil
	}
	handles := plat.monitors()
	out := make([]Monitor, 0, len(handles))
	for _, m := range handles {
		out = append(out, monitorFrom(m))
	}
	return out
}

// PrimaryMonitor returns the primary display (the one with the OS menu bar
// / taskbar). The zero Monitor is returned if none is available.
func PrimaryMonitor() Monitor {
	assertProcessUIThread("PrimaryMonitor")
	plat, err := activePlatform()
	if err != nil {
		return Monitor{}
	}
	return monitorFrom(plat.primaryMonitor())
}

// CurrentMonitor returns the display the window currently occupies, decided
// by the window's center point. Falls back to the primary monitor when the
// position is unknown (Wayland) or the center is off every display (mid-drag
// between monitors, or a stale restored position).
func (w *Window) CurrentMonitor() Monitor {
	if w == nil {
		return PrimaryMonitor()
	}
	w.assertUIThread("Window.CurrentMonitor")
	pos, ok := w.PositionOK()
	if !ok {
		return PrimaryMonitor()
	}
	sz := w.WindowSize()
	cx, cy := pos.X+sz.W/2, pos.Y+sz.H/2
	for _, m := range Monitors() {
		r := Rect{X: m.Position.X, Y: m.Position.Y, W: m.Size.W, H: m.Size.H}
		if r.W <= 0 || r.H <= 0 {
			r = m.WorkArea
		}
		if cx >= r.X && cx < r.X+r.W && cy >= r.Y && cy < r.Y+r.H {
			return m
		}
	}
	return PrimaryMonitor()
}

// ContentScale returns the window's current DPI scale factor (the ratio of
// physical to logical pixels), tracking the monitor it currently occupies.
// Unlike DevicePixelRatio — which is derived from the framebuffer/size
// ratio — this reads the platform's per-window content scale directly and
// is the value to persist for restoring layouts across displays. Returns
// {1,1} when the window has no platform backend yet.
//
// Not necessarily an integer: Windows allows arbitrary scale percentages
// and Wayland has fractional scaling, so don't round this to 1/2/3.
func (w *Window) ContentScale() Point {
	if w == nil {
		return Point{X: 1, Y: 1}
	}
	w.assertUIThread("Window.ContentScale")
	if w.plat == nil {
		return Point{X: 1, Y: 1}
	}
	x, y := w.plat.contentScale()
	return Point{X: x, Y: y}
}

// CenterOn places the window in the middle of the given monitor's work
// area. Pass the zero Monitor (or one from a disconnected display) and it
// falls back to the primary monitor.
func (w *Window) CenterOn(m Monitor) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.CenterOn")
	if w.plat == nil {
		return
	}
	if m.plat == nil {
		m = PrimaryMonitor()
	}
	area := m.WorkArea
	if area.W <= 0 || area.H <= 0 {
		return
	}
	// WindowSize, not Size: this positions the OS window against a
	// monitor's work area, so it needs the window's own logical size —
	// the content viewport shrinks under zoom and would mis-center.
	sz := w.WindowSize()
	x := area.X + (area.W-sz.W)/2
	y := area.Y + (area.H-sz.H)/2
	// Ignores failure by design: on a platform without absolute
	// positioning (Wayland) there is nothing to fall back to — the
	// compositor places the window and centering is not expressible.
	w.plat.setPos(int(x), int(y))
}

// Maximize enlarges the window to fill its monitor's work area (keeping
// the title bar). Reverse with Restore.
func (w *Window) Maximize() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Maximize")
	if w.plat == nil {
		return
	}
	w.plat.maximize()
}

// Iconify minimizes the window to the dock / taskbar. Reverse with Restore.
func (w *Window) Iconify() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Iconify")
	if w.plat == nil {
		return
	}
	w.plat.iconify()
}

// Restore returns a maximized or minimized window to its previous
// floating size and position.
func (w *Window) Restore() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Restore")
	if w.plat == nil {
		return
	}
	w.plat.restore()
}

// SetFullscreen / IsFullscreen are platform-split (window_fullscreen_*.go):
// darwin uses native NSWindow fullscreen so the menu bar and traffic-light
// controls stay reachable on hover; other platforms use GLFW's
// monitor-fullscreen.
