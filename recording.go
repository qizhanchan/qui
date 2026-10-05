package qui

import (
	"fmt"
	"sync"
	"time"
)

// Event recording — a hook into Window.dispatch that lets external
// systems (the agent layer, debug overlays, custom telemetry) see
// every event flowing through the framework.
//
// The framework provides:
//   - EventRecord, a stable serialization shape
//   - EventListener, a callback type
//   - Window.AddEventListener / RemoveEventListener
//
// dispatch calls every registered listener AFTER updating internal
// state but BEFORE invoking widget handlers. Listeners must not
// panic; a panic in a listener is recovered and logged, but state
// invariants are not guaranteed.
//
// Implementations should keep listeners cheap (drop, don't block).
// Most callers will copy the EventRecord into a ring buffer of
// their own.

// EventRecord is a serializable summary of one dispatched event.
// Field set is deliberately narrow: enough for an agent to verify
// "the click I synthesized arrived", not enough to reconstruct the
// full Event interface tree.
type EventRecord struct {
	Timestamp time.Time `json:"-"`
	Kind      string    `json:"kind"`
	TargetID  string    `json:"targetId,omitempty"`
	Target    string    `json:"target,omitempty"` // path
	X         float32   `json:"x,omitempty"`
	Y         float32   `json:"y,omitempty"`
	DX        float32   `json:"dx,omitempty"`
	DY        float32   `json:"dy,omitempty"`
	Key       string    `json:"key,omitempty"`
	Rune      rune      `json:"rune,omitempty"`
	// Scale / Rotation carry a gesture record's cumulative magnification
	// and rotation (degrees). Zero for non-gesture records.
	Scale     float32    `json:"scale,omitempty"`
	Rotation  float32    `json:"rotation,omitempty"`
	Modifiers Modifiers  `json:"modifiers,omitempty"`
	Phase     EventPhase `json:"-"`
	// Value / OldValue carry the semantic payload of input-lifecycle
	// records (Kind "Input" / "Submit") — the new (and, where known, the
	// previous) text of a text-editing widget. Empty for raw events. These
	// records answer "did my input handler fire, and what did the value
	// become?" without reconstructing it from a stream of Char events.
	Value    string `json:"value,omitempty"`
	OldValue string `json:"oldValue,omitempty"`
	// Source classifies an input record's origin (Kind "Input"/"Submit"):
	//   "user"              — a keystroke / paste / IME commit
	//   "program"           — a programmatic SetText (app code, data
	//                         binding, or an agent Type action)
	//   "program-overwrite" — a programmatic SetText that replaced a value
	//                         whose most recent change was the user's own
	//                         edit. This is the tell-tale of a controlled-
	//                         value fight: a binding writing a stale value
	//                         back over what the user just typed, so the
	//                         field "won't accept input" or reverts.
	Source string `json:"source,omitempty"`
}

// EventListener receives EventRecords. Listeners must return
// quickly; the dispatch hot path is shared with widget code.
type EventListener func(EventRecord)

// AddEventListener registers fn to receive every dispatched event.
// Returns an opaque token usable with RemoveEventListener to undo
// the registration. Safe to call from any goroutine.
func (w *Window) AddEventListener(fn EventListener) EventListenerToken {
	if w == nil || fn == nil {
		return EventListenerToken{}
	}
	w.recordersMu.Lock()
	defer w.recordersMu.Unlock()
	w.nextRecorderID++
	id := w.nextRecorderID
	w.recorders = append(w.recorders, recorderEntry{id: id, fn: fn})
	return EventListenerToken{id: id}
}

// RemoveEventListener unregisters a previously-added listener. No-op
// for unknown tokens.
func (w *Window) RemoveEventListener(tok EventListenerToken) {
	if w == nil || tok.id == 0 {
		return
	}
	w.recordersMu.Lock()
	defer w.recordersMu.Unlock()
	out := w.recorders[:0]
	for _, e := range w.recorders {
		if e.id != tok.id {
			out = append(out, e)
		}
	}
	w.recorders = out
}

// EventListenerToken identifies a registered listener. Zero value
// is a no-op token; RemoveEventListener silently ignores it.
type EventListenerToken struct {
	id int64
}

// publishEventRecord is the internal fan-out called from dispatch.
// Builds an EventRecord from the live event and notifies every
// registered listener. Cheap when no listeners are registered (one
// mutex check + length comparison).
func (w *Window) publishEventRecord(ev Event, target Widget) {
	if w == nil {
		return
	}
	w.recordersMu.Lock()
	listeners := w.recorders
	w.recordersMu.Unlock()
	if len(listeners) == 0 {
		return
	}
	rec := buildEventRecord(ev, target)
	for _, e := range listeners {
		// Defensive: a misbehaving listener shouldn't crash dispatch.
		safeCallListener(e.fn, rec)
	}
}

// RecordInputChange publishes a semantic "Input" record: a text-editing
// widget's value changed to newVal (oldVal may be "" on the incremental
// typing path where the previous value isn't cheaply available). Widgets
// call this alongside firing OnChange so the agent's /console and /events
// show the input lifecycle, not just raw keystrokes. Cheap when no
// listener is registered.
func (w *Window) RecordInputChange(target Widget, oldVal, newVal, source string) {
	if w != nil {
		w.assertUIThread("Window.RecordInputChange")
	}
	w.publishSemantic(target, "Input", oldVal, newVal, source)
}

// RecordInputSubmit publishes a semantic "Submit" record (Enter / commit
// on a single-line input). Submit is always user-originated.
func (w *Window) RecordInputSubmit(target Widget, value string) {
	if w != nil {
		w.assertUIThread("Window.RecordInputSubmit")
	}
	w.publishSemantic(target, "Submit", "", value, "user")
}

// publishSemantic fans a non-event (semantic) record out to listeners.
func (w *Window) publishSemantic(target Widget, kind, oldVal, newVal, source string) {
	if w == nil {
		return
	}
	w.recordersMu.Lock()
	listeners := w.recorders
	w.recordersMu.Unlock()
	if len(listeners) == 0 {
		return
	}
	rec := EventRecord{Timestamp: time.Now(), Kind: kind, Value: newVal, OldValue: oldVal, Source: source}
	if target != nil {
		rec.Target = widgetPath(target)
		rec.TargetID = WidgetID(target)
	}
	for _, e := range listeners {
		safeCallListener(e.fn, rec)
	}
}

func safeCallListener(fn EventListener, rec EventRecord) {
	defer func() {
		_ = recover()
	}()
	fn(rec)
}

func buildEventRecord(ev Event, target Widget) EventRecord {
	rec := EventRecord{
		Timestamp: time.Now(),
	}
	if target != nil {
		rec.Target = widgetPath(target)
		rec.TargetID = WidgetID(target)
	}
	switch e := ev.(type) {
	case MouseEvent:
		rec.Kind = mouseKindName(e.eventType)
		rec.X = e.X
		rec.Y = e.Y
		rec.DX = e.DeltaX
		rec.DY = e.DeltaY
		rec.Modifiers = e.Mods
	case KeyEvent:
		rec.Kind = keyKindName(e.eventType)
		rec.Key = fmt.Sprintf("%v", e.Key)
		rec.Modifiers = e.Mods
	case CharEvent:
		rec.Kind = "Char"
		rec.Rune = e.Rune
		rec.Modifiers = e.Mods
	case GestureEvent:
		rec.Kind = gestureKindName(e.eventType)
		rec.X = e.X
		rec.Y = e.Y
		rec.Scale = e.Scale
		rec.Rotation = e.Rotation
		rec.Modifiers = e.Mods
	default:
		rec.Kind = "Other"
	}
	return rec
}

func gestureKindName(t EventType) string {
	switch t {
	case EventGesturePinch:
		return "GesturePinch"
	case EventGestureRotate:
		return "GestureRotate"
	case EventGestureSmartMagnify:
		return "GestureSmartMagnify"
	default:
		return "Gesture"
	}
}

func mouseKindName(t EventType) string {
	switch t {
	case EventMouseDown:
		return "MouseDown"
	case EventMouseUp:
		return "MouseUp"
	case EventMouseMove:
		return "MouseMove"
	case EventMouseEnter:
		return "MouseEnter"
	case EventMouseLeave:
		return "MouseLeave"
	case EventScroll:
		return "Scroll"
	case EventDragStart:
		return "DragStart"
	case EventDragMove:
		return "DragMove"
	case EventDragOver:
		return "DragOver"
	case EventDragEnd:
		return "DragEnd"
	case EventDrop:
		return "Drop"
	case EventDragEnter:
		return "DragEnter"
	case EventDragLeave:
		return "DragLeave"
	}
	return "Mouse"
}

func keyKindName(t EventType) string {
	switch t {
	case EventKeyDown:
		return "KeyDown"
	case EventKeyUp:
		return "KeyUp"
	}
	return "Key"
}

// widgetPath returns a "TypeName[i]/…" style path for the widget,
// walking up via Parent. Mirrors collectDebugNodes' scheme — used
// for EventRecord.Target.
func widgetPath(widget Widget) string {
	if widget == nil {
		return ""
	}
	type segment struct {
		name  string
		index int
	}
	var segments []segment
	for w := widget; w != nil; w = w.Parent() {
		name := widgetTypeName(w)
		idx := -1
		if p := w.Parent(); p != nil {
			if cl, ok := p.(childLister); ok {
				for i, c := range cl.ChildList() {
					if c == w {
						idx = i
						break
					}
				}
			}
		}
		segments = append(segments, segment{name: name, index: idx})
	}
	// Reverse + join.
	out := ""
	for i := len(segments) - 1; i >= 0; i-- {
		s := segments[i]
		if i == len(segments)-1 {
			out = s.name
			continue
		}
		out = fmt.Sprintf("%s/%s[%d]", out, s.name, s.index)
	}
	return out
}

// recorderEntry is a listener with a stable ID for removal.
type recorderEntry struct {
	id int64
	fn EventListener
}

// Recording state is held directly on Window. Mutex is package-
// private; the public API is the Add/Remove methods.
var _ = sync.Mutex{} // keep sync import alive for the struct fields below
