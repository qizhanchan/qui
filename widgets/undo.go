package widgets

import "time"

// Undo/redo history shared by Input and TextArea. Each widget
// embeds an undoHistory and snapshots its own state around every
// mutation; the history coalesces rapid-fire typing / deletion into
// single undo units so Cmd+Z reverses a word or phrase instead of
// one character at a time.
//
// Coalescing rules (match common editor conventions):
//   - Consecutive "type" or "delete" entries of the same kind within
//     undoCoalesceWindow fold into the previous entry (we keep the
//     older "before" snapshot so undo jumps back the full span).
//   - Any other kind ("paste", "cut", "ime", "enter", "set") always
//     pushes a fresh entry.
//   - A cursor move / focus change calls breakCoalesce(), which
//     stamps the head entry as "stale" so the next mutation starts
//     a new group even within the time window.

// undoEntry captures the state needed to restore a text widget to a
// prior moment. Stored by value — the widgets' other fields (style,
// scrollX, hovering) aren't part of the logical document.
type undoEntry struct {
	text      string
	cursorPos int
	selStart  int
	selEnd    int

	// kind labels the mutation that produced *this entry's after*
	// state. Used only for coalescing — two consecutive entries of
	// the same coalescing-eligible kind merge.
	kind string

	// when is the wallclock time of the most recent merge into this
	// entry. Zero means "coalescing sealed" — used by breakCoalesce
	// to force the next mutation into a new entry.
	when time.Time
}

// undoHistory is a pair of stacks. Widgets hold one by value.
type undoHistory struct {
	undo []undoEntry
	redo []undoEntry
}

const (
	maxUndoDepth       = 200
	undoCoalesceWindow = 500 * time.Millisecond
)

// undoNow is the time source used for coalescing decisions. Tests
// swap it out to make the window deterministic; production uses
// time.Now.
var undoNow = time.Now

// push records a mutation. `before` is the snapshot taken *before* the
// mutation ran; `after` reflects the current widget state. Fires only
// when after.text differs from before.text — cursor-only moves aren't
// recorded. Any push clears the redo stack.
func (h *undoHistory) push(kind string, before, after undoEntry) {
	if before.text == after.text {
		return
	}
	canCoalesce := (kind == "type" || kind == "delete") &&
		len(h.undo) > 0 &&
		h.undo[len(h.undo)-1].kind == kind &&
		!h.undo[len(h.undo)-1].when.IsZero() &&
		undoNow().Sub(h.undo[len(h.undo)-1].when) < undoCoalesceWindow

	if canCoalesce {
		// Merge: keep the older "before" state (so undo reaches
		// farther back), just refresh the timestamp so further
		// rapid-fire keys stay in the same group.
		h.undo[len(h.undo)-1].when = undoNow()
	} else {
		before.kind = kind
		before.when = undoNow()
		h.undo = append(h.undo, before)
		if len(h.undo) > maxUndoDepth {
			// Drop the oldest entry. Copying leaves the underlying
			// array at full capacity; acceptable for a 200-entry cap.
			h.undo = h.undo[1:]
		}
	}
	h.redo = nil
}

// breakCoalesce seals the current coalescing group. Called on cursor
// moves, mouse clicks, and focus changes — anything that breaks the
// "user is typing in one place" invariant.
func (h *undoHistory) breakCoalesce() {
	if n := len(h.undo); n > 0 {
		h.undo[n-1].when = time.Time{}
	}
}

// popUndo removes and returns the top of the undo stack, recording
// `current` (the widget's present state) onto the redo stack so redo
// can reverse the reversal. Returns nil when the stack is empty.
func (h *undoHistory) popUndo(current undoEntry) *undoEntry {
	n := len(h.undo)
	if n == 0 {
		return nil
	}
	prev := h.undo[n-1]
	h.undo = h.undo[:n-1]
	h.redo = append(h.redo, current)
	return &prev
}

// popRedo is the inverse — moves an entry from redo back to undo.
func (h *undoHistory) popRedo(current undoEntry) *undoEntry {
	n := len(h.redo)
	if n == 0 {
		return nil
	}
	next := h.redo[n-1]
	h.redo = h.redo[:n-1]
	h.undo = append(h.undo, current)
	return &next
}

// clear wipes both stacks. Used when a widget is reset to a known
// state (e.g., fresh SetText with intent to discard history) — not
// invoked by default; left here for callers that need it.
func (h *undoHistory) clear() {
	h.undo = nil
	h.redo = nil
}
