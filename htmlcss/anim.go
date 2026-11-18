package htmlcss

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// CSS Transitions and CSS Animations (@keyframes) for the paint-only
// properties: opacity, transform and background-color. A changed value of
// a transitioned property eases from what is on screen to the new value;
// a running animation interpolates its keyframes. Each frame the animated
// values override the computed ones on the element's box (layout is
// untouched), and the lifecycle events — transitionrun / transitionstart /
// transitionend / transitioncancel, animationstart / animationiteration /
// animationend / animationcancel — reach the engine's AnimationEventHandler,
// which a script engine turns into DOM events. Other properties change
// instantly, but their transitions still report transitionend so scripts
// waiting on it carry on.

// keyframe is one @keyframes step.
type keyframe struct {
	offset float32 // 0..1
	decls  map[string]string
}

// parseKeyframes parses the body of an @keyframes rule.
func parseKeyframes(block string) []keyframe {
	var out []keyframe
	p := &cssParser{src: block}
	for {
		p.skipSpaces()
		if p.eof() {
			break
		}
		sel := strings.TrimSpace(p.readUntil('{'))
		if p.eof() {
			break
		}
		p.pos++ // '{'
		body := p.readBalancedBlock()
		decls := map[string]string{}
		for _, d := range parseDeclarations(body) {
			decls[strings.ToLower(d.Property)] = d.Value
		}
		for _, s := range strings.Split(sel, ",") {
			s = strings.ToLower(strings.TrimSpace(s))
			var off float32
			switch {
			case s == "from":
				off = 0
			case s == "to":
				off = 1
			case strings.HasSuffix(s, "%"):
				f, ok := parseFloat(strings.TrimSuffix(s, "%"))
				if !ok {
					continue
				}
				off = f / 100
			default:
				continue
			}
			out = append(out, keyframe{offset: off, decls: decls})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].offset < out[j].offset })
	return out
}

// --- timing functions ---

type easing func(t float64) float64

func cubicBezier(x1, y1, x2, y2 float64) easing {
	return func(t float64) float64 {
		if t <= 0 || t >= 1 {
			return t
		}
		// Solve x(u) = t by Newton then bisection, return y(u).
		bx := func(u float64) float64 { return 3*x1*u*(1-u)*(1-u) + 3*x2*u*u*(1-u) + u*u*u }
		by := func(u float64) float64 { return 3*y1*u*(1-u)*(1-u) + 3*y2*u*u*(1-u) + u*u*u }
		lo, hi, u := 0.0, 1.0, t
		for i := 0; i < 24; i++ {
			x := bx(u)
			if math.Abs(x-t) < 1e-5 {
				break
			}
			if x < t {
				lo = u
			} else {
				hi = u
			}
			u = (lo + hi) / 2
		}
		return by(u)
	}
}

func parseEasing(v string) easing {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "linear":
		return func(t float64) float64 { return t }
	case "", "ease":
		return cubicBezier(0.25, 0.1, 0.25, 1)
	case "ease-in":
		return cubicBezier(0.42, 0, 1, 1)
	case "ease-out":
		return cubicBezier(0, 0, 0.58, 1)
	case "ease-in-out":
		return cubicBezier(0.42, 0, 0.58, 1)
	case "step-start":
		return func(t float64) float64 { return math.Ceil(t) }
	case "step-end":
		return func(t float64) float64 { return math.Floor(t) }
	}
	if strings.HasPrefix(v, "cubic-bezier(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(v[len("cubic-bezier("):len(v)-1], ",")
		if len(parts) == 4 {
			var f [4]float64
			for i, p := range parts {
				x, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
				if err != nil {
					return cubicBezier(0.25, 0.1, 0.25, 1)
				}
				f[i] = x
			}
			return cubicBezier(f[0], f[1], f[2], f[3])
		}
	}
	if strings.HasPrefix(v, "steps(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(v[len("steps("):len(v)-1], ",")
		n, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		if n < 1 {
			n = 1
		}
		start := len(parts) > 1 && strings.Contains(parts[1], "start")
		return func(t float64) float64 {
			if start {
				return math.Min(1, math.Ceil(t*float64(n))/float64(n))
			}
			return math.Floor(t*float64(n)) / float64(n)
		}
	}
	return cubicBezier(0.25, 0.1, 0.25, 1)
}

// parseTime reads a CSS <time> ("200ms", ".3s").
func parseTime(v string) time.Duration {
	v = strings.TrimSpace(strings.ToLower(v))
	unit, num := time.Second, strings.TrimSuffix(v, "s")
	if strings.HasSuffix(v, "ms") {
		unit, num = time.Millisecond, strings.TrimSuffix(v, "ms")
	} else if !strings.HasSuffix(v, "s") {
		return 0
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	return time.Duration(math.Round(f * float64(unit)))
}

// --- specs from computed style ---

type transitionSpec struct {
	prop       string // "all" or a property name
	dur, delay time.Duration
	ease       easing
	easeSrc    string
}

type animSpec struct {
	name       string
	dur, delay time.Duration
	ease       easing
	easeSrc    string
	iterations float64 // math.Inf(1) for infinite
	direction  string  // normal reverse alternate alternate-reverse
	fill       string  // none forwards backwards both
	paused     bool
}

// splitList splits a comma-separated list at the top level.
func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := splitTopLevel(v, ',')
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isTimeToken(t string) bool {
	t = strings.ToLower(t)
	return (strings.HasSuffix(t, "ms") || strings.HasSuffix(t, "s")) && len(t) > 1 &&
		(t[0] >= '0' && t[0] <= '9' || t[0] == '.' || t[0] == '-')
}

func isEasingToken(t string) bool {
	t = strings.ToLower(t)
	switch t {
	case "linear", "ease", "ease-in", "ease-out", "ease-in-out", "step-start", "step-end":
		return true
	}
	return strings.HasPrefix(t, "cubic-bezier(") || strings.HasPrefix(t, "steps(")
}

// transitions reads transition / transition-* from the raw declarations.
func (cs *ComputedStyle) transitions() []transitionSpec {
	var out []transitionSpec
	for _, item := range splitList(cs.raw["transition"]) {
		ts := transitionSpec{prop: "all"}
		times := 0
		for _, tok := range fieldsTopLevel(item) {
			switch {
			case isTimeToken(tok):
				if times == 0 {
					ts.dur = parseTime(tok)
				} else {
					ts.delay = parseTime(tok)
				}
				times++
			case isEasingToken(tok):
				ts.easeSrc = tok
			default:
				ts.prop = strings.ToLower(tok)
			}
		}
		out = append(out, ts)
	}
	longhand := func(name string, set func(i int, v string)) {
		vals := splitList(cs.raw[name])
		if len(vals) == 0 {
			return
		}
		if len(out) == 0 {
			out = make([]transitionSpec, len(vals))
			for i := range out {
				out[i].prop = "all"
			}
		}
		for i := range out {
			set(i, vals[i%len(vals)])
		}
	}
	longhand("transition-property", func(i int, v string) { out[i].prop = strings.ToLower(v) })
	longhand("transition-duration", func(i int, v string) { out[i].dur = parseTime(v) })
	longhand("transition-delay", func(i int, v string) { out[i].delay = parseTime(v) })
	longhand("transition-timing-function", func(i int, v string) { out[i].easeSrc = v })
	for i := range out {
		out[i].ease = parseEasing(out[i].easeSrc)
	}
	return out
}

// transitionFor returns the transition covering prop, if any (the last
// matching entry wins, as in CSS).
func transitionFor(specs []transitionSpec, prop string) (transitionSpec, bool) {
	for i := len(specs) - 1; i >= 0; i-- {
		s := specs[i]
		if (s.prop == prop || s.prop == "all") && s.dur+s.delay > 0 {
			return s, true
		}
		if s.prop == "none" {
			return transitionSpec{}, false
		}
	}
	return transitionSpec{}, false
}

// animations reads animation / animation-* from the raw declarations.
func (cs *ComputedStyle) animations() []animSpec {
	var out []animSpec
	for _, item := range splitList(cs.raw["animation"]) {
		as := animSpec{iterations: 1, direction: "normal", fill: "none"}
		times := 0
		for _, tok := range fieldsTopLevel(item) {
			low := strings.ToLower(tok)
			switch {
			case isTimeToken(tok):
				if times == 0 {
					as.dur = parseTime(tok)
				} else {
					as.delay = parseTime(tok)
				}
				times++
			case isEasingToken(tok):
				as.easeSrc = tok
			case low == "infinite":
				as.iterations = math.Inf(1)
			case low == "normal" || low == "reverse" || low == "alternate" || low == "alternate-reverse":
				as.direction = low
			case low == "none" && as.name != "":
				as.fill = "none"
			case low == "forwards" || low == "backwards" || low == "both":
				as.fill = low
			case low == "running":
			case low == "paused":
				as.paused = true
			default:
				if f, ok := parseFloat(tok); ok {
					as.iterations = float64(f)
				} else {
					as.name = tok
				}
			}
		}
		out = append(out, as)
	}
	longhand := func(name string, set func(i int, v string)) {
		vals := splitList(cs.raw[name])
		if len(vals) == 0 {
			return
		}
		if len(out) == 0 {
			out = make([]animSpec, len(vals))
			for i := range out {
				out[i] = animSpec{iterations: 1, direction: "normal", fill: "none"}
			}
		}
		for i := range out {
			set(i, vals[i%len(vals)])
		}
	}
	longhand("animation-name", func(i int, v string) { out[i].name = v })
	longhand("animation-duration", func(i int, v string) { out[i].dur = parseTime(v) })
	longhand("animation-delay", func(i int, v string) { out[i].delay = parseTime(v) })
	longhand("animation-timing-function", func(i int, v string) { out[i].easeSrc = v })
	longhand("animation-iteration-count", func(i int, v string) {
		if strings.EqualFold(v, "infinite") {
			out[i].iterations = math.Inf(1)
		} else if f, ok := parseFloat(v); ok {
			out[i].iterations = float64(f)
		}
	})
	longhand("animation-direction", func(i int, v string) { out[i].direction = strings.ToLower(v) })
	longhand("animation-fill-mode", func(i int, v string) { out[i].fill = strings.ToLower(v) })
	longhand("animation-play-state", func(i int, v string) { out[i].paused = strings.EqualFold(v, "paused") })
	for i := range out {
		out[i].ease = parseEasing(out[i].easeSrc)
	}
	var live []animSpec
	for _, a := range out {
		if a.name != "" && !strings.EqualFold(a.name, "none") {
			live = append(live, a)
		}
	}
	return live
}

// --- animated values ---

// animatedProps are the properties animations drive frame by frame.
var animatedProps = []string{"opacity", "transform", "background-color"}

// transitionProps are the properties whose change is detected to start a
// transition (and, for the non-animated ones, to report transitionend).
var transitionProps = []string{"opacity", "transform", "background-color", "color",
	"width", "height", "max-height", "top", "left", "right", "bottom", "margin-top",
	"margin-left", "visibility", "border-color", "box-shadow", "filter"}

func isAnimated(prop string) bool {
	for _, p := range animatedProps {
		if p == prop {
			return true
		}
	}
	return false
}

// animVal is one property's value: a number (opacity), a color, or a
// transform (resolved per box size).
type animVal struct {
	f   float32
	c   qui.Color
	ops []transformOp // nil = none (identity)
}

func lerp32(a, b float32, t float64) float32 { return a + (b-a)*float32(t) }

func lerpVal(prop string, a, b animVal, t float64) animVal {
	switch prop {
	case "opacity":
		return animVal{f: lerp32(a.f, b.f, t)}
	case "background-color":
		return animVal{c: qui.Color{R: lerp32(a.c.R, b.c.R, t), G: lerp32(a.c.G, b.c.G, t), B: lerp32(a.c.B, b.c.B, t), A: lerp32(a.c.A, b.c.A, t)}}
	case "transform":
		// Matrix interpolation per box size: lerp the resolved matrices.
		ao, bo := a.ops, b.ops
		return animVal{ops: []transformOp{{kind: 'L', lerpA: ao, lerpB: bo, lerpT: float32(t)}}}
	}
	return b
}

// valueOf reads prop from a computed style.
func valueOf(prop string, cs *ComputedStyle) animVal {
	switch prop {
	case "opacity":
		if cs.HasOpacity {
			return animVal{f: cs.Opacity}
		}
		return animVal{f: 1}
	case "background-color":
		return animVal{c: cs.Background}
	case "transform":
		if cs.Transform != nil {
			return animVal{ops: cs.Transform.ops}
		}
	}
	return animVal{}
}

// valueFromDecl parses a keyframe declaration; ok=false when absent.
func valueFromDecl(prop string, decls map[string]string, fs float32) (animVal, bool) {
	v, ok := decls[prop]
	if !ok {
		return animVal{}, false
	}
	switch prop {
	case "opacity":
		if o, ok := parseOpacity(v); ok {
			return animVal{f: o}, true
		}
	case "background-color":
		if c, ok := parseColor(v); ok {
			return animVal{c: c}, true
		}
	case "transform":
		if t, ok := parseTransform(v, fs); ok {
			return animVal{ops: t.ops}, true
		}
		return animVal{}, true // none
	}
	return animVal{}, false
}

// rawValue is a property's specified value for change detection.
func rawValue(cs *ComputedStyle, prop string) string {
	if cs == nil {
		return ""
	}
	return strings.TrimSpace(cs.raw[prop])
}

// --- runtime state ---

type runTransition struct {
	prop        string
	from, to    animVal
	start       time.Time
	spec        transitionSpec
	startedSent bool
}

type runAnimation struct {
	spec      animSpec
	frames    []keyframe
	start     time.Time
	iterSent  int
	startSent bool
	ended     bool
}

type elAnimations struct {
	trans      map[string]*runTransition
	anims      []*runAnimation
	registered bool
	cur        map[string]animVal // what is on screen for animated props
	quiet      bool               // sampling without events or state changes
}

// AnimationEventHandler receives the element's animation and transition
// lifecycle events: typ is "transitionend", "animationstart", …; name is
// the property (transitions) or the animation name; elapsed is the
// event's elapsedTime in seconds.
type AnimationEventHandler func(el *El, typ, name string, elapsed float64)

// SetAnimationEventHandler installs the engine's animation event hook.
func (eng *StyleEngine) SetAnimationEventHandler(fn AnimationEventHandler) {
	eng.animEvents = fn
}

func (e *El) animEvent(typ, name string, elapsed float64) {
	if e.engine != nil && e.engine.animEvents != nil {
		e.engine.animEvents(e, typ, name, elapsed)
	}
}

// keyframesFor finds an @keyframes rule by name.
func (e *El) keyframesFor(name string) []keyframe {
	if e.engine == nil || e.engine.sheet == nil {
		return nil
	}
	return e.engine.sheet.Keyframes[name]
}

// updateAnimations runs after a restyle: it starts transitions for changed
// properties and starts / stops animations whose names changed.
func (e *El) updateAnimations(prev, cs *ComputedStyle) {
	if cs == nil || e.displayNone {
		e.cancelAnimations()
		return
	}
	now := time.Now()
	st := e.anim
	// Transitions: a property whose specified value changed.
	if prev != nil && !prev.displayNoneFlag() {
		specs := cs.transitions()
		if len(specs) > 0 {
			for _, prop := range transitionProps {
				if rawValue(prev, prop) == rawValue(cs, prop) {
					continue
				}
				spec, ok := transitionFor(specs, prop)
				if !ok {
					continue
				}
				if st == nil {
					st = &elAnimations{trans: map[string]*runTransition{}, cur: map[string]animVal{}}
					e.anim = st
				}
				from := valueOf(prop, prev)
				if c, ok := st.cur[prop]; ok {
					from = c
				}
				if old := st.trans[prop]; old != nil {
					e.animEvent("transitioncancel", prop, 0)
				}
				st.trans[prop] = &runTransition{prop: prop, from: from, to: valueOf(prop, cs), start: now, spec: spec}
				e.animEvent("transitionrun", prop, 0)
			}
		}
	}
	// Animations: compare the name lists.
	specs := cs.animations()
	var names []string
	for _, a := range specs {
		names = append(names, a.name)
	}
	var have []string
	if st != nil {
		for _, a := range st.anims {
			have = append(have, a.spec.name)
		}
	}
	if strings.Join(names, ",") != strings.Join(have, ",") {
		if st == nil {
			st = &elAnimations{trans: map[string]*runTransition{}, cur: map[string]animVal{}}
			e.anim = st
		}
		old := map[string]*runAnimation{}
		for _, a := range st.anims {
			old[a.spec.name] = a
		}
		st.anims = nil
		for _, sp := range specs {
			if a := old[sp.name]; a != nil {
				a.spec = sp
				st.anims = append(st.anims, a)
				delete(old, sp.name)
				continue
			}
			frames := e.keyframesFor(sp.name)
			if len(frames) == 0 {
				continue
			}
			st.anims = append(st.anims, &runAnimation{spec: sp, frames: frames, start: now})
		}
		for name := range old {
			e.animEvent("animationcancel", name, 0)
		}
	}
	if st != nil && (len(st.trans) > 0 || len(st.anims) > 0) {
		e.ensureAnimator()
		// Put the first frame's values on now, quietly: the lifecycle
		// events wait for the first animation frame, as in a browser,
		// so a script that adds its listener right after sees them.
		st.quiet = true
		e.tickAnimations(now)
		st.quiet = false
	}
}

func (cs *ComputedStyle) displayNoneFlag() bool { return cs.Display == "none" }

func (e *El) cancelAnimations() {
	st := e.anim
	if st == nil {
		return
	}
	for prop := range st.trans {
		e.animEvent("transitioncancel", prop, 0)
	}
	for _, a := range st.anims {
		if !a.ended {
			e.animEvent("animationcancel", a.spec.name, 0)
		}
	}
	e.anim = nil
}

// elAnimator ticks one element's animations on its window.
type elAnimator struct{ e *El }

func (a *elAnimator) Tick(now time.Time) (qui.Rect, bool) {
	e := a.e
	if e.anim == nil {
		return qui.Rect{}, true
	}
	done := e.tickAnimations(now)
	if done {
		e.anim.registered = false
	}
	return qui.PaintBoundsOf(e), done
}

func (a *elAnimator) Stop() {}

func (e *El) ensureAnimator() {
	if e.anim == nil || e.anim.registered {
		return
	}
	win := e.Window()
	if win == nil {
		return
	}
	e.anim.registered = true
	win.RegisterAnimator(&elAnimator{e: e})
}

// tickAnimations advances everything to now and applies the values;
// done reports nothing is left running.
func (e *El) tickAnimations(now time.Time) bool {
	st := e.anim
	cs := e.lastCS
	if st == nil || cs == nil {
		return true
	}
	fs := cs.FontSize
	over := map[string]animVal{}
	// Animations first; transitions override them.
	running := false
	for _, a := range st.anims {
		if a.ended && a.spec.fill != "forwards" && a.spec.fill != "both" {
			continue
		}
		vals, active, ended := e.sampleAnimation(a, now, fs)
		for p, v := range vals {
			over[p] = v
		}
		if active || (!a.ended && !ended) {
			running = true
		}
		_ = active
	}
	for prop, tr := range st.trans {
		el := now.Sub(tr.start) - tr.spec.delay
		if el < 0 {
			running = true
			continue
		}
		if !tr.startedSent && !st.quiet {
			tr.startedSent = true
			e.animEvent("transitionstart", prop, 0)
		}
		t := 1.0
		if tr.spec.dur > 0 {
			t = math.Min(1, float64(el)/float64(tr.spec.dur))
		}
		if isAnimated(prop) {
			over[prop] = lerpVal(prop, tr.from, tr.to, tr.spec.ease(t))
		}
		if t >= 1 && !st.quiet {
			delete(st.trans, prop)
			delete(st.cur, prop)
			e.animEvent("transitionend", prop, tr.spec.dur.Seconds())
			if isAnimated(prop) {
				over[prop] = tr.to
			}
			continue
		}
		running = true
	}
	e.applyAnimated(over)
	return !running
}

// sampleAnimation computes an animation's property values at now, firing
// its start / iteration / end events.
func (e *El) sampleAnimation(a *runAnimation, now time.Time, fs float32) (map[string]animVal, bool, bool) {
	sp := a.spec
	el := now.Sub(a.start) - sp.delay
	if sp.paused {
		el = 0
	}
	if el < 0 {
		if sp.fill == "backwards" || sp.fill == "both" {
			return e.frameValues(a.frames, 0, sp, fs), false, false
		}
		return nil, false, false
	}
	quiet := e.anim != nil && e.anim.quiet
	if !a.startSent && !quiet {
		a.startSent = true
		e.animEvent("animationstart", sp.name, 0)
	}
	dur := sp.dur
	total := math.Inf(1)
	if !math.IsInf(sp.iterations, 1) {
		total = float64(dur) * sp.iterations
	}
	if dur <= 0 || float64(el) >= total {
		if !a.ended && !quiet {
			a.ended = true
			e.animEvent("animationend", sp.name, total/float64(time.Second))
		}
		if sp.fill == "forwards" || sp.fill == "both" {
			iter := math.Max(sp.iterations, 1)
			return e.frameValues(a.frames, progressAt(1, iter-1, sp.direction), sp, fs), false, true
		}
		return nil, false, true
	}
	iterF := float64(el) / float64(dur)
	iter := math.Floor(iterF)
	if int(iter) > a.iterSent && !quiet {
		a.iterSent = int(iter)
		e.animEvent("animationiteration", sp.name, iter*dur.Seconds())
	}
	p := progressAt(iterF-iter, iter, sp.direction)
	return e.frameValues(a.frames, p, sp, fs), true, false
}

// progressAt maps an iteration's local progress through the direction.
func progressAt(p, iter float64, dir string) float64 {
	odd := int(iter)%2 == 1
	switch dir {
	case "reverse":
		return 1 - p
	case "alternate":
		if odd {
			return 1 - p
		}
	case "alternate-reverse":
		if !odd {
			return 1 - p
		}
	}
	return p
}

// frameValues interpolates the keyframes at progress p.
func (e *El) frameValues(frames []keyframe, p float64, sp animSpec, fs float32) map[string]animVal {
	out := map[string]animVal{}
	cs := e.lastCS
	for _, prop := range animatedProps {
		// Bracketing keyframes that set prop; the computed value stands
		// in for a missing 0% / 100%.
		var lo, hi *keyframe
		for i := range frames {
			if _, ok := frames[i].decls[prop]; !ok {
				continue
			}
			if float64(frames[i].offset) <= p {
				lo = &frames[i]
			}
			if float64(frames[i].offset) >= p && hi == nil {
				hi = &frames[i]
			}
		}
		if lo == nil && hi == nil {
			continue
		}
		base := valueOf(prop, cs)
		loV, loOff := base, float32(0)
		if lo != nil {
			loV, _ = valueFromDecl(prop, lo.decls, fs)
			loOff = lo.offset
		}
		hiV, hiOff := base, float32(1)
		if hi != nil {
			hiV, _ = valueFromDecl(prop, hi.decls, fs)
			hiOff = hi.offset
		}
		t := 1.0
		if hiOff > loOff {
			t = (p - float64(loOff)) / float64(hiOff-loOff)
		}
		out[prop] = lerpVal(prop, loV, hiV, sp.ease(t))
	}
	return out
}

// applyAnimated puts the animated values on the box (and restores the
// computed ones for properties no longer animated).
func (e *El) applyAnimated(over map[string]animVal) {
	st := e.anim
	cs := e.lastCS
	if st == nil || cs == nil {
		return
	}
	for _, prop := range animatedProps {
		v, animated := over[prop]
		_, was := st.cur[prop]
		if !animated && !was {
			continue
		}
		if !animated {
			v = valueOf(prop, cs)
			delete(st.cur, prop)
		} else {
			st.cur[prop] = v
		}
		switch prop {
		case "opacity":
			o := v.f
			if o <= 0 {
				o = 0.0001 // Style.Opacity 0 means unspecified
			}
			if o >= 1 {
				o = 0
			}
			e.Style().Opacity = o
		case "background-color":
			if e.Box.BackgroundShader == nil {
				e.Style().Background = v.c
			}
		case "transform":
			if len(v.ops) == 0 {
				if cs.Transform == nil || !animated {
					e.Box.Transform = boxTransformOf(cs)
				} else {
					e.Box.Transform = nil
				}
			} else {
				ops := v.ops
				t := &widgets.BoxTransform{SX: 1, SY: 1}
				if cs.Transform != nil {
					t.OX, t.OY, t.HasOrigin = cs.Transform.ox, cs.Transform.oy, cs.Transform.hasOrigin
				}
				t.Resolve = func(w, h float32) qui.Matrix { return resolveTransformOps(ops, w, h) }
				e.Box.Transform = t
			}
		}
	}
	e.Invalidate()
}

// boxTransformOf builds the box transform for a computed style (nil for
// none), as applyBox does.
func boxTransformOf(cs *ComputedStyle) *widgets.BoxTransform {
	t := cs.Transform
	if t == nil {
		return nil
	}
	bt := &widgets.BoxTransform{
		TX: t.tx, TY: t.ty, Rotate: t.rotate, SX: t.sx, SY: t.sy,
		KX: t.kx, KY: t.ky, Matrix: t.matrix,
		OX: t.ox, OY: t.oy, HasOrigin: t.hasOrigin,
	}
	if ops := t.ops; len(ops) > 0 {
		bt.Resolve = func(w, h float32) qui.Matrix { return resolveTransformOps(ops, w, h) }
	}
	return bt
}

// reapplyAnimated re-puts the on-screen animated values after a restyle
// rewrote the box from the computed style.
func (e *El) reapplyAnimated() {
	if e.anim == nil || len(e.anim.cur) == 0 {
		return
	}
	cur := map[string]animVal{}
	for k, v := range e.anim.cur {
		cur[k] = v
	}
	e.applyAnimated(cur)
}
