package qui

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Runtime control: the QUI_DEBUG_* switches as protocol flags (the
// environment gives their initial values; Debug.setFlags flips them live,
// no restart), plus the Emulation and Animation domains.

func envFlag(name string) *atomic.Bool {
	b := new(atomic.Bool)
	b.Store(os.Getenv(name) == "1")
	return b
}

// debugFlags maps protocol flag names to the engine switches.
var debugFlags = map[string]*atomic.Bool{
	"paint":         paintDebugEnabled,        // QUI_DEBUG_PAINT
	"layout":        flexDebugLayoutEnabled,   // QUI_DEBUG_LAYOUT
	"layoutCache":   layoutCacheVerify,        // QUI_DEBUG_LAYOUT_CACHE
	"draw":          drawMutationDebugEnabled, // QUI_DEBUG_DRAW
	"thread":        uiThreadChecksByDefault,  // QUI_DEBUG_THREAD
	"widgets":       widgetDebugEnabled,       // QUI_DEBUG_WIDGETS
	"i18nStrict":    i18nStrictEnabled,        // QUI_I18N_STRICT
	"survivePanics": &survivePanics,
}

// debugFlagState is every flag's value; unset fields in setFlags are left
// alone.
type debugFlagState struct {
	Paint         *bool `json:"paint,omitempty" desc:"QUI_DEBUG_PAINT: log full-repaint promotions and the first moved widget"`
	Layout        *bool `json:"layout,omitempty" desc:"QUI_DEBUG_LAYOUT: log flex overflow"`
	LayoutCache   *bool `json:"layoutCache,omitempty" desc:"QUI_DEBUG_LAYOUT_CACHE: re-measure cache hits, log widgets whose size changed without InvalidateLayout"`
	Draw          *bool `json:"draw,omitempty" desc:"QUI_DEBUG_DRAW: panic when Draw mutates the tree or layout"`
	Thread        *bool `json:"thread,omitempty" desc:"QUI_DEBUG_THREAD: panic on UI-thread ownership violations"`
	Widgets       *bool `json:"widgets,omitempty" desc:"QUI_DEBUG_WIDGETS: log children whose Parent() mismatches (a missing SetSelf)"`
	I18nStrict    *bool `json:"i18nStrict,omitempty" desc:"QUI_I18N_STRICT: render unresolved message keys as ⟦key⟧"`
	SurvivePanics *bool `json:"survivePanics,omitempty" desc:"report a panic as Runtime.exceptionThrown and keep running instead of crashing"`
}

func (s *debugFlagState) fields() map[string]**bool {
	return map[string]**bool{
		"paint": &s.Paint, "layout": &s.Layout, "layoutCache": &s.LayoutCache, "draw": &s.Draw,
		"thread": &s.Thread, "widgets": &s.Widgets, "i18nStrict": &s.I18nStrict, "survivePanics": &s.SurvivePanics,
	}
}

func currentDebugFlags() debugFlagState {
	var s debugFlagState
	for name, field := range s.fields() {
		v := debugFlags[name].Load()
		*field = &v
	}
	return s
}

func debugFlagCommands() []ProtocolCommand {
	return []ProtocolCommand{
		NewProtocolCommand("getFlags", "The engine debug switches and their current values.",
			func(*ProtocolContext, empty) (debugFlagState, error) { return currentDebugFlags(), nil }).WithAppLevel(),
		NewProtocolCommand("setFlags", "Flip engine debug switches live; omitted flags keep their value. Returns the new state. Process-wide.",
			func(ctx *ProtocolContext, p debugFlagState) (debugFlagState, error) {
				apply := func() error {
					for name, field := range p.fields() {
						if *field != nil {
							debugFlags[name].Store(**field)
						}
					}
					return nil
				}
				// The UI goroutine reads these mid-frame; flip them between frames.
				var err error
				if ctx.Window != nil {
					err = ctx.Window.synchronously(apply)
				} else {
					err = apply()
				}
				if p.I18nStrict != nil {
					bumpLocale() // re-resolve every caption under the new mode
				}
				return currentDebugFlags(), err
			}).WithStrict(),
	}
}

// -------------------------------------------------------------------
// Animation clock

// animClock maps real time to the time a window's animators see, so an
// agent can slow, pause and step animations for deterministic frames.
// Tickables (caret blink, tooltips) keep real time.
type animClock struct {
	mu       sync.Mutex
	active   bool // false = identity
	rate     float64
	paused   bool
	baseReal time.Time
	baseVirt time.Time
}

func (c *animClock) now(real time.Time) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nowLocked(real)
}

func (c *animClock) nowLocked(real time.Time) time.Time {
	if !c.active {
		return real
	}
	if c.paused {
		return c.baseVirt
	}
	return c.baseVirt.Add(time.Duration(float64(real.Sub(c.baseReal)) * c.rate))
}

// rebase pins the mapping at real time t before a parameter changes.
func (c *animClock) rebase(t time.Time) {
	v := c.nowLocked(t)
	if !c.active {
		c.active, c.rate = true, 1
	}
	c.baseReal, c.baseVirt = t, v
}

func (c *animClock) set(rate *float64, paused *bool, advance time.Duration) animationState {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rebase(time.Now())
	if rate != nil {
		c.rate = *rate
	}
	if paused != nil {
		c.paused = *paused
	}
	c.baseVirt = c.baseVirt.Add(advance)
	if c.rate == 1 && !c.paused {
		// Back to real time, keeping any offset accumulated so far.
		c.active = !c.baseVirt.Equal(c.baseReal)
	}
	return c.stateLocked()
}

func (c *animClock) stateLocked() animationState {
	rate := 1.0
	if c.active {
		rate = c.rate
	}
	return animationState{PlaybackRate: rate, Paused: c.active && c.paused}
}

type animationState struct {
	PlaybackRate float64 `json:"playbackRate"`
	Paused       bool    `json:"paused"`
}

type animationRateParams struct {
	PlaybackRate float64 `json:"playbackRate" desc:"1 = real time, 0.1 = ten times slower"`
}

type animationPausedParams struct {
	Paused bool `json:"paused"`
	// CDP's setPaused takes animation IDs; qui pauses the window's clock.
	Animations []string `json:"animations,omitempty" desc:"accepted for CDP compatibility; every animator of the window is affected"`
}

type animationAdvanceParams struct {
	Ms float64 `json:"ms" desc:"milliseconds to move the animation clock forward (works while paused: step a transition frame by frame)"`
}

func animationDomain() ProtocolDomain {
	set := func(ctx *ProtocolContext, rate *float64, paused *bool, adv time.Duration) (animationState, error) {
		w := ctx.Window
		st := w.anim.set(rate, paused, adv)
		// Repaint so the new clock reaches the animators now.
		return st, w.synchronously(func() error { w.Invalidate(); return nil })
	}
	return ProtocolDomain{
		Name:        "Animation",
		Description: "The window's animation clock (every qui.Animator: anim.Tween, Spring, Timeline). Slow, pause and step animations for deterministic screenshots.",
		Commands: []ProtocolCommand{
			NewProtocolCommand("getPlaybackRate", "The clock's rate and pause state.",
				func(ctx *ProtocolContext, _ empty) (animationState, error) {
					ctx.Window.anim.mu.Lock()
					defer ctx.Window.anim.mu.Unlock()
					return ctx.Window.anim.stateLocked(), nil
				}),
			NewProtocolCommand("setPlaybackRate", "Scale the animation clock.",
				func(ctx *ProtocolContext, p animationRateParams) (animationState, error) {
					if p.PlaybackRate < 0 {
						return animationState{}, NewProtocolError("invalid_params", "playbackRate must be >= 0")
					}
					return set(ctx, &p.PlaybackRate, nil, 0)
				}),
			NewProtocolCommand("setPaused", "Freeze or resume the animation clock.",
				func(ctx *ProtocolContext, p animationPausedParams) (animationState, error) {
					return set(ctx, nil, &p.Paused, 0)
				}),
			NewProtocolCommand("advance", "qui: move the animation clock forward.",
				func(ctx *ProtocolContext, p animationAdvanceParams) (animationState, error) {
					if p.Ms < 0 {
						return animationState{}, NewProtocolError("invalid_params", "ms must be >= 0")
					}
					return set(ctx, nil, nil, time.Duration(p.Ms*float64(time.Millisecond)))
				}).WithStrict(),
		},
	}
}

// -------------------------------------------------------------------
// Emulation

type emulationMetricsParams struct {
	Width             int     `json:"width" desc:"logical window width"`
	Height            int     `json:"height" desc:"logical window height"`
	DeviceScaleFactor float64 `json:"deviceScaleFactor,omitempty" desc:"accepted for CDP compatibility; the display decides it (QUI_HEADLESS_SCALE for headless)"`
	Mobile            bool    `json:"mobile,omitempty" desc:"accepted for CDP compatibility; ignored"`
}

type emulationLocaleParams struct {
	Locale string `json:"locale,omitempty" desc:"BCP 47 tag (de-DE, ar, zh-Hans); empty restores the locale from before the first override"`
}

type emulationZoomParams struct {
	Zoom float32 `json:"zoom" desc:"viewport zoom, 1 = 100%"`
}

var (
	localeOverrideMu     sync.Mutex
	localeBeforeOverride *Locale
)

func emulationDomain() ProtocolDomain {
	return ProtocolDomain{
		Name:        "Emulation",
		Description: "Change the conditions the UI renders under: window size, locale, zoom.",
		Commands: []ProtocolCommand{
			NewProtocolCommand("setDeviceMetricsOverride", "Resize the window to width × height logical pixels.",
				func(ctx *ProtocolContext, p emulationMetricsParams) (empty, error) {
					if p.Width <= 0 || p.Height <= 0 {
						return empty{}, NewProtocolError("invalid_params", "width and height must be > 0")
					}
					w := ctx.Window
					return empty{}, w.synchronously(func() error {
						if w.metricsBefore == nil {
							size := w.lastSize
							w.metricsBefore = &size
						}
						w.SetSize(p.Width, p.Height)
						return nil
					})
				}),
			NewProtocolCommand("clearDeviceMetricsOverride", "Restore the window size from before the first override.",
				func(ctx *ProtocolContext, _ empty) (empty, error) {
					w := ctx.Window
					return empty{}, w.synchronously(func() error {
						if w.metricsBefore != nil {
							w.SetSize(int(w.metricsBefore.W), int(w.metricsBefore.H))
							w.metricsBefore = nil
						}
						return nil
					})
				}),
			NewProtocolCommand("setLocaleOverride", "Switch the process-wide locale (every window re-renders, RTL included).",
				func(ctx *ProtocolContext, p emulationLocaleParams) (empty, error) {
					localeOverrideMu.Lock()
					defer localeOverrideMu.Unlock()
					next := Locale(p.Locale)
					if p.Locale == "" {
						if localeBeforeOverride == nil {
							return empty{}, nil
						}
						next = *localeBeforeOverride
						localeBeforeOverride = nil
					} else if localeBeforeOverride == nil {
						localeMu.Lock()
						prev := defaultLocale
						localeMu.Unlock()
						localeBeforeOverride = &prev
					}
					apply := func() error { SetDefaultLocale(next); return nil }
					if ctx.Window != nil {
						return empty{}, ctx.Window.synchronously(apply)
					}
					return empty{}, apply()
				}),
			NewProtocolCommand("setPageScaleFactor", "Set the window's viewport zoom (CDP pageScaleFactor).",
				func(ctx *ProtocolContext, p struct {
					PageScaleFactor float32 `json:"pageScaleFactor"`
				}) (empty, error) {
					if p.PageScaleFactor <= 0 {
						return empty{}, NewProtocolError("invalid_params", "pageScaleFactor must be > 0")
					}
					return empty{}, ctx.Window.synchronously(func() error { ctx.Window.SetZoom(p.PageScaleFactor); return nil })
				}),
		},
	}
}
